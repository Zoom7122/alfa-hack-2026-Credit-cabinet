//go:build integration

package adminstore_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/contracts"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/adminstore"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/bank"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/clients"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/clock"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/config"
)

const eventToken = "fictional-ms2-service-token-1234567890"

type eventRoundTripper func(*http.Request) (*http.Response, error)

func (f eventRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type eventFixtureData struct {
	Expected struct {
		Event       *contracts.AdminEvent       `json:"notification"`
		BankRequest *contracts.BankCheckRequest `json:"bank_check_request"`
		Before      contracts.BankState         `json:"bank_state_before"`
	} `json:"expected"`
}

func loadEventFixture(t *testing.T, name string) eventFixtureData {
	t.Helper()
	data, err := os.ReadFile("../../testdata/decisions/" + name + ".json")
	if err != nil {
		t.Fatal(err)
	}
	var f eventFixtureData
	if err := json.Unmarshal(data, &f); err != nil || f.Expected.Event == nil {
		t.Fatal("missing fixture", err)
	}
	return f
}
func serviceEvent(handler http.Handler, event contracts.AdminEvent) *httptest.ResponseRecorder {
	data, err := json.Marshal(event)
	if err != nil {
		panic(err)
	}
	request := httptest.NewRequest("POST", "http://admin.invalid/api/v1/credit-events", bytes.NewReader(data))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+eventToken)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, request)
	return w
}
func eventIdentity(event contracts.AdminEvent, application string) contracts.AdminEvent {
	event.ApplicationID = application
	event.EventID = application + ":" + string(event.Kind)
	return event
}

func TestEventsHTTPPersistenceDuplicateConflictAndFilters(t *testing.T) {
	repo := repositoryFixture(t)
	h := adminHTTPFixture(t, repo)
	ctx := context.Background()
	var accepted []contracts.AdminEvent
	for i, name := range []string{"uc04_ip_approved", "uc03_le_manual", "uc05_ip_bank_review"} {
		event := *loadEventFixture(t, name).Expected.Event
		stamp := time.Date(2026, 10, 23+i, 12, 0, 0, 0, time.UTC)
		event.OccurredAt = stamp.Format(time.RFC3339Nano)
		if i == 0 {
			event = eventIdentity(event, "opaque/+%? space")
			b := false
			event.Score.Model = "ip-bool-v3-test"
			event.Score.Economics.Criteria[0].ID = "new_contract_flag"
			event.Score.Economics.Criteria[0].Value = nil
			event.Score.Economics.Criteria[0].BooleanValue = &b
		}
		accepted = append(accepted, event)
		w := serviceEvent(h.handler, event)
		adminWant(t, w, 202)
		ack := adminRead[contracts.EventAck](t, w)
		if !ack.Accepted || ack.EventID != event.EventID {
			t.Fatal("wrong acknowledgement")
		}
		stored, err := repo.GetEvent(ctx, event.EventID)
		if err != nil {
			t.Fatal(err)
		}
		again := serviceEvent(h.handler, event)
		adminWant(t, again, 202)
		repeated, err := repo.GetEvent(ctx, event.EventID)
		if err != nil || !repeated.ReceivedAt.Equal(stored.ReceivedAt) || repeated.PayloadSHA256 != stored.PayloadSHA256 {
			t.Fatal("duplicate replaced first receipt")
		}
		changed := event
		changed.Score.Total--
		adminWant(t, serviceEvent(h.handler, changed), 409)
	}
	w := h.send("GET", "/api/admin/v1/events", nil)
	adminWant(t, w, 200)
	list := adminRead[[]adminstore.EventSummary](t, w)
	if len(list) != 3 || list[0].Kind != contracts.EventTypeBankReview || list[2].EventID != accepted[0].EventID {
		t.Fatal("wrong chronological list")
	}
	if bytes.Contains(w.Body.Bytes(), []byte(`"payload"`)) {
		t.Fatal("list includes full payload")
	}
	w = h.send("GET", "/api/admin/v1/events/"+url.PathEscape(accepted[0].EventID), nil)
	adminWant(t, w, 200)
	stored := adminRead[adminstore.StoredEvent](t, w)
	criterion := stored.Payload.Score.Economics.Criteria[0]
	if stored.EventID != accepted[0].EventID || criterion.BooleanValue == nil || *criterion.BooleanValue || criterion.Value != nil {
		t.Fatal("opaque ID or false changed")
	}
	for _, query := range []string{"kind=BORROWER_REVIEW", "borrower_type=LEGAL_ENTITY", "application_id=" + url.QueryEscape(accepted[0].ApplicationID), "borrower_id=" + url.QueryEscape(accepted[0].Borrower.ID), "model_version=ip-bool-v3-test", "from=2026-10-24T12%3A00%3A00Z&to=2026-10-24T12%3A00%3A00Z", "limit=1&offset=1"} {
		w = h.send("GET", "/api/admin/v1/events?"+query, nil)
		adminWant(t, w, 200)
		if len(adminRead[[]adminstore.EventSummary](t, w)) != 1 {
			t.Fatal("filter failed", query)
		}
	}
	for _, query := range []string{"kind=UNKNOWN", "borrower_type=UNKNOWN", "from=2026-10-25", "from=2026-10-25T00:00:00Z&to=2026-10-24T00:00:00Z", "limit=1&limit=2", "limit=201", "unknown=x"} {
		adminWant(t, h.send("GET", "/api/admin/v1/events?"+query, nil), 400)
	}
	adminWant(t, h.send("POST", "/api/v1/credit-events", accepted[0]), 401)
	adminWant(t, h.send("GET", "/api/admin/v1/events/absent", nil), 404)
	adminWant(t, h.send("DELETE", "/api/admin/v1/events/"+url.PathEscape(accepted[0].EventID), nil), 405)
}

