# Sift — Design Spec

**Status:** Done — v0 complete, both acceptance cases in §7 pass end to
end through the CLI. This is the canonical "what Sift is" document.
For "how we build it," see `CLAUDE.md`. When the two disagree, `language.md` wins
on the language, `CLAUDE.md` wins on process.

A small, statically-typed ETL language. Programs describe how rows of data flow
through stages, from a source to a sink. Host language and runtime is **Go**.
There is **no LLVM and no native codegen** — Sift runs on a tree-walking
interpreter, because ETL is I/O-bound and native code buys almost nothing next
to disk and network time.

---

## 1. Core mental model

A Sift program is a **dataflow graph**, not a list of imperative instructions.
Data is a stream of **rows**; a row is a record (named fields with typed values)
plus provenance (where it came from). The `|>` operator feeds the stream on its
left into the stage on its right. A program is a **source** (reads rows), some
**stages** (transform the stream), a **sink** (writes rows), and an **error
policy**.

Execution is **pull-based** (Volcano model): the sink asks the top stage for one
row; that request travels back up the chain to the source; one row flows down
through every stage; it gets written; repeat. Exactly one row is in flight at a
time, so a file larger than RAM streams without being fully materialized.

A stage is conceptually a function `stream<T> -> stream<U>`. A chain of stages
with no source or sink is therefore itself a reusable `stream<T> -> stream<U>`
value — a **named segment**.

---

## 2. Language reference

### Sources and sinks

```sift
source in  = csv("people.csv", schema: { name: string, age: int })
sink   out = jsonl("adults.jsonl")
```

A source declares the format (`csv`, `jsonl`, `xlsx`, `parquet`, ...), a path,
and format-specific options passed as keyword arguments. A source must be able
to report its output **schema** (§3): either declared inline (as above) or
carried by the format itself (Parquet).

### Pipelines and stages

```sift
pipeline main {
  in
    |> filter(.age >= 18)
    |> map({ ...row, name: upper(.name) })
    |> out
}
```

Built-in stages for v0:

- `filter(<bool expr>)` — keep rows where the predicate is true. Schema unchanged.
- `map({ ... })` — rebuild each row from a record expression. Schema recomputed.
- `check(<bool expr>, "<reason>")` — fail a row when the condition is false
  (routed per the error policy). Schema unchanged.

### Expressions

- Field access: `.age`, `.email`
- Literals: ints, doubles, strings, `true` / `false`
- Binary ops: `+ - * /`, `< > <= >= == !=`, `&& ||`
- Record spread: `{ ...row, field: expr }` — keep all fields of `row`, override
  or add the named ones. This is how `map` stays generic over columns it doesn't
  mention.

### Named segments (reusable pipelines)

```sift
pipeline clean =
     check(.email != "", "missing email")
  |> map({ ...row, email: lower(trim(.email)) })

pipeline main {
  in |> clean |> out
}
```

A pipeline with no source/sink is a `stream<T> -> stream<U>` value that can be
dropped into another pipeline.

### Error policy

Declared per program. Every row carries provenance (source name, ordinal, and
byte/line offset where the format supports it) from the moment it is read. A
stage fails a row via `check`, an explicit `fail("reason")` inside a `map`, or a
fatal `?`-propagation.

```sift
on error abort        // stop the whole run — THIS IS THE DEFAULT
on error skip         // drop failed rows silently — must be typed explicitly
on error |> errors    // route failed rows (with reason + provenance) to a sink
```

Silent data loss must be spelled out. `abort` is the default; `skip` is never
implicit.

---

## 3. Type system

### Schemas

A schema is the record type of a stream, e.g. `stream<{ name: string, age: int }>`.
For v0, schemas are **declared** on the source (CSV/xlsx have no inherent types;
declaring is simple and explicit). Inference from a sampled header is a later
option and, when added, a declared schema always overrides it.

**The checker recomputes the schema at every stage** — this is its core job:

- `filter` / `check` — schema passes through unchanged.
- `map({ ...row, adult: .age >= 18 })` — start from the input schema, keep all
  fields (`...row`), add/override the named ones with the type of their
  expression (`>=` yields `bool`), producing the output schema. Never declared,
  always computed.
- A reference to a field not in the current schema (e.g. a typo `.emial`) is a
  **compile-time error**, reported against the real column set.

### PII tags (the distinctive feature)

A field's type can carry a `@pii` tag: `email: string @pii`. Rules:

1. **Attach at the source** via the declared schema.
2. **Propagate through every operation.** `lower(.email)` is still `@pii`;
   transforming a value never launders it. Any expression that consumes a `@pii`
   value yields a `@pii` result.
3. **Sinks reject unmasked PII.** Writing a `@pii` field to a sink is a
   **compile-time error**.
4. **Clear only via explicit declassifiers** — `mask(x)`, `hash(x)`, `redact(x)`
   — the only functions that turn `string @pii` into a clean `string`.

```sift
users |> map({ email: lower(.email) }) |> out
// error: field "email" is @pii and reaches sink "out" unmasked;
//        declassify with mask/hash/redact

users |> map({ email: mask(.email) }) |> out   // ok
```

Implementation cost is low: the tag is one extra field on the type struct,
propagation is one line in each stage's type rule, and the sink check is one
comparison. This is the feature no mainstream ETL tool has — keep it correct.

---

## 4. Runtime architecture

Compilation pipeline:
`source text -> lexer -> parser (recursive descent + Pratt) -> AST -> checker ->
build -> execute`. In v0, execute by interpreting the checked AST directly. Do
**not** build a separate IR yet.

Core runtime types:

