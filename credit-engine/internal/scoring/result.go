package scoring

import (
	"fmt"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/contracts"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/model"
)

// ToAssessment maps the pure result to the existing MS1 process contract.
// Numeric coefficients keep their legacy field; boolean values stay typed.
func ToAssessment(result ModelAssessment) (Assessment, error) {
	r := result.Result
	a := Assessment{Route: Route(r.Route), Reasons: []contracts.Reason{}}
	for _, reason := range r.Reasons {
		a.Reasons = append(a.Reasons, contracts.Reason{Code: reason.Code, Message: reason.Message, Fields: append([]string{}, reason.Fields...)})
	}
	if r.Score == nil {
		return a, nil
	}
	s := r.Score
	a.CashFlow = result.CashFlow
	a.Score = &contracts.ScoreResult{Model: s.Model, Questionnaire: contracts.QuestionnaireResult{Points: s.QuestionnairePoints, MaxPoints: model.QuestionnaireMaximum, Criteria: []contracts.QuestionnaireCriterion{}}, Economics: contracts.EconomicResult{Points: s.AdditionalPoints, MaxPoints: model.AdditionalMaximum, Criteria: []contracts.EconomicCriterion{}}, RawTotal: s.RawTotal, Total: s.Total, Grade: s.Grade}
	for _, c := range s.Criteria {
		if c.Block == model.Questionnaire {
			a.Score.Questionnaire.Criteria = append(a.Score.Questionnaire.Criteria, contracts.QuestionnaireCriterion{ID: c.ID, Answer: c.Answer, Points: int(c.Points), MaxPoints: c.MaxPoints})
			continue
		}
		out := contracts.EconomicCriterion{ID: c.ID, Points: c.Points, MaxPoints: c.MaxPoints}
		if c.Value.Known() {
			switch c.Value.Type() {
			case model.Number:
				value, ok := c.Value.Float()
				if !ok {
					return Assessment{}, fmt.Errorf("scoring: nonfinite criterion %s", c.ID)
				}
				out.Value = &value
			case model.Boolean:
				value, _ := c.Value.Bool()
				out.BooleanValue = &value
			default:
				return Assessment{}, fmt.Errorf("scoring: unsupported additional value type for %s", c.ID)
			}
		}
		a.Score.Economics.Criteria = append(a.Score.Economics.Criteria, out)
	}
	return a, nil
}
