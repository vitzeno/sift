package runtime

import (
	"errors"
	"fmt"

	"github.com/vitzeno/sift/internal/eval"
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
	// PolicyRoute writes a failed row's envelope (design-errors.md §4)
	// to Run's errSink instead of the main one.
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
// row from top, and either dispatch a healthy row per route's a-branch-at-
// a-time selection (design-routing.md §4), broadcast it to every sink
// (design-multisink.md §3), or dispose of a failed one per policy, until
// top is exhausted. Exactly one row is in flight at a time — nothing
// here buffers the stream.
//
// route is nil for a broadcast program (the pre-routing behavior,
// unchanged) and non-nil for a routed one — build.go's Build resolves
// each branch's target sink name into route's SinkIndex, so Run needs no
// name lookup here, only the walk itself. Route dispatch cascades after
// the Fail check, mirroring design-routing.md §3: a failed row's data is
// suspect, so it never reaches a branch predicate regardless of which
// terminal production the program uses.
//
// sinks are written in declared order (design-multisink.md §6) for
// broadcast, or at most once for route; a write error is infra-fatal
// either way, matching the single-sink behavior this generalizes. A
// partial multi-file write on the broadcast path is inherent to writing
// N files with no cross-file transaction — declared order at least keeps
// the partial state deterministic.
//
// errSink is only used, and only need be non-nil, under PolicyRoute; it
// is nil for every program that doesn't declare `on error |> <name>`
// (design-errors.md §5).
//
// decision: Run takes both top (the fully-built stage chain, for
// pulling rows) and src (the original Source Build constructed, before
// any stage wrapped it) as separate parameters, rather than adding Err()
// to the general Stream interface. Only Source gained Err()
// (design-errors.md §2.4) — Filter/Map/Check didn't need a matching
// method just to forward it, and top's static type stays plain Stream.
func Run(top Stream, src Source, sinks []Sink, route []RouteBranch, policy Policy, errSink Sink) error {
	// closeAll closes every sink (and errSink, if present) even if an
	// earlier Close errors, aggregating rather than bailing on the first
	// (design-multisink.md §6's "close-all" rule) — a later sink's file
	// handle deserves to be released regardless of an earlier one's fate.
	closeAll := func() error {
		var errs []error
		for _, s := range sinks {
			if err := s.Close(); err != nil {
				errs = append(errs, err)
			}
		}
		if errSink != nil {
			if err := errSink.Close(); err != nil {
				errs = append(errs, err)
			}
		}
		return errors.Join(errs...)
	}

	for {
		row, ok := top.Next()
		if !ok {
			if srcErr := src.Err(); srcErr != nil {
				closeAll()
				return srcErr
			}
			break
		}
		if row.Fail != nil {
			switch policy {
			case PolicyAbort:
				closeAll()
				return &FailureError{Fail: row.Fail, Prov: row.Prov}
			case PolicySkip:
				continue
			case PolicyRoute:
				if err := errSink.Write(envelopeRow(row.Prov, row.Fail)); err != nil {
					closeAll()
					return err
				}
				continue
			default:
				panic(fmt.Sprintf("runtime: unknown policy %v", policy))
			}
		}
		if route != nil {
			for _, b := range route {
				if !b.IsElse && !eval.Eval(b.Pred, row).(bool) {
					continue
				}
				if !b.Discard {
					if err := sinks[b.SinkIndex].Write(row); err != nil {
						closeAll()
						return err
					}
				}
				break
			}
			continue
		}
		for _, s := range sinks {
			if err := s.Write(row); err != nil {
				closeAll()
				return err
			}
		}
	}
	return closeAll()
}
