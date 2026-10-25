package scoring

import (
	"fmt"
	"strconv"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/contracts"
)

func question(q *contracts.QuestionnaireResult, id string, answer any, points, max int) {
	q.Criteria = append(q.Criteria, contracts.QuestionnaireCriterion{ID: id, Answer: fmt.Sprint(answer), Points: points, MaxPoints: max})
	q.Points += points
}
func ipQuestionnaire(r contracts.DecisionRequest) contracts.QuestionnaireResult {
	p := r.Scoring.IP
	q := contracts.QuestionnaireResult{MaxPoints: 750}
	add := func(n int, answer any, points, max int) {
		if n <= 21 {
			fields := [21]string{"owner_age", "marital_status", "dependents", "primary_income_source", "qualification", "total_experience_months", "net_monthly_income_rub", "car_class", "bank_relationship_months", "education_level", "real_estate_status", "credit_history_months", "worst_repaid_dpd_days_24m", "current_dpd_days", "applications_90d", "business_age_months", "owner_experience_years", "business_location", "overdue_taxes_rub", "income_evidence", "buyer_contracts"}
			answer = fields[n-1] + "=" + fmt.Sprint(answer)
		}
		question(&q, fmt.Sprintf("IP_Q%02d", n), answer, points, max)
	}
	x := val(p.OwnerAge)
	points := 5
	switch {
	case x < 20:
		points = 5
	case x <= 25:
		points = 10
	case x <= 30:
		points = 15
	case x <= 35:
		points = 30
	case x <= 49:
		points = 25
	case x <= 65:
		points = 20
	}
	add(1, x, points, 30)
	points = 0
	switch val(p.MaritalStatus) {
	case contracts.MaritalStatusMarried:
		points = 25
	case contracts.MaritalStatusNeverMarried, contracts.MaritalStatusWidowed:
		points = 20
	case contracts.MaritalStatusDivorcedOnce:
		points = 15
	case contracts.MaritalStatusDivorcedMultiple:
		points = 5
	}
	add(2, val(p.MaritalStatus), points, 25)
	x = val(p.Dependents)
	points = 5
	switch x {
	case 0:
		points = 25
	case 1:
		points = 20
	case 2:
		points = 15
	case 3:
		points = 10
	}
	add(3, x, points, 25)
	points = 0
	switch val(p.PrimaryIncomeSource) {
	case contracts.IncomeSourceBusiness, contracts.IncomeSourcePublicEmployment:
		points = 25
	case contracts.IncomeSourcePrivateEmployment:
		points = 20
	case contracts.IncomeSourcePension, contracts.IncomeSourceSelfEmployedOrGph:
		points = 15
	}
	add(4, val(p.PrimaryIncomeSource), points, 25)
	points = 0
	switch val(p.Qualification) {
	case contracts.QualificationSupport:
		points = 5
	case contracts.QualificationSpecialist:
		points = 15
	case contracts.QualificationManagerOrHighlyQualified:
		points = 25
	}
	add(5, val(p.Qualification), points, 25)
	x = val(p.TotalExperienceMonths)
	points = 0
	switch {
	case x == 0:
	case x < 36:
		points = 10
	case x < 72:
		points = 25
	default:
		points = 35
	}
	add(6, x, points, 35)
	income := val(p.NetMonthlyIncomeRUB)
	points = 0
	switch {
	case income <= 15000:
	case income <= 30000:
		points = 15
	case income <= 60000:
		points = 30
	case income <= 90000:
		points = 40
	default:
		points = 50
	}
	add(7, income, points, 50)
	points = 10
	switch val(p.CarClass) {
	case contracts.CarClassComfort:
		points = 15
	case contracts.CarClassPremium:
		points = 25
	}
	add(8, val(p.CarClass), points, 25)
	add(9, val(p.BankRelationshipMonths), bankPoints(val(p.BankRelationshipMonths)), 30)
	points = 0
	switch val(p.EducationLevel) {
	case contracts.EducationLevelGeneral:
		points = 5
	case contracts.EducationLevelVocational:
		points = 10
	case contracts.EducationLevelHigher:
		points = 20
	case contracts.EducationLevelAcademicDegree:
		points = 25
	}
	add(10, val(p.EducationLevel), points, 25)
	points = 0
	switch val(p.RealEstateStatus) {
	case contracts.RealEstateStatusUnencumberedWhole:
		points = 25
	case contracts.RealEstateStatusUnencumberedShare:
		points = 15
	case contracts.RealEstateStatusEncumberedOnly:
		points = 10
	}
	add(11, val(p.RealEstateStatus), points, 25)
	add(12, val(p.CreditHistoryMonths), historyPoints(val(p.CreditHistoryMonths)), 50)
	add(13, val(p.WorstRepaidDPDDays24M), worstPoints(val(p.WorstRepaidDPDDays24M)), 100)
	add(14, val(p.CurrentDPDDays), currentPoints(val(p.CurrentDPDDays)), 25)
	add(15, val(p.Applications90D), applicationPoints(val(p.Applications90D)), 30)
	add(16, val(p.BusinessAgeMonths), businessPoints(val(p.BusinessAgeMonths)), 60)
	x = val(p.OwnerExperienceYears)
	points = 0
	switch {
	case x < 1:
	case x < 3:
		points = 15
	case x < 5:
		points = 25
	case x < 8:
		points = 40
	default:
		points = 50
	}
	add(17, x, points, 50)
	points = 0
	location := string(val(p.BusinessLocation))
	switch val(p.BusinessLocation) {
	case contracts.BusinessLocationVerifiedOnlineOrMobile:
		points = 20
	case contracts.BusinessLocationVerifiedPhysical:
		x = val(p.LocationStabilityMonths)
		location += "; location_stability_months=" + strconv.Itoa(x)
		switch {
		case x < 12:
			points = 5
		case x < 24:
			points = 15
		default:
			points = 20
		}
	}
	add(18, location, points, 20)
	tax := val(p.OverdueTaxesRUB)
	points = 0
	switch {
	case tax == 0:
		points = 45
	case tax <= 50000:
		points = 30
	case tax <= 200000:
		points = 15
	}
	add(19, tax, points, 45)
	points = 0
	switch val(p.IncomeEvidence) {
	case contracts.IncomeEvidenceVerified:
		points = 30
	case contracts.IncomeEvidenceDocumentsPendingCrosscheck:
		points = 15
	}
	add(20, val(p.IncomeEvidence), points, 30)
	points = 0
	switch val(p.BuyerContracts) {
	case contracts.BuyerContractsActiveAndPerformed:
		points = 20
	case contracts.BuyerContractsRepeatOrders:
		points = 10
	}
	add(21, val(p.BuyerContracts), points, 20)
	// Evaluate calls the questionnaire only after the actual completeness and
	// control checks. CONFIRMED means the facts were checked, including manual
	// risk events; it does not mean the application is approved.
	for n := 22; n <= 30; n++ {
		add(n, "CONFIRMED", 0, 0)
	}
	military := contracts.MilitaryStatusOtherOrUnknown
	if p.MilitaryStatus != nil {
		military = *p.MilitaryStatus
	}
	add(31, military, 0, 0)
	return q
}

