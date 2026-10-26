// Package sigworker проверяет электронные подписи документов заявок. По событию documents.submitted
// из Kafka берёт документы заявки из БД (путь к файлу — documents.storage_key, а не поле события),
// читает файлы из хранилища только на чтение, считает их sha256 до проверки, проверяет подписи
// (internal/signature) и пишет отчёты в signature_reports. Распознаванием документов и скорингом
// не занимается: данные из документов никуда не передаются.
package sigworker

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/client/mockapi"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/domain"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/events"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/signature"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/storage"
)

// callTimeout — на один запрос к БД; запись доводится до конца, даже если worker останавливается.
const callTimeout = 5 * time.Second

// signatureMime — MIME файлов подписи .sig/.p7s в documents (как их сохраняет API).
const signatureMime = "application/pkcs7-signature"

// Repository — документы, контекст заявки и отчёты (postgres.SignatureRepository). Объявлен у потребителя.
type Repository interface {
	Documents(ctx context.Context, applicationID string) ([]domain.Document, error)
	// Context — заявитель и последний сбор по заявке; false — заявки нет.
	Context(ctx context.Context, applicationID string) (domain.SignatureContext, bool, error)
	ReportExists(ctx context.Context, key string) (bool, error)
	// SaveReport пишет отчёт и состояние документов одной транзакцией; false — отчёт с этим ключом уже есть.
	SaveReport(ctx context.Context, r domain.SignatureReport) (bool, error)
}

// Files — файлы документов (storage.Reader): ключ не выводит за пределы хранилища.
type Files interface {
	Read(key string) ([]byte, error)
}

// Verifier — проверка подписи (signature.Verifier).
type Verifier interface {
	Verify(ctx context.Context, in signature.Input, load signature.ApplicantLoader) (*signature.Report, error)
}

// Config — ожидание сбора данных и повторы при сбоях.
type Config struct {
	AuthorityWait time.Duration // сколько ждать, пока сбор данных по заявке (ЕГРЮЛ, МЧД) завершится
	PollInterval  time.Duration // как часто проверять, завершён ли сбор
	Attempts      int           // попыток запроса к БД и чтения файла, прежде чем вернуть ошибку
	RetryBackoff  time.Duration // пауза перед повтором, удваивается
}

// Worker проверяет подписи документов по событиям. Безопасен для параллельного использования.
type Worker struct {
	repo     Repository
	files    Files
	verifier Verifier
	cfg      Config
	log      *slog.Logger
	newID    func() string
}

// Option настраивает Worker (используется в тестах).
type Option func(*Worker)

// WithIDGenerator подменяет генератор идентификаторов отчётов.
func WithIDGenerator(newID func() string) Option { return func(w *Worker) { w.newID = newID } }

func New(repo Repository, files Files, verifier Verifier, cfg Config, log *slog.Logger, opts ...Option) *Worker {
	if cfg.Attempts < 1 {
		cfg.Attempts = 1
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 500 * time.Millisecond
	}
	if cfg.RetryBackoff <= 0 {
		cfg.RetryBackoff = 200 * time.Millisecond
	}
	w := &Worker{repo: repo, files: files, verifier: verifier, cfg: cfg, log: log,
		newID: func() string { return "sigr_" + ulid.MustNew(ulid.Now(), rand.Reader).String() }}
	for _, opt := range opts {
		opt(w)
	}
	return w
}

// Handle обрабатывает событие documents.submitted: на каждую подпись документов заявки из события —
// отчёт в signature_reports. Повторная доставка отчётов не дублирует: проверка с тем же ключом
// пропускается, а запись защищена уникальным ключом. Ошибка — сбой БД или чтения файла после всех
// попыток: сообщение не подтверждается и придёт снова. Битые и чужие сообщения пропускаются.
func (w *Worker) Handle(ctx context.Context, value []byte) error {
	var ev events.DocumentsSubmittedEvent
	if err := json.Unmarshal(value, &ev); err != nil || ev.Event != events.DocumentsSubmitted || ev.ApplicationID == "" {
		w.log.WarnContext(ctx, "skip unexpected message", "err", err, "event", ev.Event)
		return nil
	}
	log := w.log.With("application", ev.ApplicationID)
	inEvent := make(map[string]bool, len(ev.Documents))
	for _, d := range ev.Documents {
		inEvent[d.ID] = true
	}

	var docs []domain.Document
	if err := w.retry(ctx, ctx, "load documents", func(ctx context.Context) (err error) {
		docs, err = w.repo.Documents(ctx, ev.ApplicationID)
		return err
	}); err != nil {
		return err
	}
	for id := range inEvent {
		if !containsDocument(docs, id) {
			log.WarnContext(ctx, "document from event is not found in application", "document", id)
		}
	}

	units, candidates := plan(docs, inEvent)
	for _, d := range candidates {
		u, ok, err := w.embedded(ctx, log, d, ev.VerificationID)
		if err != nil {
			return err
		}
		if ok {
			units = append(units, u)
		}
	}
	p := &party{w: w, applicationID: ev.ApplicationID}
	for _, u := range units {
		u.requestID = ev.VerificationID
		if err := w.process(ctx, log, u, p); err != nil {
			return err
		}
	}
	return nil
}

