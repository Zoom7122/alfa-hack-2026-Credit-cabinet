package scoring

import (
	"testing"
	"time"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/model"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/rules"
)

func TestWithSupplierFactsPreservesInputAndDate(t *testing.T) {
	p := Prepared{Date: time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC), Input: rules.Input{Values: map[model.Source]rules.Value{}}}
	source := model.Source{Kind: model.Computed, Path: "prepared.supplier_facts_age_days"}
	for _, test := range []struct{ date, days string }{{"2026-10-20", "3"}, {"2026-10-23", "0"}, {"0001-01-01", "739911"}} {
		t.Run(test.date, func(t *testing.T) {
			got, err := WithSupplierFacts(p, map[string]rules.Value{"flag": rules.Boolean(false)}, test.date)
			if err != nil {
				t.Fatal(err)
			}
			days, known := got.Input.Values[source].Rational()
			if !known || days.RatString() != test.days {
				t.Fatal(days, known)
			}
			if len(p.Input.Values) != 0 {
				t.Fatal("caller was changed")
			}
			flag, known := got.Input.Values[model.Source{Kind: model.Supplier, Path: "flag"}].Bool()
			if !known || flag {
				t.Fatal("false became unknown")
			}
		})
	}
	for _, date := range []string{"", "2026-02-30", "2026-10-24"} {
		if _, err := WithSupplierFacts(p, nil, date); err == nil {
			t.Fatal("invalid date accepted", date)
		}
	}
	if _, err := WithSupplierFacts(Prepared{}, nil, "2026-10-20"); err == nil {
		t.Fatal("missing evaluation date")
	}
}
