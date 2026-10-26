package modelgen_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/model"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/modelgen"
)

// Compile an entire release and execute the production app wiring. The overlay
// adds no active demonstration model to the working tree or runtime catalog.
func TestGeneratedModelsThroughFullHTTPPipeline(t *testing.T) {
	definitions := append(baseModels(t), fixtures(t)...)
	stopModel := fixtures(t)[2]
	stopModel.Version = "legal-facts-stop-v1"
	flag := false
	stopModel.Stops = append(stopModel.Stops, model.Rule{ID: "supplier_stop", Enabled: true, Title: "Test supplier stop", When: model.Condition{Op: model.EQ, Parameter: "has_confirmed_contracts", Value: &model.Literal{Boolean: &flag}}, Reason: model.Reason{Code: "SUPPLIER_STOP", Message: "Test stop", Fields: []string{"additional_facts.has_confirmed_contracts"}}})
	definitions = append(definitions, stopModel)
	for i := 2; i < len(definitions); i++ {
		for _, parameter := range []string{"has_confirmed_contracts", "annual_contract_value_rub"} {
			definitions[i].Criteria = append(definitions[i].Criteria, model.Criterion{ID: "TEST_" + parameter, Enabled: true, Title: "Test named fact", Block: model.Additional, ValueParameter: parameter, MaxPoints: 0, UnknownAction: model.Zero, SpecialCases: []model.ScoreCase{}, Cases: []model.ScoreCase{{ID: "known", When: model.Condition{Op: model.Always}, Points: 0}}})
		}
	}
	result, err := modelgen.GenerateModels(definitions)
	if err != nil {
		t.Fatal(err)
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	temp := t.TempDir()
	overlay := map[string]string{}
	for _, artifact := range result.Artifacts {
		file := filepath.Join(temp, strings.ReplaceAll(artifact.Path, "/", "_"))
		if err := os.WriteFile(file, artifact.Data, 0600); err != nil {
			t.Fatal(err)
		}
		overlay[filepath.Join(root, filepath.FromSlash(artifact.Path))] = file
	}
	source, err := os.ReadFile("testdata/facts_pipeline_overlay_test.go")
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(temp, "facts_pipeline_test.go")
	if err := os.WriteFile(file, source, 0600); err != nil {
		t.Fatal(err)
	}
	overlay[filepath.Join(root, "internal", "app", "generated_facts_pipeline_test.go")] = file
	docSource, err := os.ReadFile("testdata/documents_overlay_test.go")
	if err != nil {
		t.Fatal(err)
	}
	docFile := filepath.Join(temp, "documents_test.go")
	if err := os.WriteFile(docFile, docSource, 0600); err != nil {
		t.Fatal(err)
	}
	overlay[filepath.Join(root, "internal", "app", "generated_facts_documents_test.go")] = docFile
	data, err := json.Marshal(struct{ Replace map[string]string }{overlay})
	if err != nil {
		t.Fatal(err)
	}
	overlayFile := filepath.Join(temp, "overlay.json")
	if err := os.WriteFile(overlayFile, data, 0600); err != nil {
		t.Fatal(err)
	}
	goBinary := "go"
	if runtime.GOOS == "windows" {
		goBinary += ".exe"
	}
	command := exec.Command(filepath.Join(runtime.GOROOT(), "bin", goBinary), "test", "-overlay="+overlayFile, "-count=1", "-v", "-run=^TestGeneratedFactsPipeline", "./internal/app")
	command.Dir = root
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("generated full pipeline: %v\n%s", err, output)
	}
	t.Log(string(output))
}
