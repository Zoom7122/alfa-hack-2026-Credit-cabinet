package testdocument

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func TestRecognitionRequiresExactContent(t *testing.T) {
	raw := PDF()
	if !bytesHaveHash(raw) || IsSHA256("") || IsSHA256("test-financial-document.pdf") {
		t.Fatal("sample not recognised by its content")
	}
	raw[len(raw)/2] ^= 1
	if bytesHaveHash(raw) || !bytesHaveHash(PDF()) {
		t.Fatal("modified content recognised or embedded sample changed")
	}
}

func bytesHaveHash(raw []byte) bool {
	sum := sha256.Sum256(raw)
	return IsSHA256(hex.EncodeToString(sum[:]))
}
