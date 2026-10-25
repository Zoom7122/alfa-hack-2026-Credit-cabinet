package clients

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/contracts"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/bank"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/clock"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/config"
)

func TestRESTDeliveryWithRealBankOperations(t *testing.T) {
	for _, mode := range []string{"issued", "bank review", "manual"} {
		t.Run(mode, func(t *testing.T) {
			f := deal(t)
			p, err := config.LoadPolicy("../../config/policy.json")
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			var events []contracts.AdminEvent
			admin, err := NewAdmin("http://admin.invalid", httpClient(func(r *http.Request) (*http.Response, error) {
				calls++
				var e contracts.AdminEvent
				if err := json.NewDecoder(r.Body).Decode(&e); err != nil {
					t.Fatal(err)
				}
				events = append(events, e)
				if calls == 1 {
					return response(200, `{"accepted":true}`), nil
				}
				return response(202, jsonText(t, contracts.EventAck{EventID: e.EventID, Accepted: true})), nil
			}))
			if err != nil {
				t.Fatal(err)
			}
			initial := clone(t, f.Initial)
			if mode == "bank review" {
				initial.IssuanceEnabled = false
			}
			service, err := bank.NewService(p, initial, clock.Fixed{Time: time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)}, admin)
			if err != nil {
				t.Fatal(err)
			}
			bankHTTP := httpClient(func(r *http.Request) (*http.Response, error) {
				var output any
				var callErr error
				code := 200
				if r.URL.Path == "/api/v1/bank/check-and-issue" {
					var input contracts.BankCheckRequest
					if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
						t.Fatal(err)
					}
					output, callErr = service.Check(r.Context(), input)
				} else if r.URL.Path == "/api/v1/bank/manual-reviews" {
					var input contracts.ManualReviewRequest
					if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
						t.Fatal(err)
					}
					output, callErr = service.ManualReview(r.Context(), input)
					code = 202
				} else {
					t.Fatal(r.URL)
				}
				if callErr != nil {
					var failure *bank.Error
					if !errors.As(callErr, &failure) {
						t.Fatal(callErr)
					}
					return response(failure.Status, jsonText(t, contracts.Problem{Status: failure.Status, Code: failure.Code, Fields: failure.Fields})), nil
				}
				return response(code, jsonText(t, output)), nil
			})
			client, err := NewBank("http://bank.invalid", bankHTTP)
			if err != nil {
				t.Fatal(err)
			}
			invoke := func() contracts.NotificationStatus {
				if mode == "manual" {
					m := contracts.ManualReviewRequest{PolicyID: p.ID, ApplicationID: f.Request.ApplicationID, Borrower: f.Request.Borrower, RequestedTerms: f.Request.RequestedTerms, Score: f.Request.Score, Reasons: []contracts.Reason{{Code: "MANUAL", Message: "Нужно рассмотрение.", Fields: []string{}}}}
					r, err := client.ManualReview(context.Background(), m)
					if err != nil {
						t.Fatal(err)
					}
					return r.NotificationStatus
				}
				r, err := client.Check(context.Background(), f.Request)
				if err != nil {
					t.Fatal(err)
				}
				want := contracts.BankVerdictIssued
				if mode == "bank review" {
					want = contracts.BankVerdictBankReview
				}
				if r.Verdict != want {
					t.Fatal(r.Verdict)
				}
				return r.NotificationStatus
			}
			if invoke() != contracts.NotificationStatusPending {
				t.Fatal("HTTP 200 was accepted as event delivery")
			}
			firstState := service.Snapshot()
			if mode == "issued" && firstState.State.Cash != 7000000 || mode != "issued" && firstState.State.Cash != 10000000 {
				t.Fatal("incorrect commit before delivery")
			}
			if invoke() != contracts.NotificationStatusDelivered || invoke() != contracts.NotificationStatusDelivered {
				t.Fatal("delivery retry failed")
			}
			if calls != 2 || !reflect.DeepEqual(events[0], events[1]) {
				t.Fatal("event changed or delivered event was sent again")
			}
			if !reflect.DeepEqual(firstState, service.Snapshot()) {
				t.Fatal("notification retry changed bank state")
			}
			kind := contracts.EventTypeLoanIssued
			if mode == "bank review" {
				kind = contracts.EventTypeBankReview
			} else if mode == "manual" {
				kind = contracts.EventTypeBorrowerReview
			}
			if events[0].Kind != kind {
				t.Fatal("wrong event kind")
			}
			changed := f.Request
			changed.Terms.AmountRUB = 1
			_, err = client.Check(context.Background(), changed)
			status(t, err, 409)
			if calls != 2 {
				t.Fatal("conflict caused delivery")
			}
		})
	}
}
