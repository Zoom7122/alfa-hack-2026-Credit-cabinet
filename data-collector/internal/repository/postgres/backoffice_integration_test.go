//go:build integration

package postgres

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/decision"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/domain"
)

// Рабочее место: заявка после оценки на ручном рассмотрении — в очереди и счётчиках, досье,
// взять в работу (чужую — нельзя), решение с проверкой версии и ключом повтора.
// Пока идёт тест, data-collector-decision-worker стоит остановить.
func TestIntegrationBackoffice(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	suffix := testSuffix()
	now := time.Now().UTC().Truncate(time.Microsecond)
	nano := time.Now().UnixNano()
	companyINN, personINN := fmt.Sprintf("88%08d", nano%1e8), fmt.Sprintf("85%010d", nano%1e10)
	tasksTopic, docsTopic := "it.bo.tasks."+suffix, "it.bo.documents."+suffix
	apps := NewApplicationRepository(pool, tasksTopic, docsTopic)
	decisions := NewDecisionRepository(pool)
	repo := NewBackofficeRepository(pool)
	appID := "app_" + suffix
	sokolova, orlov := "usr_underwriter_sokolova", "usr_underwriter_orlov"
	cleanupApplication(t, pool, appID, companyINN, personINN, tasksTopic, docsTopic)
	t.Cleanup(func() {
		for _, q := range []string{"DELETE FROM decision_requests WHERE application_id = $1", "DELETE FROM decisions WHERE application_id = $1",
			"DELETE FROM offers WHERE application_id = $1", "DELETE FROM documents WHERE application_id = $1", "DELETE FROM info_requests WHERE application_id = $1"} {
			if _, err := pool.Exec(context.Background(), q, appID); err != nil {
				t.Errorf("cleanup %q: %v", q, err)
			}
		}
	})
	for _, u := range []string{sokolova, orlov} { // андеррайтеры заведены API при старте; в пустой базе — заводим
		if _, err := pool.Exec(ctx, `INSERT INTO users (id, full_name, role) VALUES ($1, $1, 'UNDERWRITER') ON CONFLICT (id) DO NOTHING`, u); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := apps.Register(ctx, domain.Registration{UserID: "usr_" + suffix, CompanyID: "apl_" + suffix, ApplicationID: appID, CreatedAt: now,
		Company:        domain.Company{INN: companyINN, Type: domain.CompanyLegalEntity, Name: "ИНН " + companyINN},
		Representative: domain.Representative{FullName: "Тест", INN: personINN, Role: domain.RoleDirector}}); err != nil {
		t.Fatal(err)
	}
	task := domain.CollectionTask{ID: "tsk_" + suffix, ApplicationID: appID, QuestionnaireID: "qst_" + suffix, CreatedAt: now,
		Input: domain.CollectInput{INN: companyINN}}
	if err := apps.Submit(ctx, domain.Submission{ApplicationID: appID, Task: task, SubmittedAt: now,
		Requested: &domain.RequestedTerms{Amount: 5_000_000, TermMonths: 24}, ProductCode: "WORKING_CAPITAL",
		Questionnaire: domain.Questionnaire{ID: "qst_" + suffix, Age: 42, MaritalStatus: domain.MaritalMarried, CreatedAt: now}}); err != nil {
		t.Fatal(err)
	}
	// оценка: ручное рассмотрение с условиями движка
	if _, err := pool.Exec(ctx, `UPDATE applications SET status = 'SCORING' WHERE id = $1`, appID); err != nil {
		t.Fatal(err)
	}
	review := decision.Interpret(decision.Response{Status: "UNDER_REVIEW", Score: &decision.Score{Total: 738, Grade: "B"},
		Reasons: []decision.Reason{{Code: "MANY_CREDIT_APPLICATIONS", Message: "Много обращений"}},
		Terms:   &decision.LoanTerms{AmountRUB: 3_000_000, TermMonths: 36, AnnualRate: 22}}, decision.Facts{AuthorityVerified: true})
	deadline := now.Add(4 * time.Hour)
	if err := decisions.SaveOutcome(ctx, appID, review, map[string]any{"engine": map[string]any{"status": "UNDER_REVIEW",
		"reasons": []map[string]string{{"code": "MANY_CREDIT_APPLICATIONS", "message": "Много обращений"}}},
		"proposed_offer": review.Offer}, &deadline, now); err != nil {
		t.Fatal(err)
	}

	// очередь: наша заявка среди ручных, с причиной и сроком
	items, err := repo.Queue(ctx, domain.QueueFilter{Statuses: []domain.ApplicationStatus{domain.ApplicationManualReview}, Assignee: "none", Limit: 500})
	if err != nil {
		t.Fatal(err)
	}
	i := slices.IndexFunc(items, func(it domain.QueueItem) bool { return it.ApplicationID == appID })
	if i < 0 || items[i].Grade != "B" || *items[i].Score != 738 || items[i].ReviewReasons[0] != "Много обращений" ||
		items[i].DeadlineAt == nil || items[i].Requested.Amount != 5_000_000 || items[i].Assignee != nil {
		t.Fatalf("queue item: %+v", items)
	}
	before, err := repo.Counters(ctx, sokolova)
	if err != nil || before.ManualReview < 1 {
		t.Fatalf("counters: %+v %v", before, err)
	}

	// взять в работу: Орлов — да, Соколова — уже нельзя; повтор Орлова — без изменений
	if ok, err := repo.Assign(ctx, appID, orlov); err != nil || !ok {
		t.Fatalf("assign: %v %v", ok, err)
	}
	if _, err := repo.Assign(ctx, appID, sokolova); err != domain.ErrAssignedToOther {
		t.Fatalf("assign taken: %v", err)
	}
	if _, err := repo.Assign(ctx, appID, orlov); err != nil {
		t.Fatalf("assign again: %v", err)
	}
	if ok, _ := repo.Assign(ctx, "app_nope", orlov); ok {
		t.Fatal("unknown application assigned")
	}
	mine, _ := repo.Counters(ctx, orlov)
	if mine.Mine < 1 {
		t.Errorf("mine counter: %+v", mine)
	}

	d, ok, err := repo.Dossier(ctx, appID)
	if err != nil || !ok || d.Assignee == nil || d.Assignee.ID != orlov || d.Decision == nil || d.Decision.AutoOutcome != "MANUAL_REVIEW" || d.DeadlineAt == nil {
		t.Fatalf("dossier: %+v (%v)", d, err)
	}

	// решение: чужое — нельзя, старая версия — нельзя, затем одобрение с ключом повтора
	cmd := domain.UnderwriterDecision{ApplicationID: appID, UserID: sokolova, Action: "APPROVE", Outcome: "APPROVED_WITH_CHANGES",
		Status: domain.ApplicationOffered, Message: "Банк одобрил кредит", Now: now, IdempotencyKey: "it-" + suffix,
		Offers:      []domain.OfferInput{{Kind: "REQUESTED", ProductCode: "WORKING_CAPITAL", Amount: 3_000_000, TermMonths: 36, Rate: 22, ValidDays: 7}},
		Adjustments: []domain.Adjustment{{Code: "GOV_CONTRACTS", Points: 15}}, InternalComment: "обращения — рефинансирование"}
	if _, err := repo.Decide(ctx, cmd); err != domain.ErrAssignedToOther {
		t.Fatalf("decide by other: %v", err)
	}
	cmd.UserID = orlov
	stale := d.Version - 1
	cmd.IfMatch = &stale
	if _, err := repo.Decide(ctx, cmd); err != domain.ErrVersionConflict {
		t.Fatalf("stale version: %v", err)
	}
	cmd.IfMatch = &d.Version
	if ok, err := repo.Decide(ctx, cmd); err != nil || !ok {
		t.Fatalf("decide: %v %v", ok, err)
	}
	if prev, done, err := repo.DecisionApplied(ctx, cmd.IdempotencyKey); err != nil || !done || prev != appID {
		t.Fatalf("idempotency key: %s %v %v", prev, done, err)
	}
	if ok, err := repo.Decide(ctx, cmd); err != nil || !ok { // повтор — без второго решения
		t.Fatalf("replay: %v %v", ok, err)
	}

	d, _, err = repo.Dossier(ctx, appID)
	if err != nil || d.Application.Status != domain.ApplicationOffered || d.DeadlineAt != nil || d.Decision.Outcome != "APPROVED_WITH_CHANGES" ||
		d.Decision.DecidedBy != orlov || d.Decision.Comment != "обращения — рефинансирование" {
		t.Fatalf("after decision: %+v (%v)", d.Decision, err)
	}
	if len(d.Offers) != 1 || d.Offers[0].Amount != 3_000_000 || d.Offers[0].Status != "ACTIVE" || d.Offers[0].MonthlyPayment <= 0 {
		t.Fatalf("offers: %+v", d.Offers)
	}
	var types []string
	for _, e := range d.Events {
		types = append(types, e.Type)
	}
	if !slices.Contains(types, "uw.assigned") || !slices.Contains(types, "uw.adjustment") || !slices.Contains(types, "uw.comment") ||
		types[len(types)-1] != "uw.decision" || count(types, "uw.decision") != 1 {
		t.Fatalf("events: %v", types)
	}
	if d.Staff[orlov].ID != orlov || d.Events[len(d.Events)-1].ActorRole != domain.UserUnderwriter {
		t.Errorf("staff: %+v", d.Staff)
	}
	if _, err := repo.Decide(ctx, domain.UnderwriterDecision{ApplicationID: appID, UserID: orlov, Action: "REJECT"}); err != domain.ErrNotInReview {
		t.Fatalf("decide after decision: %v", err)
	}

	// запрос данных: вопросы со сроком, статус WAITING_FOR_INFO
	if _, err := pool.Exec(ctx, `UPDATE applications SET status = 'MANUAL_REVIEW' WHERE id = $1`, appID); err != nil {
		t.Fatal(err)
	}
	due := now.Add(5 * 24 * time.Hour)
	if _, err := repo.Decide(ctx, domain.UnderwriterDecision{ApplicationID: appID, UserID: orlov, Action: "REQUEST_INFO", Outcome: "NEED_INFO",
		Status: domain.ApplicationWaitingForInfo, Message: "Нужны сведения", Now: now, InfoDueAt: due,
		InfoRequests: []domain.InfoRequestInput{{Title: "Выписка за 12 месяцев", Required: true}}}); err != nil {
		t.Fatal(err)
	}
	d, _, _ = repo.Dossier(ctx, appID)
	if d.Application.Status != domain.ApplicationWaitingForInfo || len(d.InfoRequests) != 1 || d.InfoRequests[0].DueAt == nil ||
		!d.InfoRequests[0].DueAt.Equal(due) || len(d.Offers) != 1 {
		t.Fatalf("after request info: %s %+v", d.Application.Status, d.InfoRequests)
	}

	// клиент отвечает на вопрос андеррайтера с документом — заявка возвращается к нему со сроком
	doc := domain.Document{ID: "doc_" + suffix + "_ans", ApplicationID: appID, Type: domain.DocBankStatement, FileName: "statement.pdf",
		MimeType: "application/pdf", SizeBytes: 5, SHA256: strings.Repeat("c", 64), StorageKey: appID + "/ans.pdf",
		SignatureState: domain.SignatureNotSigned, RecognitionState: domain.RecognitionPending, UploadedAt: now}
	next, ok, err := apps.AnswerInfoRequest(ctx, domain.InfoAnswer{ApplicationID: appID, RequestID: d.InfoRequests[0].ID, ActorID: "usr_" + suffix,
		CompanyINN: companyINN, Comment: "выписка", Documents: []domain.Document{doc}, Now: now, ReviewSLA: 4 * time.Hour})
	if err != nil || !ok || next != domain.ApplicationManualReview {
		t.Fatalf("answer underwriter question: %s %v %v", next, ok, err)
	}
	if _, _, err := apps.AnswerInfoRequest(ctx, domain.InfoAnswer{ApplicationID: appID, RequestID: d.InfoRequests[0].ID, Now: now}); err != domain.ErrNotWaitingForInfo {
		t.Fatalf("answer when not waiting: %v", err)
	}
	d, _, _ = repo.Dossier(ctx, appID)
	if d.Application.Status != domain.ApplicationManualReview || d.DeadlineAt == nil || d.Assignee == nil || d.Assignee.ID != orlov ||
		d.InfoRequests[0].Status != "ANSWERED" || !strings.Contains(string(d.InfoRequests[0].Answer), doc.ID) {
		t.Fatalf("after answer: %s %+v %+v", d.Application.Status, d.Assignee, d.InfoRequests)
	}

	// вопрос движка: ответ уточняет анкету scoring, заявка — на повторную оценку
	if _, err := pool.Exec(ctx, `UPDATE applications SET status = 'WAITING_FOR_INFO' WHERE id = $1`, appID); err != nil {
		t.Fatal(err)
	}
	var engineQ string
	if err := pool.QueryRow(ctx, `INSERT INTO info_requests (id, application_id, required, question) VALUES ($1, $2, true,
		'{"code":"FREE_CASH","source":"credit-engine","input":{"type":"MONEY","scoring_field":"free_cash"}}') RETURNING id`,
		"ir_"+suffix+"_e", appID).Scan(&engineQ); err != nil {
		t.Fatal(err)
	}
	next, _, err = apps.AnswerInfoRequest(ctx, domain.InfoAnswer{ApplicationID: appID, RequestID: engineQ, ActorID: "usr_" + suffix,
		Value: []byte(`20000`), ScoringPatch: map[string]any{"free_cash": 20000}, Now: now})
	if err != nil || next != domain.ApplicationVerifying {
		t.Fatalf("answer engine question: %s %v", next, err)
	}
	var freeCash int
	if err := pool.QueryRow(ctx, `SELECT (form -> 'scoring' ->> 'free_cash')::int FROM applications WHERE id = $1`, appID).Scan(&freeCash); err != nil || freeCash != 20000 {
		t.Fatalf("scoring patch: %d %v", freeCash, err)
	}
	if codes, err := decisions.AnsweredCodes(ctx, appID); err != nil || !codes["FREE_CASH"] {
		t.Fatalf("answered codes: %v %v", codes, err)
	}
}

func count(items []string, v string) int {
	n := 0
	for _, x := range items {
		if x == v {
			n++
		}
	}
	return n
}
