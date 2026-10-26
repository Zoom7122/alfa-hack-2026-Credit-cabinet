// Package runtimehash canonicalizes embedded JSON before comparing idempotent requests.
package runtimehash

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

func Digest(raw []byte) (string, error) {
	var value any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return "", err
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:]), nil
}
