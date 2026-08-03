package runtime

import (
	"fmt"

	"github.com/vitzeno/sift/internal/value"
)

// Policy is a program's error policy (design.md §2, design-errors.md
// §3.1): how the driver disposes of a row that failed somewhere
// upstream.
type Policy int

const (
	// PolicyAbort stops the run at the first failed row. It's the
	// default when a program declares no policy (design.md §2: "abort
	// is the default, skip is never implicit").
	PolicyAbort Policy = iota
	// PolicySkip discards a failed row silently and continues.
	PolicySkip
	// PolicyRoute writes a failed row's envelope to a second sink
	// instead of the main one.
	//
	// decision: Run's switch below has no case for it yet. Wiring it up
	// needs an error sink and the fixed envelope Row (design-errors.md
	// §4) — that's phase E2's job, alongside the frontend
	// (ErrorPolicyDecl) that lets a program actually select this value.
	// Nothing can construct PolicyRoute today, so hitting the default
	// panic below can only mean the checker/Build and this switch have
	// drifted out of sync with each other later, not a user-facing case.
	PolicyRoute
)

// FailureError is what Run returns when a row's Fail is non-nil under
// PolicyAbort — a data problem (bad input), not a bug, but still enough
// to stop the run per design.md §2's default "abort" policy.
type FailureError struct {
	Fail *value.Failure
	Prov value.Provenance
}

func (e *FailureError) Error() string {
	return fmt.Sprintf("row %d from %q: %s", e.Prov.Ordinal, e.Prov.Source, e.Fail.Reason)
}

// Run is the driver loop (design.md §4, design-errors.md §3.2): pull one
// row from top, and either write a healthy row to sink or dispose of a
// failed one per policy, until top is exhausted. Exactly one row is in
// flight at a time — nothing here buffers the stream.
//
// decision: Run takes both top (the fully-built stage chain, for
// pulling rows) and src (the original Source Build constructed, before
// any stage wrapped it) as separate parameters, rather than adding Err()
// to the general Stream interface. Only Source gained Err()
// (design-errors.md §2.4) — Filter/Map/Check didn't need a matching
// method just to forward it, and top's static type stays plain Stream.
func Run(top Stream, src Source, sink Sink, policy Policy) error {
	for {
		row, ok := top.Next()
		if !ok {
			if srcErr := src.Err(); srcErr != nil {
				sink.Close()
				return srcErr
			}
			break
		}
		if row.Fail != nil {
			switch policy {
			case PolicyAbort:
				sink.Close()
				return &FailureError{Fail: row.Fail, Prov: row.Prov}
			case PolicySkip:
				continue
			default:
				panic(fmt.Sprintf("runtime: policy %v not supported by Run yet", policy))
			}
		}
		if err := sink.Write(row); err != nil {
			return err
		}
	}
	return sink.Close()
}
