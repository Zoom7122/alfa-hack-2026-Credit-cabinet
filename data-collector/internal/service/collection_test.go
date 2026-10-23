package service_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/domain"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/repository/memory"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/service"
)

// fakeSystems отвечает DONE по каждой системе и запоминает, с чем его вызвали.
type fakeSystems struct {
	gotRequestID string
	gotInput     domain.CollectInput
	gotDeadline  bool
	gotCanceled  bool
}

func (f *fakeSystems) CollectAll(ctx context.Context, requestID string, in domain.CollectInput) []domain.SystemResult {
	f.gotRequestID, f.gotInput = requestID, in
	_, f.gotDeadline = ctx.Deadline()
	f.gotCanceled = ctx.Err() != nil
	out := make([]domain.SystemResult, 0, len(domain.Systems))
	for _, sys := range domain.Systems {
		out = append(out, domain.SystemResult{System: sys, State: domain.CollectDone, Data: []byte(`{}`)})
	}
	return out
}

type failingRawRepo struct{ err error }

func (r failingRawRepo) Save(context.Context, domain.RawCollection) error { return r.err }

func fullInput() domain.CollectInput {
	consent := &domain.Consent{ID: "CNS-1", SignedAt: "2026-10-21T14:02:11Z", ValidUntil: "2027-04-21"}
	return domain.CollectInput{
		INN: "7799000020",
		Director: &domain.Person{INN: "779900020136", LastName: "Орлов", FirstName: "Денис", MiddleName: "Павлович",
			BirthDate: "1979-02-11"},
		BKICompanyConsent: consent,
		BKIPersonConsent:  consent,
		TaxConsent:        &domain.Consent{ID: "TAX-1", ValidUntil: "2027-04-21", Scope: []string{"DECLARATIONS", "ENS_BALANCE"}},
		OtherBankBIKs:     []string{"044525777", "044525650"},
		MarketplaceTokens: map[string]string{"wildberries": "wb-token"},
		OFDToken:          "ofd-token",
		Collateral: domain.Collateral{CadastralNumbers: []string{"50:21:0110203:845"}, VINs: []string{"XTC652005N1498213"},
			Serials: []string{"JCB3CX2023118457"}},
		POANumber:         "0f5a2c1e-7b3d-4e8a-9c21-6d4b8e0a1f37",
		RepresentativeINN: "779900010333",
	}
}

func newCollectionService(systems service.ExternalSystems, repo service.RawCollectionRepository) *service.CollectionService {
	clock := []time.Time{time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC), time.Date(2026, 10, 22, 12, 0, 1, 0, time.UTC)}
	return service.NewCollectionService(systems, repo, time.Second,
		service.WithCollectionClock(func() time.Time { t := clock[0]; clock = clock[1:]; return t }),
		service.WithCollectionIDGenerator(func() string { return "col_test" }),
	)
}

