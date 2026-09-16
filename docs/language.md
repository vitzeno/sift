# The Sift language

Sift is a small, statically-typed ETL language. A program describes how
rows of data flow through stages, from a source to a sink. The host
language and runtime is **Go**. There is no LLVM and no native codegen:
Sift runs on a tree-walking interpreter, because ETL is I/O-bound and
native code buys almost nothing next to disk and network time.

This is the reference for what the language *is*. For a guided tour with
a runnable example per feature, see
[`../examples/README.md`](../examples/README.md).

---

## 1. Core mental model

A Sift program is a **dataflow graph**, not a list of imperative
instructions. Data is a stream of **rows**; a row is a record (named
fields with typed values) plus provenance (where it came from). The `|>`
operator feeds the stream on its left into the stage on its right. A
program is a **source** (reads rows), some **stages** (transform the
stream), one or more **sinks** (write rows), and an **error policy**.

Execution is **pull-based** (the Volcano model): the sink asks the top
stage for one row; that request travels back up the chain to the source;
one row flows down through every stage; it gets written; repeat. Exactly
one row is in flight at a time, so a file larger than RAM streams without
being fully materialized.

A stage is conceptually a function `stream<T> -> stream<U>`. A chain of
stages with no source or sink is therefore itself a reusable
`stream<T> -> stream<U>` value — a **named segment**.

---

## 2. Language reference

### Sources and sinks

```sift
source in  = csv("people.csv", schema: { name: string, age: int })
sink   out = jsonl("adults.jsonl")
```

A source declares a format, a path, and format-specific options passed as
keyword arguments. It must be able to report its output **schema** (§3),
which for csv and xlsx is declared inline, since neither format carries
reliable types of its own.

Sources: `csv`, `xlsx`. Sinks: `jsonl`, `console`.

Keyword arguments a source accepts:

| Kwarg | Applies to | Purpose |
|---|---|---|
| `schema:` | all sources | the declared record type (required) |
| `columns:` | csv, xlsx | map a schema field to a raw header string |
| `formats:` | csv, xlsx | per-field parse layout for `date`/`datetime` |
| `key:` | csv, xlsx | `env("NAME")`, required by `@deidentify` |
| `sheet:` | xlsx | worksheet name |
| `header_row:` | xlsx | 1-based row the header lives on |

`columns:` names a column whose real header isn't a valid identifier:

```sift
source in = csv("export.csv",
  schema:  { txn_id: int, amount: decimal },
  columns: { txn_id: "Transaction ID" }
)
```

Resolution per field is: the `columns:` alias, then the bare identifier
(an exact, case-sensitive match), then — for an optional field only —
absent. A required field that resolves none of those ways is an error
raised before any row is read.

### Pipelines and stages

```sift
pipeline main {
  in
    |> filter(.age >= 18)
    |> map({ ...row, name: upper(.name) })
    |> out
}
```

Built-in stages:

| Stage | Effect on the stream | Effect on the schema |
|---|---|---|
| `filter(<bool>)` | keeps rows where the predicate holds | unchanged |
| `check(<bool>, "<reason>")` | fails a row when the condition is false | unchanged |
| `map({ ...row, f: expr })` | rebuilds each row | recomputed |
| `select(col, ...)` | — | keeps only the named columns, in that order |
| `drop(col, ...)` | — | removes the named columns |
| `rename(old: new, ...)` | — | renames in place, preserving type and position |
| `limit(n)` | emits at most `n` rows | unchanged |
| `offset(n)` | discards the first `n` rows | unchanged |
| `mask(col, ...)` | replaces each value | clears `@pii` on those columns |
| `hash(col, ...)` | replaces each value | clears `@pii` on those columns |
| `redact(col, ...)` | replaces each value | clears `@pii` on those columns |

`limit` and `offset` count positionally over every row they see, healthy
or failed: a stage has no way to know what the error policy will later do
with a failed row.

### Terminal productions

A pipeline ends in one of three ways:

```sift
in |> ... |> out                       // one sink
in |> ... |> warehouse, audit          // broadcast: every sink gets every row
in |> ... |> route {                   // route: each row goes to exactly one
     .region == "EU" => eu_sink,
     .region == "US" => us_sink,
     else            => rest_sink,
   }
```

Broadcast writes every row to every listed sink, so listing the same sink
twice is a compile error — it would be a literal double-write. A route
picks the first branch, top to bottom, whose predicate is true. `else` is
mandatory: the checker rejects a route that isn't provably total. Its
target may be `discard`, which drops unmatched rows explicitly rather
than silently. Unlike broadcast, the same sink may appear in more than
one branch.

Both share one terminal schema, checked once, since neither transforms
the row.

### Expressions

- Field access: `.age`, `.email`
- Literals: ints, doubles, strings, `true` / `false`
- Binary ops: `+ - * /`, `< > <= >= == !=`, `&& ||`, `??`
- Function calls: `upper`, `lower`, `trim`, `mask`, `hash`, `redact`
- Record spread: `{ ...row, field: expr }` — keep all fields of `row`,
  override or add the named ones. This is how `map` stays generic over
  columns it doesn't mention.

