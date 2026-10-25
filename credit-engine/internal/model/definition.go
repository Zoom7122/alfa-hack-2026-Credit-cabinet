// Package model describes build-time/editor scoring definitions. It contains
// no HTTP, database access, arbitrary expressions or financial calculations.
package model

import "github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/contracts"

const (
	FormatVersion        = 1
	QuestionnaireMaximum = 750
	AdditionalMaximum    = 250
	TotalMaximum         = 1000
)

// ModelDefinition is an immutable published version or an editable draft.
// Decode is for tooling/admin use; MS1 executes definitions compiled into Go.
type ModelDefinition struct {
	FormatVersion      int                    `json:"format_version"`
	ID                 string                 `json:"id"`
	Version            string                 `json:"version"`
	BorrowerType       contracts.BorrowerType `json:"borrower_type"`
	Title              string                 `json:"title"`
	Description        string                 `json:"description,omitempty"`
	FactsSchemaVersion string                 `json:"facts_schema_version,omitempty"`
	Parameters         []Parameter            `json:"parameters"`
	Criteria           []Criterion            `json:"criteria"`
	DataChecks         []DataCheck            `json:"data_checks"`
	Stops              []Rule                 `json:"stops"`
	ManualReviews      []Rule                 `json:"manual_reviews"`
	Classes            []ClassBand            `json:"classes"`
}

type ValueType string

const (
	Number  ValueType = "NUMBER"
	Boolean ValueType = "BOOLEAN"
	String  ValueType = "STRING" // Existing categorical inputs only, not new supplier fields.
)

type SourceKind string

const (
	Application SourceKind = "APPLICATION"
	Statements  SourceKind = "STATEMENTS"
	Computed    SourceKind = "COMPUTED"
	Supplier    SourceKind = "SUPPLIER"
)

// Path identifies an existing named source binding or a new supplier JSON name.
// COMPUTED paths resolve only to the fixed catalog of existing Go calculations.
type Source struct {
	Kind SourceKind `json:"kind"`
	Path string     `json:"path"`
}

type Unit string

const (
	RUB      Unit = "RUB"
	Ratio    Unit = "RATIO"
	Percent  Unit = "PERCENT"
	Count    Unit = "COUNT"
	Months   Unit = "MONTHS"
	Years    Unit = "YEARS"
	Days     Unit = "DAYS"
	Flag     Unit = "BOOLEAN"
	Category Unit = "CATEGORY"
	Date     Unit = "DATE"
)

type Bound struct {
	Value     NumberLiteral `json:"value"`
	Inclusive bool          `json:"inclusive"`
}

// A missing lower/upper bound is unbounded. Integer distinguishes source
// counters from continuous numbers; missing facts are never numeric zero.
type NumberDomain struct {
	Integer bool   `json:"integer"`
	Lower   *Bound `json:"lower,omitempty"`
	Upper   *Bound `json:"upper,omitempty"`
}

type Parameter struct {
	ID           string        `json:"id"`
	Enabled      bool          `json:"enabled"`
	Title        string        `json:"title"`
	Description  string        `json:"description,omitempty"`
	Source       Source        `json:"source"`
	JSONName     string        `json:"json_name"`
	GoName       string        `json:"go_name,omitempty"`
	Type         ValueType     `json:"type"`
	Unit         Unit          `json:"unit"`
	Number       *NumberDomain `json:"number,omitempty"`
	StringValues []string      `json:"string_values,omitempty"`
	Required     bool          `json:"required"`
	RequiredWhen *Condition    `json:"required_when,omitempty"`
	DependsOn    []string      `json:"depends_on,omitempty"`
}

// Literal has exactly one non-nil member, including for 0 and false.
// Unknown is a fact state, not an empty or null literal in a rule.
type Literal struct {
	Number  *NumberLiteral `json:"number,omitempty"`
	Boolean *bool          `json:"boolean,omitempty"`
	String  *string        `json:"string,omitempty"`
}

type Operator string

const (
	Always  Operator = "ALWAYS"
	Never   Operator = "NEVER"
	All     Operator = "ALL"
	Any     Operator = "ANY"
	Not     Operator = "NOT"
	Known   Operator = "KNOWN"
	Unknown Operator = "UNKNOWN"
	EQ      Operator = "EQ"
	NE      Operator = "NE"
	LT      Operator = "LT"
	LE      Operator = "LE"
	GT      Operator = "GT"
	GE      Operator = "GE"
	In      Operator = "IN"
	NotIn   Operator = "NOT_IN"
)

