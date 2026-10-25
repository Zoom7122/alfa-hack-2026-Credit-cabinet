package model

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

type DefinitionComparison struct {
	BeforeSHA256 string   `json:"before_sha256"`
	AfterSHA256  string   `json:"after_sha256"`
	ChangedPaths []string `json:"changed_paths"`
}

// CompareDefinitions compares canonical documents without normalizing numeric
// literals or reordering rules. Paths use JSON Pointer escaping and array order.
func CompareDefinitions(before, after ModelDefinition) (DefinitionComparison, error) {
	left, err := Marshal(before)
	if err != nil {
		return DefinitionComparison{}, err
	}
	right, err := Marshal(after)
	if err != nil {
		return DefinitionComparison{}, err
	}
	decode := func(data []byte) (any, error) {
		var value any
		d := json.NewDecoder(bytes.NewReader(data))
		d.UseNumber()
		err := d.Decode(&value)
		return value, err
	}
	a, err := decode(left)
	if err != nil {
		return DefinitionComparison{}, err
	}
	b, err := decode(right)
	if err != nil {
		return DefinitionComparison{}, err
	}
	result := DefinitionComparison{BeforeSHA256: fmt.Sprintf("%x", sha256.Sum256(left)), AfterSHA256: fmt.Sprintf("%x", sha256.Sum256(right)), ChangedPaths: []string{}}
	var walk func(any, any, string)
	walk = func(left, right any, path string) {
		if reflect.DeepEqual(left, right) {
			return
		}
		if old, ok := left.(map[string]any); ok {
			if next, ok := right.(map[string]any); ok {
				keys := map[string]bool{}
				for key := range old {
					keys[key] = true
				}
				for key := range next {
					keys[key] = true
				}
				for key := range keys {
					child := path + "/" + strings.ReplaceAll(strings.ReplaceAll(key, "~", "~0"), "/", "~1")
					l, lOK := old[key]
					r, rOK := next[key]
					if lOK != rOK {
						result.ChangedPaths = append(result.ChangedPaths, child)
					} else {
						walk(l, r, child)
					}
				}
				return
			}
		}
		if old, ok := left.([]any); ok {
			if next, ok := right.([]any); ok {
				for i := range max(len(old), len(next)) {
					child := path + "/" + strconv.Itoa(i)
					if i >= len(old) || i >= len(next) {
						result.ChangedPaths = append(result.ChangedPaths, child)
					} else {
						walk(old[i], next[i], child)
					}
				}
				return
			}
		}
		if path == "" {
			path = "/"
		}
		result.ChangedPaths = append(result.ChangedPaths, path)
	}
	walk(a, b, "")
	sort.Strings(result.ChangedPaths)
	return result, nil
}
