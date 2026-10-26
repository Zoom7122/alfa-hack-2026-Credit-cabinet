package modelgen_test

import (
	"bytes"
	"encoding/json"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/model"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/modelgen"
)

func baseModels(t *testing.T) []model.ModelDefinition {
	t.Helper()
	result := []model.ModelDefinition{}
	for _, version := range []string{"ip-v2", "legal-v2"} {
		data, err := os.ReadFile("../../models/" + version + ".json")
		if err != nil {
			t.Fatal(err)
		}
		d, err := model.Decode(bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		result = append(result, d)
	}
	return result
}
func TestStaticGenerationMatchesCompiledFilesAndIsDeterministic(t *testing.T) {
	d := baseModels(t)
	out, err := modelgen.GenerateModels(d)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Artifacts) != 3 {
		t.Fatal(len(out.Artifacts))
	}
	for _, artifact := range out.Artifacts {
		data, err := os.ReadFile(filepath.Join("../..", filepath.FromSlash(artifact.Path)))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(data, artifact.Data) {
			t.Fatal("stale compiled artifact", artifact.Path)
		}
	}
	d[0], d[1] = d[1], d[0]
	reordered, err := modelgen.GenerateModels(d)
	if err != nil || !reflect.DeepEqual(out, reordered) {
		t.Fatal("unstable generation", err)
	}
	if _, err := modelgen.GenerateModels(nil); err == nil {
		t.Fatal("empty catalog accepted")
	}
}
func TestStaticDefinitionAndSchemaChecksumsTrackDifferentChanges(t *testing.T) {
	d := fixtures(t)[0]
	before, err := modelgen.DefinitionSHA256(d)
	if err != nil {
		t.Fatal(err)
	}
	schema, err := model.FactsSchemaSHA256(d.Parameters)
	if err != nil {
		t.Fatal(err)
	}
	d.Criteria[0].Cases[0].Points = 4
	after, err := modelgen.DefinitionSHA256(d)
	if err != nil || after == before {
		t.Fatal("rule change not hashed", err)
	}
	same, err := model.FactsSchemaSHA256(d.Parameters)
	if err != nil || same != schema {
		t.Fatal("weights changed wire shape", err)
	}
	index := len(d.Parameters) - 2
	d.Parameters[index].Required = false
	d.Parameters[index].GoName = "ExplicitRUB"
	same, err = model.FactsSchemaSHA256(d.Parameters)
	if err != nil || same != schema {
		t.Fatal("business requiredness/Go spelling changed wire shape", err)
	}
	d.Parameters[index].Number.Lower = &model.Bound{Value: "1", Inclusive: true}
	changed, err := model.FactsSchemaSHA256(d.Parameters)
	if err != nil || changed == schema {
		t.Fatal("domain change not hashed", err)
	}
}

func TestRuleEditChangesCompiledModelAndCatalog(t *testing.T) {
	d := baseModels(t)
	before, err := modelgen.GenerateModels(d)
	if err != nil {
		t.Fatal(err)
	}
	d[0].Criteria[0].Cases[0].Points = 4
	after, err := modelgen.GenerateModels(d)
	if err != nil {
		t.Fatal(err)
	}
	changed := 0
	for i, artifact := range before.Artifacts {
		if artifact.Path != after.Artifacts[i].Path {
			t.Fatal("rule edit renamed artifact")
		}
		if !bytes.Equal(artifact.Data, after.Artifacts[i].Data) {
			changed++
		}
	}
	if changed != 2 {
		t.Fatalf("rule edit should change its Go definition and catalog checksum, changed %d files", changed)
	}
}

