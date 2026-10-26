package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/domain"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/service"
)

type fakeBackoffice struct {
	items   []domain.QueueItem
	filter  domain.QueueFilter
	dossier domain.Dossier
	decided *domain.UnderwriterDecision
	applied map[string]string
}

func (f *fakeBackoffice) Queue(_ context.Context, q domain.QueueFilter) ([]domain.QueueItem, error) {
	f.filter = q
	end := min(q.Offset+q.Limit, len(f.items))
	if q.Offset >= end {
		return nil, nil
	}
	return f.items[q.Offset:end], nil
}
func (f *fakeBackoffice) Counters(context.Context, string) (domain.QueueCounters, error) {
	return domain.QueueCounters{ManualReview: 3}, nil
}
func (f *fakeBackoffice) QueueItem(context.Context, string) (domain.QueueItem, bool, error) {
	return domain.QueueItem{}, true, nil
}
func (f *fakeBackoffice) Dossier(_ context.Context, id string) (domain.Dossier, bool, error) {
	if id != f.dossier.Application.ID {
		return domain.Dossier{}, false, nil
	}
	return f.dossier, true, nil
}
func (f *fakeBackoffice) Assign(context.Context, string, string) (bool, error) { return true, nil }
func (f *fakeBackoffice) Decide(_ context.Context, cmd domain.UnderwriterDecision) (bool, error) {
	f.decided = &cmd
	return true, nil
}
func (f *fakeBackoffice) DecisionApplied(_ context.Context, key string) (string, bool, error) {
	id, ok := f.applied[key]
	return id, ok, nil
}

func reviewDossier(proposed string) domain.Dossier {
	return domain.Dossier{
		Application: domain.Application{ID: "app_1", Status: domain.ApplicationManualReview, ProductCode: "WORKING_CAPITAL",
			Requested: &domain.RequestedTerms{Amount: 5_000_000, TermMonths: 24}},
		Version:  4,
		Decision: &domain.DecisionRecord{ID: "dcs_1", Details: json.RawMessage(`{"proposed_offer":` + proposed + `}`)},
	}
}

func TestBackofficeQueuePaging(t *testing.T) {
	repo := &fakeBackoffice{items: make([]domain.QueueItem, 5)}
	svc := service.NewBackofficeService(repo)
	items, counters, next, err := svc.Queue(context.Background(), "usr_uw", domain.QueueFilter{Assignee: "me", Limit: 2})
	if err != nil || len(items) != 2 || next != 2 || counters.ManualReview != 3 || repo.filter.Assignee != "usr_uw" {
		t.Fatalf("first page: %d items, next %d, %+v (%v), filter %+v", len(items), next, counters, err, repo.filter)
	}
	if items, _, next, _ := svc.Queue(context.Background(), "usr_uw", domain.QueueFilter{Limit: 2, Offset: 4}); len(items) != 1 || next != 0 {
		t.Errorf("last page: %d items, next %d", len(items), next)
	}
	if _, _, _, _ = svc.Queue(context.Background(), "u", domain.QueueFilter{Limit: 1000}); repo.filter.Limit != 101 {
		t.Errorf("limit is capped at 100 (+1 to see next page): %d", repo.filter.Limit)
	}
}

