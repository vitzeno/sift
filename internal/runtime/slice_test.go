package runtime

import (
	"testing"

	"github.com/vitzeno/sift/internal/value"
)

// TestLimitEmitsFirstNRows is S3-A: limit(1) on the §7 data (Ada, Tom)
// emits exactly the first row (design-improvements.md §9).
func TestLimitEmitsFirstNRows(t *testing.T) {
	src := &fakeStream{rows: []value.Row{
		{Fields: map[string]any{"name": "Ada"}},
		{Fields: map[string]any{"name": "Tom"}},
	}}
	lim := NewLimit(src, 1)

	row, ok := lim.Next()
	if !ok || row.Fields["name"] != "Ada" {
		t.Fatalf("first Next() = %+v, %v, want Ada", row, ok)
	}
	if _, ok := lim.Next(); ok {
		t.Error("second Next() returned ok=true, want false after emitting N=1 rows")
	}
}

func TestLimitZeroEmitsNothing(t *testing.T) {
	src := &fakeStream{rows: []value.Row{{Fields: map[string]any{"name": "Ada"}}}}
	lim := NewLimit(src, 0)
	if _, ok := lim.Next(); ok {
		t.Error("Next() returned ok=true, want false for limit(0)")
	}
}

func TestLimitStopsPullingUpstreamOnceSatisfied(t *testing.T) {
	src := &fakeStream{rows: []value.Row{
		{Fields: map[string]any{"name": "Ada"}},
		{Fields: map[string]any{"name": "Tom"}},
		{Fields: map[string]any{"name": "Grace"}},
	}}
	lim := NewLimit(src, 1)
	lim.Next()
	if src.calls != 1 {
		t.Errorf("source.Next called %d times, want exactly 1 (limit must not over-pull)", src.calls)
	}
}

// TestLimitCountsFailedRowsPositionally is S3-B: with a failing row
// upstream, limit(n) counts it toward n same as a healthy row. A failed
// row occupies a position in the stream just like a healthy one
// (design-improvements.md §3, §9).
func TestLimitCountsFailedRowsPositionally(t *testing.T) {
	src := &fakeStream{rows: []value.Row{
		{Fields: map[string]any{"name": "Ada"}},
		{Fail: &value.Failure{Reason: "missing email", Stage: "check"}},
		{Fields: map[string]any{"name": "Tom"}},
	}}
	lim := NewLimit(src, 2)
	sink := &fakeSink{}

	// Run under PolicySkip so the failed row (the 2nd position) is
	// dropped by the driver rather than aborting, proving limit counted
	// it toward its total even though it never reaches sink.
	if err := Run(lim, src, []Sink{sink}, nil, PolicySkip, nil); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	// limit(2) sees Ada (healthy) and the failure (both positions 1-2),
	// then stops: Tom (position 3) is never pulled. Only Ada's row
	// reaches the sink; the failed row is skipped.
	if len(sink.written) != 1 || sink.written[0].Fields["name"] != "Ada" {
		t.Errorf("sink.written = %+v, want exactly Ada's row", sink.written)
	}
	if src.calls != 2 {
		t.Errorf("source.Next called %d times, want exactly 2 (Ada + the failed row, Tom never pulled)", src.calls)
	}
}

func TestOffsetDiscardsFirstNRows(t *testing.T) {
	src := &fakeStream{rows: []value.Row{
		{Fields: map[string]any{"name": "Ada"}},
		{Fields: map[string]any{"name": "Tom"}},
		{Fields: map[string]any{"name": "Grace"}},
	}}
	off := NewOffset(src, 2)

	row, ok := off.Next()
	if !ok || row.Fields["name"] != "Grace" {
		t.Fatalf("Next() = %+v, %v, want Grace (first two discarded)", row, ok)
	}
	if _, ok := off.Next(); ok {
		t.Error("second Next() returned ok=true, want false (stream exhausted)")
	}
}

func TestOffsetCountsFailedRowsPositionally(t *testing.T) {
	src := &fakeStream{rows: []value.Row{
		{Fail: &value.Failure{Reason: "bad row", Stage: "check"}},
		{Fields: map[string]any{"name": "Ada"}},
	}}
	off := NewOffset(src, 1)

	row, ok := off.Next()
	if !ok || row.Fields["name"] != "Ada" {
		t.Fatalf("Next() = %+v, %v, want Ada (the failed row counted toward the offset and was discarded)", row, ok)
	}
}

func TestOffsetLargerThanStreamEmitsNothing(t *testing.T) {
	src := &fakeStream{rows: []value.Row{{Fields: map[string]any{"name": "Ada"}}}}
	off := NewOffset(src, 5)
	if _, ok := off.Next(); ok {
		t.Error("Next() returned ok=true, want false when offset exceeds the stream length")
	}
}