func TestEventsConcurrentIdenticalAndChangedPayloads(t *testing.T) {
	repo := repositoryFixture(t)
	h := adminHTTPFixture(t, repo)
	event := eventIdentity(*loadEventFixture(t, "uc04_ip_approved").Expected.Event, "concurrent-identical")
	var wg sync.WaitGroup
	results := make(chan *httptest.ResponseRecorder, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); results <- serviceEvent(h.handler, event) }()
	}
	wg.Wait()
	close(results)
	for w := range results {
		adminWant(t, w, 202)
	}
	list, err := repo.ListEvents(context.Background(), adminstore.EventFilter{})
	if err != nil || len(list) != 1 {
		t.Fatal("duplicate event rows", len(list), err)
	}
	left := eventIdentity(event, "concurrent-different")
	right := left
	right.Score.Total--
	results = make(chan *httptest.ResponseRecorder, 2)
	for _, candidate := range []contracts.AdminEvent{left, right} {
		wg.Add(1)
		go func(e contracts.AdminEvent) { defer wg.Done(); results <- serviceEvent(h.handler, e) }(candidate)
	}
	wg.Wait()
	close(results)
	accepted, conflicts := 0, 0
	for w := range results {
		switch w.Code {
		case 202:
			accepted++
		case 409:
			conflicts++
		default:
			adminWant(t, w, 202)
		}
	}
	if accepted != 1 || conflicts != 1 {
		t.Fatal("concurrent overwrite", accepted, conflicts)
	}
	list, err = repo.ListEvents(context.Background(), adminstore.EventFilter{})
	if err != nil || len(list) != 2 {
		t.Fatal("incorrect row count", len(list), err)
	}
}

func TestEventInsertAndCommitFailureNeverAcknowledges(t *testing.T) {
	db := isolatedDatabase(t)
	migrator, _ := adminstore.NewMigrator(db)
	_, err := migrator.Up(context.Background())
	migrationOK(t, err)
	repo, _ := adminstore.NewRepository(db)
	h := adminHTTPFixture(t, repo)
	dbExec(t, db, `CREATE FUNCTION credit_engine_admin.fail_event_test() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'fictional database password=do-not-expose'; END; $$`)
	dbExec(t, db, `CREATE TRIGGER fail_event_insert BEFORE INSERT ON credit_engine_admin.credit_events FOR EACH ROW WHEN (NEW.application_id='fail-insert') EXECUTE FUNCTION credit_engine_admin.fail_event_test()`)
	dbExec(t, db, `CREATE CONSTRAINT TRIGGER fail_event_commit AFTER INSERT ON credit_engine_admin.credit_events DEFERRABLE INITIALLY DEFERRED FOR EACH ROW WHEN (NEW.application_id='fail-commit') EXECUTE FUNCTION credit_engine_admin.fail_event_test()`)
	for _, application := range []string{"fail-insert", "fail-commit"} {
		event := eventIdentity(*loadEventFixture(t, "uc04_ip_approved").Expected.Event, application)
		w := serviceEvent(h.handler, event)
		adminWant(t, w, 503)
		if strings.Contains(w.Body.String(), "do-not-expose") || strings.Contains(w.Body.String(), `"accepted":true`) {
			t.Fatal("failed write acknowledged or cause exposed")
		}
		if countRows(t, db, "SELECT count(*) FROM credit_engine_admin.credit_events WHERE event_id=$1", event.EventID) != 0 {
			t.Fatal("failed transaction persisted")
		}
	}
	dbExec(t, db, "DROP TRIGGER fail_event_insert ON credit_engine_admin.credit_events")
	dbExec(t, db, "DROP TRIGGER fail_event_commit ON credit_engine_admin.credit_events")
	for _, application := range []string{"fail-insert", "fail-commit"} {
		event := eventIdentity(*loadEventFixture(t, "uc04_ip_approved").Expected.Event, application)
		adminWant(t, serviceEvent(h.handler, event), 202)
	}
}

