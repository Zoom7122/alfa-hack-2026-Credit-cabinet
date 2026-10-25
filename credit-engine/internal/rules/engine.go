package rules

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/contracts"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/model"
)

type Route string

const (
	NeedInfo  Route = "NEED_INFO"
	Rejected  Route = "REJECTED"
	Manual    Route = "MANUAL_REVIEW"
	Selection Route = "SELECT_OFFER"
)

// Input values are keyed by trusted source, not editable parameter IDs.
// Origins retain precise missing paths for nested/periodic source structures.
type Input struct {
	Values       map[model.Source]Value
	Origins      map[model.Source][]string
	FieldOrigins map[string][]string
}
type CheckResult struct {
	ID      string
	Passed  bool
	Skipped bool
	Status  Truth
	Fields  []string
	Message string
}
type CriterionResult struct {
	ID        string
	Block     model.Block
	Answer    string
	Value     Value
	Points    float64
	MaxPoints int
	CaseID    string
	Special   bool
	Unknown   bool
}
type Score struct {
	Model               string
	Criteria            []CriterionResult
	QuestionnairePoints int
	AdditionalPoints    float64
	RawTotal            float64
	Total               int
	Grade               contracts.Grade
}
type Result struct {
	Route   Route
	Score   *Score
	Reasons []model.Reason
	Checks  []CheckResult
}

type Engine struct {
	definition model.ModelDefinition
	parameters map[string]model.Parameter
	numeric    map[model.Source]model.NumericKind
}
type DefinitionError struct{ Issues []model.Issue }

func (e *DefinitionError) Error() string {
	return fmt.Sprintf("rules: invalid definition (%d issues)", len(e.Issues))
}

// Compile validates and owns a deep copy. A published Engine is safe for
// concurrent evaluations; modifying the caller's draft cannot alter it.
func Compile(d model.ModelDefinition, catalog []model.Binding) (*Engine, error) {
	validation := model.Validate(d, catalog)
	if !validation.Valid {
		return nil, &DefinitionError{Issues: validation.Issues}
	}
	data, err := model.Marshal(d)
	if err != nil {
		return nil, err
	}
	owned, err := model.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	e := &Engine{definition: owned, parameters: map[string]model.Parameter{}, numeric: map[model.Source]model.NumericKind{}}
	for _, b := range catalog {
		if b.Type == model.Number {
			kind := b.NumericKind
			if kind == "" {
				kind = model.NumericFloat64
			}
			e.numeric[b.Source] = kind
		}
	}
	for _, p := range owned.Parameters {
		e.parameters[p.ID] = p
		if p.Source.Kind == model.Supplier && p.Type == model.Number {
			kind := model.NumericFloat64
			if p.Number.Integer {
				kind = model.NumericInt64
			}
			e.numeric[p.Source] = kind
		}
	}
	return e, nil
}
func (e *Engine) Version() string                      { return e.definition.Version }
func (e *Engine) BorrowerType() contracts.BorrowerType { return e.definition.BorrowerType }

type evaluation struct {
	engine        *Engine
	input         Input
	facts         map[string]Value
	checks        map[string]bool
	trace         []CheckResult
	missing       []string
	invalidDomain bool
	failedCheck   bool
}

