package sigworker_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/domain"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/events"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/repository/memory"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/signature"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/signature/sigtest"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/sigworker"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/storage"
)

var now = time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)

const appID = "app_test"

// Ответы ФНС и реестра МЧД, как их сохранил сбор данных (raw_collections.fns, .m4d) — по сидам mock-api.
const (
	fnsCollected = `{"egrul":{"rows":[{"c":"ООО \"СЕВЕРНЫЙ ВЕТЕР\"","i":"7799000012","o":"1217700412345",
		"g":"ГЕНЕРАЛЬНЫЙ ДИРЕКТОР: Ковалёв Игорь Сергеевич","k":"ul"}]},
		"pb":{"vyp":{"masruk":[{"inn":"779900010139","name":"КОВАЛЁВ ИГОРЬ СЕРГЕЕВИЧ","position":"ГЕНЕРАЛЬНЫЙ ДИРЕКТОР"}]}}}`
	m4dCollected = `{"search":{"items":[{"number":"0f5a2c1e-7b3d-4e8a-9c21-6d4b8e0a1f37","status":"ACTIVE",
		"valid_from":"2026-01-15","valid_until":"2027-01-14","principal":{"inn":"7799000012"},
		"representative":{"inn":"779900010333","snils":"148-562-907 41","full_name":"Литвинова Марина Олеговна"},
		"powers":[{"code":"LOAN_APPLICATION_SUBMIT"},{"code":"FINANCIAL_DOCS_SIGN"}]}]},"poa":null}`
)

// testPKI — тестовый УЦ с CRL и сертификатом Литвиновой (представитель «Северного ветра» по МЧД).
type testPKI struct {
	trust     *signature.TrustStore
	litvinova *sigtest.Signer
}

var (
	pkiOnce sync.Once
	pkiVal  testPKI
	pkiErr  error
)

func pki(t *testing.T) testPKI {
	t.Helper()
	pkiOnce.Do(func() {
		var ca *sigtest.CA
		if ca, pkiErr = sigtest.NewCA("Тестовый корневой УЦ (TEST)", now.AddDate(-1, 0, 0), now.AddDate(5, 0, 0)); pkiErr != nil {
			return
		}
		if pkiVal.litvinova, pkiErr = ca.Issue(sigtest.Subject{Surname: "Литвинова", GivenName: "Марина Олеговна",
			INN: "779900010333", SNILS: "148-562-907 41"}, sigtest.CertOptions{NotBefore: now.AddDate(0, -6, 0),
			NotAfter: now.AddDate(1, 0, 0)}); pkiErr != nil {
			return
		}
		var der []byte
		if der, pkiErr = ca.CRL(now.Add(-time.Hour), now.Add(24*time.Hour)); pkiErr != nil {
			return
		}
		var crl *x509.RevocationList
		if crl, pkiErr = x509.ParseRevocationList(der); pkiErr != nil {
			return
		}
		pkiVal.trust = signature.NewTrustStore([]*x509.Certificate{ca.Cert}, nil, []*x509.RevocationList{crl})
	})
	if pkiErr != nil {
		t.Fatal(pkiErr)
	}
	return pkiVal
}

// recordingFiles — хранилище, которое запоминает прочитанные ключи; fail — сколько первых чтений падает.
type recordingFiles struct {
	files *storage.Reader
	fail  atomic.Int32

	mu   sync.Mutex
	keys []string
}

func (f *recordingFiles) Read(key string) ([]byte, error) {
	f.mu.Lock()
	f.keys = append(f.keys, key)
	f.mu.Unlock()
	if f.fail.Add(-1) >= 0 {
		return nil, errors.New("i/o error")
	}
	return f.files.Read(key)
}

func (f *recordingFiles) read() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.keys...)
}

// countingVerifier считает проверки.
type countingVerifier struct {
	v     *signature.Verifier
	calls atomic.Int32
}

func (c *countingVerifier) Verify(ctx context.Context, in signature.Input, load signature.ApplicantLoader) (*signature.Report, error) {
	c.calls.Add(1)
	return c.v.Verify(ctx, in, load)
}