func TestLostAcknowledgementReplaysMC2WithoutIssuingTwice(t *testing.T) {
	repo := repositoryFixture(t)
	h := adminHTTPFixture(t, repo)
	f := loadEventFixture(t, "uc04_ip_approved")
	request := *f.Expected.BankRequest
	flag := false
	request.Score.Model = "ip-bool-v3-test"
	request.Score.Economics.Criteria[0].ID = "new_contract_flag"
	request.Score.Economics.Criteria[0].Value = nil
	request.Score.Economics.Criteria[0].BooleanValue = &flag
	calls := 0
	wire := [][]byte{}
	client, err := clients.NewAdminWithToken("http://admin.invalid", eventToken, &http.Client{Timeout: 5 * time.Second, Transport: eventRoundTripper(func(request *http.Request) (*http.Response, error) {
		calls++
		data, err := io.ReadAll(request.Body)
		if err != nil {
			return nil, err
		}
		wire = append(wire, append([]byte{}, data...))
		request.Body = io.NopCloser(bytes.NewReader(data))
		w := httptest.NewRecorder()
		h.handler.ServeHTTP(w, request)
		if w.Code != 202 {
			return w.Result(), nil
		}
		if calls == 1 {
			return nil, errors.New("simulated lost ACK after commit")
		}
		return w.Result(), nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	policy, err := config.LoadPolicy("../../config/policy.json")
	if err != nil {
		t.Fatal(err)
	}
	stamp, err := time.Parse(time.RFC3339, f.Expected.Event.OccurredAt)
	if err != nil {
		t.Fatal(err)
	}
	service, err := bank.NewService(policy, f.Expected.Before, clock.Fixed{Time: stamp}, client)
	if err != nil {
		t.Fatal(err)
	}
	first, err := service.Check(context.Background(), request)
	if err != nil || first.Verdict != contracts.BankVerdictIssued || first.NotificationStatus != contracts.NotificationStatusPending {
		t.Fatal("issuance/first pending status", first, err)
	}
	state := service.Snapshot().State
	stored, err := repo.GetEvent(context.Background(), first.EventID)
	if err != nil {
		t.Fatal("event not committed before lost ACK", err)
	}
	second, err := service.Check(context.Background(), request)
	if err != nil || second.NotificationStatus != contracts.NotificationStatusDelivered || second.LoanID == nil || first.LoanID == nil || *second.LoanID != *first.LoanID {
		t.Fatal(second, err)
	}
	if !reflect.DeepEqual(state, service.Snapshot().State) || calls != 2 || len(wire) != 2 || !bytes.Equal(wire[0], wire[1]) {
		t.Fatal("replay changed issuance or event")
	}
	repeated, err := repo.GetEvent(context.Background(), first.EventID)
	if err != nil || !stored.ReceivedAt.Equal(repeated.ReceivedAt) || repeated.Payload.Score.Economics.Criteria[0].BooleanValue == nil || *repeated.Payload.Score.Economics.Criteria[0].BooleanValue {
		t.Fatal("replay lost first receipt/false", err)
	}
	list, err := repo.ListEvents(context.Background(), adminstore.EventFilter{})
	if err != nil || len(list) != 1 {
		t.Fatal("lost ACK created duplicate", err)
	}
	third, err := service.Check(context.Background(), request)
	if err != nil || third.NotificationStatus != contracts.NotificationStatusDelivered || calls != 2 {
		t.Fatal("acknowledged event resent", err)
	}
}
