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

For the full language reference see [`design.md`](design.md). For how
the codebase is organized and built, see [`CLAUDE.md`](CLAUDE.md).

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

> **Note:** a source's path (`"people.csv"` above) resolves relative to
> the current working directory, not to `adults.sift`'s own location —
> so run `sift` from the directory containing both files. This is a
> deliberate v0 simplification (see `cmd/sift/run.go`), not a bug.

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
$ ./sift run testdata/adults.sift            # from within testdata/
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
  fails a row when the condition is false.
- **Expressions**: field access (`.age`), int/double/string/bool
  literals, `+ - * /`, comparisons, `&& ||`, function calls, and record
  literals with spread (`{ ...row, ... }`).
- **PII tags** attach at the source, propagate through every expression,
  and are only cleared by `mask`/`hash`/`redact`. A sink rejects any
  field still tagged `@pii`.

## Project layout

```
cmd/sift/            CLI: run, --emit-ast, --emit-schema
internal/value/       Row, Provenance, Type (+ @pii), Schema
internal/lexer/       source text -> tokens
internal/ast/         AST node types
internal/parser/      recursive descent + Pratt expression parsing
internal/checker/     name resolution, schema recompute, PII enforcement
internal/eval/        eval(expr, row) any
internal/runtime/     Stream/Source/Sink, driver loop, format registry, build
internal/format/      csv source, jsonl sink
testdata/             .sift programs and their input/expected-output fixtures
```

## Status

v0 is complete: both acceptance cases in `design.md` §7 pass end to end
through the CLI, and every module (1 through 8) has unit tests. v0's
scope is deliberately closed — see `design.md` §5 for what's built and
what's explicitly deferred (joins, dedupe, fan-out, an optimizer, schema
inference, and more formats beyond csv/jsonl).
