package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"strings"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/contracts"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/adminauth"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/jsonstrict"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/scoring"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/simulation"
	"github.com/gin-gonic/gin"
)

func writeSimulationError(c *gin.Context, err error) {
	var sample *simulation.Error
	var scoringInput *scoring.Error
	switch {
	case errors.As(err, &sample):
		writeProblem(c, 422, sample.Code, sample.Fields)
	case errors.As(err, &scoringInput):
		writeProblem(c, 422, scoringInput.Code, scoringInput.Fields)
	default:
		writeAdminError(c, err)
	}
}

func writeSampleDecodeError(c *gin.Context, err error) {
	var input *scoring.Error
	if errors.As(err, &input) {
		status := 400
		if input.Status == 422 {
			status = 422
		}
		writeProblem(c, status, input.Code, input.Fields)
		return
	}
	writeProblem(c, 400, "INVALID_SIMULATION_SAMPLE", nil)
}

// Reports may be incomplete for NEED_INFO, but their declared names remain
// strict. No alternative map of statement codes is introduced.
func sampleNames(raw json.RawMessage, typ reflect.Type) bool {
	if jsonstrict.Null(raw) {
		return true
	}
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	switch typ.Kind() {
	case reflect.Struct:
		var members map[string]json.RawMessage
		if json.Unmarshal(raw, &members) != nil || members == nil {
			return false
		}
		fields := map[string]reflect.Type{}
		for i := 0; i < typ.NumField(); i++ {
			f := typ.Field(i)
			name := strings.Split(f.Tag.Get("json"), ",")[0]
			if name != "" && name != "-" {
				fields[name] = f.Type
			}
		}
		for name, value := range members {
			field, ok := fields[name]
			if !ok || !sampleNames(value, field) {
				return false
			}
		}
	case reflect.Slice:
		var values []json.RawMessage
		if json.Unmarshal(raw, &values) != nil {
			return false
		}
		for _, value := range values {
			if !sampleNames(value, typ.Elem()) {
				return false
			}
		}
	}
	return true
}
func decodeSimulationSample(raw json.RawMessage) (simulation.Sample, error) {
	var sample simulation.Sample
	if err := jsonstrict.CheckLimit(raw, maxAdminBody); err != nil {
		return sample, err
	}
	fields, err := adminObjectNullable(raw, []string{"evaluation_date", "application", "statements", "facts_date", "additional_facts"}, []string{"statements", "facts_date", "additional_facts"})
	if err != nil {
		return sample, err
	}
	if sample.EvaluationDate, err = adminString(fields["evaluation_date"]); err != nil {
		return sample, err
	}
	if !sampleNames(fields["application"], reflect.TypeOf(contracts.DecisionRequest{})) {
		return sample, errors.New("invalid application members")
	}
	if sample.Application, err = scoring.Decode(bytes.NewReader(fields["application"])); err != nil {
		return sample, err
	}
	if !jsonstrict.Null(fields["facts_date"]) {
		value, err := adminString(fields["facts_date"])
		if err != nil {
			return sample, err
		}
		sample.FactsDate = &value
	}
	if !jsonstrict.Null(fields["statements"]) {
		if !sampleNames(fields["statements"], reflect.TypeOf(contracts.StatementsResponse{})) {
			return sample, errors.New("invalid report members")
		}
		decoder := json.NewDecoder(bytes.NewReader(fields["statements"]))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&sample.Statements); err != nil {
			return sample, err
		}
		if _, err := decoder.Token(); err != io.EOF {
			return sample, errors.New("trailing reports")
		}
	}
	sample.AdditionalFacts = append(json.RawMessage{}, fields["additional_facts"]...)
	return sample, nil
}

func RegisterAdminSimulation(router *gin.Engine, access *adminauth.Access, store AdminModels) {
	a := adminModels{store: store}
	g := router.Group("/api/admin/v1", access.Browser())
	g.POST("/simulations", func(c *gin.Context) {
		fields, ok := adminBody(c, "model", "sample")
		if !ok {
			return
		}
		reference, err := parseAdminReference(fields["model"])
		if err != nil {
			writeProblem(c, 400, "INVALID_MODEL_REFERENCE", nil)
			return
		}
		sample, err := decodeSimulationSample(fields["sample"])
		if err != nil {
			writeSampleDecodeError(c, err)
			return
		}
		definition, err := a.resolve(c.Request.Context(), reference)
		if err != nil {
			writeAdminError(c, err)
			return
		}
		result, err := simulation.Run(c.Request.Context(), definition, sample)
		if err != nil {
			writeSimulationError(c, err)
			return
		}
		c.JSON(200, result)
	})
	g.POST("/simulations/compare", func(c *gin.Context) {
		fields, ok := adminBody(c, "left", "right", "sample")
		if !ok {
			return
		}
		left, err := parseAdminReference(fields["left"])
		if err != nil {
			writeProblem(c, 400, "INVALID_MODEL_REFERENCE", nil)
			return
		}
		right, err := parseAdminReference(fields["right"])
		if err != nil {
			writeProblem(c, 400, "INVALID_MODEL_REFERENCE", nil)
			return
		}
		sample, err := decodeSimulationSample(fields["sample"])
		if err != nil {
			writeSampleDecodeError(c, err)
			return
		}
		before, err := a.resolve(c.Request.Context(), left)
		if err != nil {
			writeAdminError(c, err)
			return
		}
		after, err := a.resolve(c.Request.Context(), right)
		if err != nil {
			writeAdminError(c, err)
			return
		}
		result, err := simulation.Compare(c.Request.Context(), before, after, sample)
		if err != nil {
			writeSimulationError(c, err)
			return
		}
		c.JSON(200, result)
	})
}