// Operators select one explicit shape: groups use Conditions, presence tests
// use Parameter, comparisons use Parameter plus Value or OtherParameter,
// membership uses Parameter plus Values. No expression text is evaluated.
type Condition struct {
	Op             Operator    `json:"op"`
	Parameter      string      `json:"parameter,omitempty"`
	OtherParameter string      `json:"other_parameter,omitempty"`
	Value          *Literal    `json:"value,omitempty"`
	Values         []Literal   `json:"values,omitempty"`
	Conditions     []Condition `json:"conditions,omitempty"`
}

type Block string

const (
	Questionnaire Block = "QUESTIONNAIRE"
	Additional    Block = "ADDITIONAL"
)

type UnknownAction string

const (
	NeedInfo UnknownAction = "NEED_INFO"
	Zero     UnknownAction = "ZERO"
	Skip     UnknownAction = "SKIP"
)

// Answer templates contain only literal text and ${parameter_id} references.
// They preserve the legacy questionnaire's named answers and zero-point controls.
type Answer struct {
	Template    string       `json:"template"`
	Cases       []AnswerCase `json:"cases,omitempty"`
	UnknownText string       `json:"unknown_text,omitempty"`
}

type AnswerCase struct {
	When     Condition `json:"when"`
	Template string    `json:"template"`
}

type Criterion struct {
	ID             string        `json:"id"`
	Enabled        bool          `json:"enabled"`
	Title          string        `json:"title"`
	Description    string        `json:"description,omitempty"`
	Block          Block         `json:"block"`
	ValueParameter string        `json:"value_parameter,omitempty"`
	Answer         *Answer       `json:"answer,omitempty"`
	MaxPoints      int           `json:"max_points"`
	AppliesWhen    *Condition    `json:"applies_when,omitempty"`
	UnknownAction  UnknownAction `json:"unknown_action"`
	SpecialCases   []ScoreCase   `json:"special_cases"`
	Cases          []ScoreCase   `json:"cases"`
}

// Special cases are evaluated before the ordinary scale, for example a zero
// denominator. Matching cases within each scale must not overlap or add twice.
type ScoreCase struct {
	ID           string          `json:"id"`
	When         Condition       `json:"when"`
	Points       float64         `json:"points"`
	Description  string          `json:"description,omitempty"`
	Result       *ResultOverride `json:"result,omitempty"`
	ReviewReason *Reason         `json:"review_reason,omitempty"`
}

type ResultMode string

const (
	NullResult    ResultMode = "NULL"
	LiteralResult ResultMode = "VALUE"
)

type ResultOverride struct {
	Mode  ResultMode `json:"mode"`
	Value *Literal   `json:"value,omitempty"`
}

type Reason struct {
	Code    string   `json:"code"`
	Message string   `json:"message"`
	Fields  []string `json:"fields"`
}

type Rule struct {
	ID             string    `json:"id"`
	Enabled        bool      `json:"enabled"`
	Title          string    `json:"title"`
	When           Condition `json:"when"`
	Reason         Reason    `json:"reason"`
	RequiresChecks []string  `json:"requires_checks,omitempty"`
	AfterCriterion string    `json:"after_criterion,omitempty"`
}

// Data checks represent business completeness/acceptance, including a known
// false control or inconsistent facts. Only satisfied=true passes a check.
type DataCheck struct {
	ID            string     `json:"id"`
	Enabled       bool       `json:"enabled"`
	Title         string     `json:"title"`
	AppliesWhen   *Condition `json:"applies_when,omitempty"`
	SatisfiedWhen Condition  `json:"satisfied_when"`
	Fields        []string   `json:"fields"`
	Message       string     `json:"message"`
}

// Bounds are inclusive integer totals after one final half-up rounding.
// Grades remain A-D; their routing policy is fixed by the process specification.
type ClassBand struct {
	Grade    contracts.Grade `json:"grade"`
	MinTotal int             `json:"min_total"`
	MaxTotal int             `json:"max_total"`
	Reason   *Reason         `json:"reason,omitempty"`
}

// Binding is the trusted source catalog supplied by the existing calculations,
// not a definition of a new formula. Dependencies are trusted source bindings.
type Binding struct {
	Source       Source
	Type         ValueType
	Unit         Unit
	BorrowerType contracts.BorrowerType
	NumericKind  NumericKind
	DependsOn    []Source
}
