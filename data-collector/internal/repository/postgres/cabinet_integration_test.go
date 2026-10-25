//go:build integration

package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/domain"
)

// Кабинет: компании пользователя, список заявок (черновики коллег не видны, mine), заявки пачкой
// с документами и вердиктом подписи, отчёт по документу и по его .sig, журнал.
func TestIntegrationCabinet(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	suffix := testSuffix()
	now := time.Now().UTC().Truncate(time.Microsecond)
	nano := time.Now().UnixNano()
	companyINN := fmt.Sprintf("88%08d", nano%1e8)
	directorINN, accountantINN := fmt.Sprintf("88%010d", nano%1e10), fmt.Sprintf("87%010d", nano%1e10)
	tasksTopic, docsTopic := "it.cab.tasks."+suffix, "it.cab.documents."+suffix
	apps := NewApplicationRepository(pool, tasksTopic, docsTopic)
	sigs := NewSignatureRepository(pool)
	repo := NewCabinetRepository(pool)
	director, accountant := "usr_"+suffix+"_d", "usr_"+suffix+"_a"
	submitted, draft := "app_"+suffix+"_1", "app_"+suffix+"_2"
	t.Cleanup(func() {
		ctx := context.Background()
		for _, q := range []string{
			`DELETE FROM signature_reports WHERE document_id IN (SELECT id FROM documents WHERE application_id = ANY($1))`,
			`DELETE FROM raw_collections WHERE task_id IN (SELECT id FROM collection_tasks WHERE application_id = ANY($1))`,
			`DELETE FROM collection_tasks WHERE application_id = ANY($1)`,
			`DELETE FROM documents WHERE application_id = ANY($1) AND signed_document_id IS NOT NULL`,
			`DELETE FROM documents WHERE application_id = ANY($1)`,
			`DELETE FROM application_events WHERE application_id = ANY($1)`,
			`DELETE FROM applications WHERE id = ANY($1)`,
		} {
			if _, err := pool.Exec(ctx, q, []string{submitted, draft}); err != nil {
				t.Errorf("cleanup %q: %v", q, err)
			}
		}
		for _, q := range []struct {
			sql string
			arg any
		}{
			{"DELETE FROM outbox WHERE topic = ANY($1)", []string{tasksTopic, docsTopic}},
			{"DELETE FROM company_members WHERE company_id IN (SELECT id FROM companies WHERE inn = $1)", companyINN},
			{"DELETE FROM companies WHERE inn = $1", companyINN},
			{"DELETE FROM users WHERE id = ANY($1)", []string{director, accountant}},
		} {
			if _, err := pool.Exec(ctx, q.sql, q.arg); err != nil {
				t.Errorf("cleanup %q: %v", q.sql, err)
			}
		}
	})

	company := domain.Company{INN: companyINN, Type: domain.CompanyLegalEntity, Name: "ИНН " + companyINN}
	for _, r := range []domain.Registration{
		{UserID: director, CompanyID: "apl_" + suffix, ApplicationID: submitted, Company: company, CreatedAt: now,
			Representative: domain.Representative{FullName: "Ковалёв Игорь Сергеевич", INN: directorINN, Role: domain.RoleDirector, CanSign: true}},
		{UserID: accountant, CompanyID: "apl_" + suffix + "_x", ApplicationID: draft, Company: company, CreatedAt: now.Add(time.Second),
			Representative: domain.Representative{FullName: "Литвинова Марина Олеговна", INN: accountantINN, Role: domain.RoleAccountant}},
	} {
		if _, err := apps.Register(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	doc := domain.Document{ID: "doc_" + suffix + "_a", ApplicationID: submitted, Type: domain.DocFinancialStatements,
		FileName: "balance.pdf", MimeType: "application/pdf", SizeBytes: 10, SHA256: strings.Repeat("a", 64),
		StorageKey: submitted + "/doc_a.pdf", SignatureState: domain.SignaturePending, RecognitionState: domain.RecognitionPending, UploadedAt: now}
	sig := domain.Document{ID: "doc_" + suffix + "_b", ApplicationID: submitted, Type: domain.DocSignature,
		FileName: "balance.pdf.sig", MimeType: "application/pkcs7-signature", SizeBytes: 3, SHA256: strings.Repeat("b", 64),
		StorageKey: submitted + "/doc_b.sig", SignedDocumentID: doc.ID, SignatureState: domain.SignaturePending,
		RecognitionState: domain.RecognitionNotApplicable, UploadedAt: now}
	task := domain.CollectionTask{ID: "tsk_" + suffix, ApplicationID: submitted, QuestionnaireID: "qst_" + suffix, CreatedAt: now,
		DocumentIDs: []string{doc.ID, sig.ID}, Input: domain.CollectInput{INN: companyINN}}
	if err := apps.Submit(ctx, domain.Submission{ApplicationID: submitted, Documents: []domain.Document{doc, sig}, Task: task,
		SubmittedAt: now, Questionnaire: domain.Questionnaire{ID: "qst_" + suffix, Age: 42, MaritalStatus: domain.MaritalMarried,
			CreatedAt: now}}); err != nil {
		t.Fatal(err)
	}

	// компании пользователя
	companies, err := repo.CompaniesOf(ctx, director)
	if err != nil || len(companies) != 1 || companies[0].INN != companyINN || companies[0].Role != domain.RoleDirector || !companies[0].CanSign {
		t.Fatalf("companies = %+v (%v)", companies, err)
	}
	if known, err := repo.CompanyKnown(ctx, companyINN); err != nil || !known {
		t.Fatalf("company known: %v %v", known, err)
	}

	// список: черновик бухгалтера директору не виден; бухгалтеру видны оба, с mine — только свой
	for _, tc := range []struct {
		f    domain.ApplicationFilter
		want []string
	}{
		{domain.ApplicationFilter{UserID: director, Limit: 100}, []string{submitted}},
		{domain.ApplicationFilter{UserID: accountant, Limit: 100}, []string{draft, submitted}},
		{domain.ApplicationFilter{UserID: accountant, OnlyMine: true, Limit: 100}, []string{draft}},
		{domain.ApplicationFilter{UserID: accountant, Statuses: []domain.ApplicationStatus{domain.ApplicationSubmitted}, Limit: 100}, []string{submitted}},
		{domain.ApplicationFilter{UserID: accountant, Limit: 1}, []string{draft}},
		{domain.ApplicationFilter{UserID: accountant, CompanyID: "apl_nope", Limit: 100}, nil},
	} {
		ids, err := repo.ApplicationIDs(ctx, tc.f)
		if err != nil || !slices.Equal(ids, tc.want) {
			t.Errorf("ApplicationIDs(%+v) = %v (%v), want %v", tc.f, ids, err, tc.want)
		}
	}

	// отчёт о подписи: по документу и по .sig; вердикт — в документах заявки
	report := json.RawMessage(`{"profile":"TEST","verdict":"VALID_WITH_WARNINGS"}`)
	if saved, err := sigs.SaveReport(ctx, domain.SignatureReport{ID: "sigr_" + suffix, ApplicationID: submitted, DocumentID: doc.ID,
		SignatureDocumentID: sig.ID, DocumentSHA256: doc.SHA256, SignatureSHA256: sig.SHA256, Profile: "TEST", TrustMode: "MOCK",
		Policy: "FINANCIAL_STATEMENTS", Verdict: "VALID_WITH_WARNINGS", Integrity: domain.VerificationVerified,
		Format: domain.VerificationVerified, Certificate: domain.VerificationVerified, Identity: domain.VerificationVerified,
		Authority: domain.VerificationVerified, Decision: domain.VerificationVerified, IdempotencyKey: "it:" + suffix,
		DocumentState: domain.SignatureDone, Report: report, VerifiedAt: now}); err != nil || !saved {
		t.Fatalf("save report: %v %v", saved, err)
	}
	for _, id := range []string{doc.ID, sig.ID} {
		got, ok, err := repo.LatestSignatureReport(ctx, submitted, id)
		if err != nil || !ok || !strings.Contains(string(got), "VALID_WITH_WARNINGS") {
			t.Errorf("report of %s: %s %v %v", id, got, ok, err)
		}
	}
	if _, ok, err := repo.LatestSignatureReport(ctx, draft, doc.ID); err != nil || ok {
		t.Errorf("report via another application: %v %v", ok, err)
	}

	// заявки пачкой — в порядке id, пропавшие пропускаются, документы с вердиктом
	list, err := apps.List(ctx, []string{draft, "app_nope", submitted})
	if err != nil || len(list) != 2 || list[0].ID != draft || list[1].ID != submitted || len(list[0].Documents) != 0 {
		t.Fatalf("list = %+v (%v)", list, err)
	}
	if docs := list[1].Documents; len(docs) != 2 || docs[0].SignatureVerdict != "VALID_WITH_WARNINGS" ||
		docs[1].SignedDocumentID != doc.ID || list[1].Representative.UserID != director || list[1].Representative.HasLogin {
		t.Fatalf("submitted = %+v", list[1])
	}

	events, err := repo.Events(ctx, submitted)
	if err != nil || len(events) < 2 || events[0].Type != "application.created" {
		t.Fatalf("events = %+v (%v)", events, err)
	}
}
