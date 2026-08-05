// This file covers --print (main.go, run.go): every sink redirects to
// stdout instead of its declared file, buffered and printed one sink's
// full block at a time rather than interleaved per row.
package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestStripPrintFlag confirms --print is pulled out of args wherever it
// appears, leaving the rest in order.
func TestStripPrintFlag(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		wantRest  []string
		wantPrint bool
	}{
		{"no flag", []string{"run", "prog.sift"}, []string{"run", "prog.sift"}, false},
		{"flag last", []string{"run", "prog.sift", "--print"}, []string{"run", "prog.sift"}, true},
		{"flag first", []string{"--print", "run", "prog.sift"}, []string{"run", "prog.sift"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rest, print := stripPrintFlag(tt.args)
			if len(rest) != len(tt.wantRest) {
				t.Fatalf("rest = %v, want %v", rest, tt.wantRest)
			}
			for i := range rest {
				if rest[i] != tt.wantRest[i] {
					t.Errorf("rest = %v, want %v", rest, tt.wantRest)
				}
			}
			if print != tt.wantPrint {
				t.Errorf("print = %v, want %v", print, tt.wantPrint)
			}
		})
	}
}

// TestRunFilePrintWritesNoFile confirms --print is replace mode: the
// sink's declared file is never created.
func TestRunFilePrintWritesNoFile(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "people.csv"), "name,age\nAda,42\nTom,15\n")
	siftPath := filepath.Join(dir, "prog.sift")
	outPath := filepath.Join(dir, "out.jsonl")
	writeFile(t, siftPath, `source in = csv("people.csv", schema: { name: string, age: int })
sink   out = jsonl("out.jsonl")
pipeline main { in |> out }
`)

	got := captureStdout(t, func() {
		if err := runFile(siftPath, true); err != nil {
			t.Fatalf("runFile error: %v", err)
		}
	})

	want := `=== out ===
{"name":"Ada","age":42}
{"name":"Tom","age":15}
`
	if got != want {
		t.Errorf("stdout = %q, want %q", got, want)
	}
	if _, err := os.Stat(outPath); !os.IsNotExist(err) {
		t.Errorf("out.jsonl exists, want --print to never write it (stat err = %v)", err)
	}
}

// TestRunFilePrintMultiSinkOneBlockAtATime confirms a route program's two
// sinks each print their full block in declaration order, not
// interleaved row by row.
func TestRunFilePrintMultiSinkOneBlockAtATime(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "people.csv"), "name,age\nAda,42\nTom,15\nLiam,25\n")
	siftPath := filepath.Join(dir, "prog.sift")
	writeFile(t, siftPath, `source in = csv("people.csv", schema: { name: string, age: int })
sink adults = jsonl("adults.jsonl")
sink minors = jsonl("minors.jsonl")
pipeline main {
  in |> route {
    .age >= 18 => adults,
    else       => minors
  }
}
`)

	got := captureStdout(t, func() {
		if err := runFile(siftPath, true); err != nil {
			t.Fatalf("runFile error: %v", err)
		}
	})

	want := `=== adults ===
{"name":"Ada","age":42}
{"name":"Liam","age":25}
=== minors ===
{"name":"Tom","age":15}
`
	if got != want {
		t.Errorf("stdout = %q, want %q", got, want)
	}
	for _, f := range []string{"adults.jsonl", "minors.jsonl"} {
		if _, err := os.Stat(filepath.Join(dir, f)); !os.IsNotExist(err) {
			t.Errorf("%s exists, want --print to never write it (stat err = %v)", f, err)
		}
	}
}