func (e *Engine) prepare(input Input) (*evaluation, error) {
	x := &evaluation{engine: e, input: input, facts: map[string]Value{}, checks: map[string]bool{}, trace: []CheckResult{}}
	for _, p := range e.definition.Parameters {
		if !p.Enabled {
			continue
		}
		v := input.Values[p.Source]
		if v.Known() {
			if v.Type() != p.Type {
				return nil, fmt.Errorf("rules: source %s has type %s, expected %s", p.Source.Path, v.Type(), p.Type)
			}
			if p.Type == model.Number {
				if expected := e.numeric[p.Source]; expected != "" && v.NumericKind() != expected {
					return nil, fmt.Errorf("rules: source %s has numeric representation %s, expected %s", p.Source.Path, v.NumericKind(), expected)
				}
				if _, ok := v.Float(); !ok {
					return nil, fmt.Errorf("rules: nonfinite source %s", p.Source.Path)
				}
			}
			inside, err := domain(v, p)
			if err != nil {
				return nil, err
			}
			if !inside {
				x.invalidDomain = true
				x.needParameter(p)
				v = Unknown()
			}
		}
		x.facts[p.ID] = v
	}
	for _, check := range e.definition.DataChecks {
		if !check.Enabled {
			continue
		}
		status := True
		if check.AppliesWhen != nil {
			v, err := Condition(*check.AppliesWhen, x.facts)
			if err != nil {
				return nil, err
			}
			if v == False {
				x.checks[check.ID] = true
				x.trace = append(x.trace, CheckResult{ID: check.ID, Passed: true, Skipped: true, Status: UnknownTruth, Fields: []string{}, Message: check.Message})
				continue
			}
			status = v
		}
		if status == True {
			var err error
			status, err = Condition(check.SatisfiedWhen, x.facts)
			if err != nil {
				return nil, err
			}
		}
		passed := status == True
		fields := []string{}
		if !passed {
			x.failedCheck = true
			fields = x.fields(check.Fields)
			x.missing = append(x.missing, fields...)
		}
		x.checks[check.ID] = passed
		x.trace = append(x.trace, CheckResult{ID: check.ID, Passed: passed, Status: status, Fields: fields, Message: check.Message})
	}
	return x, nil
}
func domain(v Value, p model.Parameter) (bool, error) {
	if p.Type == model.Number {
		r, ok := v.Rational()
		if !ok {
			return false, fmt.Errorf("rules: invalid number")
		}
		if p.Number.Integer && !r.IsInt() {
			return false, nil
		}
		for _, edge := range []struct {
			bound *model.Bound
			lower bool
		}{{p.Number.Lower, true}, {p.Number.Upper, false}} {
			if edge.bound == nil {
				continue
			}
			l := model.Literal{Number: &edge.bound.Value}
			b, err := literal(&l, v.numeric)
			if err != nil {
				return false, err
			}
			cmp, err := compare(v, b)
			if err != nil {
				return false, err
			}
			if edge.lower && (cmp < 0 || cmp == 0 && !edge.bound.Inclusive) || !edge.lower && (cmp > 0 || cmp == 0 && !edge.bound.Inclusive) {
				return false, nil
			}
		}
	}
	if p.Type == model.String {
		text, _ := v.Text()
		if p.Unit == model.Date {
			_, err := time.Parse("2006-01-02", text)
			return err == nil, nil
		}
		for _, allowed := range p.StringValues {
			if text == allowed {
				return true, nil
			}
		}
		return false, nil
	}
	return true, nil
}
func (x *evaluation) fields(fields []string) []string {
	result := []string{}
	for _, field := range fields {
		if origins := x.input.FieldOrigins[field]; len(origins) > 0 {
			result = append(result, origins...)
		} else {
			result = append(result, field)
		}
	}
	return unique(result)
}
func (x *evaluation) needParameter(p model.Parameter) {
	if origins := x.input.Origins[p.Source]; len(origins) > 0 {
		x.missing = append(x.missing, origins...)
	} else {
		path := p.Source.Path
		if p.Source.Kind == model.Supplier {
			path = "additional_facts." + path
		}
		x.missing = append(x.missing, path)
	}
}
func (x *evaluation) needCondition(c model.Condition) {
	if p, ok := x.engine.parameters[c.Parameter]; ok && !x.facts[p.ID].Known() {
		x.needParameter(p)
	}
	if p, ok := x.engine.parameters[c.OtherParameter]; ok && !x.facts[p.ID].Known() {
		x.needParameter(p)
	}
	for _, child := range c.Conditions {
		x.needCondition(child)
	}
}
func (x *evaluation) confirmed(r model.Rule) (bool, error) {
	if !r.Enabled {
		return false, nil
	}
	status, err := Condition(r.When, x.facts)
	if err != nil {
		return false, err
	}
	if status != True {
		return false, nil
	}
	for _, id := range r.RequiresChecks {
		if !x.checks[id] {
			return false, nil
		}
	}
	return true, nil
}
func (x *evaluation) stops() ([]model.Reason, error) {
	// Preserve early raw-risk priority over dependent financial stops.
	for _, dependent := range []bool{false, true} {
		out := []model.Reason{}
		for _, r := range x.engine.definition.Stops {
			if (len(r.RequiresChecks) > 0) != dependent {
				continue
			}
			ok, err := x.confirmed(r)
			if err != nil {
				return nil, err
			}
			if ok {
				out = append(out, copyReason(r.Reason))
			} else if dependent && r.Enabled {
				status, err := Condition(r.When, x.facts)
				if err != nil {
					return nil, err
				}
				if status == True {
					x.missing = append(x.missing, r.Reason.Fields...)
				}
			}
		}
		if len(out) > 0 {
			return out, nil
		}
	}
	return nil, nil
}

