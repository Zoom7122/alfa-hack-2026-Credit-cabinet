package financial

import "testing"

func TestCabinetApplicantContextFromRegistry(t *testing.T) {
	for _, tc := range []struct{ name, form, registry, want string }{
		{"registered OOO", `{}`, `{"full_name":"ООО «Ромашка»"}`, "OOO"},
		{"other legal form", `{}`, `{"full_name":"АО «Ромашка»"}`, ""},
		{"explicit context wins", `{"financial_document_context":{"legal_form":"AO"}}`, `{"full_name":"ООО «Ромашка»"}`, "AO"},
		{"unknown registry", `{}`, `{}`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			applicant, err := applicantContext("LEGAL_ENTITY", "7701234567", tc.form, tc.registry)
			if err != nil || applicant.LegalForm != tc.want {
				t.Fatalf("context: %+v %v", applicant, err)
			}
		})
	}
}
