package runtimehash

import "testing"

func TestEmbeddedJSONOrderAndPrecision(t *testing.T) {
	a, err := Digest([]byte(`{"snapshot":{"z":9007199254740993,"a":null}}`))
	if err != nil {
		t.Fatal(err)
	}
	b, err := Digest([]byte(`{ "snapshot": { "a": null, "z": 9007199254740993 } }`))
	if err != nil || a != b {
		t.Fatal("JSON formatting changed identity", err)
	}
	c, err := Digest([]byte(`{"snapshot":{"z":9007199254740992,"a":null}}`))
	if err != nil || a == c {
		t.Fatal("large integer rounded", err)
	}
}
