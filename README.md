# Sift

Sift is a small, statically-typed, streaming ETL language. A program is a
dataflow graph — `source |> stage |> ... |> sink` — executed by a
pull-based, tree-walking interpreter written in Go. There's no IR and no
codegen: ETL is I/O-bound, so a native compiler buys almost nothing next
to disk and network time.

Its one distinctive feature: **compile-time PII tagging**. Mark a field
`@pii` on a source's schema and Sift tracks it through every expression
that touches it. A sink that would write an unmasked `@pii` field is a
compile error, not a runtime surprise.

This README is a tutorial: it starts from the smallest possible pipeline
and adds one concept at a time, each backed by a runnable example under
[`examples/`](examples/). For the complete grammar and semantics see
[`design/language.md`](design/language.md); for how the codebase itself is
organized, see [`CLAUDE.md`](CLAUDE.md).

## Contents

1. [Building the CLI](#building-the-cli)
2. [Your first pipeline](#your-first-pipeline)
3. [Transforming rows with map](#transforming-rows-with-map)
4. [Gating rows: filter and check](#gating-rows-filter-and-check)
5. [The PII tag](#the-pii-tag)
6. [Shaping schemas: select, drop, rename](#shaping-schemas-select-drop-rename)
7. [Slicing rows: limit and offset](#slicing-rows-limit-and-offset)
8. [Declassifying stages](#declassifying-stages)
9. [Putting it together](#putting-it-together)
10. [When a row fails: error policies](#when-a-row-fails-error-policies)
11. [Writing to more than one sink](#writing-to-more-than-one-sink)
12. [Reusable pipelines: named segments](#reusable-pipelines-named-segments)
13. [Parameterized segments](#parameterized-segments)
14. [A complete ETL, with error routing](#a-complete-etl-with-error-routing)
15. [Quick reference](#quick-reference)
16. [Project layout](#project-layout)
17. [Status](#status)

## Building the CLI

```console
$ go build -o sift ./cmd/sift
$ ./sift
usage: sift run <file> | sift --emit-ast <file> | sift --emit-schema <file>
```

Everything below assumes a `sift` binary built this way (`make build`
does the same thing — see [Project layout](#project-layout) for the rest
of the Makefile).

## Your first pipeline

Every Sift program is three things: a **source**, a **sink**, and a
**pipeline** connecting them with `|>`. Here's the smallest one that does
something — filter out anyone under 18.

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

- `source in = csv(...)` declares a source named `in`, read as `csv`,
  with an explicit **schema** — CSV carries no types of its own, so every
  field's name and type (`string`, `int`, `double`, or `bool`) is spelled
  out here.
- `sink out = jsonl(...)` declares a sink named `out`, writing one JSON
  object per line.
- `pipeline main { ... }` is the program's entry point: exactly one
  `|>`-chain, starting at a source and ending at a sink.
- `filter(.age >= 18)` is a **stage**. `.age` reads a field off the
  current row; the row survives only if the condition is true.

Run it:

```console
$ ./sift run adults.sift
$ cat adults.jsonl
{"name":"Ada","age":42}
```

Tom is filtered out silently; only Ada's row reaches the sink.

> **Path resolution:** `"people.csv"` and `"adults.jsonl"` resolve
> relative to `adults.sift`'s own directory, not your current working
> directory — this runs the same whether you're sitting right next to it
> or invoking `sift run path/to/adults.sift` from anywhere else.

Two more flags are useful while you're learning the language:
`--emit-ast` shows what the parser built, `--emit-schema` shows what the
checker computed for the source and sink:

```console
$ ./sift --emit-ast adults.sift
source in = csv("people.csv", schema: { name: string, age: int })
sink out = jsonl("adults.jsonl")
pipeline main:
  in
  filter((.age >= 18))
  out

$ ./sift --emit-schema adults.sift
source in: { name: string, age: int }
sink out: { name: string, age: int }
```

`examples/adults.sift` is this exact program.

## Transforming rows with map

`filter` only ever keeps or drops a row unchanged. To change one, use
`map`, which rebuilds it from a **record literal**:

```sift
source in  = csv("map.csv", schema: { name: string, age: int, balance: double })
sink   out = jsonl("map_out.jsonl")

pipeline main {
  in |> map({ ...row, name: upper(.name), balance_ok: .balance >= 100.0, age_next_year: .age + 1 }) |> out
}
```

Against `map.csv` (`Ada,42,250.50` / `Tom,15,50.00`):

```console
$ ./sift run map.sift
{"name":"ADA","age":42,"balance":250.5,"balance_ok":true,"age_next_year":43}
{"name":"TOM","age":15,"balance":50,"balance_ok":false,"age_next_year":16}
```

`{ ...row, ... }` spreads every field of the current row first; each named
field after it either overrides one in place (`name`) or adds a new one
(`balance_ok`, `age_next_year`). The output schema follows the same rule —
original position first, new fields appended in the order written.

This is also your first real look at Sift's expressions:

| Kind | Examples |
|---|---|
| Field access | `.age`, `.name` |
| Literals | `42`, `3.14`, `"hi"`, `true` |
| Arithmetic | `+ - * /` |
| Comparison | `< > <= >= == !=` |
| Boolean | `&& \|\|` |
| Function call | `upper(.name)`, `mask(.email)` |

There's no chained access (`.a.b`) and no user-defined scalar functions —
just field access, the operators above, and a fixed set of built-ins
(`upper`, `lower`, `trim`, and the three declassifiers you'll meet next).

`examples/map.sift` is this exact program.

## Gating rows: filter and check

`filter` and `check` both take a boolean condition, but they mean
different things when it's false:

- `filter(cond)` **silently drops** the row — it never existed downstream.
- `check(cond, "reason")` **marks the row as failed**, citing the reason.
  It doesn't disappear; it becomes a problem for the pipeline's error
  policy to deal with (see [When a row fails](#when-a-row-fails-error-policies)).

```sift
source in  = csv("check.csv", schema: { name: string, email: string })
sink   out = jsonl("check_out.jsonl")

pipeline main {
  in |> check(.email != "", "missing email") |> out
}
```

```console
$ ./sift run check.sift
{"name":"Ada","email":"ada@example.com"}
{"name":"Tom","email":"tom@example.com"}
```

Every row in `check.csv` has a non-blank email, so `check` never fires
here — `examples/check.sift` shows the pass-through case; a failing one
is coming up once error policies are on the table.

## The PII tag

Tag a field `@pii` in a source's schema and the checker tracks it through
every expression that touches it — arithmetic, function calls, `map`, all
of it. A sink that would receive a field still tagged `@pii` is a
**compile error**, not a runtime surprise:

```sift
source in  = csv("people.csv", schema: { name: string, email: string @pii })
sink   out = jsonl("out.jsonl")

pipeline main {
  in |> out
}
```

```console
$ ./sift run leaky.sift
leaky.sift:4:1: error: field "email" is @pii and reaches sink "out" unmasked; declassify with mask/hash/redact
```

Three functions, and only three, can turn a `string @pii` back into a
plain `string`: `mask`, `hash`, `redact`. Apply one directly as a stage
and the same shape of program compiles and runs:

```sift
pipeline main {
  in |> hash(email) |> out
}
```

```console
$ ./sift run pii.sift
{"name":"Ada","email":"b5fc85e55755f9e0d030a10ab4429b6b2944855f9a0d60077fe832becbc41d72"}
```

Every other function or operator that touches a `@pii` value **propagates
the tag** to its result — `upper(.email)` is still `@pii`. There's no way
to accidentally launder PII by transforming it: only `mask`/`hash`/`redact`
clear the tag, because the checker special-cases exactly those three.
`examples/pii.sift` demonstrates the same rule.

## Shaping schemas: select, drop, rename

Three stages reshape a row's schema without changing PII semantics.

**`select(col, ...)`** keeps only the named columns, in the order you
name them:

```sift
in |> select(email, name, plan) |> out
```

**`drop(col, ...)`** removes columns entirely — and dropping a `@pii`
column is a legitimate way to satisfy the sink rule with no `mask` call
at all, since the field is simply gone before the check ever runs:

```sift
source in  = csv("drop.csv", schema: { name: string, ssn: string @pii, department: string })
sink   out = jsonl("drop_out.jsonl")

pipeline main {
  in |> drop(ssn) |> out
}
```

```console
$ ./sift run drop.sift
{"name":"Ada","department":"Engineering"}
{"name":"Tom","department":"Sales"}
```

**`rename(old: new, ...)`** remaps a column's name while preserving its
exact type and position — **including a `@pii` tag**. Renaming is not a
way to launder PII either:

```sift
source in  = csv("rename.csv", schema: { name: string, dob: string, email_addr: string @pii })
sink   out = jsonl("rename_out.jsonl")

pipeline main {
  in |> rename(dob: birth_date, email_addr: email) |> mask(email) |> out
}
```

`mask(email)` is still required after the rename: `email_addr`'s `@pii`
tag rode along onto `email` unchanged. See `examples/select.sift`,
`drop.sift`, and `rename.sift`.

## Slicing rows: limit and offset

`limit(n)` emits at most the first `n` rows then stops; `offset(n)`
discards the first `n` rows and passes the rest through. Both count
*every* row that reaches them positionally, healthy or failed:

```sift
source in  = csv("limit-offset.csv", schema: { name: string, event: string })
sink   out = jsonl("limit-offset_out.jsonl")

pipeline main {
  in |> offset(2) |> limit(2) |> out
}
```

Against a six-row log, `offset(2)` discards the first two rows and
`limit(2)` takes exactly the next two, then stops — the remaining rows
are never even read:

```console
$ ./sift run limit-offset.sift
{"name":"Grace","event":"purchase"}
{"name":"Liam","event":"logout"}
```

`examples/limit-offset.sift` is the runnable version.

## Declassifying stages

`mask`/`hash`/`redact` also exist as **stages**, not just functions —
`|> hash(email)` declassifies the whole column in one step, where
`map({ ...row, email: hash(.email) })` is one missing `...row` away from
silently dropping every other column:

```sift
source in  = csv("declassify.csv", schema: { ticket_id: int, email: string @pii, phone: string @pii, subject: string })
sink   out = jsonl("declassify_out.jsonl")

pipeline main {
  in |> hash(email) |> redact(phone) |> out
}
```

```console
$ ./sift run declassify.sift
{"ticket_id":1,"email":"b5fc85e55755f9e0d030a10ab4429b6b2944855f9a0d60077fe832becbc41d72","phone":"[REDACTED]","subject":"Billing question"}
{"ticket_id":2,"email":"72bb75a959e1785b79ffe7230eaeec25880707a91b4a4f98330fc1510bd40e03","phone":"[REDACTED]","subject":"Login issue"}
```

`hash` is SHA-256, hex-encoded — stable and one-way, good for a join key
that shouldn't reveal the original value. `redact` drops the value to a
fixed placeholder entirely. `mask` (seen earlier) replaces each character
with `*`, preserving only the value's length. All three clear `@pii`;
nothing else does. As a stage, each takes a column-name list the same way
`select`/`drop` do, and only ever applies to a column that's already
`string @pii` — using one anywhere else is a compile error.

## Putting it together

A realistic pipeline chains several of these. This one turns a full CRM
dump into a GDPR-safe analytics extract:

```sift
source in  = csv("customer-export.csv", schema: {
  id: int,
  full_name: string,
  email: string @pii,
  phone: string @pii,
  ssn: string @pii,
  signup_date: string,
  plan: string,
  internal_notes: string
})
sink   out = jsonl("customer-export_out.jsonl")

pipeline main {
  in
    |> drop(ssn, internal_notes)
    |> rename(full_name: name, signup_date: joined_at)
    |> hash(email)
    |> mask(phone)
    |> select(id, name, email, phone, plan, joined_at)
    |> limit(3)
    |> out
}
```

Reading top to bottom: `ssn` and `internal_notes` never leave at all, not
even masked; the two legacy column names land on the warehouse's
canonical names; `email` becomes a stable hash and `phone` a masked
placeholder; `select` fixes the exact output shape and order; `limit(3)`
caps this run to a schema-preview export. Every `@pii` column is gone or
declassified by the time `select` runs, so the sink accepts the result
with no further work:

```console
$ ./sift run customer-export.sift
{"id":1,"name":"Ada Lovelace","email":"b5fc85e55755f9e0d030a10ab4429b6b2944855f9a0d60077fe832becbc41d72","phone":"********","plan":"pro","joined_at":"2024-01-15"}
{"id":2,"name":"Tom Reed","email":"72bb75a959e1785b79ffe7230eaeec25880707a91b4a4f98330fc1510bd40e03","phone":"********","plan":"free","joined_at":"2024-02-20"}
{"id":3,"name":"Grace Hopper","email":"b533d4547eaa5a0fa955965a1ca393ccd2ea013032a105726f232eb41bddc4fa","phone":"********","plan":"pro","joined_at":"2024-03-05"}
```

`examples/customer-export.sift` is the full, runnable version.

## When a row fails: error policies

A row can fail two ways: a `check` condition is false, or a source cell
won't coerce to its declared type (a non-numeric `age`, say). Either way
it becomes a **marked row**, not a thrown exception — and one `on error`
declaration controls what the driver does with it. There are three
policies.

**`abort`** (the default, no declaration needed) stops the run at the
first failure:

```sift
source in  = csv("on-error-abort.csv", schema: { name: string, age: int, email: string })
sink   out = jsonl("on-error-abort_out.jsonl")

pipeline main {
  in |> filter(.age >= 18) |> check(.email != "", "missing email") |> out
}
```

Against a CSV where Grace's email is blank:

```console
$ ./sift run on-error-abort.sift
on-error-abort.sift: error: row 2 from "in": missing email
$ cat on-error-abort_out.jsonl
{"name":"Ada","age":42,"email":"ada@example.com"}
```

Tom was already filtered out (age 15); Grace's check fails and the whole
run stops right there — Liam, who comes after her in the file, is never
even read.

**`on error skip`** drops just the failing row and keeps going:

```sift
on error skip

source in  = csv("on-error-skip.csv", schema: { name: string, age: int, email: string })
sink   out = jsonl("on-error-skip_out.jsonl")

pipeline main {
  in |> filter(.age >= 18) |> check(.email != "", "missing email") |> out
}
```

```console
$ ./sift run on-error-skip.sift
$ cat on-error-skip_out.jsonl
{"name":"Ada","age":42,"email":"ada@example.com"}
{"name":"Liam","age":25,"email":"liam@example.com"}
```

**`on error |> errors`** routes failures to a second sink instead of
dropping or aborting:

```sift
on error |> errors

source in     = csv("on-error-route.csv", schema: { name: string, age: int, email: string })
sink   out    = jsonl("on-error-route_out.jsonl")
sink   errors = jsonl("on-error-route_errors.jsonl")

pipeline main {
  in |> filter(.age >= 18) |> check(.email != "", "missing email") |> out
}
```

```console
$ ./sift run on-error-route.sift
$ cat on-error-route_errors.jsonl
{"source":"in","ordinal":2,"offset":4,"reason":"missing email","stage":"check"}
```

The error sink never receives a failed row's own fields — only a fixed
envelope of where and why it failed (source, position, reason, stage). A
`@pii` field can't leak through a routed failure the way it could if raw
rows were forwarded as-is.

A source cell that won't coerce to its declared type fails exactly the
same way, governed by the same policy — see `examples/bad-cell.sift`,
where a non-numeric `age` cell is skipped like any other failed row.
`examples/on-error-abort.sift`, `on-error-skip.sift`, and
`on-error-route.sift` are the complete programs above.

## Writing to more than one sink

A pipeline's terminal production can name more than one sink,
comma-separated — every row reaches all of them, in declared order:

```sift
source in        = csv("broadcast.csv", schema: { name: string, email: string @pii })
sink   warehouse = jsonl("broadcast_out.jsonl")
sink   audit     = jsonl("broadcast_audit.jsonl")

pipeline main {
  in |> mask(email) |> warehouse, audit
}
```

```console
$ ./sift run broadcast.sift
$ diff broadcast_out.jsonl broadcast_audit.jsonl && echo identical
identical
```

This is terminal *broadcast*, not branching fan-out: the stream is pulled
once, and the driver's write step fans that single row out to every
listed sink — no buffering, no per-branch transform, no DAG. It composes
with error routing exactly as you'd expect (`on error |> errsink` still
sends a failed row to `errsink` alone, while every healthy row reaches
all of `warehouse` and `audit`), and the unmasked-`@pii` check runs once
against the shared schema, naming every sink it applies to rather than
repeating the error per sink. The same sink listed twice (`|> out, out`)
is a compile error — it's always a literal double-write. `examples/broadcast.sift`
demonstrates the same broadcast pattern.

## Reusable pipelines: named segments

A `pipeline` declaration with no source or sink of its own is a reusable
`stream<T> -> stream<U>` value — a **named segment** you drop into
another pipeline by name, exactly like a built-in stage:

```sift
source in  = csv("named-segment.csv", schema: { name: string, email: string })
sink   out = jsonl("named-segment_out.jsonl")

pipeline clean =
     check(.email != "", "missing email")
  |> map({ ...row, email: lower(trim(.email)) })

pipeline main {
  in |> clean |> out
}
```

```console
$ ./sift run named-segment.sift
{"name":"Ada","email":"ada@example.com"}
{"name":"Tom","email":"tom@example.com"}
```

`clean`'s two stages are inlined wherever it's referenced — there's no
runtime indirection — and a segment that references itself, directly or
through a cycle of segments, is a compile error, not infinite recursion.
`examples/named-segment.sift` is the runnable version.

## Parameterized segments

A named segment can take parameters, turning it into Sift's own extension
mechanism for the stage vocabulary — no Go plugin required. There are
exactly two parameter kinds:

- A **column parameter** is a bare name in the parameter list — referenced
  `.col` inside an expression (`filter`/`map`/`check`), or as a bare
  column name in a column-list stage like `mask(col)`/`select(col)` — and
  bound to a bare column name at the call site: `pipeline scrub(col) =
  mask(col)`, called `scrub(email)`.
- A **scalar parameter** is `name: type`, referenced as a bare value
  inside the body, and bound to a literal at the call site:
  `pipeline adults(min: int) = filter(.age >= min)`, called `adults(18)`.

```sift
source in  = csv("segments.csv", schema: { name: string, age: int, email: string @pii, backup_email: string @pii })
sink   out = jsonl("segments_out.jsonl")

pipeline scrub(col)       = mask(col)
pipeline adults(min: int) = filter(.age >= min)

pipeline main {
  in |> scrub(email) |> scrub(backup_email) |> adults(18) |> out
}
```

```console
$ ./sift run segments.sift
{"name":"Ada","age":42,"email":"********","backup_email":"*********"}
```

`scrub(email)` and `scrub(backup_email)` are two independent
instantiations of one definition, each checked against the real schema at
its own call site: the checker copies `scrub`'s body, substitutes the
bound column or literal, and type-checks the copy with the ordinary stage
rules — PII propagation and field-existence checks fall out for free,
with no new rules of their own. A mistake inside a substituted body — a
misspelled column, an argument of the wrong type — is reported with
**dual-site** context: which segment, which argument it was called with,
and where the call itself is, on top of the position inside the
definition where the mistake actually is:

```console
$ ./sift run segments-typo.sift
segments-typo.sift:4:28: error: column "emial" not in schema { name: string, age: int }
  in segment scrub(col = emial)
  instantiated at main:7
```

Parameters are positional only — no defaults, no variadics, no named
arguments — and a segment never takes another segment as a parameter
(that would pull toward a functional core, deliberately out of scope; see
`design/segments.md` §8). `examples/segments.sift` demonstrates the same
reuse.

## A complete ETL, with error routing

Put together, that's a realistic pipeline: a reusable segment for shared
validation, a plain stage and a parameterized scalar segment for
filtering, `map` to normalize and derive a field, `drop`/`rename` to
reshape, `hash` and a parameterized column segment to declassify two
different `@pii` columns, `select` to fix the final shape, `offset`/`limit`
to slice, and a two-sink broadcast at the end:

```sift
source in = csv("etl.csv", schema: {
  id: int,
  full_name: string,
  email: string @pii,
  phone: string @pii,
  ssn: string @pii,
  age: int,
  amount: double,
  plan: string,
  signup_date: string,
  internal_notes: string
})
sink out   = jsonl("etl_out.jsonl")
sink audit = jsonl("etl_audit.jsonl")

pipeline validate =
     check(.email != "", "missing email")
  |> check(.amount > 0.0, "non-positive amount")

pipeline eligible(min: double) = filter(.amount >= min)
pipeline scrub(col)            = mask(col)

pipeline main {
  in
    |> validate
    |> filter(.age >= 18)
    |> eligible(100.0)
    |> map({ ...row, plan: upper(.plan), high_value: .amount >= 500.0 })
    |> drop(ssn, internal_notes)
    |> rename(full_name: name, signup_date: joined_at)
    |> hash(email)
    |> scrub(phone)
    |> select(id, name, email, phone, age, amount, plan, high_value, joined_at)
    |> offset(1)
    |> limit(2)
    |> out, audit
}
```

```console
$ ./sift run etl.sift
$ cat etl_out.jsonl
{"id":4,"name":"Liam Wu","email":"d9c57089f04f2b2e9cd8abcc2e1088afc708fcf60b842d42ab3dc3d3c153e13e","phone":"********","age":25,"amount":500,"plan":"FREE","high_value":true,"joined_at":"2024-04-10"}
{"id":5,"name":"Nina Simone","email":"cec43edb6a1681336ab87fa21ea576e83450826e3cc05f2ca73128e7fd69745f","phone":"********","age":29,"amount":120,"plan":"PRO","high_value":false,"joined_at":"2024-05-12"}
```

Of six source rows, `filter(.age >= 18)` removes Tom (15) and
`eligible(100.0)` removes Grace ($50 < the $100 minimum) — genuine drops,
not failures. `offset(1)` then discards Ada positionally, and `limit(2)`
takes exactly Liam and Nina, stopping before Owen is ever read — the same
`offset`/`limit` mechanics from
[Slicing rows](#slicing-rows-limit-and-offset), now composing with
everything else. `examples/etl.sift` is the full, runnable version.

### With errors

Swap in `on error |> errors` and a couple of bad rows — one with a blank
email (fails `validate`'s `check`), one with an `amount` cell that won't
even parse as a `double` (fails at the source, before `validate` runs at
all) — and both divert to the error sink as a fixed envelope, while every
healthy row still flows through the exact same pipeline to `out` and
`audit`:

```console
$ ./sift run etl-errors.sift
$ cat etl-errors_out.jsonl
{"id":1,"name":"Ada Lovelace","email":"b5fc85e55755f9e0d030a10ab4429b6b2944855f9a0d60077fe832becbc41d72","phone":"********","age":42,"amount":250.5,"plan":"PRO","high_value":false,"joined_at":"2024-01-15"}
{"id":6,"name":"Liam Wu","email":"d9c57089f04f2b2e9cd8abcc2e1088afc708fcf60b842d42ab3dc3d3c153e13e","phone":"********","age":25,"amount":500,"plan":"FREE","high_value":true,"joined_at":"2024-04-10"}
{"id":7,"name":"Nina Simone","email":"cec43edb6a1681336ab87fa21ea576e83450826e3cc05f2ca73128e7fd69745f","phone":"********","age":29,"amount":120,"plan":"PRO","high_value":false,"joined_at":"2024-05-12"}
{"id":8,"name":"Owen King","email":"b82aec285b6e6a36fd26fc7404b07b48af53f7b931a492c4b177d58351adba1f","phone":"********","age":31,"amount":999.99,"plan":"ENTERPRISE","high_value":true,"joined_at":"2024-06-18"}
$ cat etl-errors_errors.jsonl
{"source":"in","ordinal":1,"offset":3,"reason":"missing email","stage":"check"}
{"source":"in","ordinal":4,"offset":6,"reason":"cannot parse \"N/A\" as double","stage":"csv:amount"}
```

This variant drops `offset`/`limit`: since they [count every row
positionally, healthy or failed](#slicing-rows-limit-and-offset), the two
diverted rows would otherwise occupy `limit`'s quota ahead of the healthy
rows behind them in the file, leaving `out`/`audit` empty — a real
interaction worth knowing about, not a bug. `examples/etl-errors.sift` is
the full, runnable version.

## Quick reference

- **Sources and sinks** declare a format (`csv`, `jsonl`) and a path. A
  source also declares its schema, since CSV carries no types of its own.
- **Pipelines** are a linear `in |> stage |> ... |> out` chain; the
  terminal production can be a comma-separated sink list to broadcast. A
  pipeline with no source/sink is a reusable named segment, optionally
  parameterized over columns and/or scalars.
- **Stages:** `filter(<bool>)`, `map({ ...row, field: expr })`,
  `check(<bool>, "reason")`, `select(col, ...)`, `drop(col, ...)`,
  `rename(old: new, ...)`, `limit(n)`, `offset(n)`,
  `mask(col, ...)`/`hash(col, ...)`/`redact(col, ...)`.
- **Expressions:** field access (`.field`), int/double/string/bool
  literals, `+ - * /`, `< > <= >= == !=`, `&& ||`, function calls
  (`upper`, `lower`, `trim`, `mask`, `hash`, `redact`), and record
  literals with spread (`{ ...row, ... }`).
- **PII:** `@pii` attaches at the source, propagates through every
  expression, and is only cleared by `mask`/`hash`/`redact`. A sink
  rejects any field still tagged `@pii`.
- **Error policy:** `on error abort | skip | |> <sink>`. `abort` (default)
  stops the run; `skip` drops the failing row; routing sends a fixed
  envelope — never the row's own fields — to a second sink.

For the complete grammar and semantics, see
[`design/language.md`](design/language.md); each later feature has its
own design doc under [`design/`](design/) — see [Status](#status) for
which are shipped.

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
examples/             one .sift + fixture pair per language feature or error policy
design/               language spec + one design doc per build phase
```

```console
$ make build          # go build ./...
$ make test           # go test ./...
$ make check          # gofmt -l, go vet, go test
$ make run            # runs examples/adults.sift, writes adults.jsonl
$ make emit-ast       # dumps the parsed AST for examples/adults.sift
$ make emit-schema    # dumps the checked source/sink schemas
```

## Status

v0 is complete: both acceptance cases in `design/language.md` §7 pass end
to end through the CLI, and every module has unit tests. v0's scope is
deliberately closed — see `design/language.md` §5 for what's built and
what's explicitly deferred (joins, dedupe, fan-out, an optimizer, schema
inference, and more formats beyond csv/jsonl).

Four phases have shipped on top of it, each with its own acceptance tests
and a runnable `examples/` fixture:

- `design/errors.md` — failures are data, not exceptions; `on error
  abort/skip/route` is a real language feature (§7, ERR-A through ERR-E).
- `design/improvements.md` — `select`/`drop`/`rename`/`limit`/`offset` and
  `mask`/`hash`/`redact` as first-class stages (§9, S1-A through S4-B).
- `design/multisink.md` — a pipeline's terminal production can broadcast
  to more than one sink (§9, MS-A through MS-E).
- `design/segments.md` — named segments take column and/or scalar
  parameters and monomorphize at each call site, with dual-site
  diagnostics (§9, PS-A through PS-H).

Two more are designed but not built: `design/routing.md` (per-row
conditional dispatch, depends on the multi-sink driver spine), and
`design/xlsx.md`/`design/parquet.md` (connectors — xlsx depends on this
phase's `value.Coerce`, parquet depends on nothing beyond the registry).