// unit — одна проверяемая подпись.
type unit struct {
	requestID string
	doc       domain.Document  // подписанный документ; у присоединённой подписи — сам контейнер
	signature *domain.Document // файл открепленной подписи
	container signature.Container
	file      *signature.File // уже прочитанный документ со встроенной подписью
}

// key — ключ идемпотентности автоматической проверки: профиль и версии документа и подписи
// (id и sha256 из documents — строки неизменны, новая загрузка — новый документ).
func (u unit) key() string {
	k := "auto:" + signature.ProfileTest + ":" + u.doc.ID + "@" + u.doc.SHA256
	if u.signature != nil {
		k += ":" + u.signature.ID + "@" + u.signature.SHA256
	}
	if u.requestID != "" {
		k += ":request:" + u.requestID
	}
	return k
}

// plan раскладывает документы заявки из события на проверки: пара «документ + открепленная
// подпись», присоединённая подпись (.sig/.p7s без пары) и кандидаты на встроенную подпись —
// PDF и XML без открепленной подписи, их ещё надо прочитать.
func plan(docs []domain.Document, inEvent map[string]bool) (units []unit, candidates []domain.Document) {
	byID := make(map[string]domain.Document, len(docs))
	for _, d := range docs {
		byID[d.ID] = d
	}
	signed := map[string]bool{}
	for _, s := range docs {
		d, ok := byID[s.SignedDocumentID]
		if s.SignedDocumentID == "" || !ok || !(inEvent[s.ID] || inEvent[d.ID]) {
			continue
		}
		units = append(units, unit{doc: d, signature: &s, container: signature.ContainerDetached})
		signed[d.ID] = true
	}
	for _, d := range docs {
		if !inEvent[d.ID] || d.SignedDocumentID != "" || signed[d.ID] {
			continue
		}
		switch d.MimeType {
		case signatureMime:
			units = append(units, unit{doc: d, container: signature.ContainerAttached})
		case "application/pdf", "application/xml":
			candidates = append(candidates, d)
		}
	}
	return units, candidates
}

// embedded читает PDF или XML без открепленной подписи и ищет в нём встроенную подпись.
func (w *Worker) embedded(ctx context.Context, log *slog.Logger, d domain.Document, requestID string) (unit, bool, error) {
	u := unit{doc: d, requestID: requestID}
	if done, err := w.reportExists(ctx, u.key()); err != nil || done {
		return u, false, err
	}
	f, err := w.read(ctx, d)
	if err != nil {
		return u, false, err
	}
	if f.Data == nil {
		log.WarnContext(ctx, "document is unavailable, embedded signature is not checked", "document", d.ID, "reason", f.Unavailable)
		return u, false, nil
	}
	if u.container = signature.EmbeddedSignature(f.Data); u.container == "" {
		return u, false, nil
	}
	u.file = &f
	return u, true, nil
}

// process проверяет одну подпись и пишет отчёт; проверка с уже записанным отчётом пропускается.
func (w *Worker) process(ctx context.Context, log *slog.Logger, u unit, p *party) error {
	key := u.key()
	var sigID string
	if u.signature != nil {
		sigID = u.signature.ID
	}
	if done, err := w.reportExists(ctx, key); err != nil {
		return err
	} else if done {
		log.InfoContext(ctx, "signature is already verified, skip", "document", u.doc.ID, "signature_document", sigID)
		return nil
	}

	in := signature.Input{DocumentType: u.doc.Type, Container: u.container}
	var err error
	switch u.container {
	case signature.ContainerAttached:
		var f signature.File
		f, err = w.read(ctx, u.doc)
		in.Signature = &f
	case signature.ContainerDetached:
		if in.Document, err = w.read(ctx, u.doc); err == nil {
			var f signature.File
			f, err = w.read(ctx, *u.signature)
			in.Signature = &f
		}
	default:
		in.Document = *u.file
	}
	if err != nil {
		return err
	}

	rep, err := w.verifier.Verify(ctx, in, p.load)
	if err != nil {
		return fmt.Errorf("verify document %s: %w", u.doc.ID, err)
	}
	report, err := w.report(u, in, rep, key)
	if err != nil {
		return err
	}
	saved := false
	// начатую проверку сохраняем, даже если worker останавливается
	if err := w.retry(context.WithoutCancel(ctx), ctx, "save signature report", func(ctx context.Context) (err error) {
		saved, err = w.repo.SaveReport(ctx, report)
		return err
	}); err != nil {
		return err
	}
	if !saved {
		log.InfoContext(ctx, "signature report is already saved by another delivery", "document", u.doc.ID, "signature_document", sigID)
		return nil
	}
	log.InfoContext(ctx, "signature verified", "document", u.doc.ID, "signature_document", sigID, "report", report.ID,
		"profile", report.Profile, "decision", report.Decision, "verdict", report.Verdict, "integrity", report.Integrity,
		"format", report.Format, "certificate", report.Certificate, "identity", report.Identity, "authority", report.Authority,
		"failed_check", rep.FailedCheck)
	return nil
}

