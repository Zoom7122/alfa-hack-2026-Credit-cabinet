package bank

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"testing"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/contracts"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/config"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/economics"
)

func manualFrom(r contracts.BankCheckRequest) contracts.ManualReviewRequest {
	return contracts.ManualReviewRequest{PolicyID: r.PolicyID, ApplicationID: r.ApplicationID, Borrower: r.Borrower, RequestedTerms: r.RequestedTerms, Score: r.Score, Reasons: []contracts.Reason{{Code: "MANUAL", Message: "Требуется рассмотрение.", Fields: []string{}}}}
}

func TestReplayConflictsAndDecodedDTOComparison(t *testing.T) {
	f := deal(t)
	count := 0
	s := service(t, policy(t), f.Initial, notifierFunc(func(ctx context.Context, e contracts.AdminEvent) (contracts.EventAck, error) {
		count++
		return delivered(ctx, e)
	}))
	first, err := s.Check(context.Background(), f.Request)
	if err != nil {
		t.Fatal(err)
	}
	// Reorder keys by decoding into a map, then encode with different whitespace.
	b, _ := json.Marshal(f.Request)
	var tree map[string]any
	_ = json.Unmarshal(b, &tree)
	b, _ = json.MarshalIndent(tree, "", "  ")
	var reordered contracts.BankCheckRequest
	if err = json.Unmarshal(b, &reordered); err != nil {
		t.Fatal(err)
	}
	again, err := s.Check(context.Background(), reordered)
	if err != nil || !reflect.DeepEqual(first, again) || count != 1 {
		t.Fatalf("decoded replay differs: %v", err)
	}
	for _, change := range []func(*contracts.BankCheckRequest){func(r *contracts.BankCheckRequest) { r.Terms.AmountRUB = 0 }, func(r *contracts.BankCheckRequest) { r.PolicyID = "other" }, func(r *contracts.BankCheckRequest) { r.Borrower.ID = "other" }, func(r *contracts.BankCheckRequest) { r.Score.Total-- }, func(r *contracts.BankCheckRequest) { r.Selection.MaxNPVRUB++ }} {
		r := mustCopy(t, f.Request)
		change(&r)
		_, err = s.Check(context.Background(), r)
		checkError(t, err, 409)
	}
	_, err = s.ManualReview(context.Background(), manualFrom(f.Request))
	checkError(t, err, 409)
	if count != 1 {
		t.Fatal("conflict notified")
	}
	compareState(t, s.Snapshot().State, f.Expected.Saved)
	s = service(t, policy(t), f.Initial, notifierFunc(delivered))
	m := manualFrom(f.Request)
	firstManual, err := s.ManualReview(context.Background(), m)
	if err != nil {
		t.Fatal(err)
	}
	againManual, err := s.ManualReview(context.Background(), m)
	if err != nil || firstManual != againManual {
		t.Fatal("manual replay differs")
	}
	_, err = s.Check(context.Background(), f.Request)
	checkError(t, err, 409)
	m.Reasons[0].Message = "Другая причина"
	_, err = s.ManualReview(context.Background(), m)
	checkError(t, err, 409)
	compareState(t, s.Snapshot().State, f.Initial)
	// A configured issued ID without a stored payload can never be issued again.
	bState := copyState(f.Initial)
	bState.IssuedApplicationIds = []string{f.Request.ApplicationID}
	s = service(t, policy(t), bState, nil)
	_, err = s.Check(context.Background(), f.Request)
	checkError(t, err, 409)
	_, err = s.ManualReview(context.Background(), manualFrom(f.Request))
	checkError(t, err, 409)
}

