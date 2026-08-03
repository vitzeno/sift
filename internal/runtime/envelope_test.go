package runtime

import (
	"testing"

	"github.com/vitzeno/sift/internal/value"
)

func TestEnvelopeRowFields(t *testing.T) {
	row := envelopeRow(
		value.Provenance{Source: "in", Ordinal: 3, Offset: 42},
		&value.Failure{Reason: "missing email", Stage: "check"},
	)
	want := map[string]any{
		"source":  "in",
		"ordinal": 3,
		"offset":  42,
		"reason":  "missing email",
		"stage":   "check",
	}
	if len(row.Fields) != len(want) {
		t.Fatalf("Fields = %#v, want %#v", row.Fields, want)
	}
	for k, v := range want {
		if row.Fields[k] != v {
			t.Errorf("Fields[%q] = %#v, want %#v", k, row.Fields[k], v)
		}
	}
	if row.Prov.Ordinal != 3 {
		t.Errorf("Prov.Ordinal = %d, want 3", row.Prov.Ordinal)
	}
}

// TestEnvelopeSchemaHasNoPII confirms design-errors.md §4's core payoff:
// every envelope field is a plain, unmasked-clean scalar, so nothing
// about a routed failure can trip the checker's unmasked-@pii sink rule
// — not even if the pipeline's own schema had a @pii field, since the
// envelope never carries pipeline fields at all.
func TestEnvelopeSchemaHasNoPII(t *testing.T) {
	if _, ok := envelopeSchema.FirstPII(); ok {
		t.Error("envelopeSchema has a @pii field, want none — it must never carry pipeline data")
	}
	wantFields := []string{"source", "ordinal", "offset", "reason", "stage"}
	if len(envelopeSchema.Fields) != len(wantFields) {
		t.Fatalf("envelopeSchema has %d fields, want %d", len(envelopeSchema.Fields), len(wantFields))
	}
	for i, name := range wantFields {
		if envelopeSchema.Fields[i].Name != name {
			t.Errorf("Fields[%d].Name = %q, want %q", i, envelopeSchema.Fields[i].Name, name)
		}
	}
}
