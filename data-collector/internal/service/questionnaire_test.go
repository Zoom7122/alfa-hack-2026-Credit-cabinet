package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/domain"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/events"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/repository/memory"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/service"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/storage"
)

var submittedAt = time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)

// fixture — черновик заявки в памяти, хранилище файлов во временной папке и сервис анкет над ними.
type fixture struct {
	tasks *memory.CollectionTaskRepository
	apps  *memory.ApplicationRepository
	root  string
	svc   *service.QuestionnaireService
	app   domain.Application
}

func newFixture(t *testing.T, apps service.ApplicationRepository) *fixture {
	t.Helper()
	tasks := memory.NewCollectionTaskRepository()
	mem := memory.NewApplicationRepository(tasks)
	if apps == nil {
		apps = mem
	}
	root := t.TempDir()
	files, err := storage.NewLocal(root, 1<<10)
	if err != nil {
		t.Fatal(err)
	}
	app, err := service.NewApplicationService(mem).Register(context.Background(), service.RegisterInput{
		FullName: "Литвинова Марина Олеговна", PersonINN: "779900010333", Role: domain.RoleAccountant, CompanyINN: "7799000012"})
	if err != nil {
		t.Fatal(err)
	}
	collector := service.NewCollectionService(tasks, service.WithCollectionIDGenerator(func() string { return "tsk_test" }))
	n := 0
	svc := service.NewQuestionnaireService(apps, files, collector,
		service.WithClock(func() time.Time { return submittedAt }),
		service.WithIDGenerator(func() string { return "qst_test" }),
		service.WithDocumentIDGenerator(func() string { n++; return "doc_" + string(rune('0'+n)) }),
	)
	return &fixture{tasks: tasks, apps: mem, root: root, svc: svc, app: app}
}

func (f *fixture) stage(t *testing.T, name, body string) storage.Staged {
	t.Helper()
	st, err := f.svc.StageFile(name, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	return st
}

// stagingEmpty — во временной папке хранилища не осталось файлов.
func (f *fixture) stagingEmpty(t *testing.T) {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(f.root, ".staging"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("staging is not empty: %d files (%v)", len(entries), err)
	}
}

func validInput() service.SubmitInput {
	return service.SubmitInput{Age: 34, MaritalStatus: domain.MaritalMarried, ChildrenCount: 2,
		Company: domain.CollectInput{OFDToken: "secret-ofd"}}
}

func TestSubmit(t *testing.T) {
	f := newFixture(t, nil)
	in := validInput()
	in.ProductCode = " WORKING_CAPITAL "
	in.Requested = &domain.RequestedTerms{Amount: 5_000_000, TermMonths: 24}
	in.Purpose = &domain.Purpose{Code: "WORKING_CAPITAL", Description: "Закупка товара под сезон"}
	in.Files = []storage.Staged{f.stage(t, "balance.pdf.sig", "sig"), f.stage(t, "balance.pdf", "%PDF balance"),
		f.stage(t, `C:\docs\charter.pdf`, "%PDF charter")}
	in.Documents = []service.DocumentSpec{{FileName: "BALANCE.pdf", Type: domain.DocFinancialStatements}}

	res, err := f.svc.Submit(context.Background(), f.app.ID, in)
	if err != nil {
		t.Fatal(err)
	}

	app := res.Application
	if app.Status != domain.ApplicationSubmitted || !app.SubmittedAt.Equal(submittedAt) || app.CollectionTaskID != "tsk_test" ||
		app.ProductCode != "WORKING_CAPITAL" || app.Requested.Amount != 5_000_000 || app.Purpose.Code != "WORKING_CAPITAL" {
		t.Fatalf("application = %+v", app)
	}
	// задача: ИНН — из компании заявки, токены — для worker'а, документы — по id
	task := res.Task
	if task.ApplicationID != f.app.ID || task.Input.INN != "7799000012" || task.Input.OFDToken != "secret-ofd" ||
		task.QuestionnaireID != "qst_test" || len(task.DocumentIDs) != 3 {
		t.Fatalf("task = %+v", task)
	}

	docs := map[string]domain.Document{}
	for _, d := range res.Documents {
		docs[d.FileName] = d
		if data, err := os.ReadFile(filepath.Join(f.root, d.StorageKey)); err != nil || int64(len(data)) != d.SizeBytes {
			t.Errorf("%s: file %s on disk: %v", d.FileName, d.StorageKey, err)
		}
	}
	balance, sig, charter := docs["balance.pdf"], docs["balance.pdf.sig"], docs["charter.pdf"]
	if balance.Type != domain.DocFinancialStatements || balance.SignatureState != domain.SignaturePending ||
		balance.RecognitionState != domain.RecognitionPending || !strings.HasPrefix(balance.StorageKey, f.app.ID+"/") {
		t.Errorf("balance = %+v", balance)
	}
	if sig.Type != domain.DocSignature || sig.SignedDocumentID != balance.ID || sig.SignatureState != domain.SignaturePending ||
		sig.RecognitionState != domain.RecognitionNotApplicable {
		t.Errorf("signature = %+v", sig)
	}
	if charter.Type != domain.DocOther || charter.SignatureState != domain.SignatureNotSigned || charter.UploadedBy != f.app.Representative.UserID {
		t.Errorf("charter = %+v", charter)
	}
	f.stagingEmpty(t)

	// в заявку записаны анкета и данные; в outbox — событие о задаче и о документах
	sub, ok := f.apps.Submission(f.app.ID)
	if !ok || sub.Questionnaire.ID != "qst_test" || sub.Questionnaire.Age != 34 || sub.ActorID != f.app.Representative.UserID {
		t.Fatalf("submission = %+v (found %v)", sub, ok)
	}
	if ev := f.apps.Events(f.app.ID); !slices.Equal(ev, []string{"application.created", "application.submitted"}) {
		t.Fatalf("events = %v", ev)
	}
	out := f.tasks.Outbox()
	if len(out) != 2 || out[0].Topic != events.DefaultTopic || out[1].Topic != events.DocumentsTopic || out[1].Key != f.app.ID {
		t.Fatalf("outbox = %+v", out)
	}
	var docEvent events.DocumentsSubmittedEvent
	if err := json.Unmarshal(out[1].Payload, &docEvent); err != nil || len(docEvent.Documents) != 3 ||
		docEvent.CompanyINN != "7799000012" || strings.Contains(string(out[1].Payload), "secret") {
		t.Fatalf("documents event = %s (%v)", out[1].Payload, err)
	}
}

// Без файлов: заявка отправлена, события о документах нет.
func TestSubmitWithoutFiles(t *testing.T) {
	f := newFixture(t, nil)
	res, err := f.svc.Submit(context.Background(), f.app.ID, validInput())
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Documents) != 0 || len(res.Task.DocumentIDs) != 0 || len(f.tasks.Outbox()) != 1 {
		t.Fatalf("documents %v, outbox %+v", res.Documents, f.tasks.Outbox())
	}
}

