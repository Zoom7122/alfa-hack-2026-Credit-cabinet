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

func TestValidateCollectInput(t *testing.T) {
	svc := service.NewCollectionService(memory.NewCollectionTaskRepository())

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

// ctxTasks — хранилище задач, которое запоминает контекст записи; err — ошибка записи.
type ctxTasks struct {
	*memory.CollectionTaskRepository
	err         error
	gotCanceled bool
	gotDeadline bool
}

func (r *ctxTasks) Create(ctx context.Context, task domain.CollectionTask) error {
	r.gotCanceled = ctx.Err() != nil
	_, r.gotDeadline = ctx.Deadline()
	if r.err != nil {
		return r.err
	}
	return r.CollectionTaskRepository.Create(ctx, task)
}

func newCollectionService(tasks service.TaskRepository) *service.CollectionService {
	return service.NewCollectionService(tasks,
		service.WithCollectionClock(func() time.Time { return time.Date(2026, 10, 22, 12, 0, 0, 0, time.FixedZone("MSK", 3*3600)) }),
		service.WithCollectionIDGenerator(func() string { return "tsk_test" }),
	)
}

func TestEnqueue(t *testing.T) {
	tasks := memory.NewCollectionTaskRepository()
	in := fullInput()
	in.INN = " 7799000020 "
	in.OtherBankBIKs = []string{"044525777", " 044525777", "044525650"}
	in.Collateral.VINs = []string{"xtc652005n1498213"}
	in.POANumber = "0F5A2C1E-7B3D-4E8A-9C21-6D4B8E0A1F37"

	task, err := newCollectionService(tasks).Enqueue(context.Background(), "qst_test", in)
	if err != nil {
		t.Fatal(err)
	}
	if task.ID != "tsk_test" || task.QuestionnaireID != "qst_test" || task.Status != domain.TaskPending ||
		!task.CreatedAt.Equal(time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)) || task.CreatedAt.Location() != time.UTC {
		t.Fatalf("task = %+v", task)
	}
	// в задачу и worker'у уходят нормализованные данные; для полной анкеты опрашиваются все системы
	got := task.Input
	if got.INN != "7799000020" || !slices.Equal(got.OtherBankBIKs, []string{"044525777", "044525650"}) ||
		got.Collateral.VINs[0] != "XTC652005N1498213" || got.POANumber != "0f5a2c1e-7b3d-4e8a-9c21-6d4b8e0a1f37" ||
		got.MarketplaceTokens["wildberries"] != "wb-token" || len(task.Skipped) != 0 {
		t.Fatalf("task input = %+v, skipped %v", got, task.Skipped)
	}

	saved, ok, err := tasks.Get(context.Background(), "tsk_test")
	if err != nil || !ok || saved.Status != domain.TaskPending || saved.Input.INN != "7799000020" {
		t.Fatalf("saved = %+v (found %v, err %v)", saved, ok, err)
	}
	if out := tasks.Outbox(); len(out) != 1 || out[0].Key != "tsk_test" {
		t.Fatalf("outbox = %+v", out)
	}
}

// Только ИНН: системы без данных перечислены в Skipped в порядке колонок raw_collections.
func TestEnqueueSkipsSystemsWithoutInput(t *testing.T) {
	task, err := newCollectionService(memory.NewCollectionTaskRepository()).Enqueue(context.Background(), "qst_test",
		domain.CollectInput{INN: "7799000012"})
	if err != nil {
		t.Fatal(err)
	}
	want := []domain.System{domain.SystemBKI, domain.SystemTaxReports, domain.SystemEGRN, domain.SystemGosuslugi,
		domain.SystemOpenBanking, domain.SystemMarketplaces, domain.SystemOFD}
	if !slices.Equal(task.Skipped, want) {
		t.Fatalf("skipped = %v, want %v", task.Skipped, want)
	}
}

// Клиент отключился — задача всё равно ставится: анкета уже сохранена.
func TestEnqueueIgnoresClientCancel(t *testing.T) {
	tasks := &ctxTasks{CollectionTaskRepository: memory.NewCollectionTaskRepository()}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := newCollectionService(tasks).Enqueue(ctx, "qst_test", fullInput()); err != nil {
		t.Fatal(err)
	}
	if tasks.gotCanceled || !tasks.gotDeadline {
		t.Fatalf("create context: canceled %v, deadline %v", tasks.gotCanceled, tasks.gotDeadline)
	}
	if _, ok, _ := tasks.Get(context.Background(), "tsk_test"); !ok {
		t.Fatal("task must be created")
	}
}

func TestEnqueueSaveError(t *testing.T) {
	boom := errors.New("db down")
	tasks := &ctxTasks{CollectionTaskRepository: memory.NewCollectionTaskRepository(), err: boom}
	if _, err := newCollectionService(tasks).Enqueue(context.Background(), "qst_test", fullInput()); !errors.Is(err, boom) {
		t.Fatalf("want wrapped repo error, got %v", err)
	}
}

func TestGet(t *testing.T) {
	tasks := memory.NewCollectionTaskRepository()
	svc := newCollectionService(tasks)

	if _, err := svc.Get(context.Background(), "tsk_test"); !errors.Is(err, service.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	if _, err := svc.Enqueue(context.Background(), "qst_test", fullInput()); err != nil {
		t.Fatal(err)
	}
	task, err := svc.Get(context.Background(), "tsk_test")
	if err != nil || task.ID != "tsk_test" || task.Status != domain.TaskPending || task.Result != nil {
		t.Fatalf("task = %+v, err %v", task, err)
	}
}

func TestDefaultTaskIDPrefix(t *testing.T) {
	task, err := service.NewCollectionService(memory.NewCollectionTaskRepository()).Enqueue(context.Background(), "qst_test",
		domain.CollectInput{INN: "7799000012"})
	if err != nil {
		t.Fatal(err)
	}
	if len(task.ID) != len("tsk_")+26 || task.ID[:4] != "tsk_" {
		t.Fatalf("unexpected id %q", task.ID)
	}
}