```go
type Row struct {
    Fields map[string]any
    Prov   Provenance // source name, ordinal, offset
}

type Stream interface {
    Next() (Row, bool) // ok=false means the stream is finished
}

type Source interface {
    Next() (Row, bool)
    Schema() Type
}

type Sink interface {
    Write(Row) error
    Close() error
}
```

- **Computation** happens in an `eval(expr, row) any` function that switches on
  AST node types and applies Go's own operators. No bytecode.
- **File I/O** happens inside sources and sinks using the Go standard library
  (`encoding/csv`, `encoding/json`) and, later, third-party libs (`excelize`,
  a Parquet lib). The language never touches a syscall.
- **The driver is one loop**: repeatedly pull a row from the top of the chain
  and write it to the sink until the stream is exhausted.

### Format registry & custom readers/writers

The language does not know about formats — the runtime does, via a registry
mapping a format name to a constructor:

```go
registry := map[string]SourceCtor{
    "csv": newCSVSource, "jsonl": newJSONLSource,
    "xlsx": newXLSXSource, "parquet": newParquetSource,
}
```

`source in = xlsx(...)` looks up `"xlsx"` and calls its constructor. Adding a
format = writing one struct that satisfies `Source` (or `Sink`) and registering
it under a name. **No grammar, parser, checker, or executor changes.** Custom
readers/writers are first-class: Postgres, S3, HTTP, etc. are purely Go structs
behind the registry. Every source must answer `Schema()` before rows flow.

### Custom functions

- **Scalar functions** (v0-friendly):
  `func fullname(a: string, b: string): string = a + " " + b`, usable inside
  `map`/`filter`. Implemented as entries in the `eval` function-lookup table.
- **Custom stages** (later): any Go type implementing `Stream` can be a new
  stage kind. Named segments already cover most "custom stage" needs.

---

## 5. Scope

### v0 — build exactly this

`in |> out` plus `filter`, `map`, and `check`. CSV in, JSONL out. Single linear
pipeline. Declared schemas. PII tag checking on. Scalar user functions optional.

### Deferred — do NOT build yet

- The pipeline **IR** (a DAG of stage nodes). v0 interprets the AST directly;
  refactor to lower→execute only once there's a reason.
- **Joins** (and their optionality-aware result types).
- `dedupe` and other buffering/blocking stages.
- **Fan-out / fan-in** (`split`, multiple sinks, merging sources).
- **Optimizer passes** (fusion, predicate pushdown, parallel branches).
- **Schema inference** from sampling; **runtime plugins**; connectors beyond
  csv/jsonl (xlsx/parquet are nice-to-have but not required for acceptance).

---

## 6. Module build order

Build as a **vertical slice** in dependency order. Get the runtime spine
(#1, #2) reading a CSV and writing JSONL _before_ the lexer or parser exist —
prove `in |> out` end to end with a hand-built chain first, then add the
frontend.

1. **Row & value model** — `Row`, `Provenance`, the runtime value
   representation, and the `Type` representation (including the `@pii` tag).
2. **Runtime spine** — `Stream`/`Source`/`Sink` interfaces, the CSV source, the
   JSONL sink, and the one-loop driver. Prove reading + writing with a hardcoded
   chain.
3. **Lexer** — source text to tokens, each carrying line/column for diagnostics.
4. **AST** — `Program`, `SourceDecl`, `SinkDecl`, `PipelineDecl`, stages
   (`NameRef`, `Filter`, `Map`, `Check`), expressions (`FieldAccess`, literals,
   `BinaryOp`, record spread).
5. **Parser** — recursive descent for top-level and pipeline structure, Pratt
   for expressions. Prioritize good error messages.
6. **Checker** — resolve source/sink/pipeline names; compute the schema at each
   stage; type-check field access and predicates; enforce PII propagation and
   the sink rule.
7. **Build + execute** — turn the checked AST into the chain of `Stream` objects
   (`buildPipeline`) and run the driver over it. `eval` lives here.
8. **CLI** — flags: `--emit-ast`, `--emit-schema`, and `run`.

---

## 7. Acceptance tests

### Case A — the adults filter

Program `adults.sift`:

```sift
source in  = csv("people.csv", schema: { name: string, age: int })
sink   out = jsonl("adults.jsonl")

pipeline main {
  in |> filter(.age >= 18) |> out
}
```

Input `people.csv`:

```
name,age
Ada,42
Tom,15
```

Expected `adults.jsonl` (Tom is filtered out):

```
{"name":"Ada","age":42}
```

Execution trace to reproduce mentally:

```
driver: top.Next()
  Filter.Next()
    CSVSource.Next() -> Row{name:"Ada", age:42}, ok
    eval(age >= 18, row): 42 >= 18 -> true
    return Row{Ada}, true
  sink.Write(Row{Ada}) -> {"name":"Ada","age":42}
driver: top.Next()
  Filter.Next()
    CSVSource.Next() -> Row{name:"Tom", age:15}, ok
    eval(age >= 18, row): 15 >= 18 -> false  -> loop, pull again
    CSVSource.Next() -> EOF -> Row{}, false
    return Row{}, false
  ok == false -> break
done.
```

### Case B — PII enforcement

A source with `email: string @pii`. A pipeline that writes `email` to the sink
unmasked must **fail to compile** with a clear error. Wrapping it in
`mask(.email)` must compile and run.

---

## 8. Design principles

1. **Clear, boring code over cleverness** — this is a language others read to
   understand the design.
2. **Diagnostics are a feature.** Parser/checker errors carry source positions
   and read like a data tool, not a stack trace:
   `error: field "emial" not in schema {name, age}`.
3. **PII enforcement is a v0 requirement, not a later add-on.**
4. **Formats go through the registry** — never special-cased in the frontend or
   executor.
5. **When the spec doesn't answer a question,** prefer the simplest choice that
   keeps v0 shippable, leave a comment noting the decision, and keep going.
