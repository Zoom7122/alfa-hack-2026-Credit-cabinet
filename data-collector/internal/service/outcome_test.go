package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/domain"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/service"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/storage"
)

type fakeOutcome struct {
	app      domain.Application
	decision *domain.DecisionRecord
	requests []domain.InfoRequestRecord
	answered *domain.InfoAnswer
	err      error
}

func (f *fakeOutcome) LatestDecision(context.Context, string) (*domain.DecisionRecord, error) {
	return f.decision, nil
}
func (f *fakeOutcome) Offers(context.Context, string) ([]domain.OfferRecord, error) { return nil, nil }
func (f *fakeOutcome) InfoRequests(context.Context, string) ([]domain.InfoRequestRecord, error) {
	return f.requests, nil
}
func (f *fakeOutcome) Get(_ context.Context, id string) (domain.Application, bool, error) {
	return f.app, id == f.app.ID, nil
}
func (f *fakeOutcome) AnswerInfoRequest(_ context.Context, a domain.InfoAnswer) (domain.ApplicationStatus, bool, error) {
	f.answered = &a
	return domain.ApplicationVerifying, true, f.err
}

func newOutcome(t *testing.T) (*service.OutcomeService, *fakeOutcome, *storage.Local) {
	t.Helper()
	files, err := storage.NewLocal(t.TempDir(), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	q := func(id, question string) domain.InfoRequestRecord {
		return domain.InfoRequestRecord{ID: id, Status: "OPEN", Required: true, Question: json.RawMessage(question)}
	}
	repo := &fakeOutcome{
		app: domain.Application{ID: "app_1", Company: domain.Company{INN: "7799000044"},
			Representative: domain.Representative{UserID: "usr_rep", HasLogin: true}},
		requests: []domain.InfoRequestRecord{
			q("ir_money", `{"code":"FREE_CASH","input":{"type":"MONEY","scoring_field":"free_cash"}}`),
			q("ir_rate", `{"code":"MAX_RATE","input":{"type":"NUMBER","scoring_field":"max_annual_rate"}}`),
			q("ir_choice", `{"code":"CAR","input":{"type":"CHOICE","scoring_field":"car_class","options":[{"value":"NONE","label":"Нет"}]}}`),
			q("ir_doc", `{"code":"AUTHORITY","input":{"type":"NONE"},"documents":{"required":true,"accepted_types":["POWER_OF_ATTORNEY"]}}`),
			q("ir_text", `{"title":"Комментарий","input":{"type":"TEXT"}}`),
		},
	}
	return service.NewOutcomeService(repo, repo, files, 4*time.Hour), repo, files
}

func TestOutcomeAnswer(t *testing.T) {
	ctx := context.Background()
	svc, repo, files := newOutcome(t)
	answer := func(rid string, in service.AnswerInput) error {
		repo.answered = nil
		_, err := svc.Answer(ctx, "usr_rep", "app_1", rid, in)
		return err
	}
	raw := func(v string) json.RawMessage { return json.RawMessage(v) }

	// значение ложится в анкету scoring: деньги — целые, ставка — дробная, вариант — код
	if err := answer("ir_money", service.AnswerInput{Value: raw(`150000.7`)}); err != nil || repo.answered.ScoringPatch["free_cash"] != int64(150000) {
		t.Errorf("money: %v %+v", err, repo.answered)
	}
	if err := answer("ir_rate", service.AnswerInput{Value: raw(`24.5`)}); err != nil || repo.answered.ScoringPatch["max_annual_rate"] != 24.5 {
		t.Errorf("rate: %v %+v", err, repo.answered)
	}
	if err := answer("ir_choice", service.AnswerInput{Value: raw(`"NONE"`)}); err != nil || repo.answered.ScoringPatch["car_class"] != "NONE" {
		t.Errorf("choice: %v", err)
	}
	if err := answer("ir_text", service.AnswerInput{Comment: "  бизнес не менялся "}); err != nil || repo.answered.Comment != "бизнес не менялся" ||
		len(repo.answered.ScoringPatch) != 0 {
		t.Errorf("text: %v %+v", err, repo.answered)
	}

	var verr *service.ValidationError
	for rid, in := range map[string]service.AnswerInput{
		"ir_money":  {Value: raw(`"много"`)},
		"ir_choice": {Value: raw(`"TANK"`)},
		"ir_doc":    {},
		"ir_text":   {},
	} {
		if err := answer(rid, in); !errors.As(err, &verr) || repo.answered != nil {
			t.Errorf("%s must fail validation: %v", rid, err)
		}
	}

	// документ: тип из вопроса, файл — в хранилище заявки
	path := filepath.Join(t.TempDir(), "poa.pdf")
	_ = os.WriteFile(path, []byte("%PDF-1.4 poa"), 0o600)
	f, _ := os.Open(path)
	st, err := files.Stage("poa.pdf", f)
	_ = f.Close()
	if err != nil {
		t.Fatal(err)
	}
	if err := answer("ir_doc", service.AnswerInput{Files: []storage.Staged{st}}); err != nil {
		t.Fatal(err)
	}
	d := repo.answered.Documents
	if len(d) != 1 || d[0].Type != domain.DocPowerOfAttorney || !strings.HasPrefix(d[0].StorageKey, "app_1/") || repo.answered.CompanyINN != "7799000044" ||
		repo.answered.ReviewSLA != 4*time.Hour {
		t.Fatalf("document answer: %+v", repo.answered)
	}

	// отвечает только автор; заявку тестового пользователя без токена — нельзя
	if _, err := svc.Answer(ctx, "usr_other", "app_1", "ir_text", service.AnswerInput{Comment: "x"}); !errors.Is(err, service.ErrForbidden) {
		t.Errorf("other user: %v", err)
	}
	if _, err := svc.Answer(ctx, "", "app_1", "ir_text", service.AnswerInput{Comment: "x"}); !errors.Is(err, service.ErrUnauthorized) {
		t.Errorf("anonymous: %v", err)
	}
	if _, err := svc.Answer(ctx, "usr_rep", "app_1", "ir_nope", service.AnswerInput{Comment: "x"}); !errors.Is(err, service.ErrNotFound) {
		t.Errorf("unknown question: %v", err)
	}
	repo.err = domain.ErrQuestionClosed
	if _, err := svc.Answer(ctx, "usr_rep", "app_1", "ir_text", service.AnswerInput{Comment: "x"}); !errors.Is(err, domain.ErrQuestionClosed) {
		t.Errorf("closed: %v", err)
	}
}

func TestOutcomeDecision(t *testing.T) {
	ctx := context.Background()
	svc, repo, _ := newOutcome(t)
	if _, err := svc.Decision(ctx, "app_1"); !errors.Is(err, service.ErrNotFound) {
		t.Errorf("not scored yet: %v", err)
	}
	at := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	repo.decision = &domain.DecisionRecord{AutoOutcome: "REJECTED", CreatedAt: at,
		ClientReasons: json.RawMessage(`[{"code":"BANKRUPT","title":"Банкротство"}]`)}
	d, err := svc.Decision(ctx, "app_1")
	if err != nil || d.Outcome != "REJECTED" || d.ByEmployee || d.ReapplyAfter != "2026-11-24" || d.Reasons[0].Code != "BANKRUPT" {
		t.Errorf("engine rejection: %+v %v", d, err)
	}
	repo.decision = &domain.DecisionRecord{AutoOutcome: "MANUAL_REVIEW", Outcome: "APPROVED_WITH_CHANGES", CreatedAt: at,
		Details: json.RawMessage(`{"underwriter":{"decided_at":"2026-10-26T09:00:00Z"}}`)}
	if d, _ := svc.Decision(ctx, "app_1"); d.Outcome != "APPROVED_WITH_CHANGES" || !d.ByEmployee || d.DecidedAt.Day() != 26 || d.ReapplyAfter != "" {
		t.Errorf("underwriter approval: %+v", d)
	}
}

func TestOutcomeAnswerPreviouslyUploadedDocuments(t *testing.T) {
	svc, repo, _ := newOutcome(t)
	repo.app.Documents = []domain.Document{
		{ID: "poa", Type: domain.DocPowerOfAttorney, InfoRequestID: "ir_doc"},
		{ID: "sig", Type: domain.DocSignature, SignedDocumentID: "poa", InfoRequestID: "ir_doc"},
		{ID: "other-question", Type: domain.DocPowerOfAttorney, InfoRequestID: "ir_other"},
		{ID: "wrong-type", Type: domain.DocFinancialStatements, InfoRequestID: "ir_doc"},
	}
	for _, tc := range []struct {
		name  string
		ids   []string
		valid bool
	}{
		{"original and signature", []string{"poa", "sig"}, true},
		{"signature alone", []string{"sig"}, false},
		{"other question", []string{"other-question"}, false},
		{"unknown document", []string{"missing"}, false},
		{"wrong type", []string{"wrong-type"}, false},
		{"duplicate ids", []string{"poa", "poa"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo.answered = nil
			_, err := svc.Answer(context.Background(), "usr_rep", "app_1", "ir_doc", service.AnswerInput{DocumentIDs: tc.ids})
			if tc.valid {
				if err != nil || repo.answered == nil || len(repo.answered.Documents) != 0 || len(repo.answered.DocumentIDs) != 2 {
					t.Fatalf("answer: %+v %v", repo.answered, err)
				}
			} else {
				var validation *service.ValidationError
				if !errors.As(err, &validation) || repo.answered != nil {
					t.Fatalf("invalid attachments accepted: %v", err)
				}
			}
		})
	}
}
