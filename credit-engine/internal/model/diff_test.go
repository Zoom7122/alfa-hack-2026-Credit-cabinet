package model

import (
	"bytes"
	"reflect"
	"testing"
)

func TestDefinitionComparisonPreservesOrderTypesAndNumericLiterals(t *testing.T) {
	left, err := Decode(bytes.NewReader(fragment(t, "supplier")))
	if err != nil {
		t.Fatal(err)
	}
	data, err := Marshal(left)
	if err != nil {
		t.Fatal(err)
	}
	right, err := Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	identical, err := CompareDefinitions(left, right)
	if err != nil || len(identical.ChangedPaths) != 0 || identical.BeforeSHA256 != identical.AfterSHA256 {
		t.Fatal(identical, err)
	}
	right.Title = "Changed title"
	right.Parameters[0].Enabled = !right.Parameters[0].Enabled
	comparison, err := CompareDefinitions(left, right)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(comparison.ChangedPaths, []string{"/parameters/0/enabled", "/title"}) || comparison.BeforeSHA256 == comparison.AfterSHA256 {
		t.Fatal(comparison)
	}
	after, err := Marshal(left)
	if err != nil || !bytes.Equal(data, after) {
		t.Fatal("comparison changed input")
	}
	for i := range right.Parameters {
		if right.Parameters[i].Number != nil && right.Parameters[i].Number.Lower != nil {
			right.Parameters[i].Number.Lower.Value = "0e0"
			left.Parameters[i].Number.Lower.Value = "0"
			comparison, err = CompareDefinitions(left, right)
			if err != nil || comparison.BeforeSHA256 == comparison.AfterSHA256 {
				t.Fatal("numeric lexeme was normalized", err)
			}
			return
		}
	}
	t.Fatal("fixture must contain a numeric domain")
}
