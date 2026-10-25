package mockapi

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/domain"
)

func TestRegistryHeads(t *testing.T) {
	tests := []struct {
		name  string
		fns   string
		want  []domain.RegistryHead
		known bool
	}{
		{"head with INN from Transparent Business",
			`{"egrul":{"rows":[{"i":"7799000012","g":"ГЕНЕРАЛЬНЫЙ ДИРЕКТОР: Ковалёв Игорь Сергеевич","k":"ul"}]},
			  "pb":{"vyp":{"ИНН":"7799000012","masruk":[{"inn":"779900010139","name":"КОВАЛЁВ ИГОРЬ СЕРГЕЕВИЧ","position":"ГЕНЕРАЛЬНЫЙ ДИРЕКТОР","cnt":"1"}]}}}`,
			[]domain.RegistryHead{{INN: "779900010139", FullName: "КОВАЛЁВ ИГОРЬ СЕРГЕЕВИЧ", Position: "ГЕНЕРАЛЬНЫЙ ДИРЕКТОР"}}, true},
		{"only EGRUL: name without INN",
			`{"egrul":{"rows":[{"i":"7799000012","g":"ГЕНЕРАЛЬНЫЙ ДИРЕКТОР: Ковалёв Игорь Сергеевич","k":"ul"}]},"pb":null}`,
			[]domain.RegistryHead{{FullName: "Ковалёв Игорь Сергеевич", Position: "ГЕНЕРАЛЬНЫЙ ДИРЕКТОР"}}, true},
		{"sole proprietor from EGRIP",
			`{"egrul":{"rows":[{"n":"Индивидуальный предприниматель Смирнова Анна Викторовна","i":"779900010202","k":"fl"}]},
			  "pb":{"vyp":{"masruk":[]}}}`,
			[]domain.RegistryHead{{INN: "779900010202", FullName: "Смирнова Анна Викторовна", Position: "ИНДИВИДУАЛЬНЫЙ ПРЕДПРИНИМАТЕЛЬ"}}, true},
		{"company is not in EGRUL", `{"egrul":{"rows":[]}}`, nil, true},
		{"FNS did not answer", `{"egrul":null,"pb":null}`, nil, false},
		{"no FNS data", ``, nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, known := RegistryHeads(json.RawMessage(tt.fns))
			if known != tt.known || !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("heads = %+v (known %v), want %+v (known %v)", got, known, tt.want, tt.known)
			}
		})
	}
}

// Поиск по компании и МЧД по номеру из анкеты — без повторов; формат — как в сидах mock-api.
func TestPowersOfAttorney(t *testing.T) {
	active := `{"number":"0f5a2c1e-7b3d-4e8a-9c21-6d4b8e0a1f37","format_version":"5.02","status":"ACTIVE",
		"valid_from":"2026-01-15","valid_until":"2027-01-14","revoked_at":null,
		"principal":{"type":"LEGAL","inn":"7799000012","signed_by":{"inn":"779900010139"}},
		"representative":{"type":"PERSON","inn":"779900010333","snils":"148-562-907 41","full_name":"Литвинова Марина Олеговна"},
		"powers":[{"code":"LOAN_APPLICATION_SUBMIT","text":"…"},{"code":"FINANCIAL_DOCS_SIGN","text":"…"}]}`
	revoked := `{"number":"a71b4e02-9d3c-4c88-b5f0-81e6c2d3a945","status":"REVOKED","valid_from":"2025-11-03",
		"valid_until":"2026-11-02","revoked_at":"2026-05-12","principal":{"inn":"7799000012"},
		"representative":{"inn":"779900010417"},"powers":[{"code":"LOAN_APPLICATION_SUBMIT"}]}`

	got, known := PowersOfAttorney(json.RawMessage(`{"search":{"items":[` + active + `,` + revoked + `]},"poa":` + active + `}`))
	want := []domain.PowerOfAttorney{
		{Number: "0f5a2c1e-7b3d-4e8a-9c21-6d4b8e0a1f37", Status: "ACTIVE", ValidFrom: "2026-01-15", ValidUntil: "2027-01-14",
			PrincipalINN: "7799000012", RepresentativeINN: "779900010333", RepresentativeSNILS: "148-562-907 41",
			RepresentativeName: "Литвинова Марина Олеговна", Powers: []string{"LOAN_APPLICATION_SUBMIT", "FINANCIAL_DOCS_SIGN"}},
		{Number: "a71b4e02-9d3c-4c88-b5f0-81e6c2d3a945", Status: "REVOKED", ValidFrom: "2025-11-03", ValidUntil: "2026-11-02",
			RevokedAt: "2026-05-12", PrincipalINN: "7799000012", RepresentativeINN: "779900010417", Powers: []string{"LOAN_APPLICATION_SUBMIT"}},
	}
	if !known || !reflect.DeepEqual(got, want) {
		t.Fatalf("poas = %+v (known %v)", got, known)
	}

	if got, known := PowersOfAttorney(json.RawMessage(`{"search":{"items":[]},"poa":null}`)); !known || len(got) != 0 {
		t.Fatalf("empty registry answer: %+v (known %v)", got, known)
	}
	for _, raw := range []string{``, `{"search":null,"poa":null}`, `{`} {
		if _, known := PowersOfAttorney(json.RawMessage(raw)); known {
			t.Errorf("PowersOfAttorney(%q) must be unknown", raw)
		}
	}
}
