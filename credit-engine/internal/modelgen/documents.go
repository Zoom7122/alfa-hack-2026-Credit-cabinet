package modelgen

import (
	"bytes"
	"fmt"
	"regexp"
	"sort"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/model"
)

const DocumentsFormatVersion = "1"
const ModelsMarkdownStart = "<!-- modelgen:models:start -->"
const ModelsMarkdownEnd = "<!-- modelgen:models:end -->"

type DocumentOptions struct {
	ReleaseVersion string
	InputsMarkdown []byte
}
type DocumentsOutput struct{ Artifacts []Artifact }

// GenerateDocuments is pure. It validates exactly the same model definitions
// as Go generation and returns only fixed release artifact paths.
func GenerateDocuments(definitions []model.ModelDefinition, options DocumentOptions) (DocumentsOutput, error) {
	if len(definitions) == 0 || !regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`).MatchString(options.ReleaseVersion) {
		return DocumentsOutput{}, fmt.Errorf("modelgen: models and a valid release version are required")
	}
	facts, err := GenerateFactsDTOs(definitions, FactsOptions{})
	if err != nil {
		return DocumentsOutput{}, err
	}
	models := append([]model.ModelDefinition(nil), definitions...)
	sort.Slice(models, func(i, j int) bool { return models[i].Version < models[j].Version })
	main, supplier, err := generateOpenAPI(models, facts, options.ReleaseVersion)
	if err != nil {
		return DocumentsOutput{}, err
	}
	tables, err := modelsMarkdown(models, options.ReleaseVersion)
	if err != nil {
		return DocumentsOutput{}, err
	}
	markdown, err := UpdateModelsMarkdown(options.InputsMarkdown, tables)
	if err != nil {
		return DocumentsOutput{}, err
	}
	return DocumentsOutput{Artifacts: []Artifact{{Path: "contracts/openapi.json", Data: main}, {Path: "contracts/scoring_facts.openapi.json", Data: supplier}, {Path: "docs/02_inputs_and_scoring.md", Data: markdown}}}, nil
}

// UpdateModelsMarkdown replaces exactly one bounded section, or appends it on
// first generation. Every byte outside the markers is retained. Ambiguous or
// damaged markers fail closed so a generation cannot erase handwritten text.
func UpdateModelsMarkdown(original, generated []byte) ([]byte, error) {
	start, end := []byte(ModelsMarkdownStart), []byte(ModelsMarkdownEnd)
	starts, ends := bytes.Count(original, start), bytes.Count(original, end)
	newline := []byte("\n")
	if bytes.Contains(original, []byte("\r\n")) {
		newline = []byte("\r\n")
	}
	content := bytes.ReplaceAll(generated, []byte("\r\n"), []byte("\n"))
	content = bytes.Trim(content, "\n")
	if bytes.Contains(content, start) || bytes.Contains(content, end) {
		return nil, fmt.Errorf("modelgen: generated content contains managed markers")
	}
	if len(newline) == 2 {
		content = bytes.ReplaceAll(content, []byte("\n"), newline)
	}
	var out bytes.Buffer
	if starts == 0 && ends == 0 {
		out.Write(original)
		if len(original) > 0 {
			if !bytes.HasSuffix(original, []byte("\n")) {
				out.Write(newline)
			}
			out.Write(newline)
		}
		out.Write(start)
		out.Write(newline)
		out.Write(content)
		out.Write(newline)
		out.Write(end)
		out.Write(newline)
		return out.Bytes(), nil
	}
	if starts != 1 || ends != 1 {
		return nil, fmt.Errorf("modelgen: expected exactly one pair of managed Markdown markers")
	}
	left, right := bytes.Index(original, start), bytes.Index(original, end)
	wholeLine := func(index, size int) bool {
		before := index == 0 || original[index-1] == '\n'
		after := index+size == len(original) || original[index+size] == '\n' || bytes.HasPrefix(original[index+size:], []byte("\r\n"))
		return before && after
	}
	if right <= left || !wholeLine(left, len(start)) || !wholeLine(right, len(end)) {
		return nil, fmt.Errorf("modelgen: unordered or inline managed Markdown markers")
	}
	out.Write(original[:left+len(start)])
	out.Write(newline)
	out.Write(content)
	out.Write(newline)
	out.Write(original[right:])
	return out.Bytes(), nil
}