func TestConcurrentCheckOperations(t *testing.T) {
	for _, mode := range []string{"distinct requests", "same request", "pending same request"} {
		t.Run(mode, func(t *testing.T) {
			f := deal(t)
			attempts := 0
			s := service(t, policy(t), f.Initial, notifierFunc(func(ctx context.Context, e contracts.AdminEvent) (contracts.EventAck, error) {
				attempts++
				if mode == "pending same request" && attempts < 4 {
					return contracts.EventAck{}, errors.New("unavailable")
				}
				return delivered(ctx, e)
			}))
			type result struct {
				response contracts.BankCheckResponse
				err      error
			}
			results := make(chan result, 16)
			n := 16
			if mode == "distinct requests" {
				n = 2
			}
			for i := 0; i < n; i++ {
				r := mustCopy(t, f.Request)
				if mode == "distinct requests" {
					r.ApplicationID += string(rune('A' + i))
					r.Borrower.ID += string(rune('A' + i))
				}
				go func() { response, err := s.Check(context.Background(), r); results <- result{response, err} }()
			}
			issued, review := 0, 0
			for i := 0; i < n; i++ {
				r := <-results
				if r.err != nil {
					t.Fatal(r.err)
				}
				if r.response.Verdict == contracts.BankVerdictIssued {
					issued++
				} else if r.response.Verdict == contracts.BankVerdictBankReview {
					review++
				}
			}
			if mode == "distinct requests" {
				if issued != 1 || review != 1 || attempts != 2 {
					t.Fatalf("issued=%d review=%d notifications=%d", issued, review, attempts)
				}
			} else {
				wantAttempts := 1
				if mode == "pending same request" {
					wantAttempts = 4
				}
				if issued != 16 || review != 0 || attempts != wantAttempts {
					t.Fatalf("issued=%d review=%d notifications=%d", issued, review, attempts)
				}
			}
			if s.Snapshot().State.Cash != 7000000 {
				t.Fatal("funds deducted twice")
			}
			loans, err := s.Loans()
			if err != nil || len(loans) != 1 {
				t.Fatalf("loans=%v %v", loans, err)
			}
		})
	}
}

func TestSequentialUC06AndCleanBankSecondDeal(t *testing.T) {
	var first, second struct {
		Expected struct {
			Check contracts.BankCheckRequest `json:"bank_check_request"`
			Saved contracts.BankState        `json:"bank_state_saved"`
		} `json:"expected"`
	}
	read(t, "decisions/uc06_01_ip_issued.json", &first)
	read(t, "decisions/uc06_02_le_bank_review.json", &second)
	s := service(t, policy(t), initial(t), notifierFunc(delivered))
	a, err := s.Check(context.Background(), first.Expected.Check)
	if err != nil || a.Verdict != contracts.BankVerdictIssued {
		t.Fatalf("%+v %v", a, err)
	}
	b, err := s.Check(context.Background(), second.Expected.Check)
	if err != nil || b.Verdict != contracts.BankVerdictBankReview {
		t.Fatalf("%+v %v", b, err)
	}
	compareState(t, s.Snapshot().State, second.Expected.Saved)
	clean := service(t, policy(t), initial(t), nil)
	b, err = clean.Check(context.Background(), second.Expected.Check)
	if err != nil || b.Verdict != contracts.BankVerdictIssued {
		t.Fatalf("clean bank second deal: %+v %v", b, err)
	}
}

func TestManualNotificationRetry(t *testing.T) {
	f := deal(t)
	calls := 0
	var events []contracts.AdminEvent
	s := service(t, policy(t), f.Initial, notifierFunc(func(ctx context.Context, e contracts.AdminEvent) (contracts.EventAck, error) {
		calls++
		events = append(events, e)
		if calls == 1 {
			return contracts.EventAck{}, errors.New("unavailable")
		}
		return delivered(ctx, e)
	}))
	m := manualFrom(f.Request)
	a, err := s.ManualReview(context.Background(), m)
	if err != nil || a.NotificationStatus != contracts.NotificationStatusPending {
		t.Fatalf("%+v %v", a, err)
	}
	b, err := s.ManualReview(context.Background(), m)
	if err != nil || b.NotificationStatus != contracts.NotificationStatusDelivered {
		t.Fatalf("%+v %v", b, err)
	}
	_, err = s.ManualReview(context.Background(), m)
	if err != nil || calls != 2 || !reflect.DeepEqual(events[0], events[1]) {
		t.Fatal("manual repeat did not reuse event")
	}
	compareState(t, s.Snapshot().State, f.Initial)
}

