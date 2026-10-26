// Package modelbindings holds build-time generated definitions and their
// immutable runtime catalog. Evaluation never reads model files or a database.
package modelbindings

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"math"
	"sort"
	"sync"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/contracts"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/model"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/rules"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/sourcecatalog"
)

type Metadata struct {
	Active             bool                   `json:"active,omitempty"`
	ID                 string                 `json:"id"`
	Version            string                 `json:"version"`
	BorrowerType       contracts.BorrowerType `json:"borrower_type"`
	FactsSchemaVersion string                 `json:"facts_schema_version"`
	DefinitionSHA256   string                 `json:"definition_sha256"`
	FactsSchemaSHA256  string                 `json:"facts_schema_sha256"`
	UsesFacts          bool                   `json:"uses_facts"`
	FormatVersion      int                    `json:"format_version"`
}

type RuntimeMetadata struct {
	FormatVersion int        `json:"format_version"`
	Models        []Metadata `json:"models"`
}
type Facts struct {
	Metadata contracts.ScoringFactsMetadata
	Values   map[string]rules.Value
}
type FactsDecoder func(io.Reader, contracts.ScoringFactsExpectation) (Facts, error)
type Registration struct {
	Metadata    Metadata
	Definition  func() model.ModelDefinition
	DecodeFacts FactsDecoder
}
type Entry struct {
	metadata    Metadata
	engine      *rules.Engine
	definition  []byte
	decodeFacts FactsDecoder
}
type Catalog struct {
	entries map[string]*Entry
	ordered []Metadata
}

var ErrUnknownVersion = errors.New("modelbindings: unsupported model version")
var ErrBorrowerType = errors.New("modelbindings: model belongs to another borrower type")
var ErrNoFacts = errors.New("modelbindings: model does not use supplier facts")

var defaultOnce sync.Once
var defaultCatalog *Catalog
var defaultError error

// Default shares the immutable build catalog between runtime evaluators.
func Default() (*Catalog, error) {
	defaultOnce.Do(func() { defaultCatalog, defaultError = NewDefault() })
	return defaultCatalog, defaultError
}

func New(registrations []Registration) (*Catalog, error) {
	c := &Catalog{entries: map[string]*Entry{}, ordered: []Metadata{}}
	schemas := map[string]string{}
	for _, r := range registrations {
		if r.Definition == nil {
			return nil, fmt.Errorf("modelbindings: definition constructor is required")
		}
		d := r.Definition()
		data, err := model.Marshal(d)
		if err != nil {
			return nil, err
		}
		hash := fmt.Sprintf("%x", sha256.Sum256(data))
		usesFacts := false
		for _, p := range d.Parameters {
			usesFacts = usesFacts || p.Enabled && p.Source.Kind == model.Supplier
		}
		m := r.Metadata
		if m.ID != d.ID || m.Version != d.Version || m.BorrowerType != d.BorrowerType || m.FactsSchemaVersion != d.FactsSchemaVersion || m.FormatVersion != d.FormatVersion || m.DefinitionSHA256 != hash || m.UsesFacts != usesFacts {
			return nil, fmt.Errorf("modelbindings: compiled metadata/checksum mismatch for %s", d.Version)
		}
		schemaHash, err := model.FactsSchemaSHA256(d.Parameters)
		if err != nil {
			return nil, err
		}
		if usesFacts != (r.DecodeFacts != nil) || m.FactsSchemaSHA256 != schemaHash {
			return nil, fmt.Errorf("modelbindings: facts binding mismatch for %s", d.Version)
		}
		if usesFacts {
			if previous, exists := schemas[d.FactsSchemaVersion]; exists && previous != schemaHash {
				return nil, fmt.Errorf("modelbindings: incompatible facts schema %s", d.FactsSchemaVersion)
			}
			schemas[d.FactsSchemaVersion] = schemaHash
		}
		if _, exists := c.entries[d.Version]; exists {
			return nil, fmt.Errorf("modelbindings: duplicate model version %s", d.Version)
		}
		bindings, err := sourcecatalog.Bindings(d.BorrowerType)
		if err != nil {
			return nil, err
		}
		engine, err := rules.Compile(d, bindings)
		if err != nil {
			return nil, err
		}
		c.entries[d.Version] = &Entry{metadata: m, engine: engine, definition: append([]byte{}, data...), decodeFacts: r.DecodeFacts}
		c.ordered = append(c.ordered, m)
	}
	sort.Slice(c.ordered, func(i, j int) bool { return c.ordered[i].Version < c.ordered[j].Version })
	return c, nil
}
func (c *Catalog) Lookup(version string, borrower contracts.BorrowerType) (*Entry, error) {
	if c == nil {
		return nil, ErrUnknownVersion
	}
	entry, ok := c.entries[version]
	if !ok {
		return nil, ErrUnknownVersion
	}
	if entry.metadata.BorrowerType != borrower {
		return nil, ErrBorrowerType
	}
	return entry, nil
}
func (c *Catalog) List() []Metadata {
	if c == nil {
		return []Metadata{}
	}
	return append([]Metadata{}, c.ordered...)
}
func LegacyVersion(borrower contracts.BorrowerType) (string, error) {
	switch borrower {
	case contracts.BorrowerTypeSoleProprietor:
		return "ip-v2", nil
	case contracts.BorrowerTypeLegalEntity:
		return "legal-v2", nil
	}
	return "", ErrBorrowerType
}
func (e *Entry) Metadata() Metadata          { return e.metadata }
func (e *Entry) Engine() *rules.Engine       { return e.engine }
func (e *Entry) CanonicalDefinition() []byte { return append([]byte{}, e.definition...) }
func (e *Entry) Definition() (model.ModelDefinition, error) {
	return model.Decode(bytes.NewReader(e.definition))
}
func (e *Entry) DecodeFacts(reader io.Reader, expected contracts.ScoringFactsExpectation) (Facts, error) {
	if e.decodeFacts == nil {
		return Facts{}, ErrNoFacts
	}
	return e.decodeFacts(reader, expected)
}

// Used only by generated Go constructors, including pointer literals and the
// rare negative-zero point value whose IEEE bits cannot be a Go constant.
func modelPointer[T any](value T) *T   { return &value }
func modelFloat64(bits uint64) float64 { return math.Float64frombits(bits) }