func TestBackofficeDecide(t *testing.T) {
	ctx := context.Background()
	newSvc := func(d domain.Dossier) (*service.BackofficeService, *fakeBackoffice) {
		repo := &fakeBackoffice{dossier: d, applied: map[string]string{}}
		return service.NewBackofficeService(repo), repo
	}

	// одобрение без условий — берутся условия движка; сумма меньше запрошенной — «с изменениями»
	svc, repo := newSvc(reviewDossier(`{"amount_rub":2030000,"term_months":36,"annual_rate":20.75}`))
	if _, err := svc.Decide(ctx, "usr_uw", "app_1", service.DecisionInput{Action: "APPROVE", IfMatch: ptrInt(4)}); err != nil {
		t.Fatal(err)
	}
	c := repo.decided
	if c.Status != domain.ApplicationOffered || c.Outcome != "APPROVED_WITH_CHANGES" || len(c.Offers) != 1 ||
		c.Offers[0].Amount != 2_030_000 || c.Offers[0].ValidDays != 14 || c.Offers[0].ProductCode != "WORKING_CAPITAL" {
		t.Errorf("approve with engine terms: %+v", c)
	}

	// как запрошено — просто одобрено
	svc, repo = newSvc(reviewDossier(`null`))
	_, err := svc.Decide(ctx, "usr_uw", "app_1", service.DecisionInput{Action: "APPROVE",
		Offers: []domain.OfferInput{{Amount: 5_000_000, TermMonths: 24, Rate: 22}}})
	if err != nil || repo.decided.Outcome != "APPROVED" || repo.decided.Offers[0].Kind != "REQUESTED" {
		t.Errorf("approve as requested: %+v (%v)", repo.decided, err)
	}

	// ошибки полей — все сразу
	svc, repo = newSvc(reviewDossier(`null`))
	_, err = svc.Decide(ctx, "usr_uw", "app_1", service.DecisionInput{Action: "APPROVE", InternalComment: "ok",
		Offers:      []domain.OfferInput{{Kind: "RETENTION", Amount: 0, TermMonths: 0, Rate: 150, ValidDays: 60}},
		Adjustments: []domain.Adjustment{{Code: "", Points: 0}}})
	var verr *service.ValidationError
	if !errors.As(err, &verr) || len(verr.Fields) != 7 || repo.decided != nil {
		t.Errorf("approve validation: %v", err)
	}
	if _, err := svc.Decide(ctx, "usr_uw", "app_1", service.DecisionInput{Action: "APPROVE"}); !errors.As(err, &verr) || verr.Fields[0].Field != "offers" {
		t.Errorf("approve without any terms: %v", err)
	}
	for action, field := range map[string]string{"REJECT": "client_reasons", "REQUEST_INFO": "info_requests", "PAUSE": "action"} {
		if _, err := svc.Decide(ctx, "usr_uw", "app_1", service.DecisionInput{Action: action}); !errors.As(err, &verr) || verr.Fields[0].Field != field {
			t.Errorf("%s: %v", action, err)
		}
	}

	// отказ и запрос данных — сроки и статусы
	svc, repo = newSvc(reviewDossier(`null`))
	if _, err := svc.Decide(ctx, "usr_uw", "app_1", service.DecisionInput{Action: "REJECT",
		ClientReasons: []domain.ClientReason{{Code: "LOW_CASH_FLOW", Title: "Недостаточный денежный поток"}}}); err != nil {
		t.Fatal(err)
	}
	if c := repo.decided; c.Status != domain.ApplicationRejected || c.ReapplyAfter.Sub(c.Now).Hours() != 30*24 {
		t.Errorf("reject: %+v", c)
	}
	if _, err := svc.Decide(ctx, "usr_uw", "app_1", service.DecisionInput{Action: "REQUEST_INFO",
		InfoRequests: []domain.InfoRequestInput{{Title: "Выписка за 12 месяцев", Required: true}}}); err != nil {
		t.Fatal(err)
	}
	if c := repo.decided; c.Status != domain.ApplicationWaitingForInfo || c.Outcome != "NEED_INFO" || c.InfoDueAt.Sub(c.Now).Hours() != 5*24 {
		t.Errorf("request info: %+v", c)
	}

	// статус, чужой исполнитель и версия — раньше проверки полей
	d := reviewDossier(`null`)
	d.Assignee = &domain.UserRef{ID: "usr_other"}
	svc, _ = newSvc(d)
	if _, err := svc.Decide(ctx, "usr_uw", "app_1", service.DecisionInput{Action: "APPROVE"}); !errors.Is(err, domain.ErrAssignedToOther) {
		t.Errorf("other assignee: %v", err)
	}
	svc, _ = newSvc(reviewDossier(`null`))
	if _, err := svc.Decide(ctx, "usr_uw", "app_1", service.DecisionInput{Action: "APPROVE", IfMatch: ptrInt(3)}); !errors.Is(err, domain.ErrVersionConflict) {
		t.Errorf("stale version: %v", err)
	}
	d = reviewDossier(`null`)
	d.Application.Status = domain.ApplicationOffered
	svc, _ = newSvc(d)
	if _, err := svc.Decide(ctx, "usr_uw", "app_1", service.DecisionInput{Action: "APPROVE"}); !errors.Is(err, domain.ErrNotInReview) {
		t.Errorf("not in review: %v", err)
	}
	if _, err := svc.Decide(ctx, "usr_uw", "app_nope", service.DecisionInput{Action: "APPROVE"}); !errors.Is(err, service.ErrNotFound) {
		t.Errorf("unknown application: %v", err)
	}

	// повтор с тем же ключом — без нового решения, даже после смены статуса
	svc, repo = newSvc(d)
	repo.applied["key-1"] = "app_1"
	if _, err := svc.Decide(ctx, "usr_uw", "app_1", service.DecisionInput{Action: "APPROVE", IdempotencyKey: "key-1"}); err != nil || repo.decided != nil {
		t.Errorf("replay: %v %+v", err, repo.decided)
	}
	repo.applied["key-2"] = "app_other"
	if _, err := svc.Decide(ctx, "usr_uw", "app_1", service.DecisionInput{Action: "APPROVE", IdempotencyKey: "key-2"}); !errors.As(err, &verr) {
		t.Errorf("key of another application: %v", err)
	}
}

func ptrInt(v int) *int { return &v }