// flakyRepo — хранилище, у которого первые saveFailures записей отчёта падают, а контекст заявки
// отдаётся со сбором RUNNING первые runningFor раз.
type flakyRepo struct {
	*memory.SignatureRepository
	saveFailures atomic.Int32
	runningFor   atomic.Int32
	saves        atomic.Int32
}

func (r *flakyRepo) SaveReport(ctx context.Context, rep domain.SignatureReport) (bool, error) {
	r.saves.Add(1)
	if r.saveFailures.Add(-1) >= 0 {
		return false, errors.New("db down")
	}
	return r.SignatureRepository.SaveReport(ctx, rep)
}

func (r *flakyRepo) Context(ctx context.Context, id string) (domain.SignatureContext, bool, error) {
	sc, ok, err := r.SignatureRepository.Context(ctx, id)
	if r.runningFor.Add(-1) >= 0 && sc.Collection != nil {
		c := *sc.Collection
		c.Status, c.FNS, c.M4D = domain.TaskRunning, nil, nil
		sc.Collection = &c
	}
	return sc, ok, err
}

type testEnv struct {
	root     string
	repo     *flakyRepo
	files    *recordingFiles
	verifier *countingVerifier
	logs     *bytes.Buffer
	w        *sigworker.Worker
	ids      atomic.Int32
}

func newEnv(t *testing.T, cfg sigworker.Config) *testEnv {
	t.Helper()
	root := t.TempDir()
	reader, err := storage.NewReader(root, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reader.Close() })
	e := &testEnv{root: root, repo: &flakyRepo{SignatureRepository: memory.NewSignatureRepository()},
		files: &recordingFiles{files: reader}, logs: &bytes.Buffer{},
		verifier: &countingVerifier{v: signature.NewVerifier(pki(t).trust, signature.DefaultRules(),
			signature.WithClock(func() time.Time { return now }))}}
	if cfg.Attempts == 0 {
		cfg.Attempts = 3
	}
	if cfg.RetryBackoff == 0 {
		cfg.RetryBackoff = time.Millisecond
	}
	if cfg.PollInterval == 0 {
		cfg.PollInterval = time.Millisecond
	}
	if cfg.AuthorityWait == 0 {
		cfg.AuthorityWait = time.Second
	}
	log := slog.New(slog.NewJSONHandler(e.logs, nil))
	e.w = sigworker.New(e.repo, e.files, e.verifier, cfg, log, sigworker.WithIDGenerator(func() string {
		return "sigr_" + string(rune('A'+e.ids.Add(1)-1))
	}))
	e.repo.SetContext(completedContext())
	return e
}

func completedContext() domain.SignatureContext {
	return domain.SignatureContext{ApplicationID: appID,
		Company:        domain.Company{INN: "7799000012", Type: domain.CompanyLegalEntity, OGRN: "1217700412345", Name: `ООО "СЕВЕРНЫЙ ВЕТЕР"`},
		Representative: domain.Representative{UserID: "usr_1", INN: "779900010333", Role: domain.RoleAccountant},
		Collection: &domain.SignatureCollection{TaskID: "tsk_1", Status: domain.TaskCompleted, CollectionID: "col_1",
			Input: domain.CollectInput{INN: "7799000012", POANumber: "0f5a2c1e-7b3d-4e8a-9c21-6d4b8e0a1f37", RepresentativeINN: "779900010333"},
			FNS:   json.RawMessage(fnsCollected), M4D: json.RawMessage(m4dCollected),
			FNSState: domain.CollectDone, M4DState: domain.CollectDone}}
}

// addDoc кладёт файл в хранилище ({app}/{id}{ext}) и строку в documents.
func (e *testEnv) addDoc(t *testing.T, id, name, mime string, typ domain.DocumentType, data []byte, signs string) domain.Document {
	t.Helper()
	sum := sha256.Sum256(data)
	d := domain.Document{ID: id, ApplicationID: appID, Type: typ, FileName: name, MimeType: mime, SizeBytes: int64(len(data)),
		SHA256: hex.EncodeToString(sum[:]), StorageKey: appID + "/" + id + path.Ext(name), SignedDocumentID: signs,
		SignatureState: domain.SignaturePending}
	if mime == "application/pdf" && signs == "" && typ != domain.DocSignature {
		d.SignatureState = domain.SignatureNotSigned
	}
	if err := os.MkdirAll(filepath.Join(e.root, appID), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(e.root, filepath.FromSlash(d.StorageKey)), data, 0o640); err != nil {
		t.Fatal(err)
	}
	e.repo.AddDocuments(d)
	return d
}

