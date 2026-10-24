package scoring

import "github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/contracts"

func checkIP(c *inputCheck, p *contracts.IpScoring) {
	s := "scoring.ip."
	checkNumber(c, s+"owner_age", p.OwnerAge)
	checkNumber(c, s+"dependents", p.Dependents)
	checkNumber(c, s+"total_experience_months", p.TotalExperienceMonths)
	checkNumber(c, s+"net_monthly_income_rub", p.NetMonthlyIncomeRUB)
	checkNumber(c, s+"bank_relationship_months", p.BankRelationshipMonths)
	checkNumber(c, s+"credit_history_months", p.CreditHistoryMonths)
	checkNumber(c, s+"worst_repaid_dpd_days_24m", p.WorstRepaidDPDDays24M)
	checkNumber(c, s+"current_dpd_days", p.CurrentDPDDays)
	checkNumber(c, s+"applications_90d", p.Applications90D)
	checkNumber(c, s+"business_age_months", p.BusinessAgeMonths)
	checkNumber(c, s+"owner_experience_years", p.OwnerExperienceYears)
	checkNumber(c, s+"overdue_taxes_rub", p.OverdueTaxesRUB)
	if val(p.BusinessLocation) == contracts.BusinessLocationVerifiedPhysical || p.LocationStabilityMonths != nil {
		checkNumber(c, s+"location_stability_months", p.LocationStabilityMonths)
	}
	checkEnum(c, s+"marital_status", p.MaritalStatus, "MARRIED", "NEVER_MARRIED", "DIVORCED_ONCE", "DIVORCED_MULTIPLE", "WIDOWED")
	checkEnum(c, s+"primary_income_source", p.PrimaryIncomeSource, "BUSINESS", "PUBLIC_EMPLOYMENT", "PRIVATE_EMPLOYMENT", "PENSION", "SELF_EMPLOYED_OR_GPH", "NONE")
	checkEnum(c, s+"qualification", p.Qualification, "NONE", "SUPPORT", "SPECIALIST", "MANAGER_OR_HIGHLY_QUALIFIED")
	checkEnum(c, s+"car_class", p.CarClass, "NONE", "ECONOMY", "COMFORT", "PREMIUM")
	checkEnum(c, s+"education_level", p.EducationLevel, "BELOW_GENERAL", "GENERAL", "VOCATIONAL", "HIGHER", "ACADEMIC_DEGREE")
	checkEnum(c, s+"real_estate_status", p.RealEstateStatus, "UNENCUMBERED_WHOLE", "UNENCUMBERED_SHARE", "ENCUMBERED_ONLY", "NONE")
	checkEnum(c, s+"business_location", p.BusinessLocation, "VERIFIED_PHYSICAL", "VERIFIED_ONLINE_OR_MOBILE", "UNVERIFIED")
	checkEnum(c, s+"income_evidence", p.IncomeEvidence, "VERIFIED", "DOCUMENTS_PENDING_CROSSCHECK", "NONE")
	checkEnum(c, s+"buyer_contracts", p.BuyerContracts, "ACTIVE_AND_PERFORMED", "REPEAT_ORDERS", "PLANS_ONLY")
	if p.MilitaryStatus != nil {
		checkEnum(c, s+"military_status", p.MilitaryStatus, "SERVED", "EXEMPT_OR_DEFERRED", "NOT_LIABLE", "OTHER_OR_UNKNOWN")
	}
	checkRiskFields(c, s, ipRisk(p))
}

func checkLegal(c *inputCheck, p *contracts.LegalEntityScoring) {
	s := "scoring.legal_entity."
	checkNumber(c, s+"business_age_months", p.BusinessAgeMonths)
	checkNumber(c, s+"credit_history_months", p.CreditHistoryMonths)
	checkNumber(c, s+"worst_repaid_dpd_days_24m", p.WorstRepaidDPDDays24M)
	checkNumber(c, s+"current_dpd_days", p.CurrentDPDDays)
	checkNumber(c, s+"applications_90d", p.Applications90D)
	checkNumber(c, s+"manager_experience_years", p.ManagerExperienceYears)
	checkNumber(c, s+"overdue_taxes_rub", p.OverdueTaxesRUB)
	checkNumber(c, s+"bank_relationship_months", p.BankRelationshipMonths)
	checkNumber(c, s+"report_verification_level", p.ReportVerificationLevel)
	if p.ReportVerificationLevel != nil && *p.ReportVerificationLevel > 3 {
		c.invalid = append(c.invalid, s+"report_verification_level")
	}
	checkBool(c, s+"organization_and_related_debts_complete", p.OrganizationAndRelatedDebtsComplete)
	checkBool(c, s+"material_register_inaccuracy", p.MaterialRegisterInaccuracy)
	checkBool(c, s+"operating_nonfinancial_company", p.OperatingNonfinancialCompany)
	checkBool(c, s+"liquidated_or_excluded", p.LiquidatedOrExcluded)
	checkEnum(c, s+"auditor_opinion", p.AuditorOpinion, "UNMODIFIED", "NOT_REQUIRED", "MATERIAL_MODIFICATION", "REQUIRED_MISSING")
	checkEnum(c, s+"need_evidence", p.NeedEvidence, "CONTRACT_AND_COST", "VERIFIED_ESTIMATE", "ORAL_ONLY")
	checkRiskFields(c, s, legalRisk(p))
}

