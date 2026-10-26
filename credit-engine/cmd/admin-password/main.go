// admin-password reads a password from stdin and prints only its bcrypt hash.
package main

import (
	"bytes"
	"fmt"
	"io"
	"os"

	"golang.org/x/crypto/bcrypt"
)

func main() {
	value, err := io.ReadAll(io.LimitReader(os.Stdin, 75))
	value = bytes.TrimSuffix(value, []byte("\n"))
	value = bytes.TrimSuffix(value, []byte("\r"))
	if err != nil || len(value) < 8 || len(value) > 72 {
		fmt.Fprintln(os.Stderr, "provide an 8..72 byte password on stdin")
		os.Exit(1)
	}
	hash, err := bcrypt.GenerateFromPassword(value, 12)
	for i := range value {
		value[i] = 0
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "password hashing failed")
		os.Exit(1)
	}
	fmt.Println(string(hash))
}