// report — строка signature_reports из отчёта проверки.
func (w *Worker) report(u unit, in signature.Input, rep *signature.Report, key string) (domain.SignatureReport, error) {
	rep.ID, rep.ApplicationID, rep.DocumentID = w.newID(), u.doc.ApplicationID, u.doc.ID
	if u.signature != nil {
		rep.SignatureDocumentID = u.signature.ID
	}
	body, err := json.Marshal(rep)
	if err != nil {
		return domain.SignatureReport{}, fmt.Errorf("encode signature report: %w", err)
	}
	state := domain.SignatureDone
	missing := (u.container != signature.ContainerAttached && in.Document.Data == nil) || (in.Signature != nil && in.Signature.Data == nil)
	if missing || rep.InternalError {
		state = domain.SignatureFailed
	}
	s := rep.Stages
	return domain.SignatureReport{ID: rep.ID, ApplicationID: rep.ApplicationID, DocumentID: rep.DocumentID,
		SignatureDocumentID: rep.SignatureDocumentID, DocumentSHA256: rep.DocumentSHA256, SignatureSHA256: rep.SignatureSHA256,
		Profile: rep.Profile, TrustMode: rep.TrustMode, Policy: rep.Policy, Verdict: rep.Verdict,
		Integrity: s.Integrity.Status, Format: s.Format.Status, Certificate: s.Certificate.Status, Identity: s.Identity.Status,
		Authority: s.Authority.Status, Decision: rep.Decision, FailureReason: rep.FailureReason, IdempotencyKey: key,
		DocumentState: state, Report: body, VerifiedAt: rep.VerifiedAt}, nil
}

