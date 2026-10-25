package signature

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/domain"
)

// Политики — signature_reports.policy (SignaturePolicy в api/openapi.yaml).
const (
	PolicyGeneric             = "GENERIC"
	PolicyFinancialStatements = "FINANCIAL_STATEMENTS"
	PolicyPowerOfAttorney     = "POWER_OF_ATTORNEY"
	PolicyLoanContract        = "LOAN_CONTRACT"
)

var policies = []string{PolicyGeneric, PolicyFinancialStatements, PolicyPowerOfAttorney, PolicyLoanContract}

// Rules — правила банка: что требуется от подписи документа каждого типа. Целостность, формат
// и сертификат нужны всегда; личность и полномочия — по типу документа.
type Rules struct {
	Version   string                               `json:"version"`
	Default   DocumentRule                         `json:"default"`
	Documents map[domain.DocumentType]DocumentRule `json:"documents"`
}

// DocumentRule — требования к подписи документа одного типа.
type DocumentRule struct {
	Policy    string `json:"policy"`    // GENERIC, FINANCIAL_STATEMENTS, POWER_OF_ATTORNEY, LOAN_CONTRACT
	Identity  bool   `json:"identity"`  // сопоставлять подписанта с заявителем
	Authority bool   `json:"authority"` // проверять право подписи: руководитель, ИП или МЧД
	POAPower  string `json:"poa_power"` // полномочие МЧД, с которым может подписать представитель; "" — только руководитель
}

// DefaultRules — правила MVP. Документы, которые подписывает сам заявитель, — с проверкой
// личности и полномочий; выписки и документы третьих сторон (банк, Росреестр, контрагент) —
// только подпись и сертификат: подписант там не заявитель.
func DefaultRules() Rules {
	applicant := func(policy, power string) DocumentRule {
		return DocumentRule{Policy: policy, Identity: true, Authority: true, POAPower: power}
	}
	thirdParty := DocumentRule{Policy: PolicyGeneric}
	return Rules{
		Version: "mvp-2026-10",
		Default: applicant(PolicyGeneric, domain.POAPowerLoanApplication),
		Documents: map[domain.DocumentType]DocumentRule{
			domain.DocFinancialStatements:  applicant(PolicyFinancialStatements, domain.POAPowerFinancialDocs),
			domain.DocTaxReturn:            applicant(PolicyFinancialStatements, domain.POAPowerFinancialDocs),
			domain.DocManagementReport:     applicant(PolicyGeneric, domain.POAPowerFinancialDocs),
			domain.DocPowerOfAttorney:      applicant(PolicyPowerOfAttorney, ""), // доверенность подписывает руководитель
			domain.DocLoanAgreementDraft:   applicant(PolicyLoanContract, ""),    // договор — только руководитель
			domain.DocBankStatement:        thirdParty,
			domain.DocEGRNExtract:          thirdParty,
			domain.DocCollateralTitle:      thirdParty,
			domain.DocContractWithCustomer: thirdParty,
			domain.DocLeaseAgreement:       thirdParty,
		},
	}
}

// LoadRules читает правила из JSON-файла (формат Rules); пустой path — DefaultRules.
func LoadRules(path string) (Rules, error) {
	if path == "" {
		return DefaultRules(), nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Rules{}, fmt.Errorf("signature rules: %w", err)
	}
	var r Rules
	if err := json.Unmarshal(data, &r); err != nil {
		return Rules{}, fmt.Errorf("signature rules %s: %w", path, err)
	}
	if err := r.Validate(); err != nil {
		return Rules{}, fmt.Errorf("signature rules %s: %w", path, err)
	}
	return r, nil
}

// Validate проверяет политики, типы документов и что полномочия без личности не проверяются.
func (r Rules) Validate() error {
	if r.Version == "" {
		return errors.New("version is required")
	}
	check := func(name string, d DocumentRule) error {
		switch {
		case !slices.Contains(policies, d.Policy):
			return fmt.Errorf("%s: policy %q, want one of %v", name, d.Policy, policies)
		case d.Authority && !d.Identity:
			return fmt.Errorf("%s: authority requires identity", name)
		case d.POAPower != "" && !d.Authority:
			return fmt.Errorf("%s: poa_power without authority", name)
		}
		return nil
	}
	if err := check("default", r.Default); err != nil {
		return err
	}
	for t, d := range r.Documents {
		if !t.Valid() || t == domain.DocSignature {
			return fmt.Errorf("unknown document type %q", t)
		}
		if err := check(string(t), d); err != nil {
			return err
		}
	}
	return nil
}

// For — правило для типа документа.
func (r Rules) For(t domain.DocumentType) DocumentRule {
	if d, ok := r.Documents[t]; ok {
		return d
	}
	return r.Default
}