// addPair — документ и его открепленная подпись Литвиновой.
func (e *testEnv) addPair(t *testing.T, content []byte) (doc, sig domain.Document) {
	t.Helper()
	s, err := sigtest.Sign(content, pki(t).litvinova, sigtest.SignOptions{SigningTime: now.Add(-time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	doc = e.addDoc(t, "doc_1", "balance.pdf", "application/pdf", domain.DocFinancialStatements, content, "")
	sig = e.addDoc(t, "doc_2", "balance.pdf.sig", "application/pkcs7-signature", domain.DocSignature, s, doc.ID)
	return doc, sig
}

// event — сообщение documents.submitted, как его публикует outbox.
func event(t *testing.T, docs ...domain.Document) []byte {
	t.Helper()
	b, err := json.Marshal(events.NewDocumentsSubmitted(appID, "7799000012", docs, now))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func handle(t *testing.T, e *testEnv, msg []byte) {
	t.Helper()
	if err := e.w.Handle(context.Background(), msg); err != nil {
		t.Fatal(err)
	}
}

func onlyReport(t *testing.T, e *testEnv) domain.SignatureReport {
	t.Helper()
	reports := e.repo.Reports()
	if len(reports) != 1 {
		t.Fatalf("reports = %d, want 1", len(reports))
	}
	return reports[0]
}

func stages(r domain.SignatureReport) string {
	return strings.Join([]string{string(r.Integrity), string(r.Format), string(r.Certificate), string(r.Identity),
		string(r.Authority), "→", string(r.Decision)}, " ")
}

// Открепленная подпись к документу: отчёт со статусами этапов, документ и подпись — DONE с вердиктом.
func TestHandleDetachedSignature(t *testing.T) {
	e := newEnv(t, sigworker.Config{})
	content := []byte("%PDF-1.4 баланс СЕВЕРНЫЙ-ВЕТЕР-ВЫРУЧКА-48200000")
	doc, sig := e.addPair(t, content)

	handle(t, e, event(t, doc, sig))

	r := onlyReport(t, e)
	if got := stages(r); got != "VERIFIED VERIFIED VERIFIED VERIFIED VERIFIED → VERIFIED" {
		t.Fatalf("stages = %s, reason %q", got, r.FailureReason)
	}
	if r.ID != "sigr_A" || r.ApplicationID != appID || r.DocumentID != doc.ID || r.SignatureDocumentID != sig.ID ||
		r.DocumentSHA256 != doc.SHA256 || r.SignatureSHA256 != sig.SHA256 || r.Profile != "TEST" || r.TrustMode != "MOCK" ||
		r.Policy != "FINANCIAL_STATEMENTS" || r.Verdict != "VALID_WITH_WARNINGS" || r.DocumentState != domain.SignatureDone ||
		r.IdempotencyKey != "auto:TEST:doc_1@"+doc.SHA256+":doc_2@"+sig.SHA256 || !r.VerifiedAt.Equal(now) {
		t.Fatalf("report = %+v", r)
	}
	var body signature.Report
	if err := json.Unmarshal(r.Report, &body); err != nil {
		t.Fatal(err)
	}
	if body.ID != r.ID || body.DocumentID != doc.ID || body.SignatureDocumentID != sig.ID || body.Authority.Basis != "POWER_OF_ATTORNEY" ||
		!strings.Contains(body.Authority.Source, "col_1") {
		t.Fatalf("report body = %s", r.Report)
	}
	for _, id := range []string{doc.ID, sig.ID} {
		if state, verdict := e.repo.Signature(appID, id); state != domain.SignatureDone || verdict != "VALID_WITH_WARNINGS" {
			t.Fatalf("document %s: %s %s", id, state, verdict)
		}
	}
	if ev := e.repo.Events(appID); len(ev) != 1 || ev[0] != "document.signatures_verified" {
		t.Fatalf("events = %v", ev)
	}
	// содержимое документа не попадает ни в отчёт, ни в логи
	if bytes.Contains(r.Report, []byte("ВЫРУЧКА")) || strings.Contains(e.logs.String(), "ВЫРУЧКА") ||
		!strings.Contains(e.logs.String(), `"msg":"signature verified"`) {
		t.Fatalf("content leaked or no log line:\n%s", e.logs.String())
	}
}

// Повторная доставка события не создаёт второй отчёт и не запускает проверку заново.
func TestHandleDuplicateEvent(t *testing.T) {
	e := newEnv(t, sigworker.Config{})
	doc, sig := e.addPair(t, []byte("%PDF-1.4 баланс"))
	msg := event(t, doc, sig)

	for range 3 {
		handle(t, e, msg)
	}
	onlyReport(t, e)
	if calls := e.verifier.calls.Load(); calls != 1 {
		t.Fatalf("verifier called %d times", calls)
	}

	// параллельные доставки одного события: запись защищена ключом идемпотентности
	e2 := newEnv(t, sigworker.Config{})
	doc, sig = e2.addPair(t, []byte("%PDF-1.4 баланс"))
	msg = event(t, doc, sig)
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			if err := e2.w.Handle(context.Background(), msg); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	onlyReport(t, e2)
}

// Нет файла документа или подписи — отчёт INDETERMINATE, состояние FAILED; сообщение подтверждается.
func TestHandleMissingFile(t *testing.T) {
	for _, missing := range []string{"document", "signature"} {
		t.Run(missing, func(t *testing.T) {
			e := newEnv(t, sigworker.Config{})
			doc, sig := e.addPair(t, []byte("%PDF-1.4 баланс"))
			gone := doc
			if missing == "signature" {
				gone = sig
			}
			if err := os.Remove(filepath.Join(e.root, filepath.FromSlash(gone.StorageKey))); err != nil {
				t.Fatal(err)
			}

			handle(t, e, event(t, doc, sig))

			r := onlyReport(t, e)
			if r.Decision != domain.VerificationIndeterminate || r.Integrity != domain.VerificationIndeterminate ||
				r.DocumentState != domain.SignatureFailed || !strings.Contains(r.FailureReason, "не найден в хранилище") ||
				r.DocumentSHA256 != doc.SHA256 {
				t.Fatalf("report = %s, reason %q, state %s", stages(r), r.FailureReason, r.DocumentState)
			}
			if state, _ := e.repo.Signature(appID, doc.ID); state != domain.SignatureFailed {
				t.Fatalf("document state = %s", state)
			}
		})
	}
}

// Путь к файлу берётся из БД, а не из события; путь в чужую папку или наружу не читается.
func TestHandleStorageKeys(t *testing.T) {
	e := newEnv(t, sigworker.Config{})
	doc, sig := e.addPair(t, []byte("%PDF-1.4 баланс"))
	evDoc, evSig := doc, sig
	evDoc.StorageKey, evSig.StorageKey = "../../etc/passwd", "/etc/shadow" // событию не доверяем
	handle(t, e, event(t, evDoc, evSig))
	if r := onlyReport(t, e); r.Decision != domain.VerificationVerified {
		t.Fatalf("report = %s (%s)", stages(r), r.FailureReason)
	}
	for _, key := range e.files.read() {
		if key != doc.StorageKey && key != sig.StorageKey {
			t.Fatalf("worker read %q from the event", key)
		}
	}

	for name, key := range map[string]string{
		"outside the documents dir": appID + "/../../outside/secret.pdf",
		"another application":       "app_other/doc_9.pdf",
	} {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t, sigworker.Config{})
			doc, sig := e.addPair(t, []byte("%PDF-1.4 баланс"))
			bad := doc
			bad.ID, bad.StorageKey = "doc_3", key
			bad.SignatureState = domain.SignaturePending
			badSig := sig
			badSig.ID, badSig.SignedDocumentID = "doc_4", bad.ID
			e.repo.AddDocuments(bad, badSig)

			handle(t, e, event(t, bad, badSig))

			var r domain.SignatureReport
			for _, x := range e.repo.Reports() {
				if x.DocumentID == bad.ID {
					r = x
				}
			}
			if r.Decision != domain.VerificationIndeterminate || r.DocumentState != domain.SignatureFailed {
				t.Fatalf("report = %s (%s)", stages(r), r.FailureReason)
			}
			for _, read := range e.files.read() {
				if read == key {
					t.Fatalf("unsafe key %q was passed to storage", key)
				}
			}
		})
	}
}

// Сбор данных по заявке ещё идёт — worker ждёт его и проверяет полномочия по собранным ЕГРЮЛ и МЧД.
func TestHandleWaitsForCollection(t *testing.T) {
	e := newEnv(t, sigworker.Config{AuthorityWait: 5 * time.Second})
	e.repo.runningFor.Store(3)
	doc, sig := e.addPair(t, []byte("%PDF-1.4 баланс"))

	handle(t, e, event(t, doc, sig))

	if r := onlyReport(t, e); r.Authority != domain.VerificationVerified || r.Decision != domain.VerificationVerified {
		t.Fatalf("report = %s (%s)", stages(r), r.FailureReason)
	}
}

// Сбор не успел завершиться — полномочия INDETERMINATE, а подпись и сертификат — VERIFIED.
func TestHandleCollectionNotFinished(t *testing.T) {
	e := newEnv(t, sigworker.Config{AuthorityWait: 20 * time.Millisecond})
	e.repo.runningFor.Store(1 << 30)
	doc, sig := e.addPair(t, []byte("%PDF-1.4 баланс"))

	handle(t, e, event(t, doc, sig))

	r := onlyReport(t, e)
	if got := stages(r); got != "VERIFIED VERIFIED VERIFIED VERIFIED INDETERMINATE → INDETERMINATE" ||
		!strings.Contains(r.FailureReason, "не завершился") || r.DocumentState != domain.SignatureDone {
		t.Fatalf("report = %s (%s)", got, r.FailureReason)
	}
}

// Отчёт не записался — повторы, затем ошибка: сообщение не подтверждается. Следующая доставка
// записывает отчёт один раз.
func TestHandleSaveErrors(t *testing.T) {
	e := newEnv(t, sigworker.Config{Attempts: 3})
	doc, sig := e.addPair(t, []byte("%PDF-1.4 баланс"))
	msg := event(t, doc, sig)

	e.repo.saveFailures.Store(3)
	if err := e.w.Handle(context.Background(), msg); err == nil || !strings.Contains(err.Error(), "db down") {
		t.Fatalf("want save error, got %v", err)
	}
	if saves := e.repo.saves.Load(); saves != 3 || len(e.repo.Reports()) != 0 {
		t.Fatalf("saves %d, reports %d", saves, len(e.repo.Reports()))
	}

	e.repo.saveFailures.Store(1) // временный сбой: повтор внутри обработки
	handle(t, e, msg)
	handle(t, e, msg)
	onlyReport(t, e)
}

// Временный сбой чтения файла повторяется; если не проходит — ошибка, отчёта нет.
func TestHandleReadErrors(t *testing.T) {
	e := newEnv(t, sigworker.Config{Attempts: 3})
	doc, sig := e.addPair(t, []byte("%PDF-1.4 баланс"))
	e.files.fail.Store(2)
	handle(t, e, event(t, doc, sig))
	if r := onlyReport(t, e); r.Decision != domain.VerificationVerified {
		t.Fatalf("report = %s", stages(r))
	}

	e = newEnv(t, sigworker.Config{Attempts: 2})
	doc, sig = e.addPair(t, []byte("%PDF-1.4 баланс"))
	e.files.fail.Store(10)
	if err := e.w.Handle(context.Background(), event(t, doc, sig)); err == nil || len(e.repo.Reports()) != 0 {
		t.Fatalf("want read error and no report, got %v, %d reports", err, len(e.repo.Reports()))
	}
}

// PDF с подписью внутри проверяется целиком: подпись одна и покрывает весь файл — VERIFIED;
// PDF, дописанный после подписания, — INDETERMINATE; PDF без подписи — без отчёта.
func TestHandleEmbeddedPDFSignature(t *testing.T) {
	e := newEnv(t, sigworker.Config{})
	pdf, err := sigtest.SignPDF("Balance 2025", pki(t).litvinova, sigtest.PDFOptions{SigningTime: now.Add(-time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	signed := e.addDoc(t, "doc_1", "signed.pdf", "application/pdf", domain.DocFinancialStatements, pdf, "")
	updated := e.addDoc(t, "doc_2", "updated.pdf", "application/pdf", domain.DocFinancialStatements,
		append(append([]byte{}, pdf...), []byte("\n% update after signing\n%%EOF\n")...), "")
	plain := e.addDoc(t, "doc_3", "plain.pdf", "application/pdf", domain.DocCharter, []byte("%PDF-1.4 без подписи"), "")

	handle(t, e, event(t, signed, updated, plain))
	handle(t, e, event(t, signed, updated, plain)) // повторная доставка

	reports := map[string]domain.SignatureReport{}
	for _, r := range e.repo.Reports() {
		reports[r.DocumentID] = r
	}
	if len(e.repo.Reports()) != 2 {
		t.Fatalf("reports = %d, want 2", len(e.repo.Reports()))
	}
	if r := reports[signed.ID]; stages(r) != "VERIFIED VERIFIED VERIFIED VERIFIED VERIFIED → VERIFIED" || r.SignatureDocumentID != "" ||
		r.DocumentSHA256 != signed.SHA256 || r.SignatureSHA256 == "" || r.IdempotencyKey != "auto:TEST:doc_1@"+signed.SHA256 {
		t.Fatalf("signed PDF: %s (%s) %+v", stages(r), r.FailureReason, r)
	}
	if r := reports[updated.ID]; r.Decision != domain.VerificationIndeterminate || !strings.Contains(r.FailureReason, "дописано") {
		t.Fatalf("updated PDF: %s (%s)", stages(r), r.FailureReason)
	}
	if state, verdict := e.repo.Signature(appID, signed.ID); state != domain.SignatureDone || verdict != "VALID_WITH_WARNINGS" {
		t.Fatalf("signed PDF state = %s %s", state, verdict)
	}
	if state, _ := e.repo.Signature(appID, plain.ID); state != domain.SignatureNotSigned {
		t.Fatalf("plain PDF state = %s", state)
	}
}

// Подпись без пары — присоединённая: документ внутри контейнера.
func TestHandleAttachedSignature(t *testing.T) {
	e := newEnv(t, sigworker.Config{})
	s, err := sigtest.Sign([]byte("договор с покупателем"), pki(t).litvinova, sigtest.SignOptions{Attached: true,
		SigningTime: now.Add(-time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	container := e.addDoc(t, "doc_1", "contract.sig", "application/pkcs7-signature", domain.DocOther, s, "")

	handle(t, e, event(t, container))

	r := onlyReport(t, e)
	if r.Decision != domain.VerificationVerified || r.DocumentID != container.ID || r.SignatureDocumentID != "" ||
		r.DocumentSHA256 != container.SHA256 {
		t.Fatalf("report = %s (%s)", stages(r), r.FailureReason)
	}
}

// Битое или чужое сообщение и сообщение о заявке без подписей пропускаются без ошибки.
func TestHandleSkipsUnexpectedMessages(t *testing.T) {
	e := newEnv(t, sigworker.Config{})
	for _, msg := range []string{
		`{"event":`,
		`{"event":"collection.requested","application_id":"app_test"}`,
		`{"event":"documents.submitted"}`,
		`{"event":"documents.submitted","application_id":"app_unknown","documents":[{"id":"doc_x"}]}`,
	} {
		if err := e.w.Handle(context.Background(), []byte(msg)); err != nil {
			t.Fatalf("%s: %v", msg, err)
		}
	}
	if len(e.repo.Reports()) != 0 || e.verifier.calls.Load() != 0 {
		t.Fatal("nothing must be verified")
	}
}
