package simulation

import (
	"context"
	"reflect"
	"sort"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/model"
)

type CriterionDifference struct {
	ID          string           `json:"id"`
	Left        *CriterionResult `json:"left"`
	Right       *CriterionResult `json:"right"`
	PointsDelta *float64         `json:"points_delta"`
}
type Comparison struct {
	Left          Result                     `json:"left"`
	Right         Result                     `json:"right"`
	Definition    model.DefinitionComparison `json:"definition"`
	RouteChanged  bool                       `json:"route_changed"`
	GradeChanged  bool                       `json:"grade_changed"`
	TotalDelta    *int                       `json:"total_delta"`
	RawTotalDelta *float64                   `json:"raw_total_delta"`
	Criteria      []CriterionDifference      `json:"criteria"`
}

func Compare(ctx context.Context, left, right model.ModelDefinition, sample Sample) (Comparison, error) {
	allowed := []model.ModelDefinition{left, right}
	a, err := run(ctx, left, sample, allowed)
	if err != nil {
		return Comparison{}, err
	}
	b, err := run(ctx, right, sample, allowed)
	if err != nil {
		return Comparison{}, err
	}
	definition, err := model.CompareDefinitions(left, right)
	if err != nil {
		return Comparison{}, err
	}
	result := Comparison{Left: a, Right: b, Definition: definition, RouteChanged: a.Route != b.Route, GradeChanged: (a.Score == nil) != (b.Score == nil), Criteria: []CriterionDifference{}}
	if a.Score != nil && b.Score != nil {
		delta := b.Score.Total - a.Score.Total
		rawDelta := b.Score.RawTotal - a.Score.RawTotal
		result.TotalDelta = &delta
		result.RawTotalDelta = &rawDelta
		result.GradeChanged = a.Score.Grade != b.Score.Grade
	}
	before, after := map[string]*CriterionResult{}, map[string]*CriterionResult{}
	ids := map[string]bool{}
	for i := range a.Criteria {
		before[a.Criteria[i].ID] = &a.Criteria[i]
		ids[a.Criteria[i].ID] = true
	}
	for i := range b.Criteria {
		after[b.Criteria[i].ID] = &b.Criteria[i]
		ids[b.Criteria[i].ID] = true
	}
	ordered := []string{}
	for id := range ids {
		ordered = append(ordered, id)
	}
	sort.Strings(ordered)
	for _, id := range ordered {
		l, r := before[id], after[id]
		if reflect.DeepEqual(l, r) {
			continue
		}
		difference := CriterionDifference{ID: id, Left: l, Right: r}
		if l != nil && r != nil && l.ContributesToScore && r.ContributesToScore {
			delta := r.Points - l.Points
			difference.PointsDelta = &delta
		}
		result.Criteria = append(result.Criteria, difference)
	}
	return result, nil
}
