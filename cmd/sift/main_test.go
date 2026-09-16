package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vitzeno/sift/internal/checker"
	"github.com/vitzeno/sift/internal/parser"
)

// captureStdout redirects os.Stdout for the duration of f and returns what
// was written. emitAST/emitSchema print straight to os.Stdout, so this is
// the most direct way to assert on their output.
func captureStdout(t *testing.T, f func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	orig := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = orig }()

	f()

	w.Close()
	var buf bytes.Buffer
	io.Copy(&buf, r)
	return buf.String()
}

// chdir switches the working directory for the duration of the test and
// restores it on cleanup.
func chdir(t *testing.T, dir string) {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir(%q): %v", dir, err)
	}
	t.Cleanup(func() { os.Chdir(cwd) })
}

// TestRunFileResolvesSourcePathRelativeToScriptDir checks an input file
// ("in") and the output file it produces ("out") sit next to a script
// invoked from a different working directory. If paths resolved against
// cwd instead, this would fail to find people.csv.
func TestRunFileResolvesSourcePathRelativeToScriptDir(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "people.csv"), "name,age\nAda,42\nTom,15\n")
	siftPath := filepath.Join(dir, "adults.sift")
	writeFile(t, siftPath, `source in = csv("people.csv", schema: { name: string, age: int })
sink out = jsonl("adults.jsonl")

pipeline main {
  in |> filter(.age >= 18) |> out
}
`)

	// cwd is somewhere else entirely, to prove path resolution doesn't
	// depend on it.
	chdir(t, t.TempDir())

	if err := runFile(siftPath, false); err != nil {
		t.Fatalf("runFile error: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dir, "adults.jsonl"))
	if err != nil {
		t.Fatalf("reading output file: %v", err)
	}
	want := `{"name":"Ada","age":42}` + "\n"
	if string(got) != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}

// TestRunFileScriptInSubdirectory confirms resolution walks from the
// script's own directory even several levels below cwd, so `sift run
// examples/adults.sift` works from the repo root.
func TestRunFileScriptInSubdirectory(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "scripts")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	writeFile(t, filepath.Join(sub, "people.csv"), "name,age\nAda,42\n")
	writeFile(t, filepath.Join(sub, "adults.sift"), `source in = csv("people.csv", schema: { name: string, age: int })
sink out = jsonl("adults.jsonl")

pipeline main {
  in |> filter(.age >= 18) |> out
}
`)

	chdir(t, dir)

	if err := runFile("scripts/adults.sift", false); err != nil {
		t.Fatalf("runFile error: %v", err)
	}
	if _, err := os.ReadFile(filepath.Join(sub, "adults.jsonl")); err != nil {
		t.Fatalf("expected adults.jsonl next to the script, not cwd: %v", err)
	}
}

// TestRunFileAbsoluteSourcePathIsUnchanged confirms an already-absolute
// path is never rewritten, even when it points somewhere other than the
// script's directory.
func TestRunFileAbsoluteSourcePathIsUnchanged(t *testing.T) {
	dataDir := t.TempDir()
	writeFile(t, filepath.Join(dataDir, "people.csv"), "name,age\nAda,42\n")

	scriptDir := t.TempDir()
	siftPath := filepath.Join(scriptDir, "adults.sift")
	writeFile(t, siftPath, `source in = csv("`+filepath.ToSlash(filepath.Join(dataDir, "people.csv"))+`", schema: { name: string, age: int })
sink out = jsonl("adults.jsonl")

pipeline main {
  in |> filter(.age >= 18) |> out
}
`)

	if err := runFile(siftPath, false); err != nil {
		t.Fatalf("runFile error: %v", err)
	}
	if _, err := os.ReadFile(filepath.Join(scriptDir, "adults.jsonl")); err != nil {
		t.Fatalf("expected adults.jsonl next to the script: %v", err)
	}
}

func TestRunFileMissingFile(t *testing.T) {
	if err := runFile(filepath.Join(t.TempDir(), "does-not-exist.sift"), false); err == nil {
		t.Fatal("runFile succeeded, want a file-not-found error")
	}
}

func TestRunFilePIIRejected(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "people.csv"), "name,email\nAda,ada@example.com\n")
	siftPath := filepath.Join(dir, "leaky.sift")
	writeFile(t, siftPath, `source in = csv("people.csv", schema: { name: string, email: string @pii })
sink out = jsonl("out.jsonl")

pipeline main {
  in |> out
}
`)

	err := runFile(siftPath, false)
	if err == nil {
		t.Fatal("runFile succeeded, want a PII rejection")
	}
	var ce *checker.CheckError
	if !errors.As(err, &ce) {
		t.Fatalf("error type = %T, want *checker.CheckError", err)
	}
}

func TestEmitASTPrintsDeclarationsAndStages(t *testing.T) {
	dir := t.TempDir()
	siftPath := filepath.Join(dir, "adults.sift")
	writeFile(t, siftPath, `source in = csv("people.csv", schema: { name: string, age: int })
sink out = jsonl("adults.jsonl")

pipeline main {
  in |> filter(.age >= 18) |> out
}
`)

	out := captureStdout(t, func() {
		if err := emitAST(siftPath); err != nil {
			t.Fatalf("emitAST error: %v", err)
		}
	})

	wantLines := []string{
		`source in = csv("people.csv", schema: { name: string, age: int })`,
		`sink out = jsonl("adults.jsonl")`,
		`pipeline main:`,
		`  in`,
		`  filter((.age >= 18))`,
		`  out`,
	}
	for _, line := range wantLines {
		if !strings.Contains(out, line) {
			t.Errorf("emitAST output missing %q; got:\n%s", line, out)
		}
	}
}

func TestEmitSchemaPrintsSourceAndSinkSchemas(t *testing.T) {
	dir := t.TempDir()
	siftPath := filepath.Join(dir, "adults.sift")
	writeFile(t, siftPath, `source in = csv("people.csv", schema: { name: string, age: int })
sink out = jsonl("adults.jsonl")

pipeline main {
  in |> filter(.age >= 18) |> out
}
`)

	out := captureStdout(t, func() {
		if err := emitSchema(siftPath); err != nil {
			t.Fatalf("emitSchema error: %v", err)
		}
	})

	want := "source in: { name: string, age: int }\nsink out: { name: string, age: int }\n"
	if out != want {
		t.Errorf("emitSchema output = %q, want %q", out, want)
	}
}

func TestPrintErrFormatsPositionalErrors(t *testing.T) {
	_, err := parser.Parse("42")
	if err == nil {
		t.Fatal("expected a parse error")
	}

	var buf bytes.Buffer
	origStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w
	printErr("bad.sift", err)
	w.Close()
	os.Stderr = origStderr
	io.Copy(&buf, r)

	got := buf.String()
	if !strings.HasPrefix(got, "bad.sift:1:1: error: ") {
		t.Errorf("printErr output = %q, want it to start with %q", got, "bad.sift:1:1: error: ")
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}
