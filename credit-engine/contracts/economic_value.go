package contracts

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
)

// ValidateValue rejects ambiguous/nonfinite in-process values before a bank
// operation. Unknown has neither pointer; false has a non-nil BooleanValue.
func (c EconomicCriterion) ValidateValue() error {
	if c.Value != nil && c.BooleanValue != nil {
		return fmt.Errorf("economic criterion: number and boolean are both set")
	}
	if c.Value != nil && (math.IsNaN(*c.Value) || math.IsInf(*c.Value, 0)) {
		return fmt.Errorf("economic criterion: nonfinite number")
	}
	return nil
}
func (c EconomicCriterion) MarshalJSON() ([]byte, error) {
	if err := c.ValidateValue(); err != nil {
		return nil, err
	}
	value := json.RawMessage("null")
	if c.Value != nil {
		data, err := json.Marshal(*c.Value)
		if err != nil {
			return nil, err
		}
		value = data
	}
	if c.BooleanValue != nil {
		data, err := json.Marshal(*c.BooleanValue)
		if err != nil {
			return nil, err
		}
		value = data
	}
	return json.Marshal(struct {
		ID        string          `json:"id"`
		Value     json.RawMessage `json:"value"`
		Points    float64         `json:"points"`
		MaxPoints int             `json:"max_points"`
	}{c.ID, value, c.Points, c.MaxPoints})
}
func (c *EconomicCriterion) UnmarshalJSON(data []byte) error {
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return nil
	}
	// Keep the previous decoder's partial-update/extra-field behavior; only
	// the value union changes. Commit after successful decoding of every field.
	wire := struct {
		ID        string          `json:"id"`
		Value     json.RawMessage `json:"value"`
		Points    float64         `json:"points"`
		MaxPoints int             `json:"max_points"`
	}{ID: c.ID, Points: c.Points, MaxPoints: c.MaxPoints}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	decoded := EconomicCriterion{ID: wire.ID, Value: c.Value, BooleanValue: c.BooleanValue, Points: wire.Points, MaxPoints: wire.MaxPoints}
	if len(wire.Value) > 0 {
		decoded.Value, decoded.BooleanValue = nil, nil
		raw := bytes.TrimSpace(wire.Value)
		if !bytes.Equal(raw, []byte("null")) {
			if bytes.Equal(raw, []byte("true")) || bytes.Equal(raw, []byte("false")) {
				var value bool
				if err := json.Unmarshal(raw, &value); err != nil {
					return err
				}
				decoded.BooleanValue = &value
			} else {
				var value float64
				if err := json.Unmarshal(raw, &value); err != nil {
					return fmt.Errorf("economic criterion value: expected number, boolean or null: %w", err)
				}
				decoded.Value = &value
			}
		}
	}
	if err := decoded.ValidateValue(); err != nil {
		return err
	}
	*c = decoded
	return nil
}
