# Sift

Sift is a small, statically-typed, streaming ETL language. A program is a
dataflow graph — `source |> stage |> ... |> sink` — executed by a
pull-based, tree-walking interpreter written in Go. There's no IR and no
codegen: ETL is I/O-bound, so a native compiler buys almost nothing next
to disk and network time.

The distinctive feature: **compile-time PII tagging**. Mark a field
`@pii` on a source's schema and Sift tracks it through every expression
that touches it. A sink that would write an unmasked `@pii` field is a
compile error, not a runtime surprise.

For the full language reference see [`design.md`](design.md); for error
semantics (`on error`, failed rows) see [`design-errors.md`](design-errors.md).
For how the codebase is organized and built, see [`CLAUDE.md`](CLAUDE.md).

## Quick example

`people.csv`:

```csv
name,age
Ada,42
Tom,15
```

`adults.sift`:

```sift
source in  = csv("people.csv", schema: { name: string, age: int })
sink   out = jsonl("adults.jsonl")

pipeline main {
  in |> filter(.age >= 18) |> out
}
```

Run it:

```console
$ sift run adults.sift
$ cat adults.jsonl
{"name":"Ada","age":42}
```

Tom is filtered out; only Ada's row reaches the sink.

> **Note:** a source or sink's path (`"people.csv"`, `"adults.jsonl"`
> above) resolves relative to `adults.sift`'s own directory, not the
> current working directory — so this works the same way whether you run
> `sift` from right there or from anywhere else, as long as you pass it
> `path/to/adults.sift`.

## PII in one example

```sift
source in  = csv("people.csv", schema: { name: string, email: string @pii })
sink   out = jsonl("out.jsonl")

pipeline main {
  in |> out
}
```

```console
$ sift run leaky.sift
leaky.sift:4:1: error: field "email" is @pii and reaches sink "out" unmasked; declassify with mask/hash/redact
```

Declassify it explicitly and the same program compiles and runs:

```sift
pipeline main {
  in |> map({ ...row, email: mask(.email) }) |> out
}
```

`mask`, `hash`, and `redact` are the only three functions that can turn
a `string @pii` back into a plain `string`. Every other function or
operator that touches a `@pii` value propagates the tag — transforming
PII never launders it.

## More stages: select, drop, rename, limit/offset, declassifiers

Beyond `filter`/`map`/`check`, five more built-ins round out v0's stage
set. `select`/`drop` project columns — and dropping a `@pii` column is a
legitimate way to satisfy the sink rule, with no `mask` call at all.
`rename` remaps a column name while preserving its exact type,
**including a `@pii` tag** — a renamed PII column is still rejected
unmasked. `limit`/`offset` slice rows positionally, counting a failed
row the same as a healthy one. And `mask`/`hash`/`redact` exist as
stages as well as functions: `|> hash(email)` declassifies the whole
column in one step, where `map({ ...row, email: hash(.email) })` is one
missing `...row` away from silently dropping every other column.

```sift
in
  |> drop(ssn, internal_notes)
  |> rename(full_name: name, signup_date: joined_at)
  |> hash(email)
  |> mask(phone)
  |> select(id, name, email, phone, plan, joined_at)
  |> limit(3)
  |> out
```

See `testdata/select.sift`, `drop.sift`, `rename.sift`,
`limit-offset.sift`, and `declassify.sift` for one runnable example per
stage, and `testdata/customer-export.sift` for the composite pipeline
above in full — a CRM dump turned into a GDPR-safe analytics extract.

## Error policies

A row can fail — a `check` condition is false, or a source cell won't
coerce to its declared type (a non-numeric `age`, say). What happens
next is controlled by one `on error` declaration, and it's the same
mechanism either way: the row is marked, not thrown as an exception, and
the driver disposes of it per the active policy.

```sift
source in  = csv("people.csv", schema: { name: string, age: int, email: string })
sink   out = jsonl("out.jsonl")

pipeline main {
  in |> filter(.age >= 18) |> check(.email != "", "missing email") |> out
}
```

`people.csv` has one row (`Grace`) with a blank email. With no `on error`
declaration, the default is **abort**:

```console
$ sift run errors.sift
errors.sift: error: row 2 from "in": missing email
```

Add `on error skip` and the same program drops Grace's row instead and
keeps going — every other healthy row still reaches the sink, and the
run exits successfully:

```sift
on error skip

source in  = csv("people.csv", schema: { name: string, age: int, email: string })
sink   out = jsonl("out.jsonl")
...
```

