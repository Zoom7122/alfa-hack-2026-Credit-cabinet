// Package modelassets supplies the immutable baseline for administrative import.
// It is not a runtime model loader for MS1.
package modelassets

import _ "embed"

//go:embed releases/2.0.0.zip
var initialRelease []byte

const InitialReleaseSHA256 = "e3db83252137c6a352cc77969fce85ce43d6c4c99da6bf968dd9d034cbbdb1de"

func InitialReleaseArchive() []byte { return append([]byte{}, initialRelease...) }
