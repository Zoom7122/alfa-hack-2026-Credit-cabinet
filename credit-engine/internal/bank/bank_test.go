package bank

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/contracts"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/clock"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/config"
)

var fixed = clock.Fixed{Time: time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)}

type notifierFunc func(context.Context, contracts.AdminEvent) (contracts.EventAck, error)

func (f notifierFunc) Notify(ctx context.Context, e contracts.AdminEvent) (contracts.EventAck, error) {
	return f(ctx, e)
}
func policy(t *testing.T) config.Policy {
	t.Helper()
	p, err := config.LoadPolicy("../../config/policy.json")
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func initial(t *testing.T) contracts.BankState {
	t.Helper()
	s, err := config.LoadBankState("../../config/bank.default.json")
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func read(t *testing.T, path string, out any) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("../../testdata", path))
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(b, out); err != nil {
		t.Fatal(err)
	}
}
func mustCopy[T any](t *testing.T, v T) T {
	t.Helper()
	c, err := copyJSON(v)
	if err != nil {
		t.Fatal(err)
	}
	return c
}
func service(t *testing.T, p config.Policy, state contracts.BankState, n Notifier) *Service {
	t.Helper()
	s, err := NewService(p, state, fixed, n)
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func delivered(ctx context.Context, e contracts.AdminEvent) (contracts.EventAck, error) {
	return contracts.EventAck{EventID: e.EventID, Accepted: true}, nil
}

type bankFixture struct {
	Initial  contracts.BankState        `json:"initial_bank"`
	Request  contracts.BankCheckRequest `json:"request"`
	Expected struct {
		Response     contracts.BankCheckResponse `json:"response"`
		Notification contracts.AdminEvent        `json:"notification"`
		Saved        contracts.BankState         `json:"bank_state_saved"`
	} `json:"expected"`
}

func deal(t *testing.T) bankFixture {
	t.Helper()
	var f bankFixture
	read(t, "bank/bank_fixed_deal.json", &f)
	return f
}
func closeNumber(t *testing.T, name string, a, b *float64, tolerance float64) {
	t.Helper()
	if a == nil || b == nil {
		if a != nil || b != nil {
			t.Errorf("%s nil mismatch %v %v", name, a, b)
		}
		return
	}
	if math.Abs(*a-*b) > tolerance {
		t.Errorf("%s=%.14g want %.14g", name, *a, *b)
	}
}
func compareChecks(t *testing.T, got, want []contracts.CheckResult) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("checks count %d want %d", len(got), len(want))
	}
	for i, c := range want {
		a := got[i]
		if a.Code != c.Code || a.Passed != c.Passed || !reflect.DeepEqual(a.Operator, c.Operator) {
			t.Errorf("check %+v want %+v", a, c)
		}
		tol := 1e-9
		if c.Code == "CASH_MIN" || c.Code == "NPV" {
			tol = .01
		}
		closeNumber(t, c.Code+" before", a.Before, c.Before, tol)
		closeNumber(t, c.Code+" after", a.After, c.After, tol)
		closeNumber(t, c.Code+" limit", a.Limit, c.Limit, tol)
		if a.Detail == "" {
			t.Error("empty check explanation")
		}
	}
}
func compareState(t *testing.T, got, want contracts.BankState) {
	t.Helper()
	a, b := reflect.ValueOf(got), reflect.ValueOf(want)
	for i := 0; i < a.NumField(); i++ {
		name := a.Type().Field(i).Name
		if a.Field(i).Kind() == reflect.Float64 {
			closeNumber(t, name, ptr(a.Field(i).Float()), ptr(b.Field(i).Float()), .01)
		} else if !reflect.DeepEqual(a.Field(i).Interface(), b.Field(i).Interface()) {
			t.Errorf("state %s=%v want %v", name, a.Field(i).Interface(), b.Field(i).Interface())
		}
	}
}
func compareEvent(t *testing.T, got, want contracts.AdminEvent) {
	t.Helper()
	compareChecks(t, got.Problems, want.Problems)
	got.Problems = nil
	want.Problems = nil
	if len(got.Reasons) != len(want.Reasons) {
		t.Errorf("reason count: %v vs %v", got.Reasons, want.Reasons)
	} else {
		for i, r := range want.Reasons {
			if got.Reasons[i].Code != r.Code || !reflect.DeepEqual(got.Reasons[i].Fields, r.Fields) {
				t.Errorf("reason %+v want %+v", got.Reasons[i], r)
			}
		}
	}
	got.Reasons = nil
	want.Reasons = nil
	if !reflect.DeepEqual(got, want) {
		t.Errorf("event payload differs: kind=%s id=%s", got.Kind, got.EventID)
	}
}
func checkError(t *testing.T, err error, status int) {
	t.Helper()
	var e *Error
	if !errors.As(err, &e) || e.Status != status {
		t.Fatalf("error %v want status %d", err, status)
	}
}

