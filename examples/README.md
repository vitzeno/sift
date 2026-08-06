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
6. [Encrypting a column: @deidentify](#encrypting-a-column-deidentify)
7. [Fields that may be missing](#fields-that-may-be-missing)
8. [Naming a column that isn't a valid identifier](#naming-a-column-that-isnt-a-valid-identifier)
9. [Working with dates](#working-with-dates)
10. [Working with datetime](#working-with-datetime)
11. [Exact math with decimal](#exact-math-with-decimal)
12. [Reshaping schemas: select, drop, rename](#reshaping-schemas-select-drop-rename)
13. [Cutting rows: limit and offset](#cutting-rows-limit-and-offset)
14. [Masking as a stage](#masking-as-a-stage)
15. [Putting it together](#putting-it-together)
16. [When a row fails: error policies](#when-a-row-fails-error-policies)
17. [Writing to more than one sink](#writing-to-more-than-one-sink)
18. [Routing rows by condition](#routing-rows-by-condition)
19. [Reusable pipelines: named segments](#reusable-pipelines-named-segments)
20. [Segments with parameters](#segments-with-parameters)
21. [A full ETL, with error routing](#a-full-etl-with-error-routing)
22. [Reading xlsx files](#reading-xlsx-files)
23. [Quick reference](#quick-reference)
24. [Project layout](#project-layout)
25. [Status](#status)

## Building the CLI

```console
$ go build -o sift ./cmd/sift
$ ./sift
usage: sift run <file> [--print] | sift --emit-ast <file> | sift --emit-schema <file>
```

Everything below assumes a `sift` binary built this way. `make build`
does the same thing (see [Project layout](#project-layout) for the rest
of the Makefile).

`sift run <file> --print` replaces every sink with the console: nothing
is written to disk, and each sink's rows print as one labeled block
(`=== name ===`) once the run finishes, rather than interleaving row by
row across multiple sinks. Handy for a quick look at a pipeline's output
without touching the files it would otherwise write.

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

## Encrypting a column: @deidentify

`@pii` still lets the pipeline compute with the plaintext — it just has
to clear the tag before a sink. Sometimes that's too much trust: the
pipeline shouldn't hold the plaintext at all. Tag a field `@deidentify`
instead and the cell is encrypted **inside the source**, before any
stage runs. Nothing downstream ever sees the original value — there is
no `reveal()`, and no way back inside Sift:

```sift
source in = csv("deidentify.csv",
  schema: { id: int, name: string, email: string @deidentify },
  key:    env("SIFT_DEIDENTIFY_KEY")
)
sink out = jsonl("deidentify_out.jsonl")

pipeline main {
  in |> out
}
```

```console
$ export SIFT_DEIDENTIFY_KEY=$(openssl rand -hex 32)
$ ./sift run deidentify.sift
$ cat deidentify_out.jsonl
{"id":1,"name":"Ada","email":"GwUgAJ8Y5BKn0QXss9DTygtoslouybv9izPequg1A5+Vkg3+sQJivcGOK/g="}
{"id":2,"name":"Tom","email":"JTpyO28Gxqa/vyZWbecqeH0G2XgEkXwfEZ6/E3yzf2npsPcPIikcwjWyijg="}
```

`email`'s ciphertext will look different every time you run this: the
encryption (AES-256-GCM) uses a fresh random nonce per cell, so the same
input never produces the same output twice, even for two identical
emails in the same file. That's deliberate — a stable ciphertext would
leak which rows share a value. It also means a deidentified column can
never be a join key or a dedupe key; if you need one of those, `hash`
(above) already gives you a stable one, at the cost of it being
recoverable-by-comparison rather than encrypted.

`key:` accepts `env("NAME")` and nothing else — the key itself never
appears in the `.sift` file, only the name of the environment variable
that holds it, so the program stays safe to commit. It's read as hex or
base64 and must decode to exactly 32 bytes; a missing or malformed key
fails immediately, before any row is read, the same way a missing
required column does.

Once a field is `deidentified<T>` (see it with `--emit-schema`), the
checker rejects every operation on it: reading it in a `filter` or
`map`, comparing it to anything (even another deidentified column),
`mask`/`hash`/`redact` (there's nothing to declassify), and `??` (there's
no optionality left to discharge — an absent cell is encrypted too, so
even *that* isn't visible without the key). The only things you can do
with it are `select`, `drop`, `rename`, and passing it through — `deidentify.sift`
in this folder shows the whole round trip.

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

This works the same way for `xlsx` sources too, once you get there
([Reading xlsx files](#reading-xlsx-files)): `columns` doesn't know or
care which format resolved it.

`columns.sift` in this folder is this exact program.

## Working with dates

`string` has no idea what a date is: `"2026-1-5"` and `"2026-01-05"`
compare as unrelated text, and sorting them lexically doesn't match
sorting them chronologically the moment padding or format differs.
`date` is a real scalar type for exactly that: a calendar date (year,
month, day, nothing else — no time of day, no timezone), comparable
with `< > <= >= == !=`.

```sift
source in = csv("date.csv",
  schema:  { name: string, started_on: date, renewed_on: date },
  formats: { renewed_on: "02/01/2006" }
)
sink out = jsonl("date_out.jsonl")

pipeline main {
  in |> filter(.renewed_on >= .started_on) |> out
}
```

`started_on`'s cells (`2026-01-05`) parse against the default ISO-8601
layout (`YYYY-MM-DD`), which needs no kwarg at all. `renewed_on`'s cells
are written day-first (`10/02/2026`), so the `formats` kwarg names its
layout explicitly: a Go reference-layout string, the same `{ field:
"value" }` shape `columns` already uses, one entry per date field that
isn't already ISO-8601. A date field with no entry in `formats` just
uses the default; two date fields on one source can each name a
completely different layout, independently.

```console
$ ./sift run date.sift
$ cat date_out.jsonl
{"name":"Ada","started_on":"2026-01-05","renewed_on":"2026-02-10"}
{"name":"Grace","started_on":"2026-01-01","renewed_on":"2026-01-01"}
```

Tom is missing from the output on purpose: his renewal (`15/02/2026`,
15 February) comes chronologically *before* his subscription started
(`2026-03-01`, 1 March), so `filter(.renewed_on >= .started_on)` drops
his row. That's the entire reason this type exists: comparing those two
values as plain strings would ask whether `"15/02/2026" >= "2026-03-01"`,
which isn't a meaningful question either way you read it, while as
`date` the comparison answers the real one.

A cell that doesn't match its format, or names a date that doesn't
exist on the calendar at all (`2026-02-30`), fails the row exactly like
a bad `int` or `double` cell, under the same
[error policy](#when-a-row-fails-error-policies) — skipped, routed, or
aborting the run depending on `on error`, with a reason like
`cannot parse "2026-02-30" as date`.

There's no way to write a date *literal* in an expression, so every
comparison is between two date columns, never a column and a fixed
cutoff, and there's no arithmetic (`date + 1`, `date - date`) yet
either. Both are real, deliberate gaps, not oversights — see
`../design/date.md` if you're curious why.

`date.sift` in this folder is this exact program.

## Working with datetime

`date` has no idea about time of day: two rows on the same calendar date
compare equal even if one happened at 9am and the other at 11pm.
`datetime` is a real scalar type for exactly that: a naive timestamp
(year, month, day, hour, minute, second — still no timezone or offset at
all), comparable with `< > <= >= == !=` just like `date`, but
finer-grained.

```sift
source in = csv("datetime.csv",
  schema:  { name: string, clock_in: datetime, clock_out: datetime },
  formats: { clock_in: "2006-01-02 15:04:05", clock_out: "2006-01-02 15:04:05" }
)
sink out = jsonl("datetime_out.jsonl")

pipeline main {
  in |> filter(.clock_out < .clock_in) |> out
}
```

```console
$ ./sift run datetime.sift
$ cat datetime_out.jsonl
{"name":"Tom","clock_in":"2026-07-31T21:00:00","clock_out":"2026-07-31T05:00:00"}
```

`formats` here is the *exact same kwarg* `date` already uses — not a
second one. Both `clock_in` and `clock_out` are written space-separated
(`"2026-07-31 09:00:00"`), the shape a timeclock or bank export
typically writes timestamps in, so both name that layout explicitly;
with no `formats` entry at all, a `datetime` field falls back to
ISO-8601-with-a-`T` (`"2026-07-31T04:10:25"`), a different default than
`date`'s own ISO-8601-no-time one. A `date` field and a `datetime` field
can sit on the same source, each resolving its own default or override
independently.

Only Tom's row reaches the output: his `clock_out` (5am) is
chronologically *before* his `clock_in` (9pm) on the very same calendar
date — almost certainly a data-entry mistake (forgetting to roll the
date forward for a shift that runs past midnight), and exactly the kind
of bug `date` alone could never catch, since both timestamps would
compare equal once the time-of-day is thrown away. Ada's ordinary
same-day shift and Grace's correctly-dated overnight shift (`clock_out`
on August 1st) are both excluded, same as `date`'s quiet-drop `filter`
behavior always has.

`date` and `datetime` are different types, even though both are
"temporal": a schema can declare one field of each, but comparing them
directly against each other (`.a_date == .a_datetime`) is a compile
error, the same never-silently-mix rule every other pair of types
already follows. Like `date`, there's no `datetime` literal syntax and
no arithmetic (`datetime + 1`, `datetime - datetime`) — see
`../design/datetime.md` if you're curious why.

`datetime.sift` in this folder is this exact program.

## Exact math with decimal

`double` (a `float64` underneath) is inexact: money arithmetic on it
accumulates representation error, and `19.99` doesn't even round-trip
through it exactly. `decimal` is a real scalar type for exact
arithmetic instead: what you parse is what comes back out, and
`+ - * /` never introduce rounding error `double` would.

```sift
source in = csv("decimal.csv", schema: { id: int, price: decimal, discount: decimal })
sink out = jsonl("decimal_out.jsonl")

pipeline main {
  in
    |> map({ ...row,
         net: .price - .discount,
         tax: .price * 0.08
       })
    |> filter(.net > 0)
    |> out
}
```

```console
$ ./sift run decimal.sift
$ cat decimal_out.jsonl
{"id":1,"price":19.99,"discount":5.00,"net":14.99,"tax":1.5992}
{"id":3,"price":1100.00,"discount":0.00,"net":1100.00,"tax":88.0000}
```

Three things worth noticing in that output. First, trailing zeros survive
exactly: `discount` reads `5.00` from the file and writes back `5.00`,
not `5`, and `tax` naturally grows to four decimal places
(`19.99 * 0.08`) without truncating any of them. Second, row 3's `price`
column in `decimal.csv` is actually the cell `"1,100.00"` — a comma-
thousands-formatted number, the shape a bank or accounting export
typically writes money in. `decimal` strips a qualifying thousands
separator before parsing, unconditionally, with no keyword argument
needed: every `decimal` field gets this. The rule only strips a comma
that comes *before* the cell's last `.`; a comma after it (as in a
European-formatted `"1.234,56"`, comma-as-decimal-point) is left alone
and the cell fails to parse instead of silently becoming the wrong
number. Third, `0.08` is a plain double literal, not a `decimal` value,
and it still works: a bare number written directly in an expression,
standing against a `decimal` column, adapts to `decimal` on the spot.
That's different from a real column of another type:

```sift
source in = csv("orders.csv", schema: { price: decimal, rate: double })
sink   out = jsonl("out.jsonl")
pipeline main { in |> map({ ...row, tax: .price * .rate }) |> out }
```

```console
$ ./sift run bad-mix.sift
bad-mix.sift:3:49: error: cannot apply * to decimal and double
```

`rate` is a real column, not a literal, so it never silently adapts.
Two decimal and double columns still can't mix arithmetically, even
though a literal constant can — that's the whole point: `decimal`
never quietly absorbs another column's rounding error, only a number
you actually wrote yourself.

Row 2 (Tom, whose $50 discount exceeds his $42.50 price) never reaches
the output at all: `filter(.net > 0)` drops it, the same quiet-drop
behavior `filter` always has, just now backed by exact comparison
instead of `double`'s.

`decimal.sift` in this folder is this exact program.

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

## Quick reference

- **Sources and sinks** name a format (`csv`, `jsonl`, `xlsx` for
  sources) and a path. A source also declares its schema, since neither
  CSV nor xlsx carries reliable types of its own. `xlsx` also takes
  `sheet:` and `header_row:`. Either format also takes `columns:`, to
  name a column whose real header isn't a valid identifier, `formats:`,
  to give a `date`/`datetime` field its own parsing layout, and `key:
  env("NAME")`, required when the schema has a `@deidentify` field.
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
- **Types:** `string`, `int`, `double`, `bool`, `date` (a calendar date
  only, no time or timezone, comparable but with no arithmetic and no
  literal syntax of its own — a value only ever comes from a source
  column), `datetime` (a naive timestamp — date plus time-of-day, still
  no timezone — comparable and finer-grained than `date` but otherwise
  the same cuts: no arithmetic, no literal syntax, reuses `date`'s own
  `formats:` kwarg rather than a second one, and never mixes with `date`
  in a comparison even though both are temporal), and `decimal` (exact
  arithmetic, no `float64` rounding error; a bare int/double literal
  standing against a `decimal` column adapts to `decimal`, but two real
  columns of different Kind never mix, even numeric ones; a
  comma-thousands-formatted cell like `"2,100.00"` parses
  unconditionally, no keyword argument needed, unless the comma sits
  after the cell's last `.`, which fails to parse instead of silently
  reading as the wrong number).
- **PII:** `@pii` attaches at the source, is tracked through every
  expression, and is only cleared by `mask`/`hash`/`redact` — which are
  string-only, so a non-string `@pii` field (`date`, `int`, ...) can
  only be dropped, not masked in place.
- **`@deidentify`:** encrypts a field at the source (AES-256-GCM, a fresh
  nonce per cell, keyed by `key: env("NAME")`) and replaces its type with
  `deidentified<T>`. No operation accepts that type — not even `==`
  against another deidentified field, or `??` — so the only things left
  to do with it are `select`/`drop`/`rename`/passthrough. Mutually
  exclusive with `@pii`; there is no in-program decryption.
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
cmd/sift/                CLI: run, --emit-ast, --emit-schema, --print
internal/value/           Row, Provenance, Type (+ @pii, Optional, Deidentified), Schema, Coerce, Absent
internal/lexer/           source text -> tokens
internal/ast/             AST node types
internal/parser/          recursive descent + Pratt expression parsing
internal/checker/         name resolution, schema recompute, PII + error-policy enforcement
internal/eval/            eval(expr, row) any
internal/runtime/         Stream/Source/Sink, driver loop, error policy, format registry, build
internal/format/          ResolveColumns/ResolveDateFormat, shared by the subpackages below
internal/format/csv/      csv source
internal/format/jsonl/    jsonl sink
internal/format/xlsx/     xlsx source
internal/format/console/  console sink (--print)
examples/                 one .sift + fixture pair per language feature or error policy,
                          for this tutorial; never read by a test
testdata/                 a copy of every examples/ fixture an actual Go test reads
design/                   language spec + one design doc per build phase
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
(`../design/routing.md`), column aliases for headers that aren't valid
identifiers (`../design/column-aliases.md`), the `date` type
(`../design/date.md`), the `decimal` type (`../design/decimal.md`),
unconditional thousands-comma leniency for `decimal` cells
(`../design/decimal-leniency.md`), the `datetime` type
(`../design/datetime.md`), and `@deidentify`, which encrypts a column at
the source instead of tagging it (`../design/deidentify.md`).
