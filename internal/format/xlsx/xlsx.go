// Package xlsx registers a Source that reads one worksheet of an .xlsx
// workbook against a declared schema (design/xlsx.md §1). Per CLAUDE.md's
// format-registry convention, this is the only place that knows the
// string "xlsx"; the registry itself, and everything above it, is
// format-agnostic.
package xlsx

import (
	"fmt"
	"strings"

	"github.com/xuri/excelize/v2"

	"github.com/vitzeno/sift/internal/format"
	"github.com/vitzeno/sift/internal/runtime"
	"github.com/vitzeno/sift/internal/value"
)

func init() {
	runtime.RegisterSource("xlsx", NewXLSXSource)
}

// xlsxSource reads rows from one worksheet of an .xlsx workbook against a
// declared schema, the same "declared, not inferred" contract csvSource
// follows (design/xlsx.md §1): xlsx cells are nominally typed but
// unreliably so, and a declared schema keeps every source format
// consistent and explicit.
//
// Opening the workbook (excelize.OpenFile) parses the whole zip's
// structure and shared-string table up front, so an xlsx source has a
// fixed memory cost a CSV source does not (design/xlsx.md §2). Row
// reading itself still streams via the excelize row iterator
// (Rows/Next/Columns), not GetRows, keeping the data itself to one row
// in flight at a time.
type xlsxSource struct {
	f    *excelize.File
	rows *excelize.Rows
	name string
	// col maps a schema field name to its zero-based column index in
	// the sheet, bound once from header_row and reused for every row.
	// Mirrors csvSource.col.
	col map[string]int
	// dateFormats holds the source's formats kwarg (design/date.md §3):
	// a date-typed field name to the layout string to parse it against.
	dateFormats map[string]string
	schema      value.Schema
	ordinal     int
	// sheetRow is the 1-based spreadsheet row number of the last row
	// pulled from rows. Becomes each emitted Row's Provenance.Offset
	// (design/xlsx.md §2: "the one a user can act on"), distinct from
	// ordinal once header_row > 1.
	sheetRow int
	err      error
}

// NewXLSXSource is the SourceCtor registered under "xlsx". Fail-fast is
// entirely this constructor's job (design/xlsx.md §2): it opens the
// workbook, resolves the sheet, reads header_row, and binds every
// declared schema field to a column, all before any row flows. Anything
// wrong in that sequence is infra-fatal and returned here, never as a
// per-row Failure or a panic.
func NewXLSXSource(opts runtime.SourceOptions) (runtime.Source, error) {
	sheet, err := stringOpt(opts, "sheet", "")
	if err != nil {
		return nil, err
	}
	headerRowOpt, err := intOpt(opts, "header_row", 1)
	if err != nil {
		return nil, err
	}
	if headerRowOpt < 1 {
		return nil, fmt.Errorf("source %q: %q must be >= 1, got %d", opts.Name, "header_row", headerRowOpt)
	}
	headerRow := int(headerRowOpt)

	f, err := excelize.OpenFile(opts.Path, excelize.Options{RawCellValue: true})
	if err != nil {
		return nil, fmt.Errorf("source %q: opening %q: %w", opts.Name, opts.Path, err)
	}

	if sheet == "" {
		sheet = f.GetSheetName(0)
	} else if _, ok := sheetIndex(f, sheet); !ok {
		f.Close()
		return nil, fmt.Errorf("source %q: sheet %q not found in %q\n       available sheets: %s",
			opts.Name, sheet, opts.Path, strings.Join(f.GetSheetList(), ", "))
	}

	rows, err := f.Rows(sheet)
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("source %q: reading sheet %q: %w", opts.Name, sheet, err)
	}

	// Advance to header_row, discarding everything above it unread
	// (design/xlsx.md §2.1). seen counts rows actually iterated, so a
	// sheet shorter than header_row still reports its real row count.
	var header []string
	seen := 0
	for seen < headerRow {
		if !rows.Next() {
			f.Close()
			return nil, fmt.Errorf("source %q: sheet %q has %d rows; header_row is %d",
				opts.Name, sheet, seen, headerRow)
		}
		seen++
	}
	header, err = rows.Columns(excelize.Options{RawCellValue: true})
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("source %q: reading header row %d of sheet %q: %w", opts.Name, headerRow, sheet, err)
	}

	// Empty header cells are ignored along with their columns: they're
	// unnameable, so nothing can reference them (design/xlsx.md §2.1).
	// This also quietly handles a merged header cell: excelize returns
	// its value only in the top-left cell of the range, leaving the
	// rest empty. Duplicate-header detection is a structural check on
	// the file itself, independent of column aliasing, so it stays its
	// own pass rather than folding into resolveColumns.
	seenHeader := make(map[string]bool, len(header))
	for _, h := range header {
		if h == "" {
			continue
		}
		if seenHeader[h] {
			f.Close()
			return nil, fmt.Errorf("source %q: duplicate column %q in header row %d of sheet %q",
				opts.Name, h, headerRow, sheet)
		}
		seenHeader[h] = true
	}

	col, missing, ok := format.ResolveColumns(opts.Schema, header, opts.Columns)
	if !ok {
		f.Close()
		return nil, fmt.Errorf("source %q: required column %q not found in header row %d of sheet %q\n       header columns: %s",
			opts.Name, missing, headerRow, sheet, strings.Join(header, ", "))
	}

	return &xlsxSource{
		f:           f,
		rows:        rows,
		name:        opts.Name,
		col:         col,
		dateFormats: opts.DateFormats,
		schema:      opts.Schema,
		sheetRow:    headerRow,
	}, nil
}