func legalQuestionnaire(r contracts.DecisionRequest) contracts.QuestionnaireResult {
	p := r.Scoring.LegalEntity
	q := contracts.QuestionnaireResult{MaxPoints: 750}
	add := func(n int, answer any, points, max int) {
		if n <= 20 {
			fields := [20]string{"business_age_months", "credit_history_months", "worst_repaid_dpd_days_24m", "current_dpd_days", "applications_90d", "manager_experience_years", "overdue_taxes_rub", "report_verification_level", "bank_relationship_months", "organization_and_related_debts_complete", "material_register_inaccuracy", "auditor_opinion", "material_account_restriction", "material_dispute_or_enforcement", "key_buyer_loss", "structural_business_change", "need_evidence", "documents_complete", "free_cash_source_verified", "operating_nonfinancial_company"}
			answer = fields[n-1] + "=" + fmt.Sprint(answer)
		}
		question(&q, fmt.Sprintf("LE_Q%02d", n), answer, points, max)
	}
	add(1, val(p.BusinessAgeMonths), businessPoints(val(p.BusinessAgeMonths)), 60)
	add(2, val(p.CreditHistoryMonths), historyPoints(val(p.CreditHistoryMonths)), 50)
	add(3, val(p.WorstRepaidDPDDays24M), worstPoints(val(p.WorstRepaidDPDDays24M)), 100)
	add(4, val(p.CurrentDPDDays), currentPoints(val(p.CurrentDPDDays)), 25)
	add(5, val(p.Applications90D), applicationPoints(val(p.Applications90D)), 30)
	x := val(p.ManagerExperienceYears)
	points := 0
	switch {
	case x < 1:
	case x < 3:
		points = 10
	case x < 5:
		points = 20
	case x < 8:
		points = 30
	default:
		points = 35
	}
	add(6, x, points, 35)
	tax := val(p.OverdueTaxesRUB)
	points = 0
	switch {
	case tax == 0:
		points = 50
	case tax <= 100000:
		points = 35
	case tax <= 500000:
		points = 15
	}
	add(7, tax, points, 50)
	x = val(p.ReportVerificationLevel)
	points = 0
	switch x {
	case 1:
		points = 15
	case 2:
		points = 35
	case 3:
		points = 50
	}
	add(8, x, points, 50)
	add(9, val(p.BankRelationshipMonths), bankPoints(val(p.BankRelationshipMonths)), 30)
	points = 0
	if yes(p.OrganizationAndRelatedDebtsComplete) {
		points = 30
	}
	add(10, val(p.OrganizationAndRelatedDebtsComplete), points, 30)
	points = 0
	if p.MaterialRegisterInaccuracy != nil && !*p.MaterialRegisterInaccuracy {
		points = 30
	}
	add(11, val(p.MaterialRegisterInaccuracy), points, 30)
	points = 0
	switch val(p.AuditorOpinion) {
	case contracts.AuditorOpinionUnmodified, contracts.AuditorOpinionNotRequired:
		points = 35
	}
	add(12, val(p.AuditorOpinion), points, 35)
	points = 0
	if p.MaterialAccountRestriction != nil && !*p.MaterialAccountRestriction {
		points = 35
	}
	add(13, val(p.MaterialAccountRestriction), points, 35)
	points = 0
	if p.MaterialDisputeOrEnforcement != nil && !*p.MaterialDisputeOrEnforcement {
		points = 35
	}
	add(14, val(p.MaterialDisputeOrEnforcement), points, 35)
	points = 0
	if p.KeyBuyerLoss != nil && !*p.KeyBuyerLoss {
		points = 30
	}
	add(15, val(p.KeyBuyerLoss), points, 30)
	points = 0
	if p.StructuralBusinessChange != nil && !*p.StructuralBusinessChange {
		points = 30
	}
	add(16, val(p.StructuralBusinessChange), points, 30)
	points = 0
	switch val(p.NeedEvidence) {
	case contracts.NeedEvidenceContractAndCost:
		points = 30
	case contracts.NeedEvidenceVerifiedEstimate:
		points = 20
	}
	add(17, val(p.NeedEvidence), points, 30)
	points = 0
	if yes(p.DocumentsComplete) {
		points = 30
	}
	add(18, val(p.DocumentsComplete), points, 30)
	points = 0
	if yes(p.FreeCashSourceVerified) {
		points = 20
	}
	add(19, val(p.FreeCashSourceVerified), points, 20)
	points = 0
	if yes(p.OperatingNonfinancialCompany) {
		points = 15
	}
	add(20, val(p.OperatingNonfinancialCompany), points, 15)
	for n := 21; n <= 27; n++ {
		add(n, "CONFIRMED", 0, 0)
	}
	return q
}

func bankPoints(x int) int {
	switch {
	case x == 0:
		return 0
	case x < 12:
		return 10
	case x < 24:
		return 20
	default:
		return 30
	}
}
func historyPoints(x int) int {
	switch {
	case x == 0:
		return 0
	case x < 6:
		return 10
	case x < 18:
		return 25
	case x < 36:
		return 40
	default:
		return 50
	}
}
func worstPoints(x int) int {
	switch {
	case x == 0:
		return 100
	case x < 8:
		return 75
	case x < 30:
		return 50
	case x < 60:
		return 25
	default:
		return 0
	}
}
func currentPoints(x int) int {
	switch {
	case x == 0:
		return 25
	case x < 8:
		return 15
	case x < 30:
		return 5
	default:
		return 0
	}
}
func applicationPoints(x int) int {
	switch {
	case x == 0:
		return 30
	case x < 3:
		return 20
	case x < 5:
		return 15
	case x < 8:
		return 5
	default:
		return 0
	}
}
func businessPoints(x int) int {
	switch {
	case x < 12:
		return 0
	case x < 24:
		return 15
	case x < 36:
		return 30
	case x < 60:
		return 45
	default:
		return 60
	}
}