func TestOwnedInputsSnapshotsAndNotifierIsolation(t *testing.T) {
	f := deal(t)
	p := policy(t)
	s := service(t, p, f.Initial, notifierFunc(func(ctx context.Context, e contracts.AdminEvent) (contracts.EventAck, error) {
		*e.Borrower.Name = "mutated by adapter"
		e.Score.Questionnaire.Criteria[0].Points = 0
		e.Terms.AmountRUB = 1
		e.Selection.MaxNPVRUB = 0
		return delivered(ctx, e)
	}))
	f.Initial.Groups["g1"] = 1
	p.NormLimits["N6"] = config.NormLimit{Operator: "<=", Value: 0}
	original := mustCopy(t, f.Request)
	r, err := s.Check(context.Background(), f.Request)
	if err != nil || r.Verdict != contracts.BankVerdictIssued {
		t.Fatalf("%+v %v", r, err)
	}
	*f.Request.Borrower.Name = "mutated input"
	f.Request.Score.Questionnaire.Criteria[0].Points = 0
	r.Checks[0].Passed = false
	*r.LoanID = "mutated response"
	snapshot := s.Snapshot()
	snapshot.State.Groups["g1"] = 1
	snapshot.State.IssuedApplicationIds[0] = "changed"
	snapshot.Norms["N6"] = 1
	loans, err := s.Loans()
	if err != nil {
		t.Fatal(err)
	}
	*loans[0].Borrower.Name = "mutated loan"
	again, err := s.Check(context.Background(), original)
	if err != nil {
		t.Fatal(err)
	}
	if *again.LoanID != "loan-"+original.ApplicationID || !again.Checks[0].Passed {
		t.Fatal("stored result changed")
	}
	compareState(t, s.Snapshot().State, f.Expected.Saved)
	loans, err = s.Loans()
	if err != nil || *loans[0].Borrower.Name != *original.Borrower.Name {
		t.Fatal("stored borrower changed")
	}
	s = service(t, policy(t), initial(t), nil)
	_, err = s.Check(context.Background(), original)
	if err != nil {
		t.Fatal(err)
	}
	pending, _ := s.PendingEvents()
	pending[0].Score.Questionnaire.Criteria[0].Points = 0
	*pending[0].Borrower.Name = "changed"
	againPending, _ := s.PendingEvents()
	if againPending[0].Score.Questionnaire.Criteria[0].Points != 30 || *againPending[0].Borrower.Name != *original.Borrower.Name {
		t.Fatal("pending events aliased")
	}
}

func TestInvalidRequestsAndTechnicalErrorsLeaveNoRecord(t *testing.T) {
	f := deal(t)
	for _, c := range []struct {
		name   string
		status int
		change func(*contracts.BankCheckRequest)
	}{
		{"missing policy", 400, func(r *contracts.BankCheckRequest) { r.PolicyID = "" }}, {"missing application", 400, func(r *contracts.BankCheckRequest) { r.ApplicationID = "" }}, {"missing borrower", 400, func(r *contracts.BankCheckRequest) { r.Borrower.ID = "" }}, {"missing type", 400, func(r *contracts.BankCheckRequest) { r.Borrower.Type = "" }},
		{"policy mismatch", 409, func(r *contracts.BankCheckRequest) { r.PolicyID = "other" }}, {"unknown type", 422, func(r *contracts.BankCheckRequest) { r.Borrower.Type = "UNKNOWN" }},
		{"borrower incomplete", 422, func(r *contracts.BankCheckRequest) { r.Borrower.INN = nil }}, {"request incomplete", 422, func(r *contracts.BankCheckRequest) { r.RequestedTerms.MaxAnnualRate = nil }},
		{"request contradiction", 422, func(r *contracts.BankCheckRequest) { r.RequestedTerms.MinAcceptableAmountRUB = ptr(int64(9000000)) }}, {"request rate precision", 422, func(r *contracts.BankCheckRequest) { r.RequestedTerms.MaxAnnualRate = ptr(24.001) }},
		{"amount zero", 422, func(r *contracts.BankCheckRequest) { r.Terms.AmountRUB = 0 }}, {"term invalid", 422, func(r *contracts.BankCheckRequest) { r.Terms.TermMonths = 37 }}, {"rate above 100", 422, func(r *contracts.BankCheckRequest) { r.Terms.AnnualRate = 100.01 }},
		{"rate precision", 422, func(r *contracts.BankCheckRequest) { r.Terms.AnnualRate = 23.751 }}, {"NaN audit", 422, func(r *contracts.BankCheckRequest) { r.Selection.MaxNPVRUB = math.NaN() }}, {"score invalid", 422, func(r *contracts.BankCheckRequest) { r.Score.RawTotal = -1 }}, {"grade invalid", 422, func(r *contracts.BankCheckRequest) { r.Score.Grade = "E" }},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := service(t, policy(t), f.Initial, nil)
			r := mustCopy(t, f.Request)
			c.change(&r)
			_, err := s.Check(context.Background(), r)
			checkError(t, err, c.status)
			compareState(t, s.Snapshot().State, f.Initial)
			if len(s.entries) != 0 {
				t.Fatal("invalid request recorded")
			}
		})
	}
	s := service(t, policy(t), f.Initial, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := s.Check(ctx, f.Request)
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	_, err = s.ManualReview(ctx, manualFrom(f.Request))
	if !errors.Is(err, context.Canceled) || len(s.entries) != 0 {
		t.Fatal("canceled operation committed")
	}
	for _, r := range []contracts.ManualReviewRequest{{}, {PolicyID: "other", ApplicationID: "a", Borrower: f.Request.Borrower}, manualFrom(f.Request)} {
		if r.ApplicationID == f.Request.ApplicationID {
			r.Reasons = nil
		}
		if _, err := s.ManualReview(context.Background(), r); err == nil {
			t.Fatal("bad manual request accepted")
		}
	}
	p := policy(t)
	p.Economics.UpfrontCostRUB = math.MaxFloat64
	s = service(t, p, f.Initial, nil)
	_, err = s.Check(context.Background(), f.Request)
	if !errors.Is(err, economics.ErrAmountOverflow) || len(s.entries) != 0 {
		t.Fatalf("arithmetic error recorded: %v", err)
	}
	compareState(t, s.Snapshot().State, f.Initial)
}

