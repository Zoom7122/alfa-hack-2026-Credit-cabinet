package contracts

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// UnmarshalJSON distinguishes an absent legacy version from explicit null.
// The rest of the existing A envelope keeps its previous decoding behavior.
func (r *DecisionRequest) UnmarshalJSON(data []byte) error {
	type plain DecisionRequest
	decoded := plain(*r)
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return nil
	}
	d := json.NewDecoder(bytes.NewReader(data))
	if _, err := d.Token(); err != nil {
		return err
	}
	seen := false
	for d.More() {
		key, err := d.Token()
		if err != nil {
			return err
		}
		name, ok := key.(string)
		if !ok {
			return fmt.Errorf("decision request: invalid object key")
		}
		var raw json.RawMessage
		if err = d.Decode(&raw); err != nil {
			return err
		}
		if strings.EqualFold(name, "model_version") {
			if name != "model_version" || seen {
				return fmt.Errorf("decision request: ambiguous model_version")
			}
			seen = true
			var version string
			if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) || json.Unmarshal(raw, &version) != nil {
				return fmt.Errorf("decision request: model_version must be a string")
			}
		}
	}
	*r = DecisionRequest(decoded)
	return nil
}
