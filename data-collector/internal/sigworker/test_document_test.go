package sigworker

import (
	"testing"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/domain"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/testdocument"
)

func TestPlanSkipsSampleAndItsSignatureOnly(t *testing.T) {
	docs := []domain.Document{
		{ID: "test", SHA256: testdocument.SHA256, MimeType: "application/pdf"},
		{ID: "test-sig", SignedDocumentID: "test", MimeType: signatureMime},
		{ID: "real", SHA256: "real-hash", MimeType: "application/pdf"},
		{ID: "real-sig", SignedDocumentID: "real", MimeType: signatureMime},
		{ID: "unsigned", MimeType: "application/pdf"},
	}
	units, candidates := plan(docs, map[string]bool{"test": true, "test-sig": true, "real": true, "real-sig": true, "unsigned": true})
	if len(units) != 1 || units[0].doc.ID != "real" || len(candidates) != 1 || candidates[0].ID != "unsigned" {
		t.Fatalf("test verification not skipped or real verification lost: %+v %+v", units, candidates)
	}
}