func TestIndependentNPVAndStartupValidation(t *testing.T) {
	var f bankFixture
	read(t, "bank/bank_negative_npv.json", &f)
	f.Request.Selection.SelectedNPVRUB = 999999999
	s := service(t, policy(t), f.Initial, nil)
	r, err := s.Check(context.Background(), f.Request)
	if err != nil || r.Verdict != contracts.BankVerdictBankReview || findCheck(t, r.Checks, "NPV").Passed {
		t.Fatalf("trusted claimed NPV: %+v %v", r, err)
	}
	b := initial(t)
	b.D2 = 0
	if _, err = NewService(policy(t), b, fixed, nil); err == nil {
		t.Fatal("invalid initial base accepted")
	}
	b = initial(t)
	b.D2 = math.SmallestNonzeroFloat64
	if _, err = NewService(policy(t), b, fixed, nil); err == nil {
		t.Fatal("nonfinite initial ratio accepted")
	}
	projector, err := NewProjector(policy(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = projector.Project(b, "invalid-initial", "borrower", f.Request.Terms); err == nil {
		t.Fatal("pure projection accepted a nonfinite initial norm")
	}
	if _, err = NewService(policy(t), initial(t), nil, nil); err == nil {
		t.Fatal("nil clock accepted")
	}
	p := policy(t)
	p.NormLimits = nil
	if _, err = NewService(p, initial(t), fixed, nil); err == nil {
		t.Fatal("invalid policy accepted")
	}
	b = initial(t)
	b.IssuanceEnabled = false
	b.Cash = 1
	if _, err = NewService(policy(t), b, fixed, nil); err != nil {
		t.Fatal("valid but already constrained bank rejected at startup")
	}
}

func TestPercentRatesAndSnapshotOfCommittedState(t *testing.T) {
	for _, rate := range []float64{0, 1, 100} {
		f := deal(t)
		f.Request.Terms.AnnualRate = rate
		f.Request.RequestedTerms.MaxAnnualRate = ptr(100.0)
		s := service(t, policy(t), f.Initial, nil)
		r, err := s.Check(context.Background(), f.Request)
		if err != nil {
			t.Fatal(err)
		}
		want := contracts.BankVerdictBankReview
		cash := 10000000.0
		if rate == 100 {
			want = contracts.BankVerdictIssued
			cash = 7000000
		}
		if r.Verdict != want || s.Snapshot().State.Cash != cash {
			t.Fatalf("%g percent: %+v", rate, r)
		}
	}
}
