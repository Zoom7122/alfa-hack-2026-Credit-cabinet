package clients

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/contracts"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/jsonstrict"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/modelbindings"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/release"
)

type ModelsClient struct{ transport *transport }

func NewModels(base string, client *http.Client) (*ModelsClient, error) {
	t, err := newTransport(base, client)
	if err != nil {
		return nil, err
	}
	return &ModelsClient{transport: t}, nil
}

var metadataSHA = regexp.MustCompile(`^[a-f0-9]{64}$`)

func (c *ModelsClient) Models(ctx context.Context) (modelbindings.RuntimeMetadata, error) {
	var out modelbindings.RuntimeMetadata
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.transport.endpoint([]string{"api", "v1", "models"}, nil), nil)
	if err != nil {
		return out, &Error{Status: 502, Code: "SCORING_METADATA_UNAVAILABLE"}
	}
	req.Header.Set("Accept", "application/json")
	client := *c.transport.http
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := client.Do(req)
	if err != nil {
		failure := dependencyError("SCORING_METADATA_UNAVAILABLE", err)
		return out, failure
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return out, &Error{Status: 502, Code: "SCORING_METADATA_UNAVAILABLE"}
	}
	data, err := jsonstrict.Read(response.Body)
	if err != nil {
		var format *jsonstrict.Error
		if errors.As(err, &format) && format.Code == "READ_ERROR" {
			return out, dependencyError("SCORING_METADATA_UNAVAILABLE", err)
		}
		return out, &Error{Status: 502, Code: "INVALID_SCORING_METADATA", cause: err}
	}
	object, err := jsonstrict.Object(data, "", []string{"format_version", "models"}, nil)
	if err != nil || json.Unmarshal(data, &out) != nil || out.FormatVersion != 1 || out.Models == nil || len(out.Models) == 0 {
		return modelbindings.RuntimeMetadata{}, invalidResponse("INVALID_SCORING_METADATA")
	}
	var rows []json.RawMessage
	if json.Unmarshal(object["models"], &rows) != nil {
		return modelbindings.RuntimeMetadata{}, invalidResponse("INVALID_SCORING_METADATA")
	}
	seen := map[string]bool{}
	for i, raw := range rows {
		fields := []string{"id", "version", "borrower_type", "facts_schema_version", "definition_sha256", "facts_schema_sha256", "uses_facts", "format_version"}
		var object map[string]json.RawMessage
		if json.Unmarshal(raw, &object) == nil && object["active"] != nil {
			fields = append(fields, "active")
		}
		_, err := jsonstrict.Object(raw, "/models", fields, nil)
		m := out.Models[i]
		if err != nil || !release.ValidVersion(m.ID) || !release.ValidVersion(m.Version) || seen[m.Version] || m.FormatVersion != 1 ||
			(m.BorrowerType != contracts.BorrowerTypeSoleProprietor && m.BorrowerType != contracts.BorrowerTypeLegalEntity) ||
			!metadataSHA.MatchString(m.DefinitionSHA256) ||
			(m.UsesFacts && (!metadataSHA.MatchString(m.FactsSchemaSHA256) || !release.ValidVersion(m.FactsSchemaVersion))) ||
			(!m.UsesFacts && m.FactsSchemaSHA256 != "") {
			return modelbindings.RuntimeMetadata{}, invalidResponse("INVALID_SCORING_METADATA")
		}
		seen[m.Version] = true
	}
	if err := ctx.Err(); err != nil {
		return modelbindings.RuntimeMetadata{}, dependencyError("SCORING_METADATA_UNAVAILABLE", err)
	}
	return out, nil
}
