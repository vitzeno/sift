package checker

import (
	"strings"
	"testing"
)

// TestCheckRouteTerminal is design-routing.md §1's own example end to
// end: two predicate branches plus a mandatory else, each resolved to a
// distinct sink, all sharing the one terminal schema.
func TestCheckRouteTerminal(t *testing.T) {
	const src = `source in = csv("people.csv", schema: { name: string, region: string })
sink eu_sink = jsonl("eu.jsonl")
sink us_sink = jsonl("us.jsonl")
sink rest_sink = jsonl("rest.jsonl")

pipeline main {
  in |> route {
    .region == "EU" => eu_sink,
    .region == "US" => us_sink,
    else            => rest_sink,
  }
}`
	cp := mustCheck(t, src)
	if len(cp.Route) != 3 {
		t.Fatalf("Route = %d branches, want 3", len(cp.Route))
	}
	if len(cp.Sinks) != 3 {
		t.Fatalf("Sinks = %d, want 3 (all three targets are distinct)", len(cp.Sinks))
	}
	if cp.Route[0].Target != "eu_sink" || cp.Route[1].Target != "us_sink" {
		t.Errorf("Route targets = %q, %q, want eu_sink, us_sink", cp.Route[0].Target, cp.Route[1].Target)
	}
	last := cp.Route[2]
	if !last.IsElse || last.Pred != nil || last.Target != "rest_sink" {
		t.Errorf("last branch = %+v, want else => rest_sink with a nil predicate", last)
	}
}

// TestCheckRouteElseRequired is RT-B: a route with no else branch at all
// is a compile error, regardless of how many predicate branches precede
// it — arbitrary boolean predicates can't otherwise be proven total.
func TestCheckRouteElseRequired(t *testing.T) {
	const src = `source in = csv("people.csv", schema: { name: string, region: string })
sink eu_sink = jsonl("eu.jsonl")

pipeline main {
  in |> route {
    .region == "EU" => eu_sink,
  }
}`
	err := checkErr(t, src)
	want := `route is not total; add an 'else' branch (use 'else => discard' to drop unmatched rows explicitly)`
	if !strings.Contains(err.Error(), want) {
		t.Errorf("error = %q, want it to contain %q", err.Error(), want)
	}
}

// TestCheckRouteElseDischargeCompiles is RT-C's checker half: `else =>
// discard` satisfies totality explicitly, with no sink attached to the
// else branch.
func TestCheckRouteElseDischargeCompiles(t *testing.T) {
	const src = `source in = csv("people.csv", schema: { name: string })
sink out = jsonl("out.jsonl")

pipeline main {
  in |> route {
    .name != "" => out,
    else        => discard,
  }
}`
	cp := mustCheck(t, src)
	last := cp.Route[len(cp.Route)-1]
	if !last.IsElse || !last.Discard || last.Target != "" {
		t.Errorf("else branch = %+v, want IsElse=true Discard=true Target=\"\"", last)
	}
	if len(cp.Sinks) != 1 {
		t.Errorf("Sinks = %d, want 1 (discard contributes no sink)", len(cp.Sinks))
	}
}

// TestCheckRouteDuplicateSinkAllowed is RT-F: unlike broadcast's
// duplicate-sink error, the same sink naming more than one branch is
// fine — different rows, never a double-write of the same row.
func TestCheckRouteDuplicateSinkAllowed(t *testing.T) {
	const src = `source in = csv("people.csv", schema: { name: string, region: string })
sink out = jsonl("out.jsonl")

pipeline main {
  in |> route {
    .region == "EU" => out,
    .region == "US" => out,
    else            => out,
  }
}`
	cp := mustCheck(t, src)
	if len(cp.Sinks) != 1 {
		t.Fatalf("Sinks = %d, want 1 (deduplicated across all three branches)", len(cp.Sinks))
	}
	if cp.Sinks[0].Name != "out" {
		t.Errorf("Sinks[0].Name = %q, want %q", cp.Sinks[0].Name, "out")
	}
}

// TestCheckRoutePIIRejectedOnce is RT-G: an unmasked @pii field reaching
// any branch sink is a single compile error naming every distinct target
// sink, checked once against the shared terminal schema — exactly
// design-multisink.md §4's rule, reused verbatim for route.
func TestCheckRoutePIIRejectedOnce(t *testing.T) {
	const src = `source in = csv("people.csv", schema: { name: string, region: string, email: string @pii })
sink eu_sink = jsonl("eu.jsonl")
sink rest_sink = jsonl("rest.jsonl")

pipeline main {
  in |> route {
    .region == "EU" => eu_sink,
    else            => rest_sink,
  }
}`
	err := checkErr(t, src)
	want := `field "email" is @pii and reaches sink "eu_sink", "rest_sink" unmasked; declassify with mask/hash/redact`
	if !strings.Contains(err.Error(), want) {
		t.Errorf("error = %q, want it to contain %q", err.Error(), want)
	}
}

