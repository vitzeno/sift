package format

import (
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"

	"github.com/vitzeno/sift/internal/value"
)

// RequireDeidentifyKey implements design/deidentify.md §2's "construction
// error before any row is read, the same shape as a missing required
// column": a schema with a @deidentify field but no key: kwarg (envVar
// == "") is an error naming the field; otherwise the kwarg's environment
// variable is resolved via ResolveDeidentifyKey. Returns nil, nil for a
// schema with no @deidentify field at all, even if a key: kwarg was
// given anyway. The csv and xlsx source packages share this one function
// rather than each checking it their own way.
func RequireDeidentifyKey(schema value.Schema, envVar string) ([]byte, error) {
	f, ok := schema.FirstDeidentified()
	if !ok {
		return nil, nil
	}
	if envVar == "" {
		return nil, fmt.Errorf("schema declares @deidentify field %q but no key: kwarg was given", f.Name)
	}
	return ResolveDeidentifyKey(envVar)
}

// ResolveDeidentifyKey reads the key: kwarg's named environment variable
// and decodes it into design/deidentify.md §6's 32-byte AES-256 key. A
// missing variable, an undecodable value, or a decoded length other than
// 32 bytes is a construction error -- never a per-row failure -- and the
// key value itself never appears in the returned error.
func ResolveDeidentifyKey(envVar string) ([]byte, error) {
	raw, ok := os.LookupEnv(envVar)
	if !ok {
		return nil, fmt.Errorf("environment variable %q (named by key:) is not set", envVar)
	}
	key, err := decodeKey(raw)
	if err != nil {
		return nil, fmt.Errorf("environment variable %q (named by key:) is not valid hex or base64", envVar)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("environment variable %q (named by key:) decodes to %d bytes, want 32", envVar, len(key))
	}
	return key, nil
}

// decodeKey tries hex first, then standard base64 (design/deidentify.md
// §6 accepts either). Hex's alphabet is a strict subset of base64's, so
// trying hex first and falling back is enough to disambiguate every real
// key without asking the schema author to say which they used.
func decodeKey(raw string) ([]byte, error) {
	if key, err := hex.DecodeString(raw); err == nil {
		return key, nil
	}
	return base64.StdEncoding.DecodeString(raw)
}
