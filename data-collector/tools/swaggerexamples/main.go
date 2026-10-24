// Команда swaggerexamples собирает примеры запросов из examples/ в markdown для описаний ручек
// в Swagger (аннотация @Description.markdown). Запускается из make swagger перед swag init.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// scenario — тестовый субъект mock-api (docs/mock-api-guide.md).
type scenario struct {
	id    string // examples/scenario-{id}.applicant.json и examples/scenario-{id}.json
	title string
}

var scenarios = []scenario{
	{"a", "ООО «Северный ветер», ИНН 7799000012, опт продуктов — ожидается OFFERED"},
	{"b", "ООО «Стройресурс», ИНН 7799000020, стройка — OFFERED, но меньше сумма или дольше срок"},
	{"c", "ООО «Альтаир», ИНН 7799000037, опт — REJECTED"},
	{"d", "ИП Смирнова А. В., ИНН 779900010202, интернет-торговля — WAITING_FOR_INFO"},
	{"e", "ООО «Техно-Сервис», ИНН 7799000044, ремонт оборудования — MANUAL_REVIEW"},
}

const (
	applicantsIntro = "**Примеры** — заявители сценариев mock-api A–E (`examples/scenario-X.applicant.json`), " +
		"в теле по умолчанию — сценарий A. Данные в моках привязаны к компании, поэтому анкету берите того же " +
		"сценария: они в описании POST /applications/{id}/questionnaire.\n"
	questionnairesIntro = "**Примеры анкет** — сценарии mock-api A–E (`examples/scenario-X.json`). " +
		"Заявку регистрируйте примером того же сценария из POST /applicants: данные в моках привязаны к компании.\n"
)

func main() {
	examples := flag.String("examples", "examples", "папка с примерами запросов")
	out := flag.String("out", "docs/markdown", "куда положить markdown")
	flag.Parse()

	applicants := bytes.NewBufferString(applicantsIntro)
	questionnaires := bytes.NewBufferString(questionnairesIntro)
	for _, s := range scenarios {
		name := strings.ToUpper(s.id)
		fmt.Fprintf(applicants, "\n**%s** — %s.\n\n```json\n%s\n```\n", name, s.title,
			read(*examples, "scenario-"+s.id+".applicant.json"))

		questionnaire := read(*examples, "scenario-"+s.id+".json")
		fmt.Fprintf(questionnaires, "\n**%s** — %s.%s\n\n```json\n%s\n```\n", name, s.title,
			filesNote(*examples, questionnaire), questionnaire)
	}

	if err := os.MkdirAll(*out, 0o755); err != nil {
		log.Fatal(err)
	}
	write(filepath.Join(*out, "applicants.md"), applicants.Bytes())
	write(filepath.Join(*out, "questionnaire.md"), questionnaires.Bytes())
}

// filesNote — какие файлы приложить, если анкета указывает их тип в documents.
func filesNote(dir, questionnaire string) string {
	var q struct {
		Documents []struct {
			FileName string `json:"file_name"`
		} `json:"documents"`
	}
	if err := json.Unmarshal([]byte(questionnaire), &q); err != nil {
		log.Fatal(err)
	}
	var files []string
	for _, d := range q.Documents {
		f := "`" + d.FileName + "`"
		if _, err := os.Stat(filepath.Join(dir, "files", d.FileName+".sig")); err == nil {
			f += " и подпись `" + d.FileName + ".sig`"
		}
		files = append(files, f)
	}
	if len(files) == 0 {
		return ""
	}
	return " В documents указан тип файла: приложите в files " + strings.Join(files, ", ") +
		" из `examples/files` или уберите documents."
}

func read(dir, name string) string {
	body, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		log.Fatal(err)
	}
	if !json.Valid(body) {
		log.Fatalf("%s: невалидный JSON", name)
	}
	return string(bytes.TrimSpace(body))
}

func write(path string, body []byte) {
	if err := os.WriteFile(path, body, 0o644); err != nil {
		log.Fatal(err)
	}
}