There are no unary operators and no chained field access.

### Named segments

```sift
pipeline clean =
     check(.email != "", "missing email")
  |> map({ ...row, email: lower(trim(.email)) })

pipeline main {
  in |> clean |> out
}
```

A pipeline with no source or sink is a `stream<T> -> stream<U>` value
that can be dropped into another pipeline. It may take parameters, and
then reads exactly like a built-in stage at its call site:

```sift
pipeline scrub(col)        = mask(col)
pipeline adults(min: int)  = filter(.age >= min)

pipeline main {
  in |> scrub(email) |> adults(18) |> out
}
```

A **column parameter** is a bare identifier, referenced `.name` inside
the body and passed a bare column name at the call site. A **scalar
parameter** is `name: type`, referenced as a bare value inside the body
and passed a literal of that type. A call argument is always a
compile-time constant, never an expression over row data.

The checker monomorphizes at each call site — it copies the body,
substitutes the arguments, and checks the copy against the real schema —
rather than using row polymorphism. Every error from inside a substituted
body carries dual-site context: which segment, which bindings, and where
it was instantiated from.

### Error policy

Declared once per program. Every row carries provenance (source name,
ordinal, and a format-specific offset) from the moment it is read. A row
fails when `check`'s condition is false or when a source cell won't
coerce to its declared type.

```sift
on error abort        // stop the whole run — THIS IS THE DEFAULT
on error skip         // drop failed rows silently — must be typed explicitly
on error |> errors    // route failed rows to a sink
```

Silent data loss must be spelled out: `abort` is the default and `skip`
is never implicit. A routed failure is written as a fixed envelope —
`source`, `ordinal`, `offset`, `reason`, `stage` — never the row's own
fields, so a routed failure can never leak pipeline data.

A failed row is **opaque**: no stage downstream evaluates an expression
against it, since its fields are by definition suspect. Only the driver,
at the end of the chain, disposes of it.

An infrastructure failure that leaves no row to mark — a malformed record
the reader can't tokenize at all, an I/O error mid-read — aborts the run
regardless of the active policy. `skip` never swallows one.

---

## 3. Type system

### Schemas

A schema is the record type of a stream, e.g.
`stream<{ name: string, age: int }>`.

**The checker recomputes the schema at every stage** — this is its core
job:

- `filter` / `check` / `limit` / `offset` — schema passes through
  unchanged.
- `map({ ...row, adult: .age >= 18 })` — start from the input schema,
  keep all fields (`...row`), add or override the named ones with the
  type of their expression (`>=` yields `bool`). Never declared, always
  computed.
- A reference to a field not in the current schema (e.g. a typo `.emial`)
  is a **compile-time error**, reported against the real column set.

### Scalar types

| Type | Notes |
|---|---|
| `string` | |
| `int` | |
| `double` | |
| `bool` | |
| `date` | a calendar date only — no time of day, no timezone |
| `datetime` | a naive timestamp — date plus time of day, still no timezone |
| `decimal` | exact arithmetic, no `float64` rounding error |

`date` and `datetime` are comparable with `< > <= >= == !=` but have no
arithmetic and no literal syntax: a value only ever comes from a source
column. They never mix with each other in a comparison, even though both
are temporal. Parsing is driven by the per-field `formats:` kwarg, which
both share; each defaults to its own ISO-8601 layout for any field the
kwarg doesn't name.

`decimal` is backed by exact decimal arithmetic and renders at the scale
it was parsed at, so `"5.00"` comes back out as `5.00`, not `5`. A bare
int or double literal standing directly against a `decimal` operand
adapts to `decimal` in that position only, mirroring Go's own
untyped-constant model; two real columns of different type never mix this
way, even two numeric ones. A comma-thousands-formatted cell like
`"2,100.00"` parses unconditionally, with no keyword argument — unless
the comma sits after the cell's last `.`, which is European
decimal-point formatting and fails to parse rather than silently reading
as the wrong number.

> **Known gap:** a whole-number European cell with a comma as its only
> separator and no `.` at all is indistinguishable from thousands
> grouping, and reads as the wrong number.

### PII tags

A field's type can carry a `@pii` tag: `email: string @pii`. Four rules:

1. **Attach at the source** via the declared schema.
2. **Propagate through every operation.** `lower(.email)` is still
   `@pii`; transforming a value never launders it. Any expression that
   consumes a `@pii` value yields a `@pii` result, and renaming a `@pii`
   column preserves the tag.
3. **Sinks reject unmasked PII.** Writing a `@pii` field to a sink is a
   **compile-time error**.
4. **Clear only via explicit declassifiers** — `mask`, `hash`, `redact` —
   the only functions that turn `string @pii` into a clean `string`.
   Dropping the column entirely also works.

```sift
users |> map({ email: lower(.email) }) |> out
// error: field "email" is @pii and reaches sink "out" unmasked;
//        declassify with mask/hash/redact

users |> map({ email: mask(.email) }) |> out   // ok
```

