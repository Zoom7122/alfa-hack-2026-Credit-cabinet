//go:build integration

// Запись в raw_collections против живого Postgres со схемой из модуля db.
//
//	DATABASE_URL=postgres://credit:credit@localhost:5432/credit_cabinet?sslmode=disable make test-integration
//
// Без DATABASE_URL тест пропускается.
package postgres

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/domain"
)

func TestIntegrationSave(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := Connect(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	id := "col_it_" + time.Now().UTC().Format("20060102150405.000000000")
	t.Cleanup(func() { // пул закрываем после удаления тестовой строки
		if _, err := pool.Exec(ctx, "DELETE FROM raw_collections WHERE id = $1", id); err != nil {
			t.Errorf("cleanup: %v", err)
		}
		pool.Close()
	})

	rc := domain.RawCollection{
		ID: id, QuestionnaireID: "qst_it",
		Input: domain.CollectInput{INN: "7799000012", OFDToken: "secret-ofd",
			Director: &domain.Person{INN: "779900010139", LastName: "Ковалёв", FirstName: "Игорь", BirthDate: "1984-07-19"}},
		Systems: []domain.SystemResult{
			{System: domain.SystemCoreBanking, State: domain.CollectDone, Data: json.RawMessage(`{"client":{"inn":"7799000012"}}`)},
			{System: domain.SystemCBR, State: domain.CollectDone, Data: json.RawMessage(`{"key_rate":"<soap:Envelope/>"}`)},
			{System: domain.SystemEGRN, State: domain.CollectSkipped, Reason: "нет недвижимости в залог"},
		},
		StartedAt: time.Now().UTC(), FinishedAt: time.Now().UTC(),
	}
	if err := NewRawCollectionRepository(pool).Save(ctx, rc); err != nil {
		t.Fatal(err)
	}

	var (
		inn, directorINN, clientINN, keyRate, firstState string
		egrnIsNull, ofdProvided                          bool
	)
	err = pool.QueryRow(ctx, `
		SELECT inn, director_inn, core_banking->'client'->>'inn', cbr->>'key_rate', sources->0->>'state',
		       egrn IS NULL, (request->>'ofd_token_provided')::boolean
		FROM raw_collections WHERE id = $1`, id).
		Scan(&inn, &directorINN, &clientINN, &keyRate, &firstState, &egrnIsNull, &ofdProvided)
	if err != nil {
		t.Fatal(err)
	}
	if inn != "7799000012" || directorINN != "779900010139" || clientINN != "7799000012" || keyRate != "<soap:Envelope/>" ||
		firstState != "DONE" || !egrnIsNull || !ofdProvided {
		t.Fatalf("row: inn=%s director=%s client=%s key_rate=%s state=%s egrn_null=%v ofd=%v",
			inn, directorINN, clientINN, keyRate, firstState, egrnIsNull, ofdProvided)
	}
}
