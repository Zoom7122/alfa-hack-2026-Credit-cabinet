// Package fieldcatalog describes collector inputs and their model-dependent use.
// Metadata contains no applicant values. Bindings are provenance, not new formulas.
package fieldcatalog

import (
	_ "embed"
	"encoding/json"
	"regexp"
	"sort"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/model"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/sourcecatalog"
)

//go:embed collector-fields.json
var catalogJSON []byte

type System struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Condition string `json:"collection_condition"`
}
type Field struct {
	ID       string         `json:"id"`
	System   string         `json:"system"`
	Path     string         `json:"path"`
	Title    string         `json:"title"`
	Type     string         `json:"type"`
	Bindings []model.Source `json:"bindings"`
	Roles    []string       `json:"roles"`
	Criteria []string       `json:"criteria"`
	Used     bool           `json:"used"`
	Derived  bool           `json:"derived"`
	Reason   string         `json:"reason"`
}
type Catalog struct {
	FormatVersion int      `json:"format_version"`
	Systems       []System `json:"systems"`
	Fields        []Field  `json:"fields"`
}

func Read() (Catalog, error) {
	var c Catalog
	err := json.Unmarshal(catalogJSON, &c)
	for i := range c.Fields {
		c.Fields[i].Roles = []string{}
		c.Fields[i].Criteria = []string{}
	}
	return c, err
}

type usage struct {
	roles    map[string]bool
	criteria map[string]bool
	derived  bool
}

var templateRef = regexp.MustCompile(`\$\{([a-z][a-z0-9_]*)\}`)

// Analyze follows both editable parameter dependencies and trusted computed
// bindings. A zero-point control is distinct from an unused source.
func Analyze(d model.ModelDefinition) (Catalog, error) {
	c, err := Read()
	if err != nil {
		return c, err
	}
	bindings, err := sourcecatalog.Bindings(d.BorrowerType)
	if err != nil {
		return c, err
	}
	parameters := map[string]model.Parameter{}
	deps := map[model.Source][]model.Source{}
	for _, b := range bindings {
		deps[b.Source] = append(deps[b.Source], b.DependsOn...)
	}
	for _, p := range d.Parameters {
		parameters[p.ID] = p
	}
	for _, p := range d.Parameters {
		for _, id := range p.DependsOn {
			if dep, ok := parameters[id]; ok {
				deps[p.Source] = append(deps[p.Source], dep.Source)
			}
		}
	}
	sources := map[model.Source]*usage{}
	var visitSource func(model.Source, string, string, bool, map[model.Source]bool)
	visitSource = func(source model.Source, role, criterion string, indirect bool, seen map[model.Source]bool) {
		if seen[source] {
			return
		}
		seen[source] = true
		u := sources[source]
		if u == nil {
			u = &usage{roles: map[string]bool{}, criteria: map[string]bool{}}
			sources[source] = u
		}
		u.roles[role] = true
		if criterion != "" {
			u.criteria[criterion] = true
		}
		u.derived = u.derived || indirect
		for _, dependency := range deps[source] {
			visitSource(dependency, role, criterion, true, seen)
		}
	}
	visit := func(id, role, criterion string) {
		if p, ok := parameters[id]; ok && p.Enabled {
			visitSource(p.Source, role, criterion, p.Source.Kind == model.Computed, map[model.Source]bool{})
		}
	}
	condition := func(condition *model.Condition, role, criterion string) {
		var walk func(model.Condition)
		walk = func(x model.Condition) {
			visit(x.Parameter, role, criterion)
			visit(x.OtherParameter, role, criterion)
			for _, child := range x.Conditions {
				walk(child)
			}
		}
		if condition != nil {
			walk(*condition)
		}
	}
	for _, p := range d.Parameters {
		if p.Enabled && p.Required {
			visit(p.ID, "COMPLETENESS", "")
			condition(p.RequiredWhen, "COMPLETENESS", "")
		}
	}
	for _, criterion := range d.Criteria {
		role := "SCORE"
		if !criterion.Enabled {
			role = "DISABLED_SCORE"
		} else if criterion.MaxPoints == 0 {
			role = "CONTROL"
		}
		visit(criterion.ValueParameter, role, criterion.ID)
		condition(criterion.AppliesWhen, role, criterion.ID)
		for _, rows := range [][]model.ScoreCase{criterion.Cases, criterion.SpecialCases} {
			for _, row := range rows {
				condition(&row.When, role, criterion.ID)
			}
		}
		if criterion.Answer != nil {
			templates := []string{criterion.Answer.Template, criterion.Answer.UnknownText}
			for _, a := range criterion.Answer.Cases {
				condition(&a.When, role, criterion.ID)
				templates = append(templates, a.Template)
			}
			for _, text := range templates {
				for _, m := range templateRef.FindAllStringSubmatch(text, -1) {
					visit(m[1], role, criterion.ID)
				}
			}
		}
	}
	for _, check := range d.DataChecks {
		if check.Enabled {
			condition(check.AppliesWhen, "DATA_CHECK", "")
			condition(&check.SatisfiedWhen, "DATA_CHECK", "")
		}
	}
	for _, group := range []struct {
		rows []model.Rule
		role string
	}{{d.Stops, "STOP"}, {d.ManualReviews, "MANUAL_REVIEW"}} {
		for _, r := range group.rows {
			if r.Enabled {
				condition(&r.When, group.role, "")
			}
		}
	}
	for i := range c.Fields {
		f := &c.Fields[i]
		roles, criteria := map[string]bool{}, map[string]bool{}
		f.Roles = []string{}
		f.Criteria = []string{}
		for _, binding := range f.Bindings {
			if u := sources[binding]; u != nil {
				for role := range u.roles {
					roles[role] = true
					if role != "DISABLED_SCORE" {
						f.Used = true
					}
				}
				for id := range u.criteria {
					criteria[id] = true
				}
				f.Derived = f.Derived || u.derived
			}
		}
		for role := range roles {
			f.Roles = append(f.Roles, role)
		}
		for id := range criteria {
			f.Criteria = append(f.Criteria, id)
		}
		sort.Strings(f.Roles)
		sort.Strings(f.Criteria)
		switch {
		case f.Used:
			f.Reason = "Используется выбранной моделью"
		case len(f.Bindings) == 0:
			f.Reason = "Не передаётся в скоринговые признаки"
		case roles["DISABLED_SCORE"]:
			f.Reason = "Балльный критерий выключен"
		default:
			f.Reason = "Не используется выбранной моделью"
		}
	}
	return c, nil
}
