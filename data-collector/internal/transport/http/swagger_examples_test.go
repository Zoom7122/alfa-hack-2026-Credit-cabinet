package httptransport_test

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

// swaggerOperation — описание ручки и её параметры из /swagger/doc.json.
type swaggerOperation struct {
	Description string `json:"description"`
	Parameters  []struct {
		Name     string `json:"name"`
		XExample string `json:"x-example"`
	} `json:"parameters"`
}

// swaggerOperations — спецификация, которую отдаёт Swagger UI: путь → метод → ручка.
func swaggerOperations(t *testing.T, app *testApp) map[string]map[string]swaggerOperation {
	t.Helper()
	w := app.get("/swagger/doc.json")
	if w.Code != http.StatusOK {
		t.Fatalf("doc.json: status %d", w.Code)
	}
	var spec struct {
		Paths map[string]map[string]swaggerOperation `json:"paths"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &spec); err != nil {
		t.Fatal(err)
	}
	return spec.Paths
}

// Поле questionnaire в Swagger UI заполнено анкетой сценария A без documents: Execute сразу даёт 202.
func TestSwaggerQuestionnairePrefill(t *testing.T) {
	app := newTestApp(t)
	var prefill string
	for _, p := range swaggerOperations(t, app)["/applications/{id}/questionnaire"]["post"].Parameters {
		if p.Name == "questionnaire" {
			prefill = p.XExample
		}
	}

	var got, want map[string]any
	if err := json.Unmarshal([]byte(prefill), &got); err != nil {
		t.Fatalf("x-example у questionnaire: %v", err)
	}
	if err := json.Unmarshal([]byte(readExample(t, "scenario-a.json")), &want); err != nil {
		t.Fatal(err)
	}
	delete(want, "documents") // тип файла без самого файла — 422
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("x-example у questionnaire разошёлся с examples/scenario-a.json (без documents):\n%s", prefill)
	}

	id := app.register(t, readExample(t, "scenario-a.applicant.json"))
	decodeSubmitted(t, app.submitMultipart(t, id, prefill))
}

// Примеры сценариев в описаниях ручек совпадают с examples/ и проходят проверки.
func TestSwaggerScenarioExamples(t *testing.T) {
	ops := swaggerOperations(t, newTestApp(t))
	register := ops["/applicants"]["post"].Description
	submit := ops["/applications/{id}/questionnaire"]["post"].Description

	for _, sc := range []string{"a", "b", "c", "d", "e"} {
		t.Run(sc, func(t *testing.T) {
			applicant := strings.TrimSpace(readExample(t, "scenario-"+sc+".applicant.json"))
			questionnaire := strings.TrimSpace(readExample(t, "scenario-"+sc+".json"))
			if !strings.Contains(register, applicant) || !strings.Contains(submit, questionnaire) {
				t.Fatal("примеры в Swagger разошлись с examples/: запустите make swagger")
			}

			var files []file
			if sc == "a" {
				files = []file{{"balance.pdf", readExample(t, "files/balance.pdf")},
					{"balance.pdf.sig", readExample(t, "files/balance.pdf.sig")}}
			}
			app := newTestApp(t)
			decodeSubmitted(t, app.submitMultipart(t, app.register(t, applicant), questionnaire, files...))
		})
	}
}
