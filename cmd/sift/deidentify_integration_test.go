// CLI-level coverage for @deidentify, adding the cases
// TestExampleDeidentify can't show on its own: non-determinism across
// two runs, every rejection reaching the real checker through the real
// CLI, key handling failing at construction, and a failed cell's
// diagnostic never naming its own value. internal/checker,
// internal/value, and internal/format's own unit tests cover the rest.
package main

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const deidTestKeyHex = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

// decryptDeidentifiedCell is this test file's own out-of-band decrypt --
// deliberately not something Sift itself offers: there is no reveal()
// and no in-program decryption. It undoes exactly what
// internal/value's encryptDeidentifiedCell does: base64 -> nonce ‖
// ciphertext ‖ tag -> AES-256-GCM open -> a presence byte plus the
// canonical cell string.
func decryptDeidentifiedCell(t *testing.T, ciphertext string, key []byte) []byte {
	t.Helper()
	sealed, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		t.Fatalf("decryptDeidentifiedCell: not valid base64: %v", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatalf("decryptDeidentifiedCell: %v", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatalf("decryptDeidentifiedCell: %v", err)
	}
	nonceSize := gcm.NonceSize()
	nonce, rest := sealed[:nonceSize], sealed[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, rest, nil)
	if err != nil {
		t.Fatalf("decryptDeidentifiedCell: %v", err)
	}
	return plaintext
}

// TestExampleDeidentify drives testdata/deidentify.sift through the real
// CLI: id and name pass through as plain JSON, email is a
// decodable base64 string that decrypts to the original cell, and the
// raw output bytes never contain either email in the clear.
func TestExampleDeidentify(t *testing.T) {
	key, err := hex.DecodeString(deidTestKeyHex)
	if err != nil {
		t.Fatalf("decoding test key: %v", err)
	}
	t.Setenv("SIFT_DEIDENTIFY_KEY", deidTestKeyHex)

	got := runExample(t, "deidentify")

	if strings.Contains(string(got), "ada@example.com") || strings.Contains(string(got), "tom@example.com") {
		t.Fatalf("output contains a plaintext email:\n%s", got)
	}

	wantEmail := map[string]string{"Ada": "ada@example.com", "Tom": "tom@example.com"}
	lines := strings.Split(strings.TrimRight(string(got), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d output rows, want 2:\n%s", len(lines), got)
	}
	for _, line := range lines {
		var row struct {
			ID    int    `json:"id"`
			Name  string `json:"name"`
			Email string `json:"email"`
		}
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			t.Fatalf("unmarshaling %q: %v", line, err)
		}
		plaintext := decryptDeidentifiedCell(t, row.Email, key)
		if plaintext[0] != 0x01 {
			t.Errorf("%s: presence byte = %#x, want 0x01 (present)", row.Name, plaintext[0])
		}
		if got, want := string(plaintext[1:]), wantEmail[row.Name]; got != want {
			t.Errorf("%s: decrypted email = %q, want %q", row.Name, got, want)
		}
	}
}

// TestDeidentifyNonDeterministic confirms running the same program twice
// against the same input produces two different ciphertexts for the same
// cell.
func TestDeidentifyNonDeterministic(t *testing.T) {
	t.Setenv("SIFT_DEIDENTIFY_KEY", deidTestKeyHex)

	firstRun := string(runExample(t, "deidentify"))
	secondRun := string(runExample(t, "deidentify"))
	if firstRun == secondRun {
		t.Errorf("two runs produced identical output:\n%s", firstRun)
	}
}

// TestDeidentifyRejectsFilterOnDeidentifiedField is the CLI-level slice of
// the rejection table (internal/checker's own unit tests cover the
// rest): reading a @deidentify column in a filter predicate is a compile
// error, not a row failure or a panic.
func TestDeidentifyRejectsFilterOnDeidentifiedField(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "people.csv"), "name,email\nAda,ada@example.com\n")
	siftPath := filepath.Join(dir, "prog.sift")
	writeFile(t, siftPath, `source in = csv("people.csv", schema: { name: string, email: string @deidentify }, key: env("SIFT_DEIDENTIFY_KEY"))
sink   out = jsonl("out.jsonl")
pipeline main { in |> filter(.email == "x") |> out }
`)
	t.Setenv("SIFT_DEIDENTIFY_KEY", deidTestKeyHex)

	err := runFile(siftPath, false)
	if err == nil {
		t.Fatal("runFile succeeded, want a compile error")
	}
	if !strings.Contains(err.Error(), `field "email" is @deidentify`) {
		t.Errorf("error = %v, want the field-access rejection", err)
	}
}

