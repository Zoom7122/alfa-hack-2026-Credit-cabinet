package domain_test

import (
	"maps"
	"slices"
	"testing"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/domain"
)

func TestSkippedSystems(t *testing.T) {
	consent := &domain.Consent{ID: "CNS-1", ValidUntil: "2027-04-21"}
	director := &domain.Person{INN: "779900010139", LastName: "Ковалёв", FirstName: "Игорь", BirthDate: "1984-07-19"}
	full := domain.CollectInput{
		INN:               "7799000012",
		Director:          director,
		BKICompanyConsent: consent,
		BKIPersonConsent:  consent,
		TaxConsent:        consent,
		OtherBankBIKs:     []string{"044525225"},
		MarketplaceTokens: map[string]string{"ozon": "t"},
		OFDToken:          "t",
		Collateral:        domain.Collateral{CadastralNumbers: []string{"50:21:0110203:845"}},
	}

	tests := []struct {
		name   string
		mutate func(*domain.CollectInput)
		want   []domain.System
	}{
		{"full input", func(*domain.CollectInput) {}, nil},
		{"only inn", func(in *domain.CollectInput) { *in = domain.CollectInput{INN: in.INN} }, []domain.System{
			domain.SystemBKI, domain.SystemTaxReports, domain.SystemEGRN, domain.SystemGosuslugi,
			domain.SystemOpenBanking, domain.SystemMarketplaces, domain.SystemOFD,
		}},
		// БКИ опрашивается, пока есть хотя бы один отчёт, на который есть согласие
		{"bki company consent only", func(in *domain.CollectInput) { in.BKIPersonConsent = nil }, nil},
		{"bki person consent only", func(in *domain.CollectInput) { in.BKICompanyConsent = nil }, nil},
		{"bki no consents", func(in *domain.CollectInput) { in.BKICompanyConsent, in.BKIPersonConsent = nil, nil },
			[]domain.System{domain.SystemBKI}},
		{"bki person consent without director", func(in *domain.CollectInput) { in.BKICompanyConsent, in.Director = nil, nil },
			[]domain.System{domain.SystemBKI, domain.SystemGosuslugi}},
		{"no collateral real estate", func(in *domain.CollectInput) {
			in.Collateral = domain.Collateral{VINs: []string{"XTC652005N1498213"}}
		}, []domain.System{domain.SystemEGRN}},
		{"no tokens", func(in *domain.CollectInput) { in.MarketplaceTokens, in.OFDToken = nil, "" },
			[]domain.System{domain.SystemMarketplaces, domain.SystemOFD}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := full
			tt.mutate(&in)
			skipped := domain.SkippedSystems(in)
			var got []domain.System
			for _, sys := range domain.Systems { // в порядке колонок
				if reason, ok := skipped[sys]; ok {
					if reason == "" {
						t.Errorf("%s: empty reason", sys)
					}
					got = append(got, sys)
				}
			}
			if !slices.Equal(got, tt.want) {
				t.Fatalf("skipped = %v, want %v (all: %v)", got, tt.want, slices.Collect(maps.Keys(skipped)))
			}
		})
	}
}
