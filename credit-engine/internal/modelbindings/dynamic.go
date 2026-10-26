package modelbindings

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/contracts"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/jsonstrict"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/model"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/rules"
)

// Provider resolves one immutable snapshot per decision, never per criterion.
type Provider interface {
	Resolve(context.Context, string, contracts.BorrowerType) (*Entry, error)
	Models(context.Context) (RuntimeMetadata, error)
}

func (c *Catalog) Resolve(_ context.Context, version string, borrower contracts.BorrowerType) (*Entry, error) {
	if version == "" {
		var err error
		version, err = LegacyVersion(borrower)
		if err != nil {
			return nil, err
		}
	}
	return c.Lookup(version, borrower)
}
func (c *Catalog) Models(context.Context) (RuntimeMetadata, error) {
	return RuntimeMetadata{FormatVersion: 1, Models: c.List()}, nil
}

func DefinitionMetadata(d model.ModelDefinition) (Metadata, error) {
	data, err := model.Marshal(d)
	if err != nil {
		return Metadata{}, err
	}
	schema, err := model.FactsSchemaSHA256(d.Parameters)
	if err != nil {
		return Metadata{}, err
	}
	return Metadata{ID: d.ID, Version: d.Version, BorrowerType: d.BorrowerType, FactsSchemaVersion: d.FactsSchemaVersion, DefinitionSHA256: fmt.Sprintf("%x", sha256.Sum256(data)), FactsSchemaSHA256: schema, UsesFacts: schema != "", FormatVersion: d.FormatVersion}, nil
}

// FromDefinition uses the same validation and rule compiler as generated models.
// Supplier fields are decoded from the selected JSON schema, without codegen.
func FromDefinition(d model.ModelDefinition) (*Entry, error) {
	data, err := model.Marshal(d)
	if err != nil {
		return nil, err
	}
	d, err = model.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	m, err := DefinitionMetadata(d)
	if err != nil {
		return nil, err
	}
	var decoder FactsDecoder
	if m.UsesFacts {
		decoder = func(reader io.Reader, expected contracts.ScoringFactsExpectation) (Facts, error) {
			return decodeDynamicFacts(reader, expected, d)
		}
	}
	c, err := New([]Registration{{Metadata: m, Definition: func() model.ModelDefinition { return d }, DecodeFacts: decoder}})
	if err != nil {
		return nil, err
	}
	return c.Lookup(d.Version, d.BorrowerType)
}

func decodeDynamicFacts(reader io.Reader, expected contracts.ScoringFactsExpectation, d model.ModelDefinition) (Facts, error) {
	result := Facts{Values: map[string]rules.Value{}}
	fields := []string{}
	for _, p := range d.Parameters {
		if p.Enabled && p.Source.Kind == model.Supplier {
			fields = append(fields, p.JSONName)
		}
	}
	data, err := jsonstrict.Read(reader)
	if err != nil {
		return result, err
	}
	object, err := jsonstrict.Object(data, "/", []string{"application_id", "borrower_id", "borrower_type", "model_version", "facts_schema_version", "facts_date", "additional_facts"}, nil)
	if err != nil {
		return result, err
	}
	metadata, err := contracts.ValidateScoringFactsMetadata(object, expected, d.Version, d.FactsSchemaVersion, d.BorrowerType)
	if err != nil {
		return result, err
	}
	result.Metadata = metadata
	facts, err := jsonstrict.Object(object["additional_facts"], "/additional_facts", fields, fields)
	if err != nil {
		return result, err
	}
	bound := func(b *model.Bound) *jsonstrict.Bound {
		if b == nil {
			return nil
		}
		return &jsonstrict.Bound{Value: string(b.Value), Inclusive: b.Inclusive}
	}
	for _, p := range d.Parameters {
		if !p.Enabled || p.Source.Kind != model.Supplier {
			continue
		}
		raw := facts[p.JSONName]
		path := "/additional_facts/" + p.JSONName
		if jsonstrict.Null(raw) {
			result.Values[p.Source.Path] = rules.Unknown()
			continue
		}
		switch p.Type {
		case model.Boolean:
			value, err := jsonstrict.Boolean(raw, path)
			if err != nil {
				return result, err
			}
			result.Values[p.Source.Path] = rules.Boolean(value)
		case model.Number:
			if p.Number.Integer {
				v, err := jsonstrict.Int64(raw, path, bound(p.Number.Lower), bound(p.Number.Upper))
				if err != nil {
					return result, err
				}
				result.Values[p.Source.Path] = rules.Int64(v)
			} else {
				v, err := jsonstrict.Float64(raw, path, bound(p.Number.Lower), bound(p.Number.Upper))
				if err != nil {
					return result, err
				}
				result.Values[p.Source.Path] = rules.Float64(v)
			}
		default:
			return result, fmt.Errorf("unsupported supplier value type")
		}
	}
	return result, nil
}
