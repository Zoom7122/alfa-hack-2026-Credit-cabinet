package modelbindings

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/contracts"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/model"
)

func TestCompiledDefinitionsMatchCanonicalSources(t *testing.T) {
	catalog, err := NewDefault()
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.List()) != 2 {
		t.Fatal(catalog.List())
	}
	for _, metadata := range catalog.List() {
		t.Run(metadata.Version, func(t *testing.T) {
			entry, err := catalog.Lookup(metadata.Version, metadata.BorrowerType)
			if err != nil {
				t.Fatal(err)
			}
			source, err := os.ReadFile("../../models/" + metadata.Version + ".json")
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(source, entry.CanonicalDefinition()) {
				t.Fatal("compiled source differs")
			}
			if metadata.DefinitionSHA256 != fmt.Sprintf("%x", sha256.Sum256(source)) {
				t.Fatal("definition checksum")
			}
			if metadata.UsesFacts || metadata.FactsSchemaSHA256 != "" || metadata.FactsSchemaVersion != "" {
				t.Fatal("v2 gained supplier dependency")
			}
			if _, err := entry.DecodeFacts(nil, contracts.ScoringFactsExpectation{}); !errors.Is(err, ErrNoFacts) {
				t.Fatal(err)
			}
			if entry.Engine().Version() != metadata.Version || entry.Engine().BorrowerType() != metadata.BorrowerType {
				t.Fatal("engine binding")
			}
		})
	}
	if _, err := catalog.Lookup("missing", contracts.BorrowerTypeSoleProprietor); !errors.Is(err, ErrUnknownVersion) {
		t.Fatal(err)
	}
	if _, err := catalog.Lookup("legal-v2", contracts.BorrowerTypeSoleProprietor); !errors.Is(err, ErrBorrowerType) {
		t.Fatal(err)
	}
	for _, b := range []contracts.BorrowerType{contracts.BorrowerTypeSoleProprietor, contracts.BorrowerTypeLegalEntity} {
		version, err := LegacyVersion(b)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = catalog.Lookup(version, b); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := LegacyVersion("unsupported"); !errors.Is(err, ErrBorrowerType) {
		t.Fatal(err)
	}
}
func TestCatalogOwnsDefinitionsMetadataAndConcurrentLookup(t *testing.T) {
	c, err := NewDefault()
	if err != nil {
		t.Fatal(err)
	}
	list := c.List()
	list[0].Version = "changed"
	entry, err := c.Lookup("ip-v2", contracts.BorrowerTypeSoleProprietor)
	if err != nil {
		t.Fatal(err)
	}
	raw := entry.CanonicalDefinition()
	raw[0] = '['
	d, err := entry.Definition()
	if err != nil {
		t.Fatal(err)
	}
	d.Parameters[0].Title = "changed"
	d.Criteria[0].Cases[0].Points = -1
	d2, err := entry.Definition()
	if err != nil || d2.Parameters[0].Title == "changed" || d2.Criteria[0].Cases[0].Points < 0 {
		t.Fatal("definition alias", err)
	}
	registrations := generatedRegistrations()
	a := registrations[0].Definition()
	a.Criteria[0].Cases[0].Points = -1
	b := registrations[0].Definition()
	if b.Criteria[0].Cases[0].Points < 0 {
		t.Fatal("constructor alias")
	}
	var wg sync.WaitGroup
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for _, metadata := range c.List() {
				e, err := c.Lookup(metadata.Version, metadata.BorrowerType)
				if err != nil || e.Metadata() != metadata {
					t.Error("concurrent lookup", err)
				}
			}
		}()
	}
	wg.Wait()
}
func TestCatalogRejectsTamperedRegistrations(t *testing.T) {
	for _, test := range []struct {
		name string
		edit func([]Registration) []Registration
	}{
		{"nil_definition", func(r []Registration) []Registration { r[0].Definition = nil; return r }},
		{"checksum", func(r []Registration) []Registration {
			r[0].Metadata.DefinitionSHA256 = strings.Repeat("0", 64)
			return r
		}},
		{"version", func(r []Registration) []Registration { r[0].Metadata.Version = "changed"; return r }},
		{"facts_hash", func(r []Registration) []Registration {
			r[0].Metadata.FactsSchemaSHA256 = strings.Repeat("0", 64)
			return r
		}},
		{"duplicate", func(r []Registration) []Registration { return append(r, r[0]) }},
		{"constructor_drift", func(r []Registration) []Registration {
			original := r[0].Definition
			r[0].Definition = func() model.ModelDefinition { d := original(); d.Criteria[0].Cases[0].Points = 1; return d }
			return r
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := New(test.edit(generatedRegistrations())); err == nil {
				t.Fatal("invalid registration accepted")
			}
		})
	}
}