func TestValidateCollectInput(t *testing.T) {
	svc := service.NewCollectionService(&fakeSystems{}, memory.NewRawCollectionRepository(), time.Second)

	if fields := svc.Validate(fullInput()); len(fields) != 0 {
		t.Fatalf("full input must be valid: %+v", fields)
	}
	if fields := svc.Validate(domain.CollectInput{INN: "779900010202"}); len(fields) != 0 {
		t.Fatalf("only INN is required: %+v", fields)
	}

	tests := []struct {
		name   string
		mutate func(*domain.CollectInput)
		want   []string
	}{
		{"no inn", func(in *domain.CollectInput) { in.INN = "" }, []string{"inn"}},
		{"bad inn", func(in *domain.CollectInput) { in.INN = "77990000" }, []string{"inn"}},
		{"director without data", func(in *domain.CollectInput) { in.Director = &domain.Person{} },
			[]string{"director.inn", "director.last_name", "director.first_name", "director.birth_date"}},
		{"bad consent", func(in *domain.CollectInput) {
			in.BKICompanyConsent = &domain.Consent{ValidUntil: "21.04.2027", SignedAt: "вчера"}
		}, []string{"consents.bki_company.id", "consents.bki_company.valid_until", "consents.bki_company.signed_at"}},
		{"person consent without director", func(in *domain.CollectInput) { in.Director = nil }, []string{"consents.bki_person"}},
		{"tax scope", func(in *domain.CollectInput) { in.TaxConsent.Scope = []string{"SALARY"} }, []string{"consents.tax.scope[0]"}},
		{"empty tax scope", func(in *domain.CollectInput) { in.TaxConsent.Scope = nil }, []string{"consents.tax.scope"}},
		{"bad bik", func(in *domain.CollectInput) { in.OtherBankBIKs = []string{"044525777", "12345"} }, []string{"other_bank_biks[1]"}},
		{"marketplaces", func(in *domain.CollectInput) {
			in.MarketplaceTokens = map[string]string{"avito": "t", "ozon": " "}
		}, []string{"marketplace_tokens.avito", "marketplace_tokens.ozon"}},
		{"collateral", func(in *domain.CollectInput) {
			in.Collateral = domain.Collateral{CadastralNumbers: []string{"склад"}, VINs: []string{"XTC652005N149821O"}, Serials: []string{""}}
		}, []string{"collateral.cadastral_numbers[0]", "collateral.vins[0]", "collateral.serials[0]"}},
		{"poa", func(in *domain.CollectInput) { in.POANumber = "МЧД-1"; in.RepresentativeINN = "7799000012" },
			[]string{"poa_number", "representative_inn"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := fullInput()
			tt.mutate(&in)
			var got []string
			for _, f := range svc.Validate(in) {
				got = append(got, f.Field)
			}
			if !slices.Equal(got, tt.want) {
				t.Fatalf("fields = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCollect(t *testing.T) {
	systems := &fakeSystems{}
	repo := memory.NewRawCollectionRepository()
	svc := newCollectionService(systems, repo)

	in := fullInput()
	in.INN = " 7799000020 "
	in.OtherBankBIKs = []string{"044525777", " 044525777", "044525650"}
	in.Collateral.VINs = []string{"xtc652005n1498213"}
	in.POANumber = "0F5A2C1E-7B3D-4E8A-9C21-6D4B8E0A1F37"

	rc, err := svc.Collect(context.Background(), "qst_test", in)
	if err != nil {
		t.Fatal(err)
	}
	if rc.ID != "col_test" || rc.QuestionnaireID != "qst_test" || len(rc.Systems) != len(domain.Systems) ||
		!rc.FinishedAt.After(rc.StartedAt) {
		t.Fatalf("collection = %+v", rc)
	}
	// системы получают id сбора (для X-Request-Id) и нормализованные данные
	got := systems.gotInput
	if systems.gotRequestID != "col_test" || got.INN != "7799000020" ||
		!slices.Equal(got.OtherBankBIKs, []string{"044525777", "044525650"}) ||
		got.Collateral.VINs[0] != "XTC652005N1498213" || got.POANumber != "0f5a2c1e-7b3d-4e8a-9c21-6d4b8e0a1f37" {
		t.Fatalf("systems got %+v", got)
	}
	if !systems.gotDeadline {
		t.Fatal("collection must have a deadline")
	}
	if saved, ok := repo.Get(context.Background(), "col_test"); !ok || saved.Input.INN != "7799000020" {
		t.Fatalf("saved = %+v (found %v)", saved, ok)
	}
}

// Клиент отключился — сбор всё равно доводится до конца и сохраняется.
func TestCollectIgnoresClientCancel(t *testing.T) {
	systems := &fakeSystems{}
	repo := memory.NewRawCollectionRepository()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := newCollectionService(systems, repo).Collect(ctx, "qst_test", fullInput()); err != nil {
		t.Fatal(err)
	}
	if systems.gotCanceled {
		t.Fatal("collection context must not inherit the request cancellation")
	}
	if _, ok := repo.Get(context.Background(), "col_test"); !ok {
		t.Fatal("collection must be saved")
	}
}

func TestCollectSaveError(t *testing.T) {
	boom := errors.New("db down")
	_, err := newCollectionService(&fakeSystems{}, failingRawRepo{err: boom}).Collect(context.Background(), "qst_test", fullInput())
	if !errors.Is(err, boom) {
		t.Fatalf("want wrapped repo error, got %v", err)
	}
}
