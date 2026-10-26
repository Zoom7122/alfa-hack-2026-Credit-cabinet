//go:build integration

package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/decision"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/domain"
)

// Оценка: заявка после сбора берётся в SCORING, вход собирается из анкеты и raw_collections,
// итог пишет decisions, offers, info_requests, статус и событие; бухотчётность для Б, события МС3.
// Пока идёт тест, data-collector-decision-worker стоит остановить: он берёт те же заявки.
func TestIntegrationDecision(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	suffix := testSuffix()
	now := time.Now().UTC().Truncate(time.Microsecond)
	nano := time.Now().UnixNano()
	companyINN, personINN := fmt.Sprintf("88%08d", nano%1e8), fmt.Sprintf("86%010d", nano%1e10)
	tasksTopic, docsTopic := "it.dec.tasks."+suffix, "it.dec.documents."+suffix
	apps := NewApplicationRepository(pool, tasksTopic, docsTopic)
	tasks := NewCollectionTaskRepository(pool)
	repo := NewDecisionRepository(pool)
	appID := "app_" + suffix
	cleanupApplication(t, pool, appID, companyINN, personINN, tasksTopic, docsTopic)
	t.Cleanup(func() { // раньше заявки: ссылаются на неё
		for _, q := range []string{"DELETE FROM decisions WHERE application_id = $1", "DELETE FROM offers WHERE application_id = $1",
			"DELETE FROM info_requests WHERE application_id = $1"} {
			if _, err := pool.Exec(context.Background(), q, appID); err != nil {
				t.Errorf("cleanup %q: %v", q, err)
			}
		}
	})

	company := domain.Company{INN: companyINN, Type: domain.CompanyLegalEntity, Name: "ИНН " + companyINN}
	if _, err := apps.Register(ctx, domain.Registration{UserID: "usr_" + suffix, CompanyID: "apl_" + suffix, ApplicationID: appID,
		Company: company, CreatedAt: now, Representative: domain.Representative{FullName: "Тест", INN: personINN, Role: domain.RoleDirector}}); err != nil {
		t.Fatal(err)
	}
	task := domain.CollectionTask{ID: "tsk_" + suffix, ApplicationID: appID, QuestionnaireID: "qst_" + suffix, CreatedAt: now,
		Input: domain.CollectInput{INN: companyINN, BKICompanyConsent: &domain.Consent{ID: "CNS-1", ValidUntil: "2027-04-21"}}}
	answers := &domain.ScoringAnswers{MaxAnnualRate: decisionPtr(28.0), NeedEvidence: "VERIFIED_ESTIMATE", ExperienceYears: decisionPtr(8)}
	if err := apps.Submit(ctx, domain.Submission{ApplicationID: appID, Task: task, SubmittedAt: now, Scoring: answers,
		Requested: &domain.RequestedTerms{Amount: 5_000_000, TermMonths: 24}, Purpose: &domain.Purpose{Code: "WORKING_CAPITAL"},
		Questionnaire: domain.Questionnaire{ID: "qst_" + suffix, Age: 42, MaritalStatus: domain.MaritalMarried, ChildrenCount: 2, CreatedAt: now}}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := tasks.Claim(ctx, task.ID, 3); err != nil {
		t.Fatal(err)
	}
	fns := `{"egrul":{"rows":[{"r":"14.03.2021"}]},"bo_bfo":[{"period":"2025","typeCorrections":[{"correction":{"balance":{"current1600":21430,"previous1600":17260},"financialResult":{"current2110":58420}}}]}]}`
	if err := tasks.Complete(ctx, domain.RawCollection{ID: "col_" + suffix, TaskID: task.ID, QuestionnaireID: task.QuestionnaireID,
		Input: task.Input, StartedAt: now, FinishedAt: now.Add(time.Second), Systems: []domain.SystemResult{
			{System: domain.SystemFNS, State: domain.CollectDone, Data: json.RawMessage(fns)},
		}}, nil); err != nil {
		t.Fatal(err)
	}

	// заявка без подписей в проверке — сразу в оценку (среди других VERIFYING в базе ищем свою)
	claimed := false
	for range 200 {
		id, ok, err := repo.ClaimNext(ctx, time.Hour, time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			break
		}
		if id == appID {
			claimed = true
			break
		}
	}
	if !claimed {
		t.Fatal("application is not claimed for scoring")
	}
	if id, ok, _ := repo.ClaimNext(ctx, time.Hour, time.Hour); ok && id == appID {
		t.Fatal("application in SCORING claimed twice")
	}

	in, err := repo.Input(ctx, appID, now)
	if err != nil {
		t.Fatal(err)
	}
	if in.Age != 42 || in.Children != 2 || in.Scoring == nil || *in.Scoring.MaxAnnualRate != 28 || in.Collect.BKICompanyConsent == nil ||
		!strings.Contains(string(in.Systems[domain.SystemFNS]), "bo_bfo") || in.Application.Requested.Amount != 5_000_000 || in.ForgedDocument {
		t.Fatalf("input = %+v", in)
	}
	req, facts := decision.Build(in)
	if req.Scoring.LegalEntity == nil || *req.Scoring.LegalEntity.BusinessAgeMonths <= 0 {
		t.Fatalf("request from input: %+v", req.Scoring)
	}

	// итог: вопросы клиенту
	out := decision.Interpret(decision.Response{Status: "NEED_INFO", Reasons: []decision.Reason{
		{Code: "MISSING_DATA", Message: "Нужны данные", Fields: []string{"need.confirmed_cost_rub", "monthly_history"}}}}, facts)
	if err := repo.SaveOutcome(ctx, appID, out, map[string]any{"engine": "test"}, nil, now.Add(24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	var status string
	var questions int
	if err := pool.QueryRow(ctx, `SELECT status, (SELECT count(*) FROM info_requests WHERE application_id = $1 AND status = 'OPEN')
		FROM applications WHERE id = $1`, appID).Scan(&status, &questions); err != nil || status != "WAITING_FOR_INFO" || questions != 2 {
		t.Fatalf("after need info: %s, %d questions (%v)", status, questions, err)
	}
	if types := eventTypes(t, pool, appID); types[len(types)-1] != "scoring.completed" || types[len(types)-2] != "scoring.started" {
		t.Fatalf("events = %v", types)
	}
	if err := repo.SaveOutcome(ctx, appID, out, nil, nil, now); err == nil {
		t.Fatal("second outcome for application not in SCORING must fail")
	}

	// одобрение — предложение клиенту
	if _, err := pool.Exec(ctx, `UPDATE applications SET status = 'SCORING' WHERE id = $1`, appID); err != nil {
		t.Fatal(err)
	}
	approved := decision.Interpret(decision.Response{Status: "APPROVED", TermsConfirmed: true, Score: &decision.Score{Total: 873, Grade: "A"},
		Terms: &decision.LoanTerms{AmountRUB: 2_030_000, TermMonths: 36, AnnualRate: 20.75}}, facts)
	if err := repo.SaveOutcome(ctx, appID, approved, nil, nil, now.Add(14*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	var amount, payment int64
	var grade, auto string
	if err := pool.QueryRow(ctx, `SELECT o.amount, o.monthly_payment, d.grade, d.auto_outcome FROM offers o
		JOIN decisions d ON d.application_id = o.application_id AND d.auto_outcome = 'APPROVED' WHERE o.application_id = $1`, appID).
		Scan(&amount, &payment, &grade, &auto); err != nil || amount != 2_030_000 || payment <= 0 || grade != "A" {
		t.Fatalf("offer: %d %d %s %s (%v)", amount, payment, grade, auto, err)
	}

	// поставщик Б — бухотчётность компании по последнему сбору
	raw, ok, err := repo.CompanyFNS(ctx, "apl_"+suffix)
	if err != nil || !ok || !strings.Contains(string(raw), "bo_bfo") {
		t.Fatalf("company fns: %v %v", ok, err)
	}
	if _, ok, _ := repo.CompanyFNS(ctx, "apl_nope"); ok {
		t.Fatal("unknown company must have no fns")
	}

	// МС3: событие пишется один раз, неизвестная заявка — пропуск
	ev := decision.CreditEvent{EventID: appID + ":LOAN_ISSUED", Kind: "LOAN_ISSUED", ApplicationID: appID}
	for i, want := range []bool{true, false} {
		if saved, err := repo.SaveCreditEvent(ctx, ev); err != nil || saved != want {
			t.Fatalf("credit event #%d: %v %v", i, saved, err)
		}
	}
	if saved, err := repo.SaveCreditEvent(ctx, decision.CreditEvent{EventID: "x", ApplicationID: "app_nope"}); err != nil || saved {
		t.Fatalf("credit event for unknown application: %v %v", saved, err)
	}
}

func decisionPtr[T any](v T) *T { return &v }