Or route failures to a second sink with `on error |> errors` instead of
dropping them. The error sink never receives a failed row's own fields —
only a fixed envelope of where and why it failed, so a `@pii` field can
never leak through a routed failure the way it could if raw rows were
forwarded:

```console
$ cat errors.jsonl
{"source":"in","ordinal":2,"offset":4,"reason":"missing email","stage":"check"}
```

See `testdata/on-error-abort.sift`, `on-error-skip.sift`,
`on-error-route.sift`, and `bad-cell.sift` for complete, runnable
versions of each.

## Building and running

```console
$ make build          # go build ./...
$ make test           # go test ./...
$ make check          # gofmt -l, go vet, go test
$ make run            # runs testdata/adults.sift, prints nothing, writes adults.jsonl
$ make emit-ast       # dumps the parsed AST for testdata/adults.sift
$ make emit-schema    # dumps the checked source/sink schemas
```

Or drive the CLI directly:

```console
$ go build -o sift ./cmd/sift
$ ./sift run testdata/adults.sift
$ ./sift --emit-ast testdata/adults.sift
$ ./sift --emit-schema testdata/adults.sift
```

## Language at a glance

- **Sources and sinks** declare a format (`csv`, `jsonl`, ...) and a
  path. A source also declares its schema — CSV has no inherent types,
  so schemas are always explicit in v0.
- **Pipelines** are a linear chain: `in |> stage |> ... |> out`. A
  pipeline with no source/sink (`pipeline clean = check(...) |> map(...)`)
  is a reusable `stream<T> -> stream<U>` segment you can drop into
  another pipeline by name.
- **Stages** (v0's complete set): `filter(<bool>)` keeps matching rows;
  `map({ ...row, field: expr })` rebuilds each row; `check(<bool>, "reason")`
  fails a row when the condition is false; `select(col, ...)`/`drop(col, ...)`
  project columns; `rename(old: new, ...)` remaps a column name, preserving
  type and any `@pii` tag; `limit(n)`/`offset(n)` slice rows positionally;
  `mask(col, ...)`/`hash(col, ...)`/`redact(col, ...)` declassify a `@pii`
  column in place.
- **Expressions**: field access (`.age`), int/double/string/bool
  literals, `+ - * /`, comparisons, `&& ||`, function calls, and record
  literals with spread (`{ ...row, ... }`).
- **PII tags** attach at the source, propagate through every expression,
  and are only cleared by `mask`/`hash`/`redact`. A sink rejects any
  field still tagged `@pii`.
- **Error policy** (`on error abort | skip | |> <sink>`) controls what
  happens to a row that fails a `check` or a source cell that won't
  coerce to its declared type. `abort` (the default) stops the run;
  `skip` drops the row and continues; routing sends a fixed envelope —
  provenance and reason, never the row's own fields — to a second sink.

## Project layout

```
cmd/sift/            CLI: run, --emit-ast, --emit-schema
internal/value/       Row, Provenance, Type (+ @pii), Schema, Coerce
internal/lexer/       source text -> tokens
internal/ast/         AST node types
internal/parser/      recursive descent + Pratt expression parsing
internal/checker/     name resolution, schema recompute, PII + error-policy enforcement
internal/eval/        eval(expr, row) any
internal/runtime/     Stream/Source/Sink, driver loop, error policy, format registry, build
internal/format/      csv source, jsonl sink
testdata/             one .sift + fixture pair per language feature or error policy
```

## Status

v0 is complete: both acceptance cases in `design.md` §7 pass end to end
through the CLI, and every module (1 through 8) has unit tests. v0's
scope is deliberately closed — see `design.md` §5 for what's built and
what's explicitly deferred (joins, dedupe, fan-out, an optimizer, schema
inference, and more formats beyond csv/jsonl).

`design-errors.md`'s error-semantics phase is also complete: failures are
data, not exceptions (a failed row is marked and flows to the driver
rather than panicking), `on error abort/skip/route` is a real language
feature, and a source cell that won't coerce to its declared type fails
the same way a `check` does — governed by the same policy, with its own
acceptance tests (`design-errors.md` §7, ERR-A through ERR-E).

`design-improvements.md`'s built-in stages batch is also complete:
`select`/`drop`, `rename`, `limit`/`offset`, and `mask`/`hash`/`redact`
as first-class stages, each with its own acceptance tests
(`design-improvements.md` §9, S1-A through S4-B) and a runnable
`testdata/` example.

`design-xlsx.md` and `design-parquet.md` document two more connector
phases — neither is built yet; xlsx depends on this phase's
`value.Coerce`, parquet depends on nothing beyond the registry.
