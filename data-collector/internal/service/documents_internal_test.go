package service

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/domain"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/storage"
)

func staged(names ...string) []storage.Staged {
	out := make([]storage.Staged, 0, len(names))
	for _, n := range names {
		out = append(out, storage.Staged{Name: n, Path: "/tmp/" + n, Size: 10, SHA256: "sum"})
	}
	return out
}

func plan(files []storage.Staged, specs ...DocumentSpec) ([]plannedFile, []FieldError) {
	n := 0
	return planDocuments("app_1", "usr_1", files, specs, func() string { n++; return fmt.Sprintf("doc_%d", n) },
		time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC))
}

// describe — документы плана одной строкой: имя:тип:подписан_чем:состояние_подписи.
func describe(planned []plannedFile) []string {
	byID := map[string]string{}
	for _, p := range planned {
		byID[p.doc.ID] = p.doc.FileName
	}
	var out []string
	for _, p := range planned {
		out = append(out, fmt.Sprintf("%s:%s:%s:%s", p.doc.FileName, p.doc.Type, byID[p.doc.SignedDocumentID], p.doc.SignatureState))
	}
	return out
}

func TestPlanDocuments(t *testing.T) {
	tests := []struct {
		name  string
		files []string
		specs []DocumentSpec
		want  []string
	}{
		{"single file", []string{"balance.pdf"}, nil, []string{"balance.pdf:OTHER::NOT_SIGNED"}},
		{"detached signature by full name", []string{"balance.pdf.sig", "balance.pdf"}, nil,
			[]string{"balance.pdf:OTHER::PENDING", "balance.pdf.sig:SIGNATURE:balance.pdf:PENDING"}},
		{"detached signature by base name, case-insensitive", []string{"Balance.PDF", "balance.p7s"}, nil,
			[]string{"Balance.PDF:OTHER::PENDING", "balance.p7s:SIGNATURE:Balance.PDF:PENDING"}},
		{"signature without pair is attached", []string{"a.pdf", "b.pdf.sig"}, nil,
			[]string{"a.pdf:OTHER::NOT_SIGNED", "b.pdf.sig:OTHER::PENDING"}},
		{"type from spec", []string{"charter.pdf", "charter.pdf.sig"},
			[]DocumentSpec{{FileName: "Charter.pdf ", Type: domain.DocCharter}},
			[]string{"charter.pdf:CHARTER::PENDING", "charter.pdf.sig:SIGNATURE:charter.pdf:PENDING"}},
		{"path is cut from the name", []string{`C:\Users\me\scan.png`, "../../etc/report.xlsx"}, nil,
			[]string{"scan.png:OTHER::NOT_SIGNED", "report.xlsx:OTHER::NOT_SIGNED"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			planned, fields := plan(staged(tt.files...), tt.specs...)
			if len(fields) != 0 {
				t.Fatalf("fields = %+v", fields)
			}
			if got := describe(planned); !slices.Equal(got, tt.want) {
				t.Fatalf("documents = %v, want %v", got, tt.want)
			}
			for _, p := range planned {
				ext := strings.ToLower(p.doc.FileName[strings.LastIndex(p.doc.FileName, "."):])
				if p.doc.StorageKey != "app_1/"+p.doc.ID+ext || p.doc.UploadedBy != "usr_1" || p.doc.ApplicationID != "app_1" {
					t.Errorf("%s: storage key %s", p.doc.FileName, p.doc.StorageKey)
				}
			}
		})
	}
}

