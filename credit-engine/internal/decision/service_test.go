package decision

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"os"
	"testing"
	"time"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/contracts"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/clock"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/config"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/optimizer"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/scoring"
)

type failingBank struct {
	calls int
	err   error
}

func (b *failingBank) Check(context.Context, contracts.BankCheckRequest) (contracts.BankCheckResponse, error) {
	b.calls++
	return contracts.BankCheckResponse{}, b.err
}
func (b *failingBank) ManualReview(context.Context, contracts.ManualReviewRequest) (contracts.ManualReviewResponse, error) {
	b.calls++
	return contracts.ManualReviewResponse{}, b.err
}

func setup(t *testing.T, hugeCost bool) (*scoring.Evaluator, *optimizer.Selector) {
	t.Helper()
	p, err := config.LoadPolicy("../../config/policy.json")
	if err != nil {
		t.Fatal(err)
	}
	if hugeCost {
		p.Economics.UpfrontCostRUB = math.MaxFloat64 / 2
	}
	s, err := optimizer.New(p)
	if err != nil {
		t.Fatal(err)
	}
	e, err := scoring.NewEvaluator(nil, clock.Fixed{Time: time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	return e, s
}
func request(t *testing.T, file string) contracts.DecisionRequest {
	t.Helper()
	b, err := os.ReadFile("../../testdata/decisions/" + file)
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Request contracts.DecisionRequest `json:"request"`
	}
	if err = json.Unmarshal(b, &f); err != nil {
		t.Fatal(err)
	}
	return f.Request
}

func TestConstructorRequiresDependencies(t *testing.T) {
	e, s := setup(t, false)
	b := &failingBank{}
	for _, arg := range []struct {
		policy string
		e      *scoring.Evaluator
		s      *optimizer.Selector
		b      Bank
	}{{"", e, s, b}, {"p", nil, s, b}, {"p", e, nil, b}, {"p", e, s, nil}} {
		if _, err := New(arg.policy, arg.e, arg.s, arg.b); err == nil {
			t.Fatal("missing dependency accepted")
		}
	}
}

func TestFailuresReturnNoBusinessResult(t *testing.T) {
	for _, file := range []string{"uc04_ip_approved.json", "uc03_ip_manual.json"} {
		t.Run(file, func(t *testing.T) {
			e, s := setup(t, false)
			sentinel := errors.New("bank failure")
			b := &failingBank{err: sentinel}
			service, err := New("policy", e, s, b)
			if err != nil {
				t.Fatal(err)
			}
			out, err := service.Decide(context.Background(), request(t, file))
			if !errors.Is(err, sentinel) || out.Status != "" || b.calls != 1 {
				t.Fatalf("%+v %v calls=%d", out, err, b.calls)
			}
		})
	}
}

func TestCancellationAndArithmeticFailureBeforeBank(t *testing.T) {
	for _, mode := range []string{"cancelled", "arithmetic", "validation"} {
		t.Run(mode, func(t *testing.T) {
			e, s := setup(t, mode == "arithmetic")
			b := &failingBank{err: errors.New("must not call")}
			service, err := New("policy", e, s, b)
			if err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			if mode == "cancelled" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			r := request(t, "uc04_ip_approved.json")
			if mode == "validation" {
				r.ApplicationID = ""
			}
			out, err := service.Decide(ctx, r)
			if err == nil || out.Status != "" || b.calls != 0 {
				t.Fatalf("%+v %v calls=%d", out, err, b.calls)
			}
		})
	}
}
