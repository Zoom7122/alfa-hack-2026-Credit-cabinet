package clients_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/contracts"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/clients"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/modelbindings"
)

type generatedFactsRoundTrip func(*http.Request) (*http.Response, error)

func (f generatedFactsRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type generatedFactsTimeout struct{}

func (generatedFactsTimeout) Error() string { return "private timeout detail" }
func (generatedFactsTimeout) Timeout() bool { return true }

func TestGeneratedFactsClientProtocolCorpus(t *testing.T) {
	var corpus struct {
		Date  string `json:"evaluation_date"`
		Cases []struct {
			Name           string                        `json:"name"`
			Request        contracts.ScoringFactsRequest `json:"request"`
			Response       json.RawMessage               `json:"response"`
			Status         *int                          `json:"provider_status"`
			Classification string                        `json:"expected_classification"`
			HTTP           int                           `json:"expected_ms1_http_status"`
		} `json:"cases"`
	}
	data, err := os.ReadFile("../../contracts/examples/scoring_facts.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &corpus); err != nil {
		t.Fatal(err)
	}
	date, err := time.Parse("2006-01-02", corpus.Date)
	if err != nil {
		t.Fatal(err)
	}
	if len(corpus.Cases) != 27 {
		t.Fatal("changed protocol corpus")
	}
	catalog, err := modelbindings.NewDefault()
	if err != nil {
		t.Fatal(err)
	}
	for _, example := range corpus.Cases {
		t.Run(example.Name, func(t *testing.T) {
			entry, err := catalog.Lookup(example.Request.ModelVersion, example.Request.Borrower.Type)
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			transport := generatedFactsRoundTrip(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Method != "POST" || r.URL.Path != "/api/v1/scoring-facts" {
					t.Fatal("wrong request")
				}
				requestBody, err := io.ReadAll(r.Body)
				if err != nil {
					t.Fatal(err)
				}
				expected, err := json.Marshal(example.Request)
				if err != nil || !bytes.Equal(requestBody, expected) {
					t.Fatal("request mutated", err)
				}
				if example.Status == nil {
					if example.Classification == "DEPENDENCY_TIMEOUT" {
						return nil, generatedFactsTimeout{}
					}
					return nil, errors.New("private connection detail")
				}
				return &http.Response{StatusCode: *example.Status, Header: http.Header{}, Body: io.NopCloser(bytes.NewReader(example.Response))}, nil
			})
			client, err := clients.NewFacts("http://facts.invalid", &http.Client{Transport: transport, Timeout: time.Second})
			if err != nil {
				t.Fatal(err)
			}
			result, err := client.Fetch(context.Background(), entry, example.Request, date)
			if example.HTTP == 200 {
				if err != nil {
					t.Fatal(err)
				}
				value := result.Values["annual_contract_value_rub"]
				if example.Classification == "NEED_INFO" && value.Known() {
					t.Fatal("unknown became a number")
				}
				if example.Classification == "CONTINUE_SCORING" && !value.Known() {
					t.Fatal("known number lost")
				}
			} else {
				var failure *clients.Error
				if !errors.As(err, &failure) || failure.Status != example.HTTP || failure.Code != example.Classification {
					t.Fatalf("got %v want %d/%s", err, example.HTTP, example.Classification)
				}
			}
			if calls != 1 {
				t.Fatal("extra/missing provider requests", calls)
			}
		})
	}
}
