// Package testdocument identifies the exact synthetic PDF shipped with the UI.
// Names and client-supplied flags never grant an exemption to another document.
package testdocument

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
)

//go:embed test-financial-document.pdf
var pdf []byte

var digest = sha256.Sum256(pdf)

// SHA256 is calculated from the embedded, immutable sample rather than its name.
var SHA256 = hex.EncodeToString(digest[:])

const Notice = "Тестовый документ: принят без проверки, данные не участвуют в скоринге"

func IsSHA256(value string) bool { return value == SHA256 }

// PDF returns an independent copy so callers cannot change the recognised sample.
func PDF() []byte { return bytes.Clone(pdf) }