func TestAllBankFixtures(t *testing.T) {
	files, err := filepath.Glob("../../testdata/bank/*.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 7 {
		t.Fatal(len(files))
	}
	for _, path := range files {
		t.Run(filepath.Base(path), func(t *testing.T) {
			var f bankFixture
			read(t, "bank/"+filepath.Base(path), &f)
			calls := 0
			var event contracts.AdminEvent
			s := service(t, policy(t), f.Initial, notifierFunc(func(ctx context.Context, e contracts.AdminEvent) (contracts.EventAck, error) {
				calls++
				event = e
				return delivered(ctx, e)
			}))
			r, err := s.Check(context.Background(), f.Request)
			if err != nil {
				t.Fatal(err)
			}
			if r.ApplicationID != f.Expected.Response.ApplicationID || r.Verdict != f.Expected.Response.Verdict || !reflect.DeepEqual(r.LoanID, f.Expected.Response.LoanID) || r.EventID != f.Expected.Response.EventID || r.NotificationStatus != f.Expected.Response.NotificationStatus {
				t.Fatalf("response=%+v want %+v", r, f.Expected.Response)
			}
			compareChecks(t, r.Checks, f.Expected.Response.Checks)
			compareState(t, s.Snapshot().State, f.Expected.Saved)
			if calls != 1 {
				t.Fatal(calls)
			}
			compareEvent(t, event, f.Expected.Notification)
		})
	}
}

func TestDecisionMS2Operations(t *testing.T) {
	files, _ := filepath.Glob("../../testdata/decisions/*.json")
	processed := 0
	for _, path := range files {
		t.Run(filepath.Base(path), func(t *testing.T) {
			var f struct {
				Expected struct {
					Called       bool                           `json:"ms2_called"`
					Check        *contracts.BankCheckRequest    `json:"bank_check_request"`
					Manual       *contracts.ManualReviewRequest `json:"manual_review_request"`
					Response     json.RawMessage                `json:"bank_response"`
					Notification contracts.AdminEvent           `json:"notification"`
					Before       contracts.BankState            `json:"bank_state_before"`
					Saved        contracts.BankState            `json:"bank_state_saved"`
					Projected    *contracts.BankState           `json:"projected_bank_state"`
				} `json:"expected"`
			}
			read(t, "decisions/"+filepath.Base(path), &f)
			if !f.Expected.Called {
				return
			}
			processed++
			var event contracts.AdminEvent
			s := service(t, policy(t), f.Expected.Before, notifierFunc(func(ctx context.Context, e contracts.AdminEvent) (contracts.EventAck, error) {
				event = e
				return delivered(ctx, e)
			}))
			if f.Expected.Check != nil {
				var want contracts.BankCheckResponse
				if err := json.Unmarshal(f.Expected.Response, &want); err != nil {
					t.Fatal(err)
				}
				got, err := s.Check(context.Background(), *f.Expected.Check)
				if err != nil {
					t.Fatal(err)
				}
				compareChecks(t, got.Checks, want.Checks)
				got.Checks = nil
				want.Checks = nil
				if !reflect.DeepEqual(got, want) {
					t.Errorf("response %+v want %+v", got, want)
				}
				if f.Expected.Projected != nil {
					p, err := s.projector.Project(f.Expected.Before, f.Expected.Check.ApplicationID, f.Expected.Check.Borrower.ID, f.Expected.Check.Terms)
					if err != nil {
						t.Fatal(err)
					}
					compareState(t, p.State, *f.Expected.Projected)
				}
			} else {
				// Manual fixtures specify the event rather than bank_response.
				want := contracts.ManualReviewResponse{ApplicationID: f.Expected.Manual.ApplicationID, EventID: f.Expected.Notification.EventID, NotificationStatus: contracts.NotificationStatusDelivered}
				got, err := s.ManualReview(context.Background(), *f.Expected.Manual)
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(got, want) {
					t.Errorf("manual %+v want %+v", got, want)
				}
			}
			compareState(t, s.Snapshot().State, f.Expected.Saved)
			compareEvent(t, event, f.Expected.Notification)
		})
	}
	if processed != 12 {
		t.Fatalf("operations=%d", processed)
	}
}