// TestDeidentifyPIIConflictIsCompileError confirms @pii and @deidentify on
// the same field is rejected before any row is read.
func TestDeidentifyPIIConflictIsCompileError(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "people.csv"), "name,email\nAda,ada@example.com\n")
	siftPath := filepath.Join(dir, "prog.sift")
	writeFile(t, siftPath, `source in = csv("people.csv", schema: { name: string, email: string @pii @deidentify }, key: env("SIFT_DEIDENTIFY_KEY"))
sink   out = jsonl("out.jsonl")
pipeline main { in |> out }
`)
	t.Setenv("SIFT_DEIDENTIFY_KEY", deidTestKeyHex)

	err := runFile(siftPath, false)
	if err == nil {
		t.Fatal("runFile succeeded, want a compile error")
	}
	if !strings.Contains(err.Error(), `cannot be both @pii and @deidentify`) {
		t.Errorf("error = %v, want the annotation-conflict rejection", err)
	}
}

// TestDeidentifyOptionalFieldRoundTrip confirms a `string? @deidentify`
// column compiles and runs through the real CLI, and that a present cell
// and a blank one (already treated as absent, not the empty string --
// there's no third state to test here) each decrypt out of band to the
// expected presence byte: 0x01 then the cell, or a bare 0x00. Ciphertext
// length itself is deliberately not asserted equal; length leakage is a
// known gap, not something this encoding hides.
func TestDeidentifyOptionalFieldRoundTrip(t *testing.T) {
	key, err := hex.DecodeString(deidTestKeyHex)
	if err != nil {
		t.Fatalf("decoding test key: %v", err)
	}
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "people.csv"), "name,phone\nAda,555-1234\nTom,\n")
	siftPath := filepath.Join(dir, "prog.sift")
	outPath := filepath.Join(dir, "out.jsonl")
	writeFile(t, siftPath, `source in = csv("people.csv", schema: { name: string, phone: string? @deidentify }, key: env("SIFT_DEIDENTIFY_KEY"))
sink   out = jsonl("out.jsonl")
pipeline main { in |> out }
`)
	t.Setenv("SIFT_DEIDENTIFY_KEY", deidTestKeyHex)

	if err := runFile(siftPath, false); err != nil {
		t.Fatalf("runFile error: %v", err)
	}
	got, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("reading %s: %v", outPath, err)
	}
	lines := strings.Split(strings.TrimRight(string(got), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d output rows, want 2:\n%s", len(lines), got)
	}

	for _, line := range lines {
		var row struct {
			Name  string `json:"name"`
			Phone string `json:"phone"`
		}
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			t.Fatalf("unmarshaling %q: %v", line, err)
		}
		plaintext := decryptDeidentifiedCell(t, row.Phone, key)
		switch row.Name {
		case "Ada":
			if plaintext[0] != 0x01 || string(plaintext[1:]) != "555-1234" {
				t.Errorf("Ada: decrypted phone = %v, want presence byte 0x01 then %q", plaintext, "555-1234")
			}
		case "Tom":
			if len(plaintext) != 1 || plaintext[0] != 0x00 {
				t.Errorf("Tom: decrypted phone = %v, want just the 0x00 absence byte", plaintext)
			}
		}
	}
}