func TestSubmitValidation(t *testing.T) {
	tests := []struct {
		name       string
		mutate     func(*service.SubmitInput, *fixture, *testing.T)
		wantFields []string
	}{
		{"too young", func(in *service.SubmitInput, _ *fixture, _ *testing.T) { in.Age = 17 }, []string{"age"}},
		{"too old", func(in *service.SubmitInput, _ *fixture, _ *testing.T) { in.Age = 101 }, []string{"age"}},
		{"unknown status", func(in *service.SubmitInput, _ *fixture, _ *testing.T) { in.MaritalStatus = "UNKNOWN" },
			[]string{"marital_status"}},
		{"negative children", func(in *service.SubmitInput, _ *fixture, _ *testing.T) { in.ChildrenCount = -1 },
			[]string{"children_count"}},
		{"unknown product", func(in *service.SubmitInput, _ *fixture, _ *testing.T) { in.ProductCode = "YACHT_LOAN" },
			[]string{"product_code"}},
		{"terms outside product", func(in *service.SubmitInput, _ *fixture, _ *testing.T) {
			in.ProductCode = "EXPRESS"
			in.Requested = &domain.RequestedTerms{Amount: 8_000_000, TermMonths: 36}
		}, []string{"requested.amount", "requested.term_months"}},
		// ошибки анкеты, данных для сбора, заявки и файлов возвращаются вместе
		{"everything together", func(in *service.SubmitInput, f *fixture, t *testing.T) {
			in.Age = 5
			in.Company.OtherBankBIKs = []string{"1"}
			in.Requested = &domain.RequestedTerms{Amount: 0, TermMonths: 12}
			in.Purpose = &domain.Purpose{Code: "YACHT"}
			in.Files = []storage.Staged{f.stage(t, "a.pdf", "a"), f.stage(t, "a.pdf.sig", "1"), f.stage(t, "a.sig", "2")}
		}, []string{"age", "other_bank_biks[0]", "requested.amount", "purpose.code", "files[2]"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t, nil)
			in := validInput()
			tt.mutate(&in, f, t)

			_, err := f.svc.Submit(context.Background(), f.app.ID, in)

			var verr *service.ValidationError
			if !errors.As(err, &verr) {
				t.Fatalf("want *ValidationError, got %v", err)
			}
			if got := fieldNames(verr); !slices.Equal(got, tt.wantFields) {
				t.Fatalf("invalid fields = %v, want %v", got, tt.wantFields)
			}
			// ничего не сохранено, принятые файлы удалены
			if app, _, _ := f.apps.Get(context.Background(), f.app.ID); app.Status != domain.ApplicationDraft {
				t.Fatalf("application must stay a draft: %s", app.Status)
			}
			if len(f.tasks.Outbox()) != 0 {
				t.Fatal("collection must not be enqueued for an invalid questionnaire")
			}
			f.stagingEmpty(t)
		})
	}
}

