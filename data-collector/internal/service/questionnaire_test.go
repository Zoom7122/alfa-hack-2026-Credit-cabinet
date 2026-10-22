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

func TestCreate(t *testing.T) {
	fixedNow := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name       string
		in         service.CreateInput
		wantFields []string // поля с ошибками; nil — анкета валидна
	}{
		{name: "valid", in: service.CreateInput{Age: 34, MaritalStatus: domain.MaritalMarried, ChildrenCount: 2}},
		{name: "boundaries", in: service.CreateInput{Age: 18, MaritalStatus: domain.MaritalDivorcedMultiple, ChildrenCount: 0}},
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
			svc := service.NewQuestionnaireService(repo,
				service.WithClock(func() time.Time { return fixedNow }),
				service.WithIDGenerator(func() string { return "qst_test" }),
			)

			q, err := svc.Create(context.Background(), tt.in)

			if tt.wantFields != nil {
				var verr *service.ValidationError
				if !errors.As(err, &verr) {
					t.Fatalf("want *ValidationError, got %v", err)
				}
				var got []string
				for _, f := range verr.Fields {
					got = append(got, f.Field)
				}
				if !slices.Equal(got, tt.wantFields) {
					t.Fatalf("invalid fields = %v, want %v", got, tt.wantFields)
				}
				if _, ok := repo.Get(context.Background(), "qst_test"); ok {
					t.Fatal("invalid questionnaire must not be saved")
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
			if q != want {
				t.Fatalf("got %+v, want %+v", q, want)
			}
			if saved, ok := repo.Get(context.Background(), "qst_test"); !ok || saved != want {
				t.Fatalf("saved = %+v (found %v), want %+v", saved, ok, want)
			}
		})
	}
}

func TestCreateRepositoryError(t *testing.T) {
	boom := errors.New("db down")
	svc := service.NewQuestionnaireService(failingRepo{err: boom})

	_, err := svc.Create(context.Background(), service.CreateInput{Age: 30, MaritalStatus: domain.MaritalSingle})
	if !errors.Is(err, boom) {
		t.Fatalf("want wrapped repo error, got %v", err)
	}
}

func TestDefaultIDPrefix(t *testing.T) {
	svc := service.NewQuestionnaireService(memory.NewQuestionnaireRepository())
	q, err := svc.Create(context.Background(), service.CreateInput{Age: 30, MaritalStatus: domain.MaritalSingle})
	if err != nil {
		t.Fatal(err)
	}
	if len(q.ID) != len("qst_")+26 || q.ID[:4] != "qst_" {
		t.Fatalf("unexpected id %q", q.ID)
	}
}

type failingRepo struct{ err error }

func (r failingRepo) Save(context.Context, domain.Questionnaire) error { return r.err }
