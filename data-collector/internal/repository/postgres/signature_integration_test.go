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

// Контекст заявки для worker'а подписи (до и после сбора), запись отчёта с ключом идемпотентности:
// повторная запись ничего не меняет, документ и подпись — DONE с вердиктом, событие в журнале.
func TestIntegrationSignatureReports(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	suffix := testSuffix()
	now := time.Now().UTC().Truncate(time.Microsecond)
	nano := time.Now().UnixNano()
	companyINN, personINN := fmt.Sprintf("88%08d", nano%1e8), fmt.Sprintf("88%010d", nano%1e10)
	tasksTopic, docsTopic := "it.sig.tasks."+suffix, "it.sig.documents."+suffix
	apps := NewApplicationRepository(pool, tasksTopic, docsTopic)
	tasks := NewCollectionTaskRepository(pool)
	repo := NewSignatureRepository(pool)
	appID := "app_" + suffix
	cleanupApplication(t, pool, appID, companyINN, personINN, tasksTopic, docsTopic)
	t.Cleanup(func() { // раньше документов: отчёты ссылаются на них
		if _, err := pool.Exec(context.Background(), `DELETE FROM signature_reports WHERE document_id IN
			(SELECT id FROM documents WHERE application_id = $1)`, appID); err != nil {
			t.Errorf("cleanup signature_reports: %v", err)
		}
	})

	if _, err := apps.Register(ctx, domain.Registration{UserID: "usr_" + suffix, CompanyID: "apl_" + suffix, ApplicationID: appID,
		Representative: domain.Representative{FullName: "Литвинова Марина Олеговна", INN: personINN, Role: domain.RoleAccountant},
		Company:        domain.Company{INN: companyINN, Type: domain.CompanyLegalEntity, Name: "ИНН " + companyINN}, CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	doc := domain.Document{ID: "doc_" + suffix + "_a", ApplicationID: appID, Type: domain.DocFinancialStatements,
		FileName: "balance.pdf", MimeType: "application/pdf", SizeBytes: 10, SHA256: strings.Repeat("a", 64),
		StorageKey: appID + "/doc_a.pdf", SignatureState: domain.SignaturePending, RecognitionState: domain.RecognitionPending, UploadedAt: now}
	sig := domain.Document{ID: "doc_" + suffix + "_b", ApplicationID: appID, Type: domain.DocSignature,
		FileName: "balance.pdf.sig", MimeType: "application/pkcs7-signature", SizeBytes: 3, SHA256: strings.Repeat("b", 64),
		StorageKey: appID + "/doc_b.sig", SignedDocumentID: doc.ID, SignatureState: domain.SignaturePending,
		RecognitionState: domain.RecognitionNotApplicable, UploadedAt: now}
	task := domain.CollectionTask{ID: "tsk_" + suffix, ApplicationID: appID, QuestionnaireID: "qst_" + suffix, CreatedAt: now,
		DocumentIDs: []string{doc.ID, sig.ID}, Input: domain.CollectInput{INN: companyINN, POANumber: "0f5a2c1e-7b3d-4e8a-9c21-6d4b8e0a1f37",
			RepresentativeINN: personINN}}
	if err := apps.Submit(ctx, domain.Submission{ApplicationID: appID, Documents: []domain.Document{doc, sig}, Task: task, SubmittedAt: now,
		Questionnaire: domain.Questionnaire{ID: "qst_" + suffix, Age: 42, MaritalStatus: domain.MaritalMarried, CreatedAt: now}}); err != nil {
		t.Fatal(err)
	}

	docs, err := repo.Documents(ctx, appID)
	if err != nil || len(docs) != 2 || docs[1].SignedDocumentID != doc.ID || docs[0].StorageKey != doc.StorageKey {
		t.Fatalf("documents = %+v (%v)", docs, err)
	}
	sc, ok, err := repo.Context(ctx, appID)
	if err != nil || !ok || sc.Company.INN != companyINN || sc.Company.Type != domain.CompanyLegalEntity ||
		sc.Representative.INN != personINN || sc.Representative.Role != domain.RoleAccountant || sc.Collection == nil ||
		sc.Collection.Status != domain.TaskPending || sc.Collection.Finished() || sc.Collection.CollectionID != "" ||
		sc.Collection.Input.RepresentativeINN != personINN {
		t.Fatalf("context before collection = %+v (ok %v, err %v)", sc, ok, err)
	}

	// сбор завершён: ответы ФНС и МЧД — в контексте
	if _, _, err := tasks.Claim(ctx, task.ID, 3); err != nil {
		t.Fatal(err)
	}
	fns, m4d := `{"egrul":{"rows":[{"i":"`+companyINN+`","k":"ul"}]}}`, `{"search":{"items":[]},"poa":null}`
	if err := tasks.Complete(ctx, domain.RawCollection{ID: "col_" + suffix, TaskID: task.ID, QuestionnaireID: task.QuestionnaireID,
		Input: task.Input, StartedAt: now, FinishedAt: now.Add(time.Second), Systems: []domain.SystemResult{
			{System: domain.SystemFNS, State: domain.CollectDone, Data: json.RawMessage(fns)},
			{System: domain.SystemM4D, State: domain.CollectPartial, Data: json.RawMessage(m4d)},
		}}, nil); err != nil {
		t.Fatal(err)
	}
	sc, _, err = repo.Context(ctx, appID)
	if err != nil || sc.Collection.Status != domain.TaskCompleted || sc.Collection.CollectionID != "col_"+suffix ||
		!strings.Contains(string(sc.Collection.FNS), companyINN) || sc.Collection.M4D == nil ||
		sc.Collection.FNSState != domain.CollectDone || sc.Collection.M4DState != domain.CollectPartial {
		t.Fatalf("context after collection = %+v (%v)", sc.Collection, err)
	}

	key := "auto:TEST:" + doc.ID + "@" + doc.SHA256 + ":" + sig.ID + "@" + sig.SHA256
	rep := domain.SignatureReport{ID: "sigr_" + suffix, ApplicationID: appID, DocumentID: doc.ID, SignatureDocumentID: sig.ID,
		DocumentSHA256: doc.SHA256, SignatureSHA256: sig.SHA256, Profile: "TEST", TrustMode: "MOCK", Policy: "FINANCIAL_STATEMENTS",
		Verdict: "INVALID", Integrity: domain.VerificationVerified, Format: domain.VerificationVerified,
		Certificate: domain.VerificationVerified, Identity: domain.VerificationVerified, Authority: domain.VerificationInvalid,
		Decision: domain.VerificationInvalid, FailureReason: "полномочия: у подписанта нет действующей МЧД", IdempotencyKey: key,
		DocumentState: domain.SignatureDone, Report: json.RawMessage(`{"profile":"TEST"}`), VerifiedAt: now}
	if exists, err := repo.ReportExists(ctx, key); err != nil || exists {
		t.Fatalf("exists before save: %v, %v", exists, err)
	}
	if saved, err := repo.SaveReport(ctx, rep); err != nil || !saved {
		t.Fatalf("save: %v, %v", saved, err)
	}
	again := rep
	again.ID, again.Decision, again.Verdict = "sigr_"+suffix+"_2", domain.VerificationVerified, "VALID_WITH_WARNINGS"
	if saved, err := repo.SaveReport(ctx, again); err != nil || saved {
		t.Fatalf("second save with the same key: %v, %v", saved, err)
	}
	if exists, err := repo.ReportExists(ctx, key); err != nil || !exists {
		t.Fatalf("exists after save: %v, %v", exists, err)
	}

	var (
		n                                                         int
		decision, authority, profile, verdict, docSHA, reason, sd string
		satisfied                                                 bool
	)
	if err := pool.QueryRow(ctx, `SELECT count(*), min(decision), min(authority_status), min(profile), min(verdict),
		min(document_sha256), min(failure_reason), min(signature_document_id), bool_and(policy_satisfied)
		FROM signature_reports WHERE document_id = $1`, doc.ID).
		Scan(&n, &decision, &authority, &profile, &verdict, &docSHA, &reason, &sd, &satisfied); err != nil || n != 1 ||
		decision != "INVALID" || authority != "INVALID" || profile != "TEST" || verdict != "INVALID" || docSHA != doc.SHA256 ||
		!strings.HasPrefix(reason, "полномочия") || sd != sig.ID || satisfied {
		t.Fatalf("signature_reports: %d rows, %s %s %s %s %s %q %s %v (%v)", n, decision, authority, profile, verdict, docSHA, reason, sd,
			satisfied, err)
	}
	for _, id := range []string{doc.ID, sig.ID} {
		var state, docVerdict string
		if err := pool.QueryRow(ctx, `SELECT signature_state, signature_verdict FROM documents WHERE id = $1`, id).
			Scan(&state, &docVerdict); err != nil || state != "DONE" || docVerdict != "INVALID" {
			t.Fatalf("document %s: %s %s (%v)", id, state, docVerdict, err)
		}
	}
	if types := eventTypes(t, pool, appID); slices.Index(types, "document.signatures_verified") < 0 ||
		slices.Index(types, "document.signatures_verified") != len(types)-1 {
		t.Fatalf("events = %v", types)
	}

	// отчёт worker'а без статуса этапа не пройдёт CHECK из миграции
	broken := rep
	broken.ID, broken.IdempotencyKey, broken.Identity = "sigr_"+suffix+"_3", key+":broken", ""
	if _, err := repo.SaveReport(ctx, broken); err == nil {
		t.Fatal("report without identity status must violate signature_reports_stages_chk")
	}
}
