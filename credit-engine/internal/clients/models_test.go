package clients

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/modelbindings"
)

type metadataErrorReader struct{}

func (metadataErrorReader) Read([]byte) (int, error) { return 0, context.DeadlineExceeded }

func TestRunningModelsClientProtocolAndFailures(t *testing.T) {
	catalog, err := modelbindings.Default()
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(modelbindings.RuntimeMetadata{FormatVersion: 1, Models: catalog.List()})
	base := string(data)
	for _, tc := range []struct {
		name, body   string
		status, want int
	}{
		{"valid", base, 200, 0},
		{"redirect", base, 302, 502},
		{"unavailable", base, 503, 502},
		{"duplicate", strings.Replace(base, `"format_version":1`, `"format_version":1,"format_version":1`, 1), 200, 502},
		{"alias", strings.Replace(base, `"uses_facts"`, `"Uses_Facts"`, 1), 200, 502},
		{"null", strings.Replace(base, `"uses_facts":false`, `"uses_facts":null`, 1), 200, 502},
		{"missing", `{"format_version":1,"models":[]}`, 200, 502},
		{"bad hash", strings.Replace(base, catalog.List()[0].DefinitionSHA256, "bad", 1), 200, 502},
		{"large", strings.Repeat(" ", 1<<20) + base, 200, 502},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			client, err := NewModels("http://scoring.invalid/prefix", httpClient(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.URL.Path != "/prefix/api/v1/models" || r.Method != "GET" {
					t.Fatal(r.URL, r.Method)
				}
				return response(tc.status, tc.body), nil
			}))
			if err != nil {
				t.Fatal(err)
			}
			got, err := client.Models(context.Background())
			if calls != 1 {
				t.Fatal("unexpected retry")
			}
			if tc.want == 0 {
				if err != nil || len(got.Models) != 2 {
					t.Fatal(got, err)
				}
				return
			}
			var failure *Error
			if !errors.As(err, &failure) || failure.Status != tc.want {
				t.Fatal(err)
			}
		})
	}
	t.Run("deadline", func(t *testing.T) {
		client, _ := NewModels("http://scoring.invalid", &http.Client{Timeout: time.Second, Transport: roundTripper(func(r *http.Request) (*http.Response, error) { return nil, context.DeadlineExceeded })})
		_, err := client.Models(context.Background())
		var failure *Error
		if !errors.As(err, &failure) || failure.Status != 504 {
			t.Fatal(err)
		}
	})
	t.Run("body deadline", func(t *testing.T) {
		client, _ := NewModels("http://scoring.invalid", httpClient(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(metadataErrorReader{})}, nil
		}))
		_, err := client.Models(context.Background())
		var failure *Error
		if !errors.As(err, &failure) || failure.Status != 504 {
			t.Fatal(err)
		}
	})
}