// TestDeidentifyMissingKeyKwargIsConstructionError confirms a schema with a
// @deidentify field but no key: kwarg fails at construction, before any
// row is read.
func TestDeidentifyMissingKeyKwargIsConstructionError(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "people.csv"), "name,email\nAda,ada@example.com\n")
	siftPath := filepath.Join(dir, "prog.sift")
	writeFile(t, siftPath, `source in = csv("people.csv", schema: { name: string, email: string @deidentify })
sink   out = jsonl("out.jsonl")
pipeline main { in |> out }
`)

	err := runFile(siftPath, false)
	if err == nil {
		t.Fatal("runFile succeeded, want a construction error")
	}
	if !strings.Contains(err.Error(), `schema declares @deidentify field "email" but no key: kwarg was given`) {
		t.Errorf("error = %v", err)
	}
}

// TestDeidentifyUnsetEnvVarIsConstructionError confirms a key: kwarg naming
// an environment variable that isn't set fails at construction, and the
// error never contains key material (there is none to contain here --
// the variable was never set).
func TestDeidentifyUnsetEnvVarIsConstructionError(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "people.csv"), "name,email\nAda,ada@example.com\n")
	siftPath := filepath.Join(dir, "prog.sift")
	writeFile(t, siftPath, `source in = csv("people.csv", schema: { name: string, email: string @deidentify }, key: env("SIFT_DEIDENTIFY_KEY_UNSET"))
sink   out = jsonl("out.jsonl")
pipeline main { in |> out }
`)

	err := runFile(siftPath, false)
	if err == nil {
		t.Fatal("runFile succeeded, want a construction error")
	}
	if !strings.Contains(err.Error(), `"SIFT_DEIDENTIFY_KEY_UNSET"`) || !strings.Contains(err.Error(), "not set") {
		t.Errorf("error = %v, want it to name the unset variable", err)
	}
}

// TestDeidentifyWrongLengthKeyIsConstructionError confirms a key: kwarg whose
// environment variable decodes to something other than 32 bytes fails
// at construction, and the error never contains the key material
// itself.
func TestDeidentifyWrongLengthKeyIsConstructionError(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "people.csv"), "name,email\nAda,ada@example.com\n")
	siftPath := filepath.Join(dir, "prog.sift")
	writeFile(t, siftPath, `source in = csv("people.csv", schema: { name: string, email: string @deidentify }, key: env("SIFT_DEIDENTIFY_KEY"))
sink   out = jsonl("out.jsonl")
pipeline main { in |> out }
`)
	const shortKey = "deadbeef"
	t.Setenv("SIFT_DEIDENTIFY_KEY", shortKey)

	err := runFile(siftPath, false)
	if err == nil {
		t.Fatal("runFile succeeded, want a construction error")
	}
	if !strings.Contains(err.Error(), "want 32") {
		t.Errorf("error = %v, want it to mention the expected key length", err)
	}
	if strings.Contains(err.Error(), shortKey) {
		t.Errorf("error = %v, must not contain the key material itself", err)
	}
}

// TestDeidentifyFailedCellDiagnosticOmitsValue confirms a @deidentify field
// with a cell that fails to parse as its declared type fails the row
// under the error policy with a reason that names the kind but never
// the cell's own value.
func TestDeidentifyFailedCellDiagnosticOmitsValue(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "accounts.csv"), "name,balance\nAda,not-a-number\n")
	siftPath := filepath.Join(dir, "prog.sift")
	errPath := filepath.Join(dir, "errors.jsonl")
	writeFile(t, siftPath, `on error |> errors

source in     = csv("accounts.csv", schema: { name: string, balance: int @deidentify }, key: env("SIFT_DEIDENTIFY_KEY"))
sink   out    = jsonl("out.jsonl")
sink   errors = jsonl("errors.jsonl")
pipeline main { in |> out }
`)
	t.Setenv("SIFT_DEIDENTIFY_KEY", deidTestKeyHex)

	if err := runFile(siftPath, false); err != nil {
		t.Fatalf("runFile error: %v", err)
	}
	errGot, err := os.ReadFile(errPath)
	if err != nil {
		t.Fatalf("reading %s: %v", errPath, err)
	}
	if !strings.Contains(string(errGot), "cannot parse cell as int") {
		t.Errorf("errors.jsonl = %q, want a reason naming the kind", errGot)
	}
	if strings.Contains(string(errGot), "not-a-number") {
		t.Errorf("errors.jsonl = %q, must not contain the offending cell value", errGot)
	}
}
