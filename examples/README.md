# Sift tutorial

This is the full tour of the language. It starts from the smallest
possible pipeline and adds one idea at a time. Every section has a
working example in this folder you can run yourself.

For the full grammar and the exact rules, see
[`../design/language.md`](../design/language.md). For how the codebase
is put together, see [`../CLAUDE.md`](../CLAUDE.md).

## Contents

1. [Building the CLI](#building-the-cli)
2. [Your first pipeline](#your-first-pipeline)
3. [Changing rows with map](#changing-rows-with-map)
4. [Keeping or failing rows: filter and check](#keeping-or-failing-rows-filter-and-check)
5. [The PII tag](#the-pii-tag)
6. [Reshaping schemas: select, drop, rename](#reshaping-schemas-select-drop-rename)
7. [Cutting rows: limit and offset](#cutting-rows-limit-and-offset)
8. [Masking as a stage](#masking-as-a-stage)
9. [Putting it together](#putting-it-together)
10. [When a row fails: error policies](#when-a-row-fails-error-policies)
11. [Writing to more than one sink](#writing-to-more-than-one-sink)
12. [Routing rows by condition](#routing-rows-by-condition)
13. [Reusable pipelines: named segments](#reusable-pipelines-named-segments)
14. [Segments with parameters](#segments-with-parameters)
15. [A full ETL, with error routing](#a-full-etl-with-error-routing)
16. [Reading xlsx files](#reading-xlsx-files)
17. [Fields that may be missing](#fields-that-may-be-missing)
18. [Naming a column that isn't a valid identifier](#naming-a-column-that-isnt-a-valid-identifier)
19. [Quick reference](#quick-reference)
20. [Project layout](#project-layout)
21. [Status](#status)

## Building the CLI

```console
$ go build -o sift ./cmd/sift
$ ./sift
usage: sift run <file> | sift --emit-ast <file> | sift --emit-schema <file>
```

Everything below assumes a `sift` binary built this way. `make build`
does the same thing (see [Project layout](#project-layout) for the rest
of the Makefile).

## Your first pipeline

Every Sift program is three things: a **source**, a **sink**, and a
**pipeline** that connects them with `|>`. Here is the smallest one that
does something: it keeps only the rows where age is 18 or over.

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

- `source in = csv(...)` names a source `in`, read as `csv`, with a
  **schema**. CSV has no types of its own, so each field's name and type
  (`string`, `int`, `double`, or `bool`) is written out here.
- `sink out = jsonl(...)` names a sink `out`, writing one JSON object per
  line.
- `pipeline main { ... }` is where the program runs from: one `|>` chain,
  starting at a source and ending at a sink.
- `filter(.age >= 18)` is a **stage**. `.age` reads a field off the
  current row. The row only carries on if the condition is true.

Run it:

```console
$ ./sift run adults.sift
$ cat adults.jsonl
{"name":"Ada","age":42}
```

Tom is dropped quietly. Only Ada's row reaches the sink.

> **Paths:** `"people.csv"` and `"adults.jsonl"` are read relative to
> `adults.sift`'s own folder, not the folder you happen to be standing
> in. That means it runs the same whether you're right next to the file
> or calling `sift run path/to/adults.sift` from somewhere else.

Two flags are useful while you're learning the language: `--emit-ast`
shows what the parser built, and `--emit-schema` shows what the checker
worked out for the source and sink.

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

`adults.sift` in this folder is this exact program.

## Changing rows with map

`filter` only ever keeps or drops a row as it is. To change a row, use
`map`, which rebuilds it from a **record literal**:

```sift
source in  = csv("map.csv", schema: { name: string, age: int, balance: double })
sink   out = jsonl("map_out.jsonl")

pipeline main {
  in |> map({ ...row, name: upper(.name), balance_ok: .balance >= 100.0, age_next_year: .age + 1 }) |> out
}
```

Against `map.csv` (`Ada,42,250.50` and `Tom,15,50.00`):

```console
$ ./sift run map.sift
{"name":"ADA","age":42,"balance":250.5,"balance_ok":true,"age_next_year":43}
{"name":"TOM","age":15,"balance":50,"balance_ok":false,"age_next_year":16}
```

`{ ...row, ... }` copies every field on the current row first. Each named
field after that either replaces one (`name`) or adds a new one
(`balance_ok`, `age_next_year`). The output schema follows the same
rule: the original fields keep their order, and new fields are added at
the end.

This is also your first real look at Sift's expressions:

| Kind | Examples |
|---|---|
| Field access | `.age`, `.name` |
| Literals | `42`, `3.14`, `"hi"`, `true` |
| Arithmetic | `+ - * /` |
| Comparison | `< > <= >= == !=` |
| Boolean | `&& \|\|` |
| Function call | `upper(.name)`, `mask(.email)` |

There's no chaining a field access (`.a.b`) and no way to write your own
scalar functions. You get field access, the operators above, and a fixed
set of built ins (`upper`, `lower`, `trim`, and the three masking
functions you'll meet next).

`map.sift` in this folder is this exact program.

## Keeping or failing rows: filter and check

`filter` and `check` both take a boolean condition, but they mean
different things when it's false:

- `filter(cond)` **quietly drops** the row. It's gone, as if it never
  came through.
- `check(cond, "reason")` **marks the row as failed**, with a reason
  attached. It doesn't disappear. It becomes a problem for the pipeline's
  error policy to deal with (see
  [When a row fails](#when-a-row-fails-error-policies)).

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

Every row in `check.csv` has an email, so `check` never fires here.
`check.sift` in this folder shows the case where every row passes. A
case where a row fails is coming up once error policies are on the
table.

## The PII tag

Tag a field `@pii` in a source's schema and the checker follows it
through every expression that touches it: arithmetic, function calls,
`map`, all of it. A sink that would receive a field still tagged `@pii`
is a **compile error**, not a surprise you find at run time:

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
plain `string`: `mask`, `hash`, `redact`. Use one as a stage and the same
program compiles and runs:

```sift
pipeline main {
  in |> hash(email) |> out
}
```

```console
$ ./sift run pii.sift
{"name":"Ada","email":"b5fc85e55755f9e0d030a10ab4429b6b2944855f9a0d60077fe832becbc41d72"}
```

Every other function or operator that touches a `@pii` value **carries
the tag over** to its result. `upper(.email)` is still `@pii`. There's
no way to accidentally launder personal data by transforming it: only
`mask`/`hash`/`redact` clear the tag, because the checker only trusts
those three. `pii.sift` in this folder shows the same rule.

## Reshaping schemas: select, drop, rename

Three stages reshape a row's schema without changing how PII is tracked.

**`select(col, ...)`** keeps only the named columns, in the order you
name them:

```sift
in |> select(email, name, plan) |> out
```

**`drop(col, ...)`** removes columns completely. Dropping a `@pii`
column is a fair way to satisfy the sink rule with no `mask` call at
all, since the field is simply gone before the check ever runs:

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

**`rename(old: new, ...)`** gives a column a new name while keeping its
exact type and position, **including a `@pii` tag**. Renaming a field is
not a way to launder it either:

```sift
source in  = csv("rename.csv", schema: { name: string, dob: string, email_addr: string @pii })
sink   out = jsonl("rename_out.jsonl")

pipeline main {
  in |> rename(dob: birth_date, email_addr: email) |> mask(email) |> out
}
```

`mask(email)` is still needed after the rename: `email_addr`'s `@pii`
tag came along onto `email` unchanged. See `select.sift`, `drop.sift`,
and `rename.sift` in this folder.

## Cutting rows: limit and offset

`limit(n)` lets through at most the first `n` rows, then stops.
`offset(n)` throws away the first `n` rows and lets the rest through.
Both count *every* row that reaches them by position, whether it's
healthy or failed:

```sift
source in  = csv("limit-offset.csv", schema: { name: string, event: string })
sink   out = jsonl("limit-offset_out.jsonl")

pipeline main {
  in |> offset(2) |> limit(2) |> out
}
```

Against a six row log, `offset(2)` throws away the first two rows and
`limit(2)` takes exactly the next two, then stops. The rows after that
are never even read:

```console
$ ./sift run limit-offset.sift
{"name":"Grace","event":"purchase"}
{"name":"Liam","event":"logout"}
```

`limit-offset.sift` in this folder is the runnable version.

## Masking as a stage

`mask`/`hash`/`redact` also work as **stages**, not just as functions.
`|> hash(email)` clears the whole column in one step. Compare that to
`map({ ...row, email: hash(.email) })`, where one missing `...row` would
quietly drop every other column:

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

`hash` is SHA-256, written as hex. It's stable and one way, which makes
it a good join key that doesn't reveal the original value. `redact`
throws the value away entirely and puts a fixed placeholder in its
place. `mask` (seen earlier) swaps every character for `*`, so you can
still see the length but nothing else. All three clear `@pii`; nothing
else does. As a stage, each takes a list of column names the same way
`select`/`drop` do, and each one only works on a column that's already
`string @pii`. Using one anywhere else is a compile error.

## Putting it together

A real pipeline chains a few of these together. This one turns a full
CRM export into a safe extract for analytics:

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

Reading top to bottom: `ssn` and `internal_notes` never leave at all,
not even masked. The two old column names get the warehouse's standard
names. `email` becomes a stable hash and `phone` a masked placeholder.
`select` fixes the exact shape and order of the output, and `limit(3)`
caps this run to a small preview. Every `@pii` column is gone or masked
by the time `select` runs, so the sink takes the result with no more
work to do:

```console
$ ./sift run customer-export.sift
{"id":1,"name":"Ada Lovelace","email":"b5fc85e55755f9e0d030a10ab4429b6b2944855f9a0d60077fe832becbc41d72","phone":"********","plan":"pro","joined_at":"2024-01-15"}
{"id":2,"name":"Tom Reed","email":"72bb75a959e1785b79ffe7230eaeec25880707a91b4a4f98330fc1510bd40e03","phone":"********","plan":"free","joined_at":"2024-02-20"}
{"id":3,"name":"Grace Hopper","email":"b533d4547eaa5a0fa955965a1ca393ccd2ea013032a105726f232eb41bddc4fa","phone":"********","plan":"pro","joined_at":"2024-03-05"}
```

`customer-export.sift` in this folder is the full, runnable version.

## When a row fails: error policies

A row can fail in two ways: a `check` condition is false, or a source
cell won't convert to its declared type (a non numeric `age`, say).
Either way it becomes a **marked row**, not a thrown exception, and one
`on error` line controls what happens to it. There are three choices.

**`abort`** (the default, no line needed) stops the run at the first
failure:

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

Tom was already filtered out (age 15). Grace's check fails and the
whole run stops right there. Liam, who comes after her in the file, is
never even read.

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

**`on error |> errors`** sends failures to a second sink instead of
dropping or stopping the run:

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

The error sink never gets a failed row's own fields, only a fixed
record of where and why it failed (source, position, reason, stage). A
`@pii` field can't leak out through a routed failure the way it could if
raw rows were sent through as they are.

A source cell that won't convert to its declared type fails the same
way, under the same policy. See `bad-cell.sift` in this folder, where a
non numeric `age` cell is skipped like any other failed row.
`on-error-abort.sift`, `on-error-skip.sift`, and `on-error-route.sift`
are the full programs above.

## Writing to more than one sink

A pipeline's last step can name more than one sink, separated by
commas. Every row reaches all of them, in the order they're listed:

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

This sends each row to every sink; it does not split rows between them.
The stream is read once, and the driver's write step sends that one row
to every sink in the list. No buffering, no branching. It works
alongside error routing just as you'd expect: `on error |> errsink`
still sends a failed row to `errsink` alone, while every healthy row
still reaches both `warehouse` and `audit`. The unmasked `@pii` check
runs once against the shared schema and names every sink it applies to,
instead of repeating the error for each one. Listing the same sink twice
(`|> out, out`) is a compile error, since it would always write the row
twice. `broadcast.sift` in this folder shows the same pattern.

## Routing rows by condition

Broadcast's opposite: instead of every row reaching every sink, `route`
sends each row to exactly **one** sink, chosen by the first branch whose
condition is true, checked top to bottom:

```sift
source in       = csv("routing.csv", schema: { name: string, email: string @pii, region: string })
sink   eu_sink  = jsonl("eu.jsonl")
sink   us_sink  = jsonl("us.jsonl")
sink   rest_sink = jsonl("rest.jsonl")

pipeline main {
  in
    |> mask(email)
    |> route {
         .region == "EU" => eu_sink,
         .region == "US" => us_sink,
         else            => rest_sink,
       }
}
```

`else` is not optional. A `route` with no `else` branch is a compile
error, because Sift won't let rows disappear without you saying so:

```console
$ ./sift run leaky-route.sift
leaky-route.sift:4:1: error: route is not total; add an 'else' branch (use 'else => discard' to drop unmatched rows explicitly)
```

If you really do want to drop the rows that don't match anything, say so
directly with `else => discard`. `discard` (not `drop`, which is the
column stage) is the word that means "no sink, and that's fine." A
branch always points at a sink name or `discard`, never at a
transformation. Do any masking or reshaping *before* `route`, since
branches can't run stages of their own.

Every branch shares one schema, since routing changes nothing about the
row. So the unmasked `@pii` check (and the missing field check) runs
only once, the same way it does for broadcast, and covers every branch's
sink with one error if it fires. The same sink can appear in more than
one branch (unlike broadcast, where that's a compile error), since
different rows can't collide there. Routing works with error routing
just like broadcast does: a failed row is handled by `on error` before
any branch ever sees it, since its fields can't be trusted.
`routing.sift` in this folder shows the same pattern.

## Reusable pipelines: named segments

A `pipeline` with no source or sink of its own is a reusable step you
can drop into another pipeline by name, just like a built in stage:

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

`clean`'s two steps are copied in wherever it's used. There's no extra
work at run time. A segment that refers to itself, directly or through
a loop of segments, is a compile error, not an endless loop.
`named-segment.sift` in this folder is the runnable version.

## Segments with parameters

A named segment can take parameters, which turns it into Sift's own way
of adding new stages, with no Go code required. There are exactly two
kinds of parameter:

- A **column parameter** is a bare name in the parameter list. Inside
  the segment it's used as `.col` in an expression (`filter`/`map`/
  `check`), or as a bare column name in a stage like
  `mask(col)`/`select(col)`. At the call site it's given a bare column
  name: `pipeline scrub(col) = mask(col)`, called as `scrub(email)`.
- A **scalar parameter** is written `name: type`, used as a plain value
  inside the segment, and given a literal value at the call site:
  `pipeline adults(min: int) = filter(.age >= min)`, called as
  `adults(18)`.

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

`scrub(email)` and `scrub(backup_email)` are two separate uses of one
definition, each checked against the real schema at its own call site.
The checker copies `scrub`'s body, fills in the column or value it was
given, and checks the copy with the normal stage rules. PII tracking and
field checks just fall out of that, with no extra rules needed. A
mistake inside a used segment, a misspelled column, an argument of the
wrong type, is reported with context from **both sides**: which segment,
what it was called with, and where the call is, together with the exact
spot inside the segment's own definition where the mistake shows up:

```console
$ ./sift run segments-typo.sift
segments-typo.sift:4:28: error: column "emial" not in schema { name: string, age: int }
  in segment scrub(col = emial)
  instantiated at main:7
```

Parameters are given by position only. No defaults, no variable length
lists, no named arguments. And a segment can never take another segment
as a parameter (see `../design/segments.md` §8 for why). `segments.sift`
in this folder shows the same reuse.

## A full ETL, with error routing

Put together, that's a real pipeline: a reusable segment for shared
checks, a plain stage and a segment with a parameter for filtering,
`map` to clean up and add a field, `drop`/`rename` to reshape, `hash`
and a segment with a column parameter to mask two different `@pii`
columns, `select` to fix the final shape, `offset`/`limit` to cut it
down, and a two sink broadcast at the end:

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

Of six rows in the source, `filter(.age >= 18)` removes Tom (15) and
`eligible(100.0)` removes Grace (her $50 amount is under the $100
minimum). Both are plain drops, not failures. `offset(1)` then throws
away Ada by position, and `limit(2)` takes exactly Liam and Nina,
stopping before Owen is ever read. Same `offset`/`limit` behavior as
[Cutting rows](#cutting-rows-limit-and-offset), now working alongside
everything else. `etl.sift` in this folder is the full, runnable
version.

### With errors

Swap in `on error |> errors` and add a couple of bad rows: one with a
blank email (fails `validate`'s `check`), and one with an `amount` cell
that won't even parse as a `double` (fails at the source, before
`validate` ever runs). Both go to the error sink as a fixed record,
while every healthy row still flows through the same pipeline to `out`
and `audit`:

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

This version drops `offset`/`limit`. Since they [count every row by
position, healthy or failed](#cutting-rows-limit-and-offset), the two
diverted rows would otherwise use up `limit`'s quota ahead of the
healthy rows that come after them in the file, leaving `out`/`audit`
empty. Worth knowing about, not a bug. `etl-errors.sift` in this folder
is the full, runnable version.

## Reading xlsx files

Every source so far has been `csv`. Swap the format name for `xlsx` and
nothing else about the pipeline changes. That's what the format
registry ([Project layout](#project-layout)) buys you: a format is one
Go struct plus one line in the registry, never a special case in the
lexer, parser, checker, or executor.

`people.xlsx`, sheet "People":

| | A | B |
|---|---|---|
| **1** | People Export | |
| **2** | name | age |
| **3** | Ada | 42 |
| **4** | Tom | 15 |

`xlsx.sift`:

```sift
source in = xlsx("people.xlsx", sheet: "People", header_row: 2, schema: { name: string, age: int })
sink out = jsonl("xlsx_out.jsonl")

pipeline main {
  in |> filter(.age >= 18) |> out
}
```

```console
$ ./sift run xlsx.sift
$ cat xlsx_out.jsonl
{"name":"Ada","age":42}
```

Two keyword arguments beyond `schema:` that `csv` never needed:

- `sheet:` picks the worksheet by name. Leave it out and Sift reads the
  workbook's first sheet.
- `header_row:` counts from 1, matching the row number you see in
  Excel, and defaults to `1`. Real workbooks often put a title or an
  export date above the real header. Row 1 here is a title, so the
  header itself is row 2.

A few things worth knowing about how the header lines up with your
schema: column **order in the sheet doesn't matter**. Each field is
matched to a header cell by name, the same as `csv`, and **extra
columns are simply ignored**. A field missing from the header, or a
header name used twice, fails as soon as the source is built, before any
row is read. That's the same fail-fast behavior as a missing `csv`
column, just with a message that names the sheet and the header row it
read:

```console
$ ./sift run xlsx-typo.sift
xlsx-typo.sift: error: source "in": column "yeras" not found in header row 2 of sheet "People"
       header columns: name, age
```

A blank row (every mapped column empty) is skipped. It's not treated as
data, and it doesn't end the sheet, since real exports often have
trailing blank rows and stopping at the first one would quietly cut off
everything after it. A cell that won't convert to its declared type is a
row failure just like a bad `csv` cell, under the same
[error policy](#when-a-row-fails-error-policies). The only difference is
the stage name in the record reads `xlsx:age` instead of `csv:age`. And a
failed row's `offset` is the real row number in the spreadsheet, not a
count of data rows. With a title row above the header the two are
different, and the offset is the one you can act on: open the workbook
and go straight to that row.

`xlsx.sift` in this folder is this exact program. `people.xlsx` is the
workbook it reads.

## Fields that may be missing

Add `?` after a schema field's type and a missing or blank cell no
longer fails the row. It reads as a well typed **absent** value instead,
the same way `@pii` adds a tag rather than needing its own type:

```sift
source in = csv("people.csv", schema: { name: string, phone: string? })
sink   out = jsonl("out.jsonl")

pipeline main {
  in |> out
}
```

```console
$ ./sift run leaky-optional.sift
leaky-optional.sift:4:1: error: field "phone" is optional and reaches sink "out" undischarged; resolve with ??
```

A field marked this way has to be resolved before it can reach a sink or
be used as a true/false check. The checker tracks it through every
expression the same way it tracks `@pii`, and `??` is the one operator
that clears it, by giving a default value for when the field is absent:

```sift
pipeline main {
  in |> map({ ...row, phone: .phone ?? "n/a" }) |> out
}
```

```console
$ ./sift run optional.sift
{"name":"Ada","phone":"555-0100"}
{"name":"Tom","phone":"n/a"}
```

The two tags don't interfere with each other: a field can be both
`string?` and `@pii`, and both have to be cleared, in either order,
before the row can reach a sink:

```sift
source in = csv("people.csv", schema: { name: string, email: string? @pii })
```

```sift
pipeline main {
  in |> map({ ...row, email: mask(.email ?? "n/a") }) |> out
}
```

`optional.sift` and `pii-optional.sift` in this folder show both cases.

## Naming a column that isn't a valid identifier

A schema field name has to be a plain identifier: letters, digits,
underscore, no spaces. Real files don't always cooperate. A bank export
might have a header like `Transaction ID`, and there's no identifier
that equals that text, so it can never be named in a schema on its own.

Add a `columns` kwarg to bridge the two: it maps a clean identifier to
the raw header text to look for instead.

```sift
source in = csv("columns.csv",
  schema:  { txn_id: int, txn_date: string, amount: double },
  columns: { txn_id: "Transaction ID", txn_date: "Date", amount: "Amount" }
)
sink out = jsonl("columns_out.jsonl")

pipeline main {
  in |> out
}
```

```console
$ ./sift run columns.sift
$ cat columns_out.jsonl
{"txn_id":1001,"txn_date":"2026-01-05","amount":42.5}
{"txn_id":1002,"txn_date":"2026-01-06","amount":17.25}
```

A field with no `columns` entry still falls back to matching its own
name against the header, exactly like before this existed, and that
match is exact and case-sensitive: `amount` won't match a header spelled
`Amount` on its own, which is why it's aliased here too, even though it
has no space. Only fields you actually need to rename require an entry;
`columns` is optional per field, not all-or-nothing.

A required field that resolves neither by alias nor by name is a
construction error, the same shape as a plain missing column, naming the
field and the real header found in the file:

```console
$ ./sift run leaky-columns.sift
leaky-columns.sift: error: csv source "in": required column "txn_date" not found in header Transaction ID, Amount
```

An **optional** field (`string?`) with an alias whose header doesn't
exist in the file at all is not an error. It resolves to absent, the
same as a plain optional field with no matching column at all, and `??`
discharges it downstream as usual.

This works the same way for `xlsx` sources, alongside `header_row` and
`sheet`: `columns` doesn't know or care which format resolved it.

`columns.sift` in this folder is this exact program.

## Quick reference

- **Sources and sinks** name a format (`csv`, `jsonl`, `xlsx` for
  sources) and a path. A source also declares its schema, since neither
  CSV nor xlsx carries reliable types of its own. `xlsx` also takes
  `sheet:` and `header_row:`. Either format also takes `columns:`, to
  name a column whose real header isn't a valid identifier.
- **Pipelines** are a plain `in |> stage |> ... |> out` chain. The last
  step can be a comma separated list of sinks to broadcast to, or
  `route { <bool> => sink, ..., else => sink|discard }` to send each row
  to exactly one sink (`else` is required). A pipeline with no source or
  sink is a reusable named segment, which can take column and/or scalar
  parameters.
- **Stages:** `filter(<bool>)`, `map({ ...row, field: expr })`,
  `check(<bool>, "reason")`, `select(col, ...)`, `drop(col, ...)`,
  `rename(old: new, ...)`, `limit(n)`, `offset(n)`,
  `mask(col, ...)`/`hash(col, ...)`/`redact(col, ...)`.
- **Expressions:** field access (`.field`), int/double/string/bool
  literals, `+ - * /`, `< > <= >= == !=`, `&& ||`, function calls
  (`upper`, `lower`, `trim`, `mask`, `hash`, `redact`), and record
  literals with a spread (`{ ...row, ... }`).
- **PII:** `@pii` attaches at the source, is tracked through every
  expression, and is only cleared by `mask`/`hash`/`redact`. A sink
  rejects any field still tagged `@pii`.
- **Optional fields:** `T?` in a source schema attaches at the source. A
  missing or blank cell reads as absent, that state is tracked through
  every expression, and only `??` clears it, by giving a default. A
  sink rejects any field still marked this way, and so does a true/false
  check.
- **Error policy:** `on error abort | skip | |> <sink>`. `abort` (the
  default) stops the run. `skip` drops the failing row. Routing sends a
  fixed record, never the row's own fields, to a second sink.

For the full grammar and rules, see
[`../design/language.md`](../design/language.md). Each later feature has
its own design doc under [`../design/`](../design/); see
[Status](#status) for what's built.

## Project layout

```
cmd/sift/            CLI: run, --emit-ast, --emit-schema
internal/value/       Row, Provenance, Type (+ @pii, Optional), Schema, Coerce, Absent
internal/lexer/       source text -> tokens
internal/ast/         AST node types
internal/parser/      recursive descent + Pratt expression parsing
internal/checker/     name resolution, schema recompute, PII + error-policy enforcement
internal/eval/        eval(expr, row) any
internal/runtime/     Stream/Source/Sink, driver loop, error policy, format registry, build
internal/format/      csv source, jsonl sink, xlsx source
examples/             one .sift + fixture pair per language feature or error policy,
                       for this tutorial; never read by a test
testdata/             a copy of every examples/ fixture an actual Go test reads
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

The base language is done: both example cases in
`../design/language.md` §7 pass end to end through the CLI, and every
part of the code has tests. That base is kept small on purpose; see
`../design/language.md` §5 for the full list of what's in and what's
left out for now (joins, dedupe, fan out, an optimizer, schema guessing,
and formats beyond csv/jsonl).

Sift is still growing. `../CLAUDE.md` keeps the up to date list of what's
shipped and what's still a draft, one design doc at a time. As of this
tutorial, these have shipped: failing rows as data with `on error`
(`../design/errors.md`), `select`/`drop`/`rename`/`limit`/`offset` and
mask/hash/redact as stages (`../design/improvements.md`), writing to more
than one sink (`../design/multisink.md`), named segments with parameters
(`../design/segments.md`), the `xlsx` source (`../design/xlsx.md`),
optional fields (`../design/optional-fields.md`), conditional routing
(`../design/routing.md`), and column aliases for headers that aren't
valid identifiers (`../design/column-aliases.md`).