func (s *xlsxSource) Schema() value.Schema {
	return s.schema
}

func (s *xlsxSource) Err() error {
	return s.err
}

func (s *xlsxSource) Next() (value.Row, bool) {
	for {
		if !s.rows.Next() {
			// An exhausted iterator can still carry a real read error
			// (design-errors.md §2.4: infra-fatal, checked after Next
			// reports EOF). A clean end of sheet leaves this nil.
			s.err = s.rows.Error()
			return value.Row{}, false
		}
		s.sheetRow++

		cells, err := s.rows.Columns(excelize.Options{RawCellValue: true})
		if err != nil {
			s.err = fmt.Errorf("source %q: reading row %d: %w", s.name, s.sheetRow, err)
			return value.Row{}, false
		}

		if blankRow(cells, s.col) {
			// Skipped, not emitted and not end-of-stream
			// (design/xlsx.md §7): blank rows are common and must not
			// abort a run, and stopping on the first one would
			// silently truncate any sheet with a gap in it.
			continue
		}

		prov := value.Provenance{
			Source:  s.name,
			Ordinal: s.ordinal,
			Offset:  s.sheetRow,
		}
		s.ordinal++

		fields := make(map[string]any, len(s.schema.Fields))
		for _, field := range s.schema.Fields {
			// A missing key here means an Optional field's column isn't
			// in the header at all. idx -1 makes cellAt read it as an
			// empty cell, which Coerce turns into Absent (mirrors
			// csvSource's equivalent guard).
			idx, ok := s.col[field.Name]
			if !ok {
				idx = -1
			}
			raw := cellAt(cells, idx)
			v, fail := value.Coerce(field.Type, raw, format.ResolveDateFormat(s.dateFormats, field.Name, format.DefaultFormatForKind(field.Type.Kind)))
			if fail != nil {
				fail.Stage = fmt.Sprintf("xlsx:%s", field.Name)
				return value.Row{Fail: fail, Prov: prov}, true
			}
			fields[field.Name] = v
		}
		return value.Row{Fields: fields, Prov: prov}, true
	}
}

// cellAt indexes cells defensively: rows.Columns trims trailing empty
// cells (design/xlsx.md §2.2), so a row shorter than the header is
// normal, not an error. A missing index is just an empty cell.
func cellAt(cells []string, idx int) string {
	if idx < 0 || idx >= len(cells) {
		return ""
	}
	return cells[idx]
}

// blankRow reports whether every schema-mapped column in a row is empty
// (design/xlsx.md §7). Only mapped columns count: an unmapped column
// (ignored per §2.1) having stray content doesn't make an otherwise
// blank row "data".
func blankRow(cells []string, col map[string]int) bool {
	for _, idx := range col {
		if cellAt(cells, idx) != "" {
			return false
		}
	}
	return true
}

// sheetIndex reports whether sheet exists in f, alongside its index.
// Wraps GetSheetIndex's ambiguous "-1 means missing" contract into a
// plain ok bool at the one call site that needs it.
func sheetIndex(f *excelize.File, sheet string) (int, bool) {
	i, err := f.GetSheetIndex(sheet)
	if err != nil || i < 0 {
		return 0, false
	}
	return i, true
}

// stringOpt and intOpt read one keyword argument out of
// runtime.SourceOptions.Opts, applying a default when absent and
// producing a clear, positioned-by-name error when the argument was
// given but is the wrong type: the "let constructors validate types"
// half of design/xlsx.md §1's registry-boundary note. The parser and
// checker never know these names mean anything; only this constructor
// does.
func stringOpt(opts runtime.SourceOptions, name, def string) (string, error) {
	v, ok := opts.Opts[name]
	if !ok {
		return def, nil
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("source %q: %q must be a string", opts.Name, name)
	}
	return s, nil
}

func intOpt(opts runtime.SourceOptions, name string, def int64) (int64, error) {
	v, ok := opts.Opts[name]
	if !ok {
		return def, nil
	}
	n, ok := v.(int64)
	if !ok {
		return 0, fmt.Errorf("source %q: %q must be an integer", opts.Name, name)
	}
	return n, nil
}