// read читает файл документа. Ключ — из БД и только в папке своей заявки. Файла нет, путь
// небезопасен, файл больше лимита — File с причиной: это результат проверки, а не сбой.
// Сбой чтения повторяется; после всех попыток — ошибка.
func (w *Worker) read(ctx context.Context, d domain.Document) (signature.File, error) {
	f := signature.File{Name: d.FileName, Recorded: d.SHA256, Size: d.SizeBytes}
	// ключ API — ровно {application_id}/{document_id}.{ext}; всё остальное не читаем вовсе
	if key := d.StorageKey; path.Clean(key) != key || path.Dir(key) != d.ApplicationID || strings.Contains(key, `\`) {
		f.Unavailable = "путь к файлу в БД ведёт не в папку заявки — файл не читался"
		return f, nil
	}
	for attempt := 1; ; attempt++ {
		data, err := w.files.Read(d.StorageKey)
		switch {
		case err == nil:
			sum := sha256.Sum256(data)
			f.Data, f.SHA256 = data, hex.EncodeToString(sum[:])
			return f, nil
		case errors.Is(err, storage.ErrNotFound):
			f.Unavailable = "не найден в хранилище"
			return f, nil
		case errors.Is(err, storage.ErrUnsafeKey):
			f.Unavailable = "путь к файлу выходит за пределы хранилища или ведёт через symlink — файл не читался"
			return f, nil
		case errors.Is(err, storage.ErrNotRegular):
			f.Unavailable = "по этому пути в хранилище не обычный файл"
			return f, nil
		case errors.Is(err, storage.ErrTooLarge):
			f.Unavailable = "больше лимита размера файла"
			return f, nil
		}
		if attempt >= w.cfg.Attempts {
			return f, fmt.Errorf("read document %s: %w", d.ID, err)
		}
		w.log.WarnContext(ctx, "read document failed, retry", "document", d.ID, "attempt", attempt, "err", err)
		if err := sleep(ctx, w.backoff(attempt)); err != nil {
			return f, err
		}
	}
}

func (w *Worker) reportExists(ctx context.Context, key string) (bool, error) {
	var exists bool
	err := w.retry(ctx, ctx, "check signature report", func(ctx context.Context) (err error) {
		exists, err = w.repo.ReportExists(ctx, key)
		return err
	})
	return exists, err
}

// retry выполняет запрос к БД с таймаутом, при ошибке повторяет с паузой (cfg.Attempts попыток).
// sleepCtx прерывает паузы: для записи результата это контекст без отмены.
func (w *Worker) retry(sleepCtx, logCtx context.Context, op string, fn func(context.Context) error) error {
	for attempt := 1; ; attempt++ {
		callCtx, cancel := context.WithTimeout(context.WithoutCancel(sleepCtx), callTimeout)
		err := fn(callCtx)
		cancel()
		if err == nil {
			return nil
		}
		if attempt >= w.cfg.Attempts {
			return fmt.Errorf("%s: %w", op, err)
		}
		w.log.WarnContext(logCtx, "storage call failed, retry", "op", op, "attempt", attempt, "err", err)
		if err := sleep(sleepCtx, w.backoff(attempt)); err != nil {
			return fmt.Errorf("%s: %w", op, err)
		}
	}
}

func (w *Worker) backoff(attempt int) time.Duration {
	return w.cfg.RetryBackoff << (attempt - 1)
}

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// party — заявитель и данные реестров для всех подписей одного события: загружаются один раз
// и только если понадобились (подпись прошла криптографическую проверку).
type party struct {
	w             *Worker
	applicationID string

	mu        sync.Mutex
	loaded    bool
	applicant signature.Applicant
}

func (p *party) load(ctx context.Context) (signature.Applicant, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.loaded {
		return p.applicant, nil
	}
	sc, waited, err := p.w.waitForCollection(ctx, p.applicationID)
	if err != nil {
		return signature.Applicant{}, err
	}
	p.applicant, p.loaded = applicantOf(sc, waited), true
	return p.applicant, nil
}

// waitForCollection ждёт, пока сбор данных по заявке завершится (не дольше cfg.AuthorityWait):
// событие о документах публикуется вместе с задачей на сбор, а ЕГРЮЛ и МЧД нужны для полномочий.
func (w *Worker) waitForCollection(ctx context.Context, applicationID string) (domain.SignatureContext, time.Duration, error) {
	start := time.Now()
	for {
		var (
			sc domain.SignatureContext
			ok bool
		)
		if err := w.retry(ctx, ctx, "load application context", func(ctx context.Context) (err error) {
			sc, ok, err = w.repo.Context(ctx, applicationID)
			return err
		}); err != nil {
			return sc, 0, err
		}
		waited := time.Since(start)
		if !ok || sc.Collection.Finished() || waited >= w.cfg.AuthorityWait {
			return sc, waited, nil
		}
		if err := sleep(ctx, w.cfg.PollInterval); err != nil {
			return sc, waited, err
		}
	}
}

// applicantOf — заявитель и собранные ответы ЕГРЮЛ и реестра МЧД. Нет данных — причина вместо
// данных: этап полномочий станет INDETERMINATE, а не INVALID.
func applicantOf(sc domain.SignatureContext, waited time.Duration) signature.Applicant {
	a := signature.Applicant{INN: sc.Company.INN, Type: sc.Company.Type, OGRN: sc.Company.OGRN, Name: sc.Company.Name}
	c := sc.Collection
	var unavailable string
	switch {
	case sc.Company.INN == "":
		unavailable = "заявка не найдена"
	case c == nil:
		unavailable = "сбор данных по заявке не запускался"
	case c.Status == domain.TaskFailed:
		unavailable = "сбор данных по заявке завершился ошибкой"
	case c.Status != domain.TaskCompleted:
		unavailable = fmt.Sprintf("сбор данных по заявке не завершился за %s", waited.Round(time.Second))
	}
	if unavailable != "" {
		a.HeadsUnavailable, a.POAsUnavailable = unavailable, unavailable
		return a
	}
	a.Source = "сбор данных " + c.CollectionID + " (mock-api)"
	a.POAScopeINN = c.Input.RepresentativeINN
	if heads, ok := mockapi.RegistryHeads(c.FNS); ok {
		a.Heads, a.HeadsKnown = heads, true
	} else {
		a.HeadsUnavailable = "ФНС не ответила при сборе данных (" + string(c.FNSState) + ")"
	}
	if poas, ok := mockapi.PowersOfAttorney(c.M4D); ok {
		a.POAs, a.POAsKnown = poas, true
	} else {
		a.POAsUnavailable = "реестр МЧД не ответил при сборе данных (" + string(c.M4DState) + ")"
	}
	return a
}

func containsDocument(docs []domain.Document, id string) bool {
	for _, d := range docs {
		if d.ID == id {
			return true
		}
	}
	return false
}
