package value

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"strconv"
)

// deidentifyPresent and deidentifyAbsent are the presence byte prefixed
// to every plaintext before encryption, so that an absent cell and a
// genuinely empty one both produce full-length ciphertext -- neither is
// visible without the key.
const (
	deidentifyPresent byte = 0x01
	deidentifyAbsent  byte = 0x00
)

// coerceDeidentified parses the cell as its declared inner type first
// (so bad data still fails, and fails before encryption), then encrypts
// its presence-byte-prefixed canonical form. inner is never itself
// Deidentified -- Coerce's own Deidentified case is the only caller.
//
// A failure from the inner parse is rebuilt without raw, since a
// @deidentify field's reason must not embed the offending cell value.
// That's why this doesn't just return the recursive call's own Failure
// unchanged.
func coerceDeidentified(inner Type, raw, dateFormat string, key []byte) (any, *Failure) {
	v, fail := Coerce(inner, raw, dateFormat, nil)
	if fail != nil {
		return nil, &Failure{Reason: fmt.Sprintf("cannot parse cell as %s", inner.Kind)}
	}

	var plaintext []byte
	if _, absent := v.(Absent); absent {
		plaintext = []byte{deidentifyAbsent}
	} else {
		plaintext = append([]byte{deidentifyPresent}, []byte(canonicalString(v))...)
	}

	ciphertext, err := encryptDeidentifiedCell(plaintext, key)
	if err != nil {
		return nil, &Failure{Reason: fmt.Sprintf("cannot encrypt cell: %v", err)}
	}
	return ciphertext, nil
}

// canonicalString renders one of Coerce's own successful return values
// back to the string that gets encrypted. Every struct-backed Kind
// (Date, Decimal, DateTime) already implements String() for exactly this
// "Go's default doesn't match Sift's semantics" reason, so a single
// fmt.Stringer case covers all three uniformly.
func canonicalString(v any) string {
	switch v := v.(type) {
	case string:
		return v
	case int:
		return strconv.Itoa(v)
	case float64:
		return strconv.FormatFloat(v, 'g', -1, 64)
	case bool:
		return strconv.FormatBool(v)
	case fmt.Stringer:
		return v.String()
	default:
		panic(fmt.Sprintf("value: canonicalString: unhandled type %T", v))
	}
}

// encryptDeidentifiedCell applies the cipher: AES-256-GCM with a fresh
// random 96-bit nonce per value, encoded as
// base64(nonce ‖ ciphertext ‖ auth tag). key is validated to be exactly
// 32 bytes before any row is read (format.ResolveDeidentifyKey), so a
// failure here only ever means crypto/rand couldn't supply a nonce.
func encryptDeidentifiedCell(plaintext, key []byte) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nonce, nonce, plaintext, nil)
	return base64.StdEncoding.EncodeToString(sealed), nil
}
