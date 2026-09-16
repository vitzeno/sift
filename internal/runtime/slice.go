package runtime

import "github.com/vitzeno/sift/internal/value"

// Limit emits at most N rows then stops.
//
// emitted counts every row Limit itself returns, healthy or failed, so
// the limit is positional over all rows: Limit has no idea what the
// active error policy is (a failed row
// might later be skipped, routed, or abort the run), so it just tracks
// what it has pulled and handed upstream, uniformly.
type Limit struct {
	in      Stream
	n       int64
	emitted int64
}

func NewLimit(in Stream, n int64) *Limit {
	return &Limit{in: in, n: n}
}

func (l *Limit) Next() (value.Row, bool) {
	if l.emitted >= l.n {
		return value.Row{}, false
	}
	row, ok := l.in.Next()
	if !ok {
		return value.Row{}, false
	}
	l.emitted++
	return row, true
}

// Offset discards the first N rows pulled from upstream, healthy or
// failed (the same positional-over-all-rows rule as Limit), then passes
// everything else through unchanged.
type Offset struct {
	in      Stream
	n       int64
	skipped int64
}

func NewOffset(in Stream, n int64) *Offset {
	return &Offset{in: in, n: n}
}

func (o *Offset) Next() (value.Row, bool) {
	for o.skipped < o.n {
		if _, ok := o.in.Next(); !ok {
			return value.Row{}, false
		}
		o.skipped++
	}
	return o.in.Next()
}
