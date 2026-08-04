# Sift

Sift is a small, typed language for writing ETL pipelines. A program reads
rows from a source, runs them through a few steps, and writes the result
to a sink.

It has one feature most tools don't have: it tracks personal data for you.
Mark a field `@pii` in a source and Sift follows it through every step of
the pipeline. If you try to write that field out without masking it first,
the program will not compile.

## Build it

```console
$ go build -o sift ./cmd/sift
```

## Run it

One example that touches most of the language: a column whose real
header isn't a valid identifier, a masked PII field, a phone number
that's allowed to be missing, rows sent to different sinks by region,
and a bad row that goes to an error sink instead of stopping the whole
run.

`showcase.csv`:

```csv
Full Name,email,phone,region,age
Ada,ada@example.com,555-0100,EU,42
Tom,tom@example.com,,US,15
Liam,liam@example.com,555-0177,AU,29
Grace,grace@example.com,555-0199,APAC,not-a-number
```

`showcase.sift`:

```sift
on error |> errors

source in = csv("showcase.csv",
  schema: {
    name: string,
    email: string @pii,
    phone: string?,
    region: string,
    age: int
  },
  columns: {
    name: "Full Name"
  }
)
sink eu_sink   = jsonl("showcase_eu.jsonl")
sink us_sink   = jsonl("showcase_us.jsonl")
sink rest_sink = jsonl("showcase_rest.jsonl")
sink errors    = jsonl("showcase_errors.jsonl")

pipeline main {
  in
    |> map({ ...row, email: mask(.email), phone: .phone ?? "unknown" })
    |> route {
         .region == "EU" => eu_sink,
         .region == "US" => us_sink,
         else            => rest_sink,
       }
}
```

Run it:

```console
$ ./sift run showcase.sift
$ cat showcase_eu.jsonl
{"name":"Ada","email":"***************","phone":"555-0100","region":"EU","age":42}
$ cat showcase_us.jsonl
{"name":"Tom","email":"***************","phone":"unknown","region":"US","age":15}
$ cat showcase_rest.jsonl
{"name":"Liam","email":"****************","phone":"555-0177","region":"AU","age":29}
$ cat showcase_errors.jsonl
{"source":"in","ordinal":3,"offset":5,"reason":"cannot parse \"not-a-number\" as int","stage":"csv:age"}
```

A few things happened here, all in one pass over the file:

- The file's real header is `Full Name`, and that's never a valid schema
  field name (it has a space). `columns` maps the clean identifier
  `name` to that raw header text instead.
- `email` is marked `@pii`. It gets masked before it can reach any sink.
  Sift would refuse to compile this program if it didn't.
- `phone` is marked optional with `?`. Tom's phone is blank, so it comes
  through as absent, and `??` gives it the default `"unknown"` instead
  of failing the row.
- `route` sends each row to exactly one sink based on `region`. Ada goes
  to `eu_sink`, Tom to `us_sink`, and Liam, who doesn't match either,
  falls through to `rest_sink` via the required `else` branch.
- Grace's `age` cell can't be read as a number. Instead of stopping the
  run, that one row is sent to the `errors` sink with a note saying
  what went wrong and where, and everyone else still gets processed.

## Learn more

That's enough to get something running. For the full tour of the
language, one feature at a time with a working example for each, see
[examples/README.md](examples/README.md).

For how the code itself is organized, see [CLAUDE.md](CLAUDE.md).