// Stops supports the MS1 phase before statements/facts dependency calls.
func (e *Engine) Stops(input Input) ([]model.Reason, error) {
	x, err := e.prepare(input)
	if err != nil {
		return nil, err
	}
	return x.stops()
}

func (e *Engine) Evaluate(input Input) (Result, error) {
	x, err := e.prepare(input)
	if err != nil {
		return Result{}, err
	}
	stops, err := x.stops()
	if err != nil {
		return Result{}, err
	}
	if len(stops) > 0 {
		return Result{Route: Rejected, Reasons: stops, Checks: x.trace}, nil
	}
	for _, p := range e.definition.Parameters {
		if !p.Enabled || !p.Required {
			continue
		}
		if p.RequiredWhen != nil {
			status, err := Condition(*p.RequiredWhen, x.facts)
			if err != nil {
				return Result{}, err
			}
			if status == False {
				continue
			}
			if status == UnknownTruth {
				x.needCondition(*p.RequiredWhen)
			}
		}
		if !x.facts[p.ID].Known() {
			x.needParameter(p)
		}
	}
	if len(x.missing) > 0 || x.invalidDomain || x.failedCheck {
		return x.needInfo(), nil
	}
	manual := []model.Reason{}
	appendManual := func(after string) error {
		for _, r := range e.definition.ManualReviews {
			if r.AfterCriterion != after {
				continue
			}
			ok, err := x.confirmed(r)
			if err != nil {
				return err
			}
			if ok {
				manual = append(manual, copyReason(r.Reason))
			}
		}
		return nil
	}
	if err := appendManual(""); err != nil {
		return Result{}, err
	}
	score := &Score{Model: e.definition.Version, Criteria: []CriterionResult{}}
	for _, c := range e.definition.Criteria {
		if !c.Enabled {
			continue
		}
		if c.AppliesWhen != nil {
			status, err := Condition(*c.AppliesWhen, x.facts)
			if err != nil {
				return Result{}, err
			}
			if status == False {
				if err := appendManual(c.ID); err != nil {
					return Result{}, err
				}
				continue
			}
			if status == UnknownTruth {
				x.needCondition(*c.AppliesWhen)
				return x.needInfo(), nil
			}
		}
		out, reason, skip, err := x.criterion(c)
		if err != nil {
			return Result{}, err
		}
		if len(x.missing) > 0 {
			return x.needInfo(), nil
		}
		if !skip {
			score.Criteria = append(score.Criteria, out)
			if c.Block == model.Questionnaire {
				score.QuestionnairePoints += int(out.Points)
			} else {
				score.AdditionalPoints += out.Points
			}
			if reason != nil {
				manual = append(manual, *reason)
			}
		}
		if err := appendManual(c.ID); err != nil {
			return Result{}, err
		}
	}
	score.RawTotal = float64(score.QuestionnairePoints) + score.AdditionalPoints
	classification, err := e.Classify(score.RawTotal)
	if err != nil {
		return Result{}, err
	}
	score.Total, score.Grade = classification.Total, classification.Grade
	if score.Grade == contracts.GradeD {
		return Result{Route: Rejected, Score: score, Reasons: []model.Reason{*classification.Reason}, Checks: x.trace}, nil
	}
	if score.Grade == contracts.GradeC {
		manual = append([]model.Reason{*classification.Reason}, manual...)
	}
	route := Selection
	if len(manual) > 0 {
		route = Manual
	}
	return Result{Route: route, Score: score, Reasons: manual, Checks: x.trace}, nil
}
func (x *evaluation) needInfo() Result {
	return Result{Route: NeedInfo, Reasons: []model.Reason{{Code: "MISSING_DATA", Message: "Нужны полные и достоверные сведения.", Fields: unique(x.missing)}}, Checks: x.trace}
}
func copyReason(r model.Reason) model.Reason { r.Fields = append([]string{}, r.Fields...); return r }
func unique(values []string) []string {
	seen := map[string]bool{}
	result := []string{}
	for _, v := range values {
		if !seen[v] {
			seen[v] = true
			result = append(result, v)
		}
	}
	return result
}

