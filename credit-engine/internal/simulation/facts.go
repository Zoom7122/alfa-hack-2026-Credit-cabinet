package simulation

import (
	"encoding/json"
	"strings"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/jsonstrict"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/model"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/rules"
)

func bound(value *model.Bound) *jsonstrict.Bound {
	if value == nil {
		return nil
	}
	return &jsonstrict.Bound{Value: string(value.Value), Inclusive: value.Inclusive}
}

// The simulation test set is editor input, not a provider response. Omitted or
// null names represent unknown facts. All names must belong to the selected
// model(s); each run applies its own named field type/domain exactly as its DTO.
func decodeFacts(definition model.ModelDefinition, allowed []model.ModelDefinition, raw json.RawMessage) (map[string]rules.Value, error) {
	members := map[string]json.RawMessage{}
	if len(raw) > 0 && !jsonstrict.Null(raw) {
		if err := jsonstrict.Check(raw); err != nil {
			return nil, invalid("INVALID_ADDITIONAL_FACTS", "/additional_facts")
		}
		if err := json.Unmarshal(raw, &members); err != nil || members == nil {
			return nil, invalid("INVALID_ADDITIONAL_FACTS", "/additional_facts")
		}
	}
	names := map[string]bool{}
	for _, d := range allowed {
		for _, p := range d.Parameters {
			if p.Enabled && p.Source.Kind == model.Supplier {
				names[p.JSONName] = true
			}
		}
	}
	for name := range members {
		if !names[name] {
			return nil, invalid("UNKNOWN_ADDITIONAL_FACT", "/additional_facts/"+strings.ReplaceAll(strings.ReplaceAll(name, "~", "~0"), "/", "~1"))
		}
	}
	values := map[string]rules.Value{}
	for _, p := range definition.Parameters {
		if !p.Enabled || p.Source.Kind != model.Supplier {
			continue
		}
		raw, exists := members[p.JSONName]
		if !exists || jsonstrict.Null(raw) {
			values[p.Source.Path] = rules.Unknown()
			continue
		}
		path := "/additional_facts/" + p.JSONName
		switch p.Type {
		case model.Boolean:
			var value bool
			if json.Unmarshal(raw, &value) != nil {
				return nil, invalid("INVALID_ADDITIONAL_FACT_TYPE", path)
			}
			values[p.Source.Path] = rules.Boolean(value)
		case model.Number:
			if p.Number.Integer {
				value, err := jsonstrict.Int64(raw, path, bound(p.Number.Lower), bound(p.Number.Upper))
				if err != nil {
					return nil, invalid("INVALID_ADDITIONAL_FACT_NUMBER", path)
				}
				values[p.Source.Path] = rules.Int64(value)
			} else {
				value, err := jsonstrict.Float64(raw, path, bound(p.Number.Lower), bound(p.Number.Upper))
				if err != nil {
					return nil, invalid("INVALID_ADDITIONAL_FACT_NUMBER", path)
				}
				values[p.Source.Path] = rules.Float64(value)
			}
		default:
			return nil, invalid("UNSUPPORTED_ADDITIONAL_FACT_TYPE", path)
		}
	}
	return values, nil
}