> **Known gap:** the three declassifiers are string-only, so a non-string
> `@pii` field (`date`, `int`, `decimal`, ...) has no in-place
> declassifier at all and can only be dropped.

### Optional fields

A `T?` schema field may be absent. At the source, a missing column or a
blank cell becomes a well-typed **absent** value rather than a row
failure; a present-but-malformed cell still fails the row. Optionality
propagates through every expression exactly like `@pii`, and a sink
rejects a field that's still optional, as does any true/false test.

`??` is the discharge operator: `left ?? right` always yields a
non-optional result, with `right` supplying the value for `left`'s absent
case. The default must share `left`'s type and must not itself be
optional.

```sift
map({ ...row, phone: .phone ?? "unknown" })
```

`??` binds tighter than comparisons (`.age ?? 0 >= 18` reads as
`(.age ?? 0) >= 18`) and looser than arithmetic (`.amount ?? 0 + 5` reads
as `.amount ?? (0 + 5)`).

The two tags are orthogonal: a `string? @pii` field must clear both
before it reaches a sink.

### `@deidentify`

`email: string @deidentify` encrypts a field inside the source, before
any stage runs, and offers no way back. The field's type becomes
`deidentified<string>` at every later stage.

Unlike `@pii` and optionality, this is not a tag alongside the type — it
*replaces* the type. Nothing can consume it, so there is no propagation
rule to write anywhere: field access alone is rejected, which covers
every operator, function call, comparison (even against another
deidentified field), `??`, and segment parameter for free. Reassigning
the column in a `map` record is rejected too, since that's the one case
that never reads the column back.

What remains is structural: passthrough, `select`, `drop`, and `rename`.
Reaching a sink is the whole point and never an error.

Encryption is AES-256-GCM with a fresh random nonce per cell,
base64-encoded, prefixed with a presence byte so an absent cell and a
genuinely empty one are indistinguishable without the key. The 32-byte
key comes from `key: env("NAME")`, resolved from the environment at
construction time — never at parse or check time, so `--emit-ast` and
`--emit-schema` still work on a machine that doesn't hold the key.

`@pii` and `@deidentify` on the same field is a compile error: they
describe incompatible handling of the same column.

> **Out of scope:** Sift never decrypts anything it encrypts, in-program
> or otherwise. There is no `reveal()`. Who holds the decryption key and
> decrypts downstream — a separate tool, a KMS, a warehouse UDF — is
> deliberately outside the language.
>
> **Known gap:** ciphertext length still varies with plaintext length.

---

## 4. Runtime architecture

The compilation pipeline is
`source text -> lexer -> parser (recursive descent + Pratt) -> AST ->
checker -> build -> execute`. Execution interprets the checked AST
directly; there is no separate IR.

Core runtime types:

```go
type Row struct {
    Fields map[string]any
    Prov   Provenance // source name, ordinal, offset
    Fail   *Failure   // nil for a healthy row
}

type Stream interface {
    Next() (Row, bool) // ok=false means the stream is finished
}

type Source interface {
    Stream
    Schema() Schema
    Err() error   // infra-fatal failure, checked after Next reports EOF
    Close() error // released by the driver on every exit path
}

type Sink interface {
    Write(Row) error
    Close() error
}
```

- **Computation** happens in `eval(expr, row) any`, which switches on AST
  node types and applies Go's own operators. No bytecode.
- **File I/O** happens inside sources and sinks, using the Go standard
  library (`encoding/csv`, `encoding/json`) and, for xlsx, excelize. The
  language never touches a syscall.
- **The driver is one loop**: pull a row from the top of the chain, send
  it to the terminal production or to the error policy, repeat until the
  stream is exhausted.

### Format registry

The language does not know about formats — the runtime does, via a
registry mapping a format name to a constructor. `source in = xlsx(...)`
looks up `"xlsx"` and calls its constructor.

Adding a format means writing one struct that satisfies `Source` (or
`Sink`) and registering it under a name from its own `init()`. **No
grammar, parser, checker, or executor changes.** Custom readers and
writers are first-class: Postgres, S3, HTTP and the like are purely Go
structs behind the registry.

A format's keyword arguments follow the same boundary. The parser
collects them uninterpreted and only the registered constructor knows
what a given name means or validates its type, so a format can grow
options without the frontend learning anything about it.

---

## 5. Design principles

1. **Clear, boring code over cleverness.** This is a language others read
   to understand the design.
2. **Diagnostics are a feature.** Parser and checker errors carry source
   positions and read like a data tool, not a stack trace:
   `error: field "emial" not in schema {name, age}`.
3. **PII enforcement is never optional.** The tag attaches at the source,
   propagates through every operation, and sinks reject it unmasked. A
   new stage or connector that doesn't preserve this is a bug, not a
   design choice.
4. **Formats only ever enter through the registry**, never special-cased
   in the lexer, parser, checker, or executor.
5. **Failures are data, not exceptions.** A bad row is marked and carried,
   and what happens to it is a decision the program makes explicitly.