var placeholder = regexp.MustCompile(`\$\{([a-z][a-z0-9_]*)\}`)

func (x *evaluation) answer(a *model.Answer, unknown bool) (string, error) {
	if a == nil {
		return "", nil
	}
	if unknown && a.UnknownText != "" {
		return a.UnknownText, nil
	}
	template := a.Template
	for _, candidate := range a.Cases {
		status, err := Condition(candidate.When, x.facts)
		if err != nil {
			return "", err
		}
		if status == True {
			template = candidate.Template
			break
		}
	}
	return placeholder.ReplaceAllStringFunc(template, func(part string) string {
		id := strings.TrimSuffix(strings.TrimPrefix(part, "${"), "}")
		return x.facts[id].Format()
	}), nil
}
func (x *evaluation) criterion(c model.Criterion) (CriterionResult, *model.Reason, bool, error) {
	out := CriterionResult{ID: c.ID, Block: c.Block, Value: x.facts[c.ValueParameter], MaxPoints: c.MaxPoints}
	unknown := false
	for groupIndex, cases := range [][]model.ScoreCase{c.SpecialCases, c.Cases} {
		if groupIndex == 1 && c.ValueParameter != "" && !out.Value.Known() {
			unknown = true
			break
		}
		for _, candidate := range cases {
			status, err := Condition(candidate.When, x.facts)
			if err != nil {
				return out, nil, false, err
			}
			if status == UnknownTruth {
				unknown = true
			}
			if status != True {
				continue
			}
			out.Points = candidate.Points
			out.CaseID = candidate.ID
			out.Special = groupIndex == 0
			if candidate.Result != nil {
				if candidate.Result.Mode == model.NullResult {
					out.Value = Unknown()
				} else {
					var err error
					kind := out.Value.numeric
					if kind == "" {
						kind = model.NumericFloat64
					}
					out.Value, err = literal(candidate.Result.Value, kind)
					if err != nil {
						return out, nil, false, err
					}
				}
			}
			out.Answer, err = x.answer(c.Answer, false)
			if err != nil {
				return out, nil, false, err
			}
			var reason *model.Reason
			if candidate.ReviewReason != nil {
				r := copyReason(*candidate.ReviewReason)
				reason = &r
			}
			return out, reason, false, nil
		}
		if unknown {
			break
		}
	}
	if unknown {
		out.Unknown = true
		switch c.UnknownAction {
		case model.Skip:
			return out, nil, true, nil
		case model.Zero:
			var err error
			out.Value = Unknown()
			out.Answer, err = x.answer(c.Answer, true)
			return out, nil, false, err
		case model.NeedInfo:
			if p, ok := x.engine.parameters[c.ValueParameter]; ok && !x.facts[p.ID].Known() {
				x.needParameter(p)
			}
			for _, cases := range [][]model.ScoreCase{c.SpecialCases, c.Cases} {
				for _, candidate := range cases {
					x.needCondition(candidate.When)
				}
			}
			if len(x.missing) == 0 {
				x.missing = append(x.missing, c.ID)
			}
			return out, nil, false, nil
		}
	}
	return out, nil, false, fmt.Errorf("rules: no case matched enabled criterion %s", c.ID)
}
