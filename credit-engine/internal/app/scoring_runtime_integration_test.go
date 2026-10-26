//go:build integration

package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/httpapi"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/modelbindings"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/contracts"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/bank"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/clock"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/config"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/decision"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/optimizer"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/scoring"
)

type dropBankReply struct {
	bank    *bank.PersistentService
	dropped atomic.Bool
}

func (b *dropBankReply) Check(ctx context.Context, r contracts.BankCheckRequest) (contracts.BankCheckResponse, error) {
	out, err := b.bank.Check(ctx, r)
	if err == nil && !b.dropped.Swap(true) {
		return contracts.BankCheckResponse{}, errors.New("simulated lost response after bank commit")
	}
	return out, err
}
func (b *dropBankReply) ManualReview(ctx context.Context, r contracts.ManualReviewRequest) (contracts.ManualReviewResponse, error) {
	return b.bank.ManualReview(ctx, r)
}

type testNotifier struct {
	ok    atomic.Bool
	calls atomic.Int64
}

func (n *testNotifier) Notify(ctx context.Context, e contracts.AdminEvent) (contracts.EventAck, error) {
	n.calls.Add(1)
	return contracts.EventAck{EventID: e.EventID, Accepted: n.ok.Load()}, nil
}
func runtimeFixture(t *testing.T) (config.Policy, contracts.BankState, contracts.DecisionRequest) {
	t.Helper()
	p, err := config.LoadPolicy("../../config/policy.json")
	if err != nil {
		t.Fatal(err)
	}
	state, err := config.LoadBankState("../../config/bank.default.json")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile("../../testdata/decisions/uc04_ip_approved.json")
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Request contracts.DecisionRequest `json:"request"`
	}
	if err = json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	version := "ip-v2"
	f.Request.ModelVersion = &version
	f.Request.InputSnapshot = &contracts.InputSnapshot{ID: "snapshot", EvaluationDate: "2026-10-23"}
	return p, state, f.Request
}
func runtimeCore(t *testing.T, p config.Policy, b decision.Bank, date time.Time) *decision.Service {
	t.Helper()
	eval, err := scoring.NewEvaluator(nil, clock.Fixed{Time: date})
	if err != nil {
		t.Fatal(err)
	}
	selector, err := optimizer.New(p)
	if err != nil {
		t.Fatal(err)
	}
	s, err := decision.New(p.ID, eval, selector, b)
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func TestScoringRuntimeRecoveryAfterBankCommit(t *testing.T) {
	pool := scoringTestDB(t)
	ctx := context.Background()
	policy, state, request := runtimeFixture(t)
	fixed := clock.Fixed{Time: time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)}
	b, err := bank.NewPersistent(ctx, pool, policy, state, fixed, nil)
	if err != nil {
		t.Fatal(err)
	}
	drop := &dropBankReply{bank: b}
	s := &decision.Persistent{Pool: pool, Core: runtimeCore(t, policy, drop, fixed.Time)}
	if _, err = s.DecideWithKey(ctx, request, "round-1"); err == nil {
		t.Fatal("fault injection did not happen")
	}
	var prepared bool
	if err = pool.QueryRow(ctx, "SELECT prepared IS NOT NULL FROM credit_engine_runtime.scoring_runs WHERE idempotency_key='round-1'").Scan(&prepared); err != nil || !prepared {
		t.Fatal("bank called before prepared commit", err)
	}
	// A fresh process and a different clock must resume the prepared operation.
	b2, err := bank.NewPersistent(ctx, pool, policy, state, fixed, nil)
	if err != nil {
		t.Fatal(err)
	}
	s = &decision.Persistent{Pool: pool, Core: runtimeCore(t, policy, b2, fixed.Time.AddDate(1, 0, 0))}
	out, err := s.DecideWithKey(ctx, request, "round-1")
	if err != nil || out.Status != contracts.DecisionStatusApproved {
		t.Fatalf("recovery: %+v %v", out, err)
	}
	again, err := s.DecideWithKey(ctx, request, "round-1")
	if err != nil || !reflect.DeepEqual(out, again) {
		t.Fatal("cached response changed", err)
	}
	loans, err := b2.Loans()
	if err != nil || len(loans) != 1 {
		t.Fatal("duplicate issuance", loans, err)
	}
	if b2.Snapshot().State.Cash != state.Cash-float64(*request.CreditRequest.AmountRUB) {
		t.Fatal("bank effects duplicated")
	}
	changed := request
	changed.Borrower.Name = new(string)
	*changed.Borrower.Name = "different"
	if _, err = s.DecideWithKey(ctx, changed, "round-1"); err == nil {
		t.Fatal("conflicting key accepted")
	}
}
func TestScoringRuntimeConcurrentReplicasAndOutbox(t *testing.T) {
	pool := scoringTestDB(t)
	ctx := context.Background()
	policy, state, request := runtimeFixture(t)
	fixed := clock.Fixed{Time: time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)}
	notifier := &testNotifier{}
	b1, err := bank.NewPersistent(ctx, pool, policy, state, fixed, notifier)
	if err != nil {
		t.Fatal(err)
	}
	b2, err := bank.NewPersistent(ctx, pool, policy, state, fixed, notifier)
	if err != nil {
		t.Fatal(err)
	}
	s1 := &decision.Persistent{Pool: pool, Core: runtimeCore(t, policy, b1, fixed.Time)}
	s2 := &decision.Persistent{Pool: pool, Core: runtimeCore(t, policy, b2, fixed.Time)}
	var wg sync.WaitGroup
	for i := range 30 {
		wg.Go(func() {
			s := s1
			if i%2 == 1 {
				s = s2
			}
			_, err := s.DecideWithKey(ctx, request, "shared-round")
			var busy *scoring.Error
			if err != nil && (!errors.As(err, &busy) || busy.Code != "OPERATION_IN_PROGRESS") {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	out, err := s2.DecideWithKey(ctx, request, "shared-round")
	if err != nil || out.Status != contracts.DecisionStatusApproved {
		t.Fatal(out, err)
	}
	loans, err := b1.Loans()
	if err != nil || len(loans) != 1 {
		t.Fatal(loans, err)
	}
	if ok, err := b1.PublishNext(ctx); err != nil || !ok {
		t.Fatal(ok, err)
	}
	events, err := b2.PendingEvents()
	if err != nil || len(events) != 1 {
		t.Fatal("failed notification lost", events, err)
	}
	notifier.ok.Store(true)
	if _, err = pool.Exec(ctx, "UPDATE credit_engine_runtime.bank_outbox SET next_attempt_at=now()"); err != nil {
		t.Fatal(err)
	}
	if ok, err := b2.PublishNext(ctx); err != nil || !ok {
		t.Fatal(ok, err)
	}
	events, err = b1.PendingEvents()
	if err != nil || len(events) != 0 {
		t.Fatal(events, err)
	}
	if notifier.calls.Load() != 2 {
		t.Fatal("unexpected deliveries", notifier.calls.Load())
	}
	// A new round is allowed to change a review, but cannot issue a second loan.
	if _, err = s1.DecideWithKey(ctx, request, "different-round"); err == nil {
		t.Fatal("second round issued twice")
	}
}

// A new request resolves the active model, but recovery and replay must retain
// the metadata of the already prepared operation even when the model store is down.
type runtimeModelsWithOutage struct {
	modelbindings.Provider
	unavailable bool
}

func (p *runtimeModelsWithOutage) Resolve(ctx context.Context, version string, borrower contracts.BorrowerType) (*modelbindings.Entry, error) {
	if p.unavailable {
		return nil, errors.New("model store unavailable")
	}
	return p.Provider.Resolve(ctx, version, borrower)
}
func TestScoringRuntimePinsModelMetadataOnRecoveryAndReplay(t *testing.T) {
	pool := scoringTestDB(t)
	ctx := context.Background()
	policy, state, request := runtimeFixture(t)
	request.ModelVersion = nil
	fixed := clock.Fixed{Time: time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)}
	b, err := bank.NewPersistent(ctx, pool, policy, state, fixed, nil)
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := modelbindings.NewDefault()
	if err != nil {
		t.Fatal(err)
	}
	provider := &runtimeModelsWithOutage{Provider: catalog}
	evaluator, err := scoring.NewEvaluatorWithProvider(nil, nil, fixed, provider)
	if err != nil {
		t.Fatal(err)
	}
	selector, err := optimizer.New(policy)
	if err != nil {
		t.Fatal(err)
	}
	core, err := decision.New(policy.ID, evaluator, selector, &dropBankReply{bank: b})
	if err != nil {
		t.Fatal(err)
	}
	router := httpapi.NewScoringRouter(&decision.Persistent{Pool: pool, Core: core})
	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	call := func(key string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/decisions", bytes.NewReader(raw))
		req.Header.Set("Idempotency-Key", key)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}
	first := call("model-round")
	version, sha := first.Header().Get("X-Scoring-Model-Version"), first.Header().Get("X-Scoring-Model-SHA256")
	if first.Code < 500 || version != "ip-v2" || len(sha) != 64 {
		t.Fatalf("initial preparation: %d %s %s", first.Code, version, sha)
	}
	provider.unavailable = true
	recovered := call("model-round")
	cached := call("model-round")
	for _, r := range []*httptest.ResponseRecorder{recovered, cached} {
		if r.Code != 200 || r.Header().Get("X-Scoring-Model-Version") != version || r.Header().Get("X-Scoring-Model-SHA256") != sha {
			t.Fatalf("metadata changed on retry: %d %v %s", r.Code, r.Header(), r.Body.String())
		}
	}
	if recovered.Body.String() != cached.Body.String() {
		t.Fatal("replay response changed")
	}
	if r := call("new-model-round"); r.Code != 503 {
		t.Fatalf("new round bypassed model store: %d", r.Code)
	}
	loans, err := b.Loans()
	if err != nil || len(loans) != 1 {
		t.Fatal("duplicate loan", loans, err)
	}
}
