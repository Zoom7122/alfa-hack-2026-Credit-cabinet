package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/domain"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/service"
)

// fakeBank отвечает минимальными данными; системы из failing возвращают ошибку.
type fakeBank struct {
	failing map[domain.BankSource]bool
	gotINN  chan string
}

func (f fakeBank) err(src domain.BankSource, inn string) error {
	if f.gotINN != nil {
		f.gotINN <- inn
	}
	if f.failing[src] {
		return errors.New(string(src) + " unavailable")
	}
	return nil
}

func (f fakeBank) Accounts(_ context.Context, inn string) (domain.AccountsReport, error) {
	return domain.AccountsReport{}, f.err(domain.SourceAccounts, inn)
}

func (f fakeBank) Credits(_ context.Context, inn string) (domain.CreditReport, error) {
	return domain.CreditReport{Bureau: "НБКИ"}, f.err(domain.SourceCredits, inn)
}

func (f fakeBank) Acquiring(_ context.Context, inn string) (domain.AcquiringReport, error) {
	return domain.AcquiringReport{Acquirer: "acq"}, f.err(domain.SourceAcquiring, inn)
}

func (f fakeBank) SalaryProject(_ context.Context, inn string) (domain.SalaryProjectReport, error) {
	return domain.SalaryProjectReport{EmployeesWithCards: 38}, f.err(domain.SourceSalaryProject, inn)
}

func (f fakeBank) CardIndex(_ context.Context, inn string) (domain.CardIndexReport, error) {
	return domain.CardIndexReport{TotalUnpaid: "0.00"}, f.err(domain.SourceCardIndex, inn)
}

func TestCollectAllSources(t *testing.T) {
	got := make(chan string, 5)
	svc := service.NewInternalDataService(fakeBank{gotINN: got})

	data, err := svc.Collect(context.Background(), "7701234567")
	if err != nil {
		t.Fatal(err)
	}
	close(got)
	for inn := range got {
		if inn != "7701234567" {
			t.Fatalf("bank called with inn %q", inn)
		}
	}

	if data.Accounts == nil || data.Credits == nil || data.Acquiring == nil || data.SalaryProject == nil || data.CardIndex == nil {
		t.Fatalf("missing sections: %+v", data)
	}
	wantOrder := []domain.BankSource{
		domain.SourceAccounts, domain.SourceCredits, domain.SourceAcquiring, domain.SourceSalaryProject, domain.SourceCardIndex,
	}
	if len(data.Sources) != len(wantOrder) {
		t.Fatalf("sources = %+v", data.Sources)
	}
	for i, s := range data.Sources {
		if s.Source != wantOrder[i] || s.State != domain.SourceDone || s.Error != "" {
			t.Fatalf("sources[%d] = %+v", i, s)
		}
	}
}

func TestCollectPartialFailure(t *testing.T) {
	svc := service.NewInternalDataService(fakeBank{failing: map[domain.BankSource]bool{
		domain.SourceAcquiring: true,
		domain.SourceCardIndex: true,
	}})

	data, err := svc.Collect(context.Background(), "770123456789")
	if err != nil {
		t.Fatal(err)
	}
	if data.Acquiring != nil || data.CardIndex != nil {
		t.Fatal("failed sources must stay nil")
	}
	if data.Accounts == nil || data.Credits == nil || data.SalaryProject == nil {
		t.Fatal("successful sources must be filled")
	}
	failed := 0
	for _, s := range data.Sources {
		if s.State == domain.SourceFailed {
			failed++
			if s.Error == "" {
				t.Fatalf("failed source without error: %+v", s)
			}
		}
	}
	if failed != 2 {
		t.Fatalf("failed = %d, sources = %+v", failed, data.Sources)
	}
}

func TestCollectInvalidINN(t *testing.T) {
	svc := service.NewInternalDataService(fakeBank{})

	for _, inn := range []string{"", "123", "77012345678", "770123456a"} {
		_, err := svc.Collect(context.Background(), inn)
		var verr *service.ValidationError
		if !errors.As(err, &verr) || verr.Fields[0].Field != "inn" {
			t.Fatalf("inn %q: want ValidationError on inn, got %v", inn, err)
		}
	}
}
