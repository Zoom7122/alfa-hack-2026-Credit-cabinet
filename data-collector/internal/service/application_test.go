package service_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/domain"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/repository/memory"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/service"
)

func newApplicationService() (*service.ApplicationService, *memory.ApplicationRepository) {
	apps := memory.NewApplicationRepository(memory.NewCollectionTaskRepository())
	n := 0
	return service.NewApplicationService(apps,
		service.WithApplicationClock(func() time.Time { return time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC) }),
		service.WithApplicationIDGenerator(func(prefix string) string { n++; return prefix + strings.Repeat("0", n) }),
	), apps
}

func TestRegister(t *testing.T) {
	svc, apps := newApplicationService()

	app, err := svc.Register(context.Background(), service.RegisterInput{
		FullName: "  Литвинова   Марина Олеговна ", PersonINN: " 779900010333 ", Role: "accountant",
		CompanyINN: " 7799000012 ", CompanyName: " ООО «Северный ветер» "})
	if err != nil {
		t.Fatal(err)
	}
	rep, c := app.Representative, app.Company
	if app.Status != domain.ApplicationDraft || app.Number != "A-2026-000001" || !strings.HasPrefix(app.ID, "app_") {
		t.Fatalf("application = %+v", app)
	}
	if rep.FullName != "Литвинова Марина Олеговна" || rep.INN != "779900010333" || rep.Role != domain.RoleAccountant || rep.CanSign {
		t.Fatalf("representative = %+v", rep)
	}
	if c.INN != "7799000012" || c.Type != domain.CompanyLegalEntity || c.Name != "ООО «Северный ветер»" || c.AlreadyKnown {
		t.Fatalf("company = %+v", c)
	}
	if ev := apps.Events(app.ID); !slices.Equal(ev, []string{"application.created"}) {
		t.Fatalf("events = %v", ev)
	}

	// тот же человек и та же компания — тот же черновик; другая роль запоминается
	again, err := svc.Register(context.Background(), service.RegisterInput{FullName: "Литвинова Марина Олеговна",
		PersonINN: "779900010333", Role: domain.RoleCFO, CompanyINN: "7799000012"})
	if err != nil {
		t.Fatal(err)
	}
	if again.ID != app.ID || again.Representative.UserID != rep.UserID || again.Representative.Role != domain.RoleCFO ||
		!again.Company.AlreadyKnown || again.Company.Name != "ООО «Северный ветер»" {
		t.Fatalf("second registration = %+v", again)
	}
}

// Директор и сам ИП подписывают без доверенности; имя компании до сбора — по ИНН.
func TestRegisterCanSign(t *testing.T) {
	svc, _ := newApplicationService()
	ip, err := svc.Register(context.Background(), service.RegisterInput{FullName: "Смирнова Анна Викторовна",
		PersonINN: "779900010202", Role: domain.RoleOwner, CompanyINN: "779900010202"})
	if err != nil {
		t.Fatal(err)
	}
	if !ip.Representative.CanSign || ip.Company.Type != domain.CompanySoleProprietor || ip.Company.Name != "ИНН 779900010202" {
		t.Fatalf("ip = %+v", ip)
	}
	owner, err := svc.Register(context.Background(), service.RegisterInput{FullName: "Кузнецов Павел Андреевич",
		Role: domain.RoleOwner, CompanyINN: "7799000044"})
	if err != nil {
		t.Fatal(err)
	}
	if owner.Representative.CanSign {
		t.Fatal("owner of a legal entity needs a power of attorney to sign")
	}
}

func TestRegisterValidation(t *testing.T) {
	svc, _ := newApplicationService()
	_, err := svc.Register(context.Background(), service.RegisterInput{FullName: " ", PersonINN: "7799",
		Role: "CEO", CompanyINN: "123", CompanyName: strings.Repeat("я", 501)})

	var verr *service.ValidationError
	if !errors.As(err, &verr) {
		t.Fatalf("want *ValidationError, got %v", err)
	}
	want := []string{"representative.full_name", "representative.inn", "representative.role", "company.inn", "company.name"}
	if got := fieldNames(verr); !slices.Equal(got, want) {
		t.Fatalf("fields = %v, want %v", got, want)
	}
}

func TestGetApplication(t *testing.T) {
	svc, _ := newApplicationService()
	if _, err := svc.Get(context.Background(), "app_unknown"); !errors.Is(err, service.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	app, err := svc.Register(context.Background(), service.RegisterInput{FullName: "Орлов Денис Павлович",
		Role: domain.RoleDirector, CompanyINN: "7799000020"})
	if err != nil {
		t.Fatal(err)
	}
	got, err := svc.Get(context.Background(), app.ID)
	if err != nil || got.ID != app.ID || got.Representative.FullName != "Орлов Денис Павлович" || !got.Representative.CanSign {
		t.Fatalf("got %+v, err %v", got, err)
	}
}