func TestPlanDocumentsStates(t *testing.T) {
	planned, fields := plan(staged("scan.jpg", "poa.xml", "contract.sig"))
	if len(fields) != 0 {
		t.Fatal(fields)
	}
	got := map[string]domain.Document{}
	for _, p := range planned {
		got[p.doc.FileName] = p.doc
	}
	if d := got["scan.jpg"]; d.MimeType != "image/jpeg" || d.RecognitionState != domain.RecognitionPending {
		t.Errorf("scan = %+v", d)
	}
	if d := got["poa.xml"]; d.MimeType != "application/xml" || d.RecognitionState != domain.RecognitionNotApplicable {
		t.Errorf("poa = %+v", d)
	}
	if d := got["contract.sig"]; d.MimeType != "application/pkcs7-signature" || d.SignatureState != domain.SignaturePending ||
		d.RecognitionState != domain.RecognitionNotApplicable || d.SignedDocumentID != "" {
		t.Errorf("attached signature = %+v", d)
	}
}

func TestPlanDocumentsErrors(t *testing.T) {
	empty := staged("empty.pdf")
	empty[0].Size = 0
	tests := []struct {
		name   string
		files  []storage.Staged
		specs  []DocumentSpec
		fields []string
		codes  []string
	}{
		{"too many", staged("1.pdf", "2.pdf", "3.pdf", "4.pdf", "5.pdf", "6.pdf", "7.pdf", "8.pdf", "9.pdf", "10.pdf", "11.pdf"), nil,
			[]string{"files"}, []string{"too_many"}},
		{"bad type and empty", append(staged("virus.exe"), empty...), nil,
			[]string{"files[0]", "files[1]"}, []string{"invalid_type", "empty"}},
		{"duplicate name", staged("a.pdf", "A.PDF"), nil, []string{"files[1]"}, []string{"duplicate"}},
		{"bad name", staged(".."), nil, []string{"files[0]"}, []string{"invalid_name"}},
		{"two signatures for one document", staged("a.pdf", "a.pdf.sig", "a.p7s"), nil, []string{"files[2]"}, []string{"second_signature"}},
		{"signature of a signature", staged("a.sig", "a.sig.sig"), nil, []string{"files[1]"}, []string{"signature_of_signature"}},
		{"ambiguous signature", staged("a.pdf", "a.xlsx", "a.sig"), nil, []string{"files[2]"}, []string{"ambiguous_signature"}},
		{"spec for missing file", staged("a.pdf"), []DocumentSpec{{FileName: "b.pdf", Type: domain.DocCharter}},
			[]string{"documents[0].file_name"}, []string{"unknown_file"}},
		{"spec with bad type", staged("a.pdf"), []DocumentSpec{{FileName: "a.pdf", Type: "PASSPORT"}},
			[]string{"documents[0].type"}, []string{"invalid_value"}},
		{"spec for detached signature", staged("a.pdf", "a.pdf.sig"), []DocumentSpec{{FileName: "a.pdf.sig", Type: domain.DocCharter}},
			[]string{"documents[0].file_name"}, []string{"signature_file"}},
		{"two specs for one file", staged("a.pdf"), []DocumentSpec{{FileName: "a.pdf", Type: domain.DocCharter},
			{FileName: "a.pdf", Type: domain.DocTaxReturn}}, []string{"documents[1].file_name"}, []string{"duplicate"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			planned, fields := plan(tt.files, tt.specs...)
			if planned != nil {
				t.Fatalf("planned = %v", describe(planned))
			}
			var gotFields, gotCodes []string
			for _, f := range fields {
				gotFields, gotCodes = append(gotFields, f.Field), append(gotCodes, f.Code)
			}
			if !slices.Equal(gotFields, tt.fields) || !slices.Equal(gotCodes, tt.codes) {
				t.Fatalf("fields = %v %v, want %v %v", gotFields, gotCodes, tt.fields, tt.codes)
			}
		})
	}
}

// Id документов возрастают в порядке файлов даже в пределах одной миллисекунды.
func TestMonotonicDocumentIDs(t *testing.T) {
	next := monotonicID("doc_")
	prev := next()
	for range 1000 {
		id := next()
		if id <= prev || !strings.HasPrefix(id, "doc_") {
			t.Fatalf("%s after %s", id, prev)
		}
		prev = id
	}
}