func TestSubmitNotFound(t *testing.T) {
	f := newFixture(t, nil)
	in := validInput()
	in.Files = []storage.Staged{f.stage(t, "a.pdf", "a")}
	if _, err := f.svc.Submit(context.Background(), "app_unknown", in); !errors.Is(err, service.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	f.stagingEmpty(t)
}

// Анкету по заявке второй раз не принять: файлы второй попытки удаляются.
func TestSubmitTwice(t *testing.T) {
	f := newFixture(t, nil)
	if _, err := f.svc.Submit(context.Background(), f.app.ID, validInput()); err != nil {
		t.Fatal(err)
	}
	in := validInput()
	in.Files = []storage.Staged{f.stage(t, "late.pdf", "late")}
	if _, err := f.svc.Submit(context.Background(), f.app.ID, in); !errors.Is(err, service.ErrInvalidTransition) {
		t.Fatalf("want ErrInvalidTransition, got %v", err)
	}
	f.stagingEmpty(t)
	if entries, _ := os.ReadDir(filepath.Join(f.root, f.app.ID)); len(entries) != 0 {
		t.Fatalf("files of the rejected attempt are kept: %d", len(entries))
	}
}

// failingSubmit — хранилище заявок, у которого не получается записать отправку.
type failingSubmit struct {
	service.ApplicationRepository
	err         error
	gotCanceled bool
}

func (r *failingSubmit) Submit(ctx context.Context, sub domain.Submission) error {
	r.gotCanceled = ctx.Err() != nil
	if r.err != nil {
		return r.err
	}
	return r.ApplicationRepository.Submit(ctx, sub)
}

// Транзакция не прошла — файлы, уже перенесённые на постоянное место, удаляются.
func TestSubmitRepositoryError(t *testing.T) {
	boom := errors.New("db down")
	repo := &failingSubmit{err: boom}
	f := newFixture(t, repo)
	repo.ApplicationRepository = f.apps
	in := validInput()
	in.Files = []storage.Staged{f.stage(t, "a.pdf", "a"), f.stage(t, "a.pdf.sig", "s")}

	if _, err := f.svc.Submit(context.Background(), f.app.ID, in); !errors.Is(err, boom) {
		t.Fatalf("want wrapped repo error, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(f.root, f.app.ID)); !os.IsNotExist(err) {
		t.Fatalf("committed files must be removed: %v", err)
	}
	f.stagingEmpty(t)
}

// Клиент отключился, когда файлы уже перенесены, — заявка всё равно отправляется.
func TestSubmitIgnoresClientCancel(t *testing.T) {
	repo := &failingSubmit{}
	f := newFixture(t, repo)
	repo.ApplicationRepository = f.apps
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := f.svc.Submit(ctx, f.app.ID, validInput()); err != nil {
		t.Fatal(err)
	}
	if repo.gotCanceled {
		t.Fatal("submit context must not inherit the request cancellation")
	}
}

func TestStageFileTooLarge(t *testing.T) {
	f := newFixture(t, nil)
	if _, err := f.svc.StageFile("big.pdf", strings.NewReader(strings.Repeat("x", 2<<10))); !errors.Is(err, service.ErrFileTooLarge) {
		t.Fatalf("want ErrFileTooLarge, got %v", err)
	}
	f.stagingEmpty(t)
}

func TestDefaultIDPrefixes(t *testing.T) {
	tasks := memory.NewCollectionTaskRepository()
	apps := memory.NewApplicationRepository(tasks)
	files, err := storage.NewLocal(t.TempDir(), 1<<10)
	if err != nil {
		t.Fatal(err)
	}
	app, err := service.NewApplicationService(apps).Register(context.Background(), service.RegisterInput{
		FullName: "Орлов Денис Павлович", Role: domain.RoleDirector, CompanyINN: "7799000020"})
	if err != nil {
		t.Fatal(err)
	}
	svc := service.NewQuestionnaireService(apps, files, service.NewCollectionService(tasks))
	in := validInput()
	st, err := svc.StageFile("a.pdf", strings.NewReader("a"))
	if err != nil {
		t.Fatal(err)
	}
	in.Files = []storage.Staged{st}
	res, err := svc.Submit(context.Background(), app.ID, in)
	if err != nil {
		t.Fatal(err)
	}
	for id, prefix := range map[string]string{res.Questionnaire.ID: "qst_", res.Documents[0].ID: "doc_", res.Task.ID: "tsk_",
		app.ID: "app_", app.Company.ID: "apl_", app.Representative.UserID: "usr_"} {
		if len(id) != len(prefix)+26 || !strings.HasPrefix(id, prefix) {
			t.Errorf("unexpected id %q, want prefix %s", id, prefix)
		}
	}
}

func fieldNames(verr *service.ValidationError) []string {
	var got []string
	for _, f := range verr.Fields {
		got = append(got, f.Field)
	}
	return got
}
