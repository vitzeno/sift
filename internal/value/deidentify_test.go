package value

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"testing"
)

// testKey is a throwaway 32-byte AES-256 key, valid only for this test
// file. It is never read from an environment variable here -- that
// resolution is format.ResolveDeidentifyKey's job, exercised by its own
// package's tests.
var testKey = []byte("0123456789abcdef0123456789abcdef")

// decryptForTest is the test suite's own out-of-band decrypt, deliberately
// never exposed by the value package itself (design/deidentify.md §8: "no
// reveal(), no in-program decryption"). It undoes exactly what
// encryptDeidentifiedCell does, so a round-trip test can assert on the
// plaintext without Sift ever offering a way to do so.
func decryptForTest(t *testing.T, ciphertext string, key []byte) []byte {
	t.Helper()
	sealed, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		t.Fatalf("decryptForTest: not valid base64: %v", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatalf("decryptForTest: %v", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatalf("decryptForTest: %v", err)
	}
	nonceSize := gcm.NonceSize()
	if len(sealed) < nonceSize {
		t.Fatalf("decryptForTest: ciphertext shorter than one nonce")
	}
	nonce, rest := sealed[:nonceSize], sealed[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, rest, nil)
	if err != nil {
		t.Fatalf("decryptForTest: %v", err)
	}
	return plaintext
}

func TestCoerceDeidentifiedRoundTrip(t *testing.T) {
	typ := Type{Kind: Deidentified, Inner: &Type{Kind: String}}
	got, fail := Coerce(typ, "ada@example.com", "", testKey)
	if fail != nil {
		t.Fatalf("Coerce failed: %+v", fail)
	}
	ciphertext, ok := got.(string)
	if !ok {
		t.Fatalf("Coerce returned %T, want string", got)
	}

	plaintext := decryptForTest(t, ciphertext, testKey)
	if len(plaintext) == 0 || plaintext[0] != deidentifyPresent {
		t.Fatalf("plaintext = %v, want presence byte 0x01 first", plaintext)
	}
	if string(plaintext[1:]) != "ada@example.com" {
		t.Errorf("decrypted cell = %q, want %q", plaintext[1:], "ada@example.com")
	}
}

func TestCoerceDeidentifiedNonDeterministic(t *testing.T) {
	typ := Type{Kind: Deidentified, Inner: &Type{Kind: String}}
	got1, fail := Coerce(typ, "ada@example.com", "", testKey)
	if fail != nil {
		t.Fatalf("Coerce failed: %+v", fail)
	}
	got2, fail := Coerce(typ, "ada@example.com", "", testKey)
	if fail != nil {
		t.Fatalf("Coerce failed: %+v", fail)
	}
	if got1 == got2 {
		t.Errorf("two encryptions of the same cell produced identical ciphertext %q", got1)
	}
}

func TestCoerceDeidentifiedAbsentAndEmptyAreIndistinguishableInLength(t *testing.T) {
	typ := Type{Kind: Deidentified, Inner: &Type{Kind: String, Optional: true}}

	absentCipher, fail := Coerce(typ, "", "", testKey)
	if fail != nil {
		t.Fatalf("Coerce(absent) failed: %+v", fail)
	}
	presentCipher, fail := Coerce(Type{Kind: Deidentified, Inner: &Type{Kind: String}}, "x", "", testKey)
	if fail != nil {
		t.Fatalf("Coerce(present) failed: %+v", fail)
	}

	absentPlain := decryptForTest(t, absentCipher.(string), testKey)
	if len(absentPlain) != 1 || absentPlain[0] != deidentifyAbsent {
		t.Errorf("absent plaintext = %v, want just the 0x00 presence byte", absentPlain)
	}
	presentPlain := decryptForTest(t, presentCipher.(string), testKey)
	if presentPlain[0] != deidentifyPresent {
		t.Errorf("present plaintext = %v, want presence byte 0x01 first", presentPlain)
	}
}

func TestCoerceDeidentifiedBadCellReasonOmitsRawValue(t *testing.T) {
	typ := Type{Kind: Deidentified, Inner: &Type{Kind: Int}}
	_, fail := Coerce(typ, "not-a-number", "", testKey)
	if fail == nil {
		t.Fatal("Coerce succeeded, want a Failure for a non-numeric cell")
	}
	if fail.Reason != "cannot parse cell as int" {
		t.Errorf("Reason = %q, want a reason that names the kind but omits the cell value", fail.Reason)
	}
}

func TestCoerceDeidentifiedNonStringKindsCanonicalize(t *testing.T) {
	tests := []struct {
		name string
		typ  Type
		raw  string
	}{
		{"int", Type{Kind: Deidentified, Inner: &Type{Kind: Int}}, "42"},
		{"bool", Type{Kind: Deidentified, Inner: &Type{Kind: Bool}}, "true"},
		{"date", Type{Kind: Deidentified, Inner: &Type{Kind: Date}}, "2026-08-06"},
		{"decimal", Type{Kind: Deidentified, Inner: &Type{Kind: Decimal}}, "19.99"},
		{"datetime", Type{Kind: Deidentified, Inner: &Type{Kind: DateTime}}, "2026-08-06T12:00:00"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, fail := Coerce(tt.typ, tt.raw, "", testKey)
			if fail != nil {
				t.Fatalf("Coerce(%q) failed: %+v", tt.raw, fail)
			}
			plaintext := decryptForTest(t, got.(string), testKey)
			if plaintext[0] != deidentifyPresent {
				t.Fatalf("plaintext = %v, want presence byte 0x01 first", plaintext)
			}
			if string(plaintext[1:]) != tt.raw {
				t.Errorf("decrypted cell = %q, want %q", plaintext[1:], tt.raw)
			}
		})
	}
}
