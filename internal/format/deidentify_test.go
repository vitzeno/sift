package format

import (
	"encoding/base64"
	"encoding/hex"
	"testing"

	"github.com/vitzeno/sift/internal/value"
)

var testKeyBytes = []byte("0123456789abcdef0123456789abcdef") // 32 bytes

func deidentifiedSchema() value.Schema {
	return value.Schema{Fields: []value.Field{
		{Name: "name", Type: value.Type{Kind: value.String}},
		{Name: "email", Type: value.Type{Kind: value.Deidentified, Inner: &value.Type{Kind: value.String}}},
	}}
}

func TestRequireDeidentifyKeyNoDeidentifiedFieldIsNoop(t *testing.T) {
	schema := value.Schema{Fields: []value.Field{{Name: "name", Type: value.Type{Kind: value.String}}}}
	key, err := RequireDeidentifyKey(schema, "")
	if err != nil || key != nil {
		t.Errorf("RequireDeidentifyKey = %v, %v; want nil, nil for a schema with no @deidentify field", key, err)
	}
}

func TestRequireDeidentifyKeyMissingKwarg(t *testing.T) {
	_, err := RequireDeidentifyKey(deidentifiedSchema(), "")
	if err == nil {
		t.Fatal("RequireDeidentifyKey succeeded, want an error for a missing key: kwarg")
	}
	if got := err.Error(); got != `schema declares @deidentify field "email" but no key: kwarg was given` {
		t.Errorf("error = %q", got)
	}
}

func TestRequireDeidentifyKeyResolvesEnvVar(t *testing.T) {
	t.Setenv("SIFT_TEST_KEY", hex.EncodeToString(testKeyBytes))
	key, err := RequireDeidentifyKey(deidentifiedSchema(), "SIFT_TEST_KEY")
	if err != nil {
		t.Fatalf("RequireDeidentifyKey failed: %v", err)
	}
	if string(key) != string(testKeyBytes) {
		t.Errorf("key = %x, want %x", key, testKeyBytes)
	}
}

func TestResolveDeidentifyKeyUnsetEnvVar(t *testing.T) {
	_, err := ResolveDeidentifyKey("SIFT_TEST_KEY_NOT_SET")
	if err == nil {
		t.Fatal("ResolveDeidentifyKey succeeded, want an error for an unset environment variable")
	}
}

func TestResolveDeidentifyKeyHex(t *testing.T) {
	t.Setenv("SIFT_TEST_KEY", hex.EncodeToString(testKeyBytes))
	key, err := ResolveDeidentifyKey("SIFT_TEST_KEY")
	if err != nil {
		t.Fatalf("ResolveDeidentifyKey failed: %v", err)
	}
	if string(key) != string(testKeyBytes) {
		t.Errorf("key = %x, want %x", key, testKeyBytes)
	}
}

func TestResolveDeidentifyKeyBase64(t *testing.T) {
	t.Setenv("SIFT_TEST_KEY", base64.StdEncoding.EncodeToString(testKeyBytes))
	key, err := ResolveDeidentifyKey("SIFT_TEST_KEY")
	if err != nil {
		t.Fatalf("ResolveDeidentifyKey failed: %v", err)
	}
	if string(key) != string(testKeyBytes) {
		t.Errorf("key = %x, want %x", key, testKeyBytes)
	}
}

func TestResolveDeidentifyKeyWrongLength(t *testing.T) {
	t.Setenv("SIFT_TEST_KEY", hex.EncodeToString([]byte("too-short")))
	_, err := ResolveDeidentifyKey("SIFT_TEST_KEY")
	if err == nil {
		t.Fatal("ResolveDeidentifyKey succeeded, want an error for a key that isn't 32 bytes")
	}
}

func TestResolveDeidentifyKeyUndecodable(t *testing.T) {
	t.Setenv("SIFT_TEST_KEY", "not hex and not base64 !!!")
	_, err := ResolveDeidentifyKey("SIFT_TEST_KEY")
	if err == nil {
		t.Fatal("ResolveDeidentifyKey succeeded, want an error for an undecodable value")
	}
}
