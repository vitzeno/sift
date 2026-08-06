package format

import (
	"testing"

	"github.com/vitzeno/sift/internal/value"
)

func txnSchema(optional bool) value.Schema {
	return value.Schema{Fields: []value.Field{
		{Name: "txn_id", Type: value.Type{Kind: value.Int}},
		{Name: "txn_date", Type: value.Type{Kind: value.String, Optional: optional}},
	}}
}

// TestResolveColumnsAlias is A-1: an alias resolves a header the field's
// own identifier could never match (it has a space).
func TestResolveColumnsAlias(t *testing.T) {
	headers := []string{"Transaction ID", "Date"}
	aliases := map[string]string{"txn_id": "Transaction ID", "txn_date": "Date"}

	got, missing, ok := ResolveColumns(txnSchema(false), headers, aliases)
	if !ok {
		t.Fatalf("ok = false, missing %q, want ok", missing)
	}
	if got["txn_id"] != 0 || got["txn_date"] != 1 {
		t.Errorf("resolved = %+v, want txn_id:0 txn_date:1", got)
	}
}

// TestResolveColumnsBareIdentifierFallback is A-2: with no alias entry,
// a field resolves by its own identifier text matching the header
// exactly.
func TestResolveColumnsBareIdentifierFallback(t *testing.T) {
	headers := []string{"txn_id", "txn_date"}

	got, missing, ok := ResolveColumns(txnSchema(false), headers, nil)
	if !ok {
		t.Fatalf("ok = false, missing %q, want ok", missing)
	}
	if got["txn_id"] != 0 || got["txn_date"] != 1 {
		t.Errorf("resolved = %+v, want txn_id:0 txn_date:1", got)
	}
}

// TestResolveColumnsCaseSensitive confirms the bare-identifier fallback
// is exact, case-sensitive match, not a lenient one (design/column-aliases.md
// §8's locked default).
func TestResolveColumnsCaseSensitive(t *testing.T) {
	headers := []string{"Transaction ID", "TXN_DATE"}
	aliases := map[string]string{"txn_id": "Transaction ID"}

	_, missing, ok := ResolveColumns(txnSchema(false), headers, aliases)
	if ok {
		t.Fatal("ok = true, want txn_date to fail: TXN_DATE != txn_date by exact match")
	}
	if missing != "txn_date" {
		t.Errorf("missing = %q, want txn_date", missing)
	}
}

// TestResolveColumnsRequiredMissingFails is A-3: no alias, no matching
// identifier, and the field is required, so resolution fails outright.
func TestResolveColumnsRequiredMissingFails(t *testing.T) {
	headers := []string{"Date"}

	_, missing, ok := ResolveColumns(txnSchema(false), headers, nil)
	if ok {
		t.Fatal("ok = true, want txn_id to fail: no alias, no matching header")
	}
	if missing != "txn_id" {
		t.Errorf("missing = %q, want txn_id", missing)
	}
}

// TestResolveColumnsOptionalMissingSucceeds is design-column-aliases.md's
// acceptance case D: an Optional field with an alias whose header isn't
// in the file at all resolves to absent, not a failure -- the same
// design/optional-fields.md carve-out this doc must not regress.
func TestResolveColumnsOptionalMissingSucceeds(t *testing.T) {
	headers := []string{"Transaction ID"}
	aliases := map[string]string{"txn_id": "Transaction ID", "txn_date": "Date"}

	got, missing, ok := ResolveColumns(txnSchema(true), headers, aliases)
	if !ok {
		t.Fatalf("ok = false, missing %q, want ok (txn_date is Optional)", missing)
	}
	if _, present := got["txn_date"]; present {
		t.Errorf("resolved[txn_date] = %d, want no entry at all", got["txn_date"])
	}
	if got["txn_id"] != 0 {
		t.Errorf("resolved[txn_id] = %d, want 0", got["txn_id"])
	}
}

// TestResolveColumnsBlankHeaderNeverMatches: a blank header cell (xlsx's
// merged-cell quirk, design/xlsx.md §2.1) is never a legal alias target,
// even if a columns entry happens to have an empty string value.
func TestResolveColumnsBlankHeaderNeverMatches(t *testing.T) {
	headers := []string{"Transaction ID", ""}
	aliases := map[string]string{"txn_id": "Transaction ID", "txn_date": ""}

	_, missing, ok := ResolveColumns(txnSchema(false), headers, aliases)
	if ok {
		t.Fatal("ok = true, want txn_date to fail: a blank header cell must never match")
	}
	if missing != "txn_date" {
		t.Errorf("missing = %q, want txn_date", missing)
	}
}

// TestResolveColumnsUnclaimedAliasEntryIgnored: a columns entry for a
// field name that isn't in the schema at all is silently unused, the
// same way an extra column in the file is silently dropped (design/column-aliases.md
// §3's "fully optional and additive").
func TestResolveColumnsUnclaimedAliasEntryIgnored(t *testing.T) {
	headers := []string{"Transaction ID", "Date"}
	aliases := map[string]string{
		"txn_id":      "Transaction ID",
		"txn_date":    "Date",
		"nonexistent": "Some Other Column",
	}

	got, missing, ok := ResolveColumns(txnSchema(false), headers, aliases)
	if !ok {
		t.Fatalf("ok = false, missing %q, want ok", missing)
	}
	if len(got) != 2 {
		t.Errorf("resolved = %+v, want exactly 2 entries", got)
	}
}
