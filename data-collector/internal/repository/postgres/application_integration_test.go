//go:build integration

package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/domain"
)

// cleanupApplication удаляет после теста заявку со всем, что к ней относится, компанию и пользователя.
func cleanupApplication(t *testing.T, pool *pgxpool.Pool, appID, companyINN, personINN string, topics ...string) {
	t.Helper()
	t.Cleanup(func() {
		ctx := context.Background()
		for _, q := range []struct {
			sql string
			arg any
		}{
			{"DELETE FROM raw_collections WHERE task_id IN (SELECT id FROM collection_tasks WHERE application_id = $1)", appID},
			{"DELETE FROM collection_tasks WHERE application_id = $1", appID},
			{"DELETE FROM outbox WHERE topic = ANY($1)", topics},
			{"DELETE FROM documents WHERE application_id = $1 AND signed_document_id IS NOT NULL", appID},
			{"DELETE FROM documents WHERE application_id = $1", appID},
			{"DELETE FROM application_events WHERE application_id = $1", appID},
			{"DELETE FROM applications WHERE id = $1", appID},
			{"DELETE FROM company_members WHERE company_id IN (SELECT id FROM companies WHERE inn = $1)", companyINN},
			{"DELETE FROM companies WHERE inn = $1", companyINN},
			{"DELETE FROM users WHERE inn = $1", personINN},
		} {
			if _, err := pool.Exec(ctx, q.sql, q.arg); err != nil {
				t.Errorf("cleanup %q: %v", q.sql, err)
			}
		}
	})
}

func eventTypes(t *testing.T, pool *pgxpool.Pool, appID string) []string {
	t.Helper()
	rows, err := pool.Query(context.Background(), `SELECT type FROM application_events WHERE application_id = $1 ORDER BY id`, appID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var types []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatal(err)
		}
		types = append(types, s)
	}
	return types
}