func TestReplayCommitAndPendingNotification(t *testing.T) {
	f := deal(t)
	attempts := 0
	var events []contracts.AdminEvent
	var s *Service
	s = service(t, policy(t), f.Initial, notifierFunc(func(ctx context.Context, e contracts.AdminEvent) (contracts.EventAck, error) {
		attempts++
		if s.state.Cash != 7000000 || s.entries[f.Request.ApplicationID] == nil {
			t.Fatal("notification preceded commit")
		}
		events = append(events, mustCopy(t, e))
		if attempts == 1 {
			return contracts.EventAck{}, errors.New("delivery failure")
		}
		return delivered(ctx, e)
	}))
	first, err := s.Check(context.Background(), f.Request)
	if err != nil {
		t.Fatal(err)
	}
	if first.Verdict != contracts.BankVerdictIssued || first.NotificationStatus != contracts.NotificationStatusPending {
		t.Fatal(first)
	}
	pending, err := s.PendingEvents()
	if err != nil || len(pending) != 1 {
		t.Fatalf("%v %v", pending, err)
	}
	second, err := s.Check(context.Background(), f.Request)
	if err != nil || second.NotificationStatus != contracts.NotificationStatusDelivered {
		t.Fatalf("%+v %v", second, err)
	}
	third, err := s.Check(context.Background(), f.Request)
	if err != nil || !reflect.DeepEqual(second, third) {
		t.Fatal("replay changed response")
	}
	if attempts != 2 || !reflect.DeepEqual(events[0], events[1]) {
		t.Fatal("retry must deliver only the original event")
	}
	compareState(t, s.Snapshot().State, f.Expected.Saved)
	loans, err := s.Loans()
	if err != nil || len(loans) != 1 || loans[0].LoanID != *first.LoanID {
		t.Fatalf("%v %v", loans, err)
	}
	pending, err = s.PendingEvents()
	if err != nil || len(pending) != 0 {
		t.Fatal("delivered event remains pending")
	}
	// Bank-review replay must also use the original result and event.
	g := f.Request
	g.ApplicationID = "other"
	g.Terms.AmountRUB = 3000000
	review, err := s.Check(context.Background(), g)
	if err != nil || review.Verdict != contracts.BankVerdictBankReview {
		t.Fatalf("%+v %v", review, err)
	}
	count := attempts
	again, err := s.Check(context.Background(), g)
	if err != nil || !reflect.DeepEqual(review, again) || attempts != count {
		t.Fatal("review replay was recalculated or resent")
	}
}

func TestAckFailuresAndManualPending(t *testing.T) {
	for _, mode := range []string{"nil notifier", "error", "timeout", "wrong ID", "not accepted"} {
		t.Run(mode, func(t *testing.T) {
			f := deal(t)
			var n Notifier
			if mode != "nil notifier" {
				n = notifierFunc(func(ctx context.Context, e contracts.AdminEvent) (contracts.EventAck, error) {
					switch mode {
					case "error":
						return contracts.EventAck{}, errors.New("failed")
					case "timeout":
						return contracts.EventAck{}, context.DeadlineExceeded
					case "wrong ID":
						return contracts.EventAck{EventID: "other", Accepted: true}, nil
					default:
						return contracts.EventAck{EventID: e.EventID, Accepted: false}, nil
					}
				})
			}
			s := service(t, policy(t), f.Initial, n)
			r, err := s.Check(context.Background(), f.Request)
			if err != nil || r.Verdict != contracts.BankVerdictIssued || r.NotificationStatus != contracts.NotificationStatusPending {
				t.Fatalf("%+v %v", r, err)
			}
			compareState(t, s.Snapshot().State, f.Expected.Saved)
		})
	}
	f := deal(t)
	p := policy(t)
	p.Economics.UpfrontCostRUB = math.MaxFloat64
	s := service(t, p, f.Initial, nil)
	r := contracts.ManualReviewRequest{PolicyID: p.ID, ApplicationID: "manual", Borrower: f.Request.Borrower, RequestedTerms: f.Request.RequestedTerms, Score: f.Request.Score, Reasons: []contracts.Reason{{Code: "MANUAL", Message: "Нужна проверка.", Fields: []string{}}}}
	a, err := s.ManualReview(context.Background(), r)
	if err != nil || a.NotificationStatus != contracts.NotificationStatusPending {
		t.Fatalf("manual attempted financial calculations: %+v %v", a, err)
	}
	b, err := s.ManualReview(context.Background(), r)
	if err != nil || a != b {
		t.Fatal("manual replay")
	}
	compareState(t, s.Snapshot().State, f.Initial)
	pending, err := s.PendingEvents()
	if err != nil || len(pending) != 1 || pending[0].Selection != nil || pending[0].Terms != nil || len(pending[0].Problems) != 0 {
		t.Fatalf("manual event %+v %v", pending, err)
	}
}
