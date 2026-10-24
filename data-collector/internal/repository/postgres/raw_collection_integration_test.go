//go:build integration

// Запись в raw_collections, collection_tasks и outbox против живого Postgres со схемой из модуля db.
//
//	DATABASE_URL=postgres://credit:credit@localhost:5432/credit_cabinet?sslmode=disable make test-integration
//
// Без DATABASE_URL тесты пропускаются. Строки тестов уникальны и удаляются после теста;
// события пишутся в отдельный топик, поэтому запущенный publisher их не трогает.
package postgres

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/domain"
)

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not set")
	}
	pool, err := Connect(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// testSuffix — уникальная часть id и топиков теста.
func testSuffix() string { return "it_" + time.Now().UTC().Format("20060102150405.000000000") }

// cleanupTasks удаляет после теста результаты, события и сами задачи (до закрытия пула).
func cleanupTasks(t *testing.T, pool *pgxpool.Pool, topic string, taskIDs ...string) {
	t.Helper()
	t.Cleanup(func() {
		ctx := context.Background()
		if _, err := pool.Exec(ctx, "DELETE FROM outbox WHERE topic = $1", topic); err != nil {
			t.Errorf("cleanup outbox: %v", err)
		}
		for _, q := range []string{"DELETE FROM raw_collections WHERE task_id = ANY($1)", "DELETE FROM collection_tasks WHERE id = ANY($1)"} {
			if _, err := pool.Exec(ctx, q, taskIDs); err != nil {
				t.Errorf("cleanup: %v", err)
			}
		}
	})
}

func TestIntegrationComplete(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	suffix := testSuffix()
	taskID, topic := "tsk_"+suffix, "it.collection-tasks."+suffix
	repo := NewCollectionTaskRepository(pool, topic)
	cleanupTasks(t, pool, topic, taskID)

	in := domain.CollectInput{INN: "7799000012", OFDToken: "secret-ofd",
		Director: &domain.Person{INN: "779900010139", LastName: "Ковалёв", FirstName: "Игорь", BirthDate: "1984-07-19"}}
	if err := repo.Create(ctx, domain.CollectionTask{ID: taskID, QuestionnaireID: "qst_it", Input: in, CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	task, ok, err := repo.Claim(ctx, taskID, 3)
	if err != nil || !ok {
		t.Fatalf("claim: %v (ok %v)", err, ok)
	}

	rc := domain.RawCollection{
		ID: "col_" + suffix, TaskID: taskID, QuestionnaireID: "qst_it", Input: task.Input,
		Systems: []domain.SystemResult{
			{System: domain.SystemCoreBanking, State: domain.CollectDone, Data: json.RawMessage(`{"client":{"inn":"7799000012"}}`)},
			{System: domain.SystemCBR, State: domain.CollectDone, Data: json.RawMessage(`{"key_rate":"<soap:Envelope/>"}`)},
			{System: domain.SystemEGRN, State: domain.CollectSkipped, Reason: "нет недвижимости в залог"},
		},
		StartedAt: time.Now().UTC(), FinishedAt: time.Now().UTC(),
	}
	if err := repo.Complete(ctx, rc); err != nil {
		t.Fatal(err)
	}

	var (
		gotTaskID, inn, directorINN, clientINN, keyRate, firstState string
		egrnIsNull, ofdProvided                                     bool
	)
	err = pool.QueryRow(ctx, `
		SELECT task_id, inn, director_inn, core_banking->'client'->>'inn', cbr->>'key_rate', sources->0->>'state',
		       egrn IS NULL, (request->>'ofd_token_provided')::boolean
		FROM raw_collections WHERE id = $1`, rc.ID).
		Scan(&gotTaskID, &inn, &directorINN, &clientINN, &keyRate, &firstState, &egrnIsNull, &ofdProvided)
	if err != nil {
		t.Fatal(err)
	}
	if gotTaskID != taskID || inn != "7799000012" || directorINN != "779900010139" || clientINN != "7799000012" ||
		keyRate != "<soap:Envelope/>" || firstState != "DONE" || !egrnIsNull || !ofdProvided {
		t.Fatalf("row: task=%s inn=%s director=%s client=%s key_rate=%s state=%s egrn_null=%v ofd=%v",
			gotTaskID, inn, directorINN, clientINN, keyRate, firstState, egrnIsNull, ofdProvided)
	}
}