// TestCheckRoutePIIAllowedOnceMasked is RT-G's success mode: masking
// before the route clears the tag for every branch at once.
func TestCheckRoutePIIAllowedOnceMasked(t *testing.T) {
	const src = `source in = csv("people.csv", schema: { name: string, region: string, email: string @pii })
sink eu_sink = jsonl("eu.jsonl")
sink rest_sink = jsonl("rest.jsonl")

pipeline main {
  in |> map({ ...row, email: mask(.email) }) |> route {
    .region == "EU" => eu_sink,
    else            => rest_sink,
  }
}`
	cp := mustCheck(t, src)
	if f, ok := cp.SinkSchema.FirstPII(); ok {
		t.Errorf("SinkSchema still has a PII field: %+v", f)
	}
}

// TestCheckRouteOptionalRejectedOnce mirrors TestCheckRoutePIIRejectedOnce
// for design/optional-fields.md's discharge rule (design-routing.md §5's
// "shared terminal schema" note applies to both tags, not just PII).
func TestCheckRouteOptionalRejectedOnce(t *testing.T) {
	const src = `source in = csv("people.csv", schema: { name: string, region: string, phone: string? })
sink eu_sink = jsonl("eu.jsonl")
sink rest_sink = jsonl("rest.jsonl")

pipeline main {
  in |> route {
    .region == "EU" => eu_sink,
    else            => rest_sink,
  }
}`
	err := checkErr(t, src)
	want := `field "phone" is optional and reaches sink "eu_sink", "rest_sink" undischarged; resolve with ??`
	if !strings.Contains(err.Error(), want) {
		t.Errorf("error = %q, want it to contain %q", err.Error(), want)
	}
}

// TestCheckRouteBadPredicateFieldTypo is RT-H: a field typo in a branch
// predicate errors against the real column set, exactly like a filter
// predicate would.
func TestCheckRouteBadPredicateFieldTypo(t *testing.T) {
	const src = `source in = csv("people.csv", schema: { name: string, region: string })
sink out = jsonl("out.jsonl")

pipeline main {
  in |> route {
    .regoin == "EU" => out,
    else            => out,
  }
}`
	err := checkErr(t, src)
	if !strings.Contains(err.Error(), `field "regoin" not in schema`) {
		t.Errorf("error = %v, want a field-not-in-schema error", err)
	}
}

// TestCheckRouteNonBoolPredicate rejects a branch predicate that isn't
// bool, and TestCheckRouteOptionalBoolPredicate rejects one that's an
// undischarged bool? — design/optional-fields.md §3's third discharge
// rule applies to a route branch exactly like it does to filter/check.
func TestCheckRouteNonBoolPredicate(t *testing.T) {
	const src = `source in = csv("people.csv", schema: { name: string, region: string })
sink out = jsonl("out.jsonl")

pipeline main {
  in |> route {
    .region => out,
    else    => out,
  }
}`
	err := checkErr(t, src)
	if !strings.Contains(err.Error(), "route branch predicate must be bool, got string") {
		t.Errorf("error = %v, want a route branch predicate type error", err)
	}
}

func TestCheckRouteOptionalBoolPredicate(t *testing.T) {
	const src = `source in = csv("people.csv", schema: { name: string, active: bool? })
sink out = jsonl("out.jsonl")

pipeline main {
  in |> route {
    .active => out,
    else    => out,
  }
}`
	err := checkErr(t, src)
	if !strings.Contains(err.Error(), "route branch predicate must be bool, got bool?") {
		t.Errorf("error = %v, want a bool? route branch predicate error", err)
	}
}

// TestCheckRouteTargetNotASink is RT-H's other half: a branch target
// naming something that isn't a declared sink is a compile error.
func TestCheckRouteTargetNotASink(t *testing.T) {
	const src = `source in = csv("people.csv", schema: { name: string })
sink out = jsonl("out.jsonl")

pipeline main {
  in |> route {
    .name != "" => nosuch,
    else        => out,
  }
}`
	err := checkErr(t, src)
	if !strings.Contains(err.Error(), `route target "nosuch" is not a declared sink`) {
		t.Errorf("error = %v, want a not-a-declared-sink error", err)
	}
}

// TestCheckRouteReservedAgainstNamedSegment covers design-routing.md
// §7/§11: "route" is reserved the same way select/drop/... are, so a
// named segment can never shadow the terminal keyword.
func TestCheckRouteReservedAgainstNamedSegment(t *testing.T) {
	const src = `source in = csv("people.csv", schema: { name: string })
sink out = jsonl("out.jsonl")

pipeline route {
  filter(.name != "")
}

pipeline main {
  in |> out
}`
	err := checkErr(t, src)
	if !strings.Contains(err.Error(), `"route" is a built-in stage name`) {
		t.Errorf("error = %v, want a reserved-name error", err)
	}
}