// Go's overlay compiles a complete hypothetical release without writing any
// of its files into the working catalog or making demo versions active.
func TestGeneratedCatalogAndNamedFactsCompileTogether(t *testing.T) {
	definitions := append(baseModels(t), fixtures(t)...)
	// Check uncommon literal forms in actual compiled Go, not just source text.
	definitions[2].Criteria[0].Cases[0].Points = math.Copysign(0, -1)
	definitions[3].Parameters[len(definitions[3].Parameters)-2].Number.Lower = &model.Bound{Value: "9007199254740993", Inclusive: true}
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
		if err = os.WriteFile(file, artifact.Data, 0600); err != nil {
			t.Fatal(err)
		}
		overlay[filepath.Join(root, filepath.FromSlash(artifact.Path))] = file
	}
	testSource := `package modelbindings
import("strings";"testing";"time";"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/contracts")
func TestGeneratedCatalogWithFacts(t *testing.T){c,err:=NewDefault();if err!=nil{t.Fatal(err)};if len(c.List())!=5{t.Fatal(c.List())};for _,v:=range []string{"ip-demo-v1","ip-demo-v2","legal-demo-v1"}{b:=contracts.BorrowerTypeSoleProprietor;if strings.HasPrefix(v,"legal"){b=contracts.BorrowerTypeLegalEntity};e,err:=c.Lookup(v,b);if err!=nil{t.Fatal(err)};schema,number:="demo-facts-v1","0";if v=="ip-demo-v2"{schema,number="demo-facts-v2","9007199254740993"};body:="{\"application_id\":\"app\",\"borrower_id\":\"borrower\",\"borrower_type\":\""+string(b)+"\",\"model_version\":\""+v+"\",\"facts_schema_version\":\""+schema+"\",\"facts_date\":\"2026-10-20\",\"additional_facts\":{\"annual_contract_value_rub\":"+number+",\"has_confirmed_contracts\":false}}";expected:=contracts.ScoringFactsExpectation{Request:contracts.ScoringFactsRequest{ApplicationID:"app",Borrower:contracts.ScoringFactsBorrower{ID:"borrower",Type:b},ModelVersion:v,FactsSchemaVersion:schema},Date:time.Date(2026,10,23,0,0,0,0,time.UTC)};facts,err:=e.DecodeFacts(strings.NewReader(body),expected);if err!=nil{t.Fatal(err)};flag,known:=facts.Values["has_confirmed_contracts"].Bool();if !known||flag||facts.Metadata.ModelVersion!=v{t.Fatal(facts)};if v=="ip-demo-v2"{r,ok:=facts.Values["annual_contract_value_rub"].Rational();if !ok||r.RatString()!="9007199254740993"{t.Fatal("int64 rounded")}}}}
`
	testFile := filepath.Join(temp, "overlay_test.go")
	if err = os.WriteFile(testFile, []byte(testSource), 0600); err != nil {
		t.Fatal(err)
	}
	overlay[filepath.Join(root, "internal", "modelbindings", "overlay_facts_generated_test.go")] = testFile
	clientTest, err := os.ReadFile("testdata/facts_client_overlay_test.go")
	if err != nil {
		t.Fatal(err)
	}
	clientFile := filepath.Join(temp, "facts_client_test.go")
	if err = os.WriteFile(clientFile, clientTest, 0600); err != nil {
		t.Fatal(err)
	}
	overlay[filepath.Join(root, "internal", "clients", "generated_facts_overlay_test.go")] = clientFile
	simulationTest, err := os.ReadFile("testdata/simulation_overlay_test.go")
	if err != nil {
		t.Fatal(err)
	}
	simulationFile := filepath.Join(temp, "simulation_test.go")
	if err := os.WriteFile(simulationFile, simulationTest, 0600); err != nil {
		t.Fatal(err)
	}
	overlay[filepath.Join(root, "internal", "simulation", "generated_simulation_overlay_test.go")] = simulationFile
	data, err := json.Marshal(struct{ Replace map[string]string }{overlay})
	if err != nil {
		t.Fatal(err)
	}
	overlayFile := filepath.Join(temp, "overlay.json")
	if err = os.WriteFile(overlayFile, data, 0600); err != nil {
		t.Fatal(err)
	}
	goBinary := "go"
	if runtime.GOOS == "windows" {
		goBinary += ".exe"
	}
	command := exec.Command(filepath.Join(runtime.GOROOT(), "bin", goBinary), "test", "-overlay="+overlayFile, "-count=1", "-run=^TestGenerated(CatalogWithFacts|FactsClientProtocolCorpus|SimulationParity)$", "./internal/modelbindings", "./internal/clients", "./internal/simulation")
	command.Dir = root
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("generated release does not compile/run: %v\n%s", err, output)
	}
	t.Log(string(output))
}
