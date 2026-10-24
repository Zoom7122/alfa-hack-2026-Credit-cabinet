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

var company = domain.CollectInput{INN: "7799000012"}

// fakeCollector запоминает вызовы; fields — ошибки валидации, err — ошибка постановки сбора.
type fakeCollector struct {
	fields []service.FieldError
	err    error
	calls  []string // id анкет, по которым ставили сбор
}

func (f *fakeCollector) Validate(domain.CollectInput) []service.FieldError { return f.fields }

func (f *fakeCollector) Enqueue(_ context.Context, questionnaireID string, in domain.CollectInput) (domain.CollectionTask, error) {
	f.calls = append(f.calls, questionnaireID)
	if f.err != nil {
		return domain.CollectionTask{}, f.err
	}
	return domain.CollectionTask{ID: "tsk_test", QuestionnaireID: questionnaireID, Input: in, Status: domain.TaskPending}, nil
}

func TestCreate(t *testing.T) {
	fixedNow := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name       string
		in         service.CreateInput
		wantFields []string // поля с ошибками; nil — анкета валидна
	}{
		{name: "valid", in: service.CreateInput{Age: 34, MaritalStatus: domain.MaritalMarried, ChildrenCount: 2, Company: company}},
		{name: "boundaries", in: service.CreateInput{Age: 18, MaritalStatus: domain.MaritalDivorcedMultiple, ChildrenCount: 0, Company: company}},
		{name: "too young", in: service.CreateInput{Age: 17, MaritalStatus: domain.MaritalSingle}, wantFields: []string{"age"}},
		{name: "too old", in: service.CreateInput{Age: 101, MaritalStatus: domain.MaritalSingle}, wantFields: []string{"age"}},
		{name: "unknown status", in: service.CreateInput{Age: 30, MaritalStatus: "UNKNOWN"}, wantFields: []string{"marital_status"}},
		{name: "negative children", in: service.CreateInput{Age: 30, MaritalStatus: domain.MaritalSingle, ChildrenCount: -1}, wantFields: []string{"children_count"}},
		{
			name:       "all fields invalid",
			in:         service.CreateInput{Age: 5, MaritalStatus: "", ChildrenCount: 21},
			wantFields: []string{"age", "marital_status", "children_count"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := memory.NewQuestionnaireRepository()
			collector := &fakeCollector{}
			svc := service.NewQuestionnaireService(repo, collector,
				service.WithClock(func() time.Time { return fixedNow }),
				service.WithIDGenerator(func() string { return "qst_test" }),
			)

			res, err := svc.Create(context.Background(), tt.in)

			if tt.wantFields != nil {
				var verr *service.ValidationError
				if !errors.As(err, &verr) {
					t.Fatalf("want *ValidationError, got %v", err)
				}
				if got := fieldNames(verr); !slices.Equal(got, tt.wantFields) {
					t.Fatalf("invalid fields = %v, want %v", got, tt.wantFields)
				}
				if _, ok := repo.Get(context.Background(), "qst_test"); ok {
					t.Fatal("invalid questionnaire must not be saved")
				}
				if len(collector.calls) != 0 {
					t.Fatal("collection must not be enqueued for an invalid questionnaire")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			want := domain.Questionnaire{
				ID:            "qst_test",
				Age:           tt.in.Age,
				MaritalStatus: tt.in.MaritalStatus,
				ChildrenCount: tt.in.ChildrenCount,
				CreatedAt:     fixedNow,
			}
			if res.Questionnaire != want {
				t.Fatalf("got %+v, want %+v", res.Questionnaire, want)
			}
			if saved, ok := repo.Get(context.Background(), "qst_test"); !ok || saved != want {
				t.Fatalf("saved = %+v (found %v), want %+v", saved, ok, want)
			}
			if !slices.Equal(collector.calls, []string{"qst_test"}) || res.Task.ID != "tsk_test" ||
				res.Task.Input.INN != company.INN {
				t.Fatalf("task = %+v, calls %v", res.Task, collector.calls)
			}
		})
	}
}

// Ошибки анкеты и данных для сбора возвращаются вместе, за один запрос.
func TestCreateMergesCollectionErrors(t *testing.T) {
	collector := &fakeCollector{fields: []service.FieldError{{Field: "inn", Code: "invalid_format"}}}
	svc := service.NewQuestionnaireService(memory.NewQuestionnaireRepository(), collector)

	_, err := svc.Create(context.Background(), service.CreateInput{Age: 5, MaritalStatus: domain.MaritalSingle})

	var verr *service.ValidationError
	if !errors.As(err, &verr) {
		t.Fatalf("want *ValidationError, got %v", err)
	}
	if got := fieldNames(verr); !slices.Equal(got, []string{"age", "inn"}) {
		t.Fatalf("fields = %v", got)
	}
}

func TestCreateRepositoryError(t *testing.T) {
	boom := errors.New("db down")
	collector := &fakeCollector{}
	svc := service.NewQuestionnaireService(failingRepo{err: boom}, collector)

	_, err := svc.Create(context.Background(), service.CreateInput{Age: 30, MaritalStatus: domain.MaritalSingle, Company: company})
	if !errors.Is(err, boom) {
		t.Fatalf("want wrapped repo error, got %v", err)
	}
	if len(collector.calls) != 0 {
		t.Fatal("collection must not be enqueued when the questionnaire is not saved")
	}
}

func TestCreateEnqueueError(t *testing.T) {
	boom := errors.New("collection_tasks insert failed")
	repo := memory.NewQuestionnaireRepository()
	svc := service.NewQuestionnaireService(repo, &fakeCollector{err: boom},
		service.WithIDGenerator(func() string { return "qst_test" }))

	res, err := svc.Create(context.Background(), service.CreateInput{Age: 30, MaritalStatus: domain.MaritalSingle, Company: company})
	if !errors.Is(err, boom) {
		t.Fatalf("want wrapped enqueue error, got %v", err)
	}
	if res.Questionnaire.ID != "qst_test" {
		t.Fatalf("questionnaire must be returned: %+v", res)
	}
	if _, ok := repo.Get(context.Background(), "qst_test"); !ok {
		t.Fatal("questionnaire is saved before the collection is enqueued")
	}
}

func TestDefaultIDPrefix(t *testing.T) {
	svc := service.NewQuestionnaireService(memory.NewQuestionnaireRepository(), &fakeCollector{})
	res, err := svc.Create(context.Background(), service.CreateInput{Age: 30, MaritalStatus: domain.MaritalSingle, Company: company})
	if err != nil {
		t.Fatal(err)
	}
	if id := res.Questionnaire.ID; len(id) != len("qst_")+26 || id[:4] != "qst_" {
		t.Fatalf("unexpected id %q", id)
	}
}

type failingRepo struct{ err error }

func (r failingRepo) Save(context.Context, domain.Questionnaire) error { return r.err }

func fieldNames(verr *service.ValidationError) []string {
	var got []string
	for _, f := range verr.Fields {
		got = append(got, f.Field)
	}
	return got
}
