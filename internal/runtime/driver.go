package runtime

import (
	"errors"
	"fmt"

	"github.com/vitzeno/sift/internal/eval"
	"github.com/vitzeno/sift/internal/value"
)

// Policy is a program's error policy: what the driver does with a row
// that failed upstream.
type Policy int

const (
	// PolicyAbort stops the run at the first failed row. This is the
	// default.
	PolicyAbort Policy = iota
	// PolicySkip drops a failed row and keeps going.
	PolicySkip
	// PolicyRoute writes a failed row's envelope to errSink instead of
	// the main sink.
	PolicyRoute
)

// FailureError is what Run returns when a row fails under PolicyAbort.
// It's bad input, not a bug, but it still stops the run.
type FailureError struct {
	Fail *value.Failure
	Prov value.Provenance
}

func (e *FailureError) Error() string {
	return fmt.Sprintf("row %d from %q: %s", e.Prov.Ordinal, e.Prov.Source, e.Fail.Reason)
}

// Run is the driver loop: pull one row from top at a time, and either
// send a healthy row through route's branch-by-branch pick, broadcast it
// to every sink, or hand a failed one to the error policy. Stops when
// top runs out. Only one row is ever in flight.
//
// route is nil for a plain broadcast program. When set, Build has
// already turned each branch's sink name into an index in sinks, so Run
// just walks the branches, no name lookup here. Route is checked after
// the Fail check either way: a failed row's data can't be trusted, so it
// never reaches a branch.
//
// sinks are written in the order they're declared, whether that's
// broadcast (every sink) or route (at most one). A write error stops the
// run right there.
//
// errSink is only used, and only needs to be set, under PolicyRoute. It
// stays nil for any program with no `on error |> <name>`.
//
// Run takes both top (the built stage chain) and src (the original
// source) as separate arguments, instead of adding Err() to the general
// Stream interface, since only Source needs it.
func Run(top Stream, src Source, sinks []Sink, route []RouteBranch, policy Policy, errSink Sink) error {
	// closeAll closes every sink even if an earlier one errors, and
	// collects every error rather than stopping at the first.
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
				_ = closeAll()
				return srcErr
			}
			break
		}
		if row.Fail != nil {
			switch policy {
			case PolicyAbort:
				_ = closeAll()
				return &FailureError{Fail: row.Fail, Prov: row.Prov}
			case PolicySkip:
				continue
			case PolicyRoute:
				if err := errSink.Write(envelopeRow(row.Prov, row.Fail)); err != nil {
					_ = closeAll()
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
						_ = closeAll()
						return err
					}
				}
				break
			}
			continue
		}
		for _, s := range sinks {
			if err := s.Write(row); err != nil {
				_ = closeAll()
				return err
			}
		}
	}
	return closeAll()
}
