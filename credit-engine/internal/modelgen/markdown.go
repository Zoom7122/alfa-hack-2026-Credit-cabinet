package modelgen

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html"
	"strconv"
	"strings"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/model"
)

func markdownText(value string) string {
	value = html.EscapeString(value)
	return strings.NewReplacer("\\", "&#92;", "|", "&#124;", "`", "&#96;", "*", "&#42;", "_", "&#95;", "[", "&#91;", "]", "&#93;", "\r\n", "<br>", "\n", "<br>", "\r", "<br>").Replace(value)
}
func tableRow(out *bytes.Buffer, values ...string) {
	out.WriteString("| ")
	for i, value := range values {
		if i > 0 {
			out.WriteString(" | ")
		}
		out.WriteString(markdownText(value))
	}
	out.WriteString(" |\n")
}
func tableHeader(out *bytes.Buffer, values ...string) {
	tableRow(out, values...)
	out.WriteString("|")
	for range values {
		out.WriteString("---|")
	}
	out.WriteString("\n")
}
func compactJSON(value any) string { data, _ := json.Marshal(value); return string(data) }
func conditionText(c *model.Condition) string {
	if c == nil {
		return "ALWAYS"
	}
	switch c.Op {
	case model.All, model.Any, model.Not:
		parts := []string{}
		for _, child := range c.Conditions {
			parts = append(parts, conditionText(&child))
		}
		return string(c.Op) + "(" + strings.Join(parts, ", ") + ")"
	case model.Always, model.Never:
		return string(c.Op)
	case model.Known, model.Unknown:
		return string(c.Op) + "(" + c.Parameter + ")"
	default:
		right := c.OtherParameter
		if c.Value != nil {
			right = literalText(*c.Value)
		}
		if c.Values != nil {
			parts := []string{}
			for _, v := range c.Values {
				parts = append(parts, literalText(v))
			}
			right = "[" + strings.Join(parts, ", ") + "]"
		}
		return c.Parameter + " " + string(c.Op) + " " + right
	}
}
func literalText(v model.Literal) string {
	if v.Number != nil {
		return v.Number.String()
	}
	if v.Boolean != nil {
		return strconv.FormatBool(*v.Boolean)
	}
	if v.String != nil {
		return compactJSON(*v.String)
	}
	return "null"
}
func parameterDomain(p model.Parameter) string {
	if p.Number != nil {
		kind := "number"
		if p.Number.Integer {
			kind = "integer/int64"
		}
		lower, upper, left, right := "−∞", "+∞", "(", ")"
		if bound := p.Number.Lower; bound != nil {
			lower = bound.Value.String()
			if bound.Inclusive {
				left = "["
			}
		}
		if bound := p.Number.Upper; bound != nil {
			upper = bound.Value.String()
			if bound.Inclusive {
				right = "]"
			}
		}
		return kind + " " + left + lower + ", " + upper + right
	}
	if p.Type == model.Boolean {
		return "true / false"
	}
	return strings.Join(p.StringValues, ", ")
}
func modelsMarkdown(definitions []model.ModelDefinition, release string) ([]byte, error) {
	var out bytes.Buffer
	fmt.Fprintln(&out, "## 9. Модели комплекта релиза")
	fmt.Fprintf(&out, "\nРелиз: %s. Формат генератора документов: %s.\n\n", markdownText(release), DocumentsFormatVersion)
	fmt.Fprintln(&out, "Таблицы этого раздела получены из определений моделей. Разделы 1–8 сохраняют исходную методику v2. Новые модели действуют после генерации, сборки и развёртывания. Максимумы активных критериев каждой модели: 750 + 250 = 1000. Порядок: подтверждённые стопы → полнота → D → ручные основания → подбор и банк.")
	for _, d := range definitions {
		hash, err := DefinitionSHA256(d)
		if err != nil {
			return nil, err
		}
		schema, err := model.FactsSchemaSHA256(d.Parameters)
		if err != nil {
			return nil, err
		}
		fmt.Fprintf(&out, "\n### %s — %s\n\n", markdownText(d.Version), markdownText(d.Title))
		tableHeader(&out, "Свойство", "Значение")
		for _, row := range [][2]string{{"ID", d.ID}, {"Тип заёмщика", string(d.BorrowerType)}, {"Версия модели", d.Version}, {"Версия схемы поставщика", d.FactsSchemaVersion}, {"SHA256 определения", hash}, {"SHA256 схемы поставщика", schema}, {"Описание", d.Description}} {
			tableRow(&out, row[0], row[1])
		}
		fmt.Fprint(&out, "\n#### Параметры и обязательность\n\n")
		tableHeader(&out, "ID / название", "Активен", "Источник / JSON", "Тип / единица / область", "Обязателен / условие", "Зависимости")
		for _, p := range d.Parameters {
			required := strconv.FormatBool(p.Required)
			if p.RequiredWhen != nil {
				required += "; " + conditionText(p.RequiredWhen)
			}
			tableRow(&out, p.ID+" / "+p.Title, strconv.FormatBool(p.Enabled), string(p.Source.Kind)+":"+p.Source.Path+" / "+p.JSONName, string(p.Type)+" / "+string(p.Unit)+" / "+parameterDomain(p), required, strings.Join(p.DependsOn, ", "))
		}
		fmt.Fprint(&out, "\n#### Именованные дополнительные факты поставщика\n\n")
		usesSupplier := false
		for _, p := range d.Parameters {
			usesSupplier = usesSupplier || p.Enabled && p.Source.Kind == model.Supplier
		}
		if usesSupplier {
			fmt.Fprint(&out, "Все включённые ключи обязательны в JSON и допускают null. Бизнес-обязательность проверяется отдельно: null не заменяется 0/false. Новые факты запрашивает МС1; в заявку А они не добавляются.\n\n")
		} else {
			fmt.Fprint(&out, "Эта модель не использует дополнительные факты поставщика; запрос не выполняется.\n\n")
		}
		tableHeader(&out, "JSON / Go-поле", "Тип", "Единица / область", "Бизнес-обязательность", "Описание")
		for _, p := range d.Parameters {
			if !p.Enabled || p.Source.Kind != model.Supplier {
				continue
			}
			tableRow(&out, p.JSONName+" / "+model.GeneratedGoName(p), fieldType(p)+"; null", string(p.Unit)+" / "+parameterDomain(p), strconv.FormatBool(p.Required)+"; "+conditionText(p.RequiredWhen), p.Title+". "+p.Description)
		}
		for _, block := range []model.Block{model.Questionnaire, model.Additional} {
			title := "Классическая анкета — 750"
			if block == model.Additional {
				title = "Дополнительные показатели — 250"
			}
			fmt.Fprintf(&out, "\n#### %s\n\n", title)
			tableHeader(&out, "Критерий / вопрос", "Активен", "Параметр значения", "Макс.", "Применимость / unknown", "Представление ответа")
			for _, c := range d.Criteria {
				if c.Block != block {
					continue
				}
				answer := ""
				if c.Answer != nil {
					answer = compactJSON(c.Answer)
				}
				tableRow(&out, c.ID+" / "+c.Title, strconv.FormatBool(c.Enabled), c.ValueParameter, strconv.Itoa(c.MaxPoints), conditionText(c.AppliesWhen)+" / "+string(c.UnknownAction), answer)
			}
			out.WriteString("\n")
			tableHeader(&out, "Критерий / случай", "Порядок / вид", "Условие", "Баллы", "Значение результата", "Ручное основание / описание")
			for _, c := range d.Criteria {
				if c.Block != block {
					continue
				}
				for _, scale := range []struct {
					name  string
					cases []model.ScoreCase
				}{{"SPECIAL", c.SpecialCases}, {"ORDINARY", c.Cases}} {
					for i, entry := range scale.cases {
						result := "Исходное значение"
						if entry.Result != nil {
							result = compactJSON(entry.Result)
						}
						reason := entry.Description
						if entry.ReviewReason != nil {
							reason += "; " + compactJSON(entry.ReviewReason)
						}
						tableRow(&out, c.ID+" / "+entry.ID, strconv.Itoa(i+1)+" / "+scale.name, conditionText(&entry.When), strconv.FormatFloat(entry.Points, 'g', -1, 64), result, reason)
					}
				}
			}
		}
		fmt.Fprint(&out, "\n#### Проверки полноты и допустимости\n\n")
		tableHeader(&out, "ID / название", "Активна", "Применимость", "Условие достаточности", "Поля / сообщение")
		for _, c := range d.DataChecks {
			tableRow(&out, c.ID+" / "+c.Title, strconv.FormatBool(c.Enabled), conditionText(c.AppliesWhen), conditionText(&c.SatisfiedWhen), strings.Join(c.Fields, ", ")+" / "+c.Message)
		}
		for _, section := range []struct {
			name  string
			rules []model.Rule
		}{{"Подтверждённые стопы", d.Stops}, {"Основания ручного рассмотрения", d.ManualReviews}} {
			fmt.Fprintf(&out, "\n#### %s\n\n", section.name)
			tableHeader(&out, "Порядок / ID / название", "Активно", "Условие", "Проверки / после критерия", "Код / сообщение / поля")
			for i, r := range section.rules {
				tableRow(&out, strconv.Itoa(i+1)+" / "+r.ID+" / "+r.Title, strconv.FormatBool(r.Enabled), conditionText(&r.When), strings.Join(r.RequiresChecks, ", ")+" / "+r.AfterCriterion, r.Reason.Code+" / "+r.Reason.Message+" / "+strings.Join(r.Reason.Fields, ", "))
			}
		}
		fmt.Fprint(&out, "\n#### Классы после единственного half-up округления\n\n")
		tableHeader(&out, "Класс", "От (включительно)", "До (включительно)")
		for _, band := range d.Classes {
			tableRow(&out, string(band.Grade), strconv.Itoa(band.MinTotal), strconv.Itoa(band.MaxTotal))
		}
	}
	return out.Bytes(), nil
}