// riskFields shares identically defined controls without erasing nullable inputs.
type riskFields struct {
	active, bankrupt, forgery, prohibited, identity, consents, bureau, debts, documents, freeCash, restriction, dispute, buyerLoss, change *bool
	currentDPD, worstDPD, history, business, applications                                                                                  *int
	taxes                                                                                                                                  *int64
}

func ipRisk(p *contracts.IpScoring) riskFields {
	return riskFields{
		active: p.Active, bankrupt: p.Bankrupt, forgery: p.MaterialForgery, prohibited: p.CreditProhibited,
		identity: p.IdentityAndAuthorityVerified, consents: p.ConsentsVerified, bureau: p.BureauReceived, debts: p.DebtsComplete, documents: p.DocumentsComplete, freeCash: p.FreeCashSourceVerified,
		restriction: p.MaterialAccountRestriction, dispute: p.MaterialDisputeOrEnforcement, buyerLoss: p.KeyBuyerLoss, change: p.StructuralBusinessChange,
		currentDPD: p.CurrentDPDDays, worstDPD: p.WorstRepaidDPDDays24M, history: p.CreditHistoryMonths, business: p.BusinessAgeMonths, applications: p.Applications90D, taxes: p.OverdueTaxesRUB,
	}
}
func legalRisk(p *contracts.LegalEntityScoring) riskFields {
	return riskFields{
		active: p.Active, bankrupt: p.Bankrupt, forgery: p.MaterialForgery, prohibited: p.CreditProhibited,
		identity: p.IdentityAndAuthorityVerified, consents: p.ConsentsVerified, bureau: p.BureauReceived, debts: p.DebtsComplete, documents: p.DocumentsComplete, freeCash: p.FreeCashSourceVerified,
		restriction: p.MaterialAccountRestriction, dispute: p.MaterialDisputeOrEnforcement, buyerLoss: p.KeyBuyerLoss, change: p.StructuralBusinessChange,
		currentDPD: p.CurrentDPDDays, worstDPD: p.WorstRepaidDPDDays24M, history: p.CreditHistoryMonths, business: p.BusinessAgeMonths, applications: p.Applications90D, taxes: p.OverdueTaxesRUB,
	}
}
func risk(r contracts.DecisionRequest) (riskFields, string) {
	if r.Borrower.Type == contracts.BorrowerTypeSoleProprietor {
		if r.Scoring != nil && r.Scoring.IP != nil {
			return ipRisk(r.Scoring.IP), "scoring.ip."
		}
		return riskFields{}, "scoring.ip."
	}
	if r.Scoring != nil && r.Scoring.LegalEntity != nil {
		return legalRisk(r.Scoring.LegalEntity), "scoring.legal_entity."
	}
	return riskFields{}, "scoring.legal_entity."
}
func checkRiskFields(c *inputCheck, s string, p riskFields) {
	checkBool(c, s+"active", p.active)
	checkBool(c, s+"bankrupt", p.bankrupt)
	checkBool(c, s+"material_forgery", p.forgery)
	checkBool(c, s+"credit_prohibited", p.prohibited)
	checkBool(c, s+"identity_and_authority_verified", p.identity)
	checkBool(c, s+"consents_verified", p.consents)
	checkBool(c, s+"bureau_received", p.bureau)
	checkBool(c, s+"debts_complete", p.debts)
	checkBool(c, s+"documents_complete", p.documents)
	checkBool(c, s+"free_cash_source_verified", p.freeCash)
	checkBool(c, s+"material_account_restriction", p.restriction)
	checkBool(c, s+"material_dispute_or_enforcement", p.dispute)
	checkBool(c, s+"key_buyer_loss", p.buyerLoss)
	checkBool(c, s+"structural_business_change", p.change)
}
