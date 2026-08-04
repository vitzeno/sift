package checker

import (
	"github.com/vitzeno/sift/internal/ast"
	"github.com/vitzeno/sift/internal/value"
)

// checkRename computes rename's output schema: each pair's Old column
// becomes New in place, keeping its exact type, including the @pii tag
// (design-improvements.md §2). This matters: renaming a PII column must
// never launder it.
//
// Every New must avoid two collisions: a surviving column (one not
// itself being renamed) already using that name, and another pair's New
// in the same call. Checking against the full "surviving" set, rather
// than mutating field names one pair at a time, keeps the result
// independent of the order the pairs happen to be written in.
func (c *checker) checkRename(st *ast.Rename, schema value.Schema) (value.Schema, error) {
	renamedOld := map[string]bool{}
	for _, pair := range st.Pairs {
		if renamedOld[pair.Old] {
			return value.Schema{}, errorf(pair.Pos, "column %q renamed more than once", pair.Old)
		}
		renamedOld[pair.Old] = true
	}

	surviving := map[string]bool{}
	for _, f := range schema.Fields {
		if !renamedOld[f.Name] {
			surviving[f.Name] = true
		}
	}

	newFor := map[string]string{} // old -> new
	seenNew := map[string]bool{}
	for _, pair := range st.Pairs {
		if _, ok := schema.Lookup(pair.Old); !ok {
			return value.Schema{}, errorf(pair.Pos, "column %q not in schema %s", pair.Old, schema)
		}
		if surviving[pair.New] {
			return value.Schema{}, errorf(pair.Pos, "rename target %q collides with an existing column", pair.New)
		}
		if seenNew[pair.New] {
			return value.Schema{}, errorf(pair.Pos, "duplicate rename target %q", pair.New)
		}
		seenNew[pair.New] = true
		newFor[pair.Old] = pair.New
	}

	fields := make([]value.Field, len(schema.Fields))
	for i, f := range schema.Fields {
		if newName, ok := newFor[f.Name]; ok {
			fields[i] = value.Field{Name: newName, Type: f.Type}
		} else {
			fields[i] = f
		}
	}
	return value.Schema{Fields: fields}, nil
}
