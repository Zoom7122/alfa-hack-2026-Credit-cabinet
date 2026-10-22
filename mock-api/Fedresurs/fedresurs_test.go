package fedresurs_test

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	fedresurs "github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/mock-api/Fedresurs"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/mock-api/internal/mockkit"
	mt "github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/mock-api/internal/mocktest"
)

var router = mt.Router("/fedresurs/v1", fedresurs.Register)

type summary struct {
	BankruptcyStage        string `json:"bankruptcy_stage"`
	HasBankruptcyIntention bool   `json:"has_bankruptcy_intention"`
	ActiveLeasingCount     int    `json:"active_leasing_count"`
	Liquidation            bool   `json:"liquidation"`
	Reorganization         bool   `json:"reorganization"`
}

type message struct {
	Type        string `json:"type"`
	PublishedAt string `json:"published_at"`
	Payload     struct {
		ContractNumber string `json:"contract_number"`
		PeriodTo       string `json:"period_to"`
		Stage          string `json:"stage"`
	} `json:"payload"`
}

type companyResp struct {
	INN      string    `json:"inn"`
	Summary  summary   `json:"summary"`
	Messages []message `json:"messages"`
}

func messages(t *testing.T, inn, query string) companyResp {
	t.Helper()
	return mt.Decode[companyResp](t, mt.Get(router, "/fedresurs/v1/companies/"+inn+"/messages"+query), http.StatusOK)
}

func TestCompanies(t *testing.T) {
	leasing := map[string]int{mt.InnA: 2, mt.InnB: 4, mt.InnC: 0, mt.InnD: 0, mt.InnE: 0}
	for inn, n := range leasing {
		r := messages(t, inn, "")
		if r.INN != inn || r.Summary.ActiveLeasingCount != n {
			t.Errorf("%s: %+v", inn, r.Summary)
		}
		if inn != mt.InnC && r.Summary.BankruptcyStage != "NONE" {
			t.Errorf("%s: stage %s", inn, r.Summary.BankruptcyStage)
		}
	}
	c := messages(t, mt.InnC, "")
	if c.Summary.BankruptcyStage != "PETITION_FILED" || !c.Summary.HasBankruptcyIntention {
		t.Errorf("C: %+v", c.Summary)
	}
	recent := messages(t, mt.InnC, "?from=2026-10-01")
	if len(recent.Messages) != 2 || recent.Summary != c.Summary {
		t.Errorf("C from 2026-10-01: %+v", recent)
	}
	for _, m := range recent.Messages {
		if m.PublishedAt < "2026-10-01" {
			t.Errorf("message %s published %s", m.Type, m.PublishedAt)
		}
	}
	if u := messages(t, mt.Unknown, ""); u.Messages == nil || len(u.Messages) != 0 || u.Summary.BankruptcyStage != "NONE" {
		t.Errorf("unknown: %+v", u)
	}
	mt.ErrorCode(t, mt.Get(router, "/fedresurs/v1/companies/1/messages"), http.StatusBadRequest)
	mt.ErrorCode(t, mt.Get(router, "/fedresurs/v1/companies/"+mt.InnA+"/messages?from=x"), http.StatusBadRequest)
}

func TestPersons(t *testing.T) {
	type person struct {
		INN             string  `json:"inn"`
		FullName        *string `json:"full_name"`
		BankruptcyStage string  `json:"bankruptcy_stage"`
	}
	for _, inn := range mt.Directors {
		r := mt.Decode[person](t, mt.Get(router, "/fedresurs/v1/persons/"+inn+"/bankruptcy"), http.StatusOK)
		if r.INN != inn || r.FullName == nil || r.BankruptcyStage != "NONE" {
			t.Errorf("%s: %+v", inn, r)
		}
	}
	u := mt.Decode[person](t, mt.Get(router, "/fedresurs/v1/persons/"+mt.Unknown+"99/bankruptcy"), http.StatusOK)
	if u.FullName != nil || u.BankruptcyStage != "NONE" {
		t.Errorf("unknown: %+v", u)
	}
	if code := mt.ErrorCode(t, mt.Get(router, "/fedresurs/v1/persons/1/bankruptcy"), http.StatusBadRequest); code != mockkit.CodeInvalidINN {
		t.Errorf("code %s", code)
	}
}

func TestSeedSummaries(t *testing.T) {
	files, _ := filepath.Glob("seed/companies/*.json")
	if len(files) == 0 {
		t.Fatal("no seeds")
	}
	for _, f := range files {
		body, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		var r companyResp
		if err := json.Unmarshal(body, &r); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		want := summary{BankruptcyStage: "NONE"}
		terminated := map[string]bool{}
		lastBankruptcy := ""
		for _, m := range r.Messages {
			switch m.Type {
			case "LEASING_TERMINATION":
				terminated[m.Payload.ContractNumber] = true
			case "BANKRUPTCY_INTENTION":
				want.HasBankruptcyIntention = true
			case "BANKRUPTCY":
				if m.PublishedAt > lastBankruptcy {
					lastBankruptcy, want.BankruptcyStage = m.PublishedAt, m.Payload.Stage
				}
			case "LIQUIDATION_DECISION":
				want.Liquidation = true
			case "REORGANIZATION":
				want.Reorganization = true
			}
		}
		for _, m := range r.Messages {
			if m.Type == "LEASING_CONTRACT" && m.Payload.PeriodTo >= mockkit.AsOf && !terminated[m.Payload.ContractNumber] {
				want.ActiveLeasingCount++
			}
		}
		if r.Summary != want {
			t.Errorf("%s: summary %+v, want %+v", f, r.Summary, want)
		}
	}
}
