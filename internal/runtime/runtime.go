// Package runtime holds the pull-based execution machinery: the
// Stream/Source/Sink interfaces, the format registry, built-in stages, and
// the driver loop. It knows nothing about Sift syntax; that's the frontend's
// job (lexer/parser/checker, modules 3-6) which doesn't exist yet.
package runtime

import "github.com/vitzeno/sift/internal/value"

// Stream is anything that produces rows one at a time. Next returns
// ok=false exactly once, when the stream is exhausted; callers must not
// call Next again afterward.
type Stream interface {
	Next() (value.Row, bool)
}

// Source is a Stream that can also report the schema of the rows it
// produces. Every source must be able to answer Schema() before rows
// flow; the schema is always declared up front, so this is available
// immediately after construction.
//
// Err reports an infrastructure failure, one that leaves no row to mark
// (e.g. a dead file handle mid-read), as opposed
// to a data failure on one row, which rides on that Row's Fail field
// instead. The driver calls Err() only after Next returns ok == false;
// nil means a clean EOF, non-nil means abort regardless of the active
// error policy.
//
// Close releases whatever the source holds open -- a file handle, a
// parsed workbook -- and the driver always calls it, on every exit path,
// exactly as it already does for a sink. Without it a source's handle
// stays open until the process exits, which leaks a descriptor per run
// and, on Windows, leaves the input file locked against deletion.
type Source interface {
	Stream
	Schema() value.Schema
	Err() error
	Close() error
}

// Sink consumes a finished stream. Close flushes and releases any
// underlying resource (e.g. an open file) and is called once, after the
// driver observes ok=false.
type Sink interface {
	Write(value.Row) error
	Close() error
}