// Регистрация → отправка анкеты с документами → worker: заявка DRAFT → SUBMITTED → ENRICHING → VERIFYING,
// компания получает реквизиты из ЕГРЮЛ.
func TestIntegrationApplicationFlow(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	suffix := testSuffix()
	now := time.Now().UTC().Truncate(time.Microsecond)
	nano := time.Now().UnixNano()
	companyINN, personINN := fmt.Sprintf("88%08d", nano%1e8), fmt.Sprintf("88%010d", nano%1e10)
	tasksTopic, docsTopic := "it.tasks."+suffix, "it.documents."+suffix
	repo := NewApplicationRepository(pool, tasksTopic, docsTopic)
	tasks := NewCollectionTaskRepository(pool)
	cleanupApplication(t, pool, "app_"+suffix, companyINN, personINN, tasksTopic, docsTopic)

	reg := domain.Registration{UserID: "usr_" + suffix, CompanyID: "apl_" + suffix, ApplicationID: "app_" + suffix,
		Representative: domain.Representative{FullName: "Литвинова Марина Олеговна", INN: personINN,
			Role: domain.RoleAccountant},
		Company:   domain.Company{INN: companyINN, Type: domain.CompanyLegalEntity, Name: "ИНН " + companyINN},
		CreatedAt: now}
	app, err := repo.Register(ctx, reg)
	if err != nil {
		t.Fatal(err)
	}
	if app.ID != reg.ApplicationID || app.Status != domain.ApplicationDraft || !regexp.MustCompile(`^A-\d{4}-\d{6}$`).MatchString(app.Number) ||
		app.Company.AlreadyKnown || app.Company.ID != reg.CompanyID || app.Representative.UserID != reg.UserID ||
		app.Representative.Role != domain.RoleAccountant || app.Representative.CanSign {
		t.Fatalf("registered = %+v", app)
	}

	// тот же человек (по ИНН) и та же компания (по ИНН): новые id не нужны, черновик тот же, роль обновлена
	reg2 := reg
	reg2.UserID, reg2.CompanyID, reg2.ApplicationID = "usr_"+suffix+"_2", "apl_"+suffix+"_2", "app_"+suffix+"_2"
	reg2.Representative.Role, reg2.Representative.FullName = domain.RoleCFO, "Литвинова М. О."
	again, err := repo.Register(ctx, reg2)
	if err != nil {
		t.Fatal(err)
	}
	if again.ID != app.ID || !again.Company.AlreadyKnown || again.Representative.UserID != reg.UserID ||
		again.Representative.Role != domain.RoleCFO || again.Representative.FullName != "Литвинова М. О." {
		t.Fatalf("second registration = %+v", again)
	}

	// отправка: подпись в списке раньше документа — в БД документ всё равно пишется первым
	doc := domain.Document{ID: "doc_" + suffix + "_a", ApplicationID: app.ID, Type: domain.DocFinancialStatements,
		FileName: "balance.pdf", MimeType: "application/pdf", SizeBytes: 10, SHA256: strings.Repeat("a", 64),
		StorageKey: app.ID + "/doc_a.pdf", UploadedBy: reg.UserID, SignatureState: domain.SignaturePending,
		RecognitionState: domain.RecognitionPending, UploadedAt: now}
	sig := domain.Document{ID: "doc_" + suffix + "_b", ApplicationID: app.ID, Type: domain.DocSignature,
		FileName: "balance.pdf.sig", MimeType: "application/pkcs7-signature", SizeBytes: 3, SHA256: strings.Repeat("b", 64),
		StorageKey: app.ID + "/doc_b.sig", UploadedBy: reg.UserID, SignedDocumentID: doc.ID,
		SignatureState: domain.SignaturePending, RecognitionState: domain.RecognitionNotApplicable, UploadedAt: now}
	task := domain.CollectionTask{ID: "tsk_" + suffix, ApplicationID: app.ID, QuestionnaireID: "qst_" + suffix,
		DocumentIDs: []string{doc.ID, sig.ID}, CreatedAt: now,
		Input: domain.CollectInput{INN: companyINN, OFDToken: "secret-ofd",
			Director: &domain.Person{INN: "779900010139", LastName: "Ковалёв", FirstName: "Игорь", BirthDate: "1984-07-19"}}}
	sub := domain.Submission{ApplicationID: app.ID, ActorID: reg.UserID,
		Questionnaire: domain.Questionnaire{ID: "qst_" + suffix, Age: 42, MaritalStatus: domain.MaritalMarried, ChildrenCount: 2, CreatedAt: now},
		ProductCode:   "WORKING_CAPITAL", Requested: &domain.RequestedTerms{Amount: 5_000_000, TermMonths: 24},
		Purpose:   &domain.Purpose{Code: "WORKING_CAPITAL", Description: "Закупка товара"},
		Documents: []domain.Document{sig, doc}, Task: task, SubmittedAt: now}
	if err := repo.Submit(ctx, sub); err != nil {
		t.Fatal(err)
	}
	if err := repo.Submit(ctx, sub); !errors.Is(err, domain.ErrApplicationNotDraft) {
		t.Fatalf("second submit: %v", err)
	}

	got, ok, err := repo.Get(ctx, app.ID)
	if err != nil || !ok || got.Status != domain.ApplicationSubmitted || !got.SubmittedAt.Equal(now) || got.ProductCode != "WORKING_CAPITAL" ||
		got.Requested == nil || got.Requested.Amount != 5_000_000 || got.Purpose == nil || got.Purpose.Description != "Закупка товара" ||
		got.CollectionTaskID != task.ID || len(got.Documents) != 2 {
		t.Fatalf("submitted = %+v (ok %v, err %v)", got, ok, err)
	}
	if d := got.Documents[1]; d.ID != sig.ID || d.SignedDocumentID != doc.ID || d.RecognitionState != domain.RecognitionNotApplicable {
		t.Fatalf("signature = %+v", d)
	}
	var (
		formQuestionnaire, form string
		docIDs                  []string
		outboxTopics            []string
	)
	if err := pool.QueryRow(ctx, `SELECT form -> 'questionnaire' ->> 'id', form::text FROM applications WHERE id = $1`, app.ID).
		Scan(&formQuestionnaire, &form); err != nil || formQuestionnaire != sub.Questionnaire.ID || strings.Contains(form, "secret") {
		t.Fatalf("form = %s (%v)", form, err)
	}
	if err := pool.QueryRow(ctx, `SELECT document_ids FROM collection_tasks WHERE id = $1 AND application_id = $2`, task.ID, app.ID).
		Scan(&docIDs); err != nil || !slices.Equal(docIDs, task.DocumentIDs) {
		t.Fatalf("task documents = %v (%v)", docIDs, err)
	}
	if err := pool.QueryRow(ctx, `SELECT array_agg(topic || ':' || key ORDER BY id) FROM outbox WHERE topic = ANY($1)`,
		[]string{tasksTopic, docsTopic}).Scan(&outboxTopics); err != nil ||
		!slices.Equal(outboxTopics, []string{tasksTopic + ":" + task.ID, docsTopic + ":" + app.ID}) {
		t.Fatalf("outbox = %v (%v)", outboxTopics, err)
	}
	var docEvent json.RawMessage
	if err := pool.QueryRow(ctx, `SELECT payload FROM outbox WHERE topic = $1`, docsTopic).Scan(&docEvent); err != nil ||
		!strings.Contains(string(docEvent), `"signed_document_id": "`+doc.ID+`"`) {
		t.Fatalf("documents event = %s (%v)", docEvent, err)
	}

	// worker: захват → ENRICHING, результат → VERIFYING и реквизиты компании
	if _, ok, err := tasks.Claim(ctx, task.ID, 3); err != nil || !ok {
		t.Fatalf("claim: ok %v, err %v", ok, err)
	}
	if got, _, _ := repo.Get(ctx, app.ID); got.Status != domain.ApplicationEnriching {
		t.Fatalf("after claim: %s", got.Status)
	}
	rc := domain.RawCollection{ID: "col_" + suffix, TaskID: task.ID, QuestionnaireID: task.QuestionnaireID, Input: task.Input,
		Systems:   []domain.SystemResult{{System: domain.SystemFNS, State: domain.CollectDone, Data: json.RawMessage(`{"egrul":{}}`)}},
		StartedAt: now, FinishedAt: now.Add(time.Second)}
	card := &domain.CompanyCard{INN: companyINN, Type: domain.CompanyLegalEntity, OGRN: "1217700412345", KPP: "779901001",
		FullName: `ОБЩЕСТВО С ОГРАНИЧЕННОЙ ОТВЕТСТВЕННОСТЬЮ "ИТ"`, ShortName: `ООО "ИТ"`, RegistrationDate: "2021-03-14",
		DirectorName: "Ковалёв Игорь Сергеевич"}
	if err := tasks.Complete(ctx, rc, card); err != nil {
		t.Fatal(err)
	}
	got, _, _ = repo.Get(ctx, app.ID)
	if got.Status != domain.ApplicationVerifying || got.Company.Name != `ООО "ИТ"` || got.Company.OGRN != "1217700412345" {
		t.Fatalf("after complete = %+v", got.Company)
	}
	var kpp, directorINN string
	if err := pool.QueryRow(ctx, `SELECT registry ->> 'kpp', director_inn FROM companies WHERE inn = $1`, companyINN).
		Scan(&kpp, &directorINN); err != nil || kpp != "779901001" || directorINN != "779900010139" {
		t.Fatalf("company registry: kpp %q, director %q (%v)", kpp, directorINN, err)
	}
	want := []string{"application.created", "application.submitted", "enrich.started", "enrich.completed"}
	if types := eventTypes(t, pool, app.ID); !slices.Equal(types, want) {
		t.Fatalf("events = %v, want %v", types, want)
	}
}

func TestIntegrationApplicationNotFound(t *testing.T) {
	pool := testPool(t)
	if _, ok, err := NewApplicationRepository(pool, "t", "d").Get(context.Background(), "app_"+testSuffix()); err != nil || ok {
		t.Fatalf("ok %v, err %v", ok, err)
	}
}
