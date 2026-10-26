package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/domain"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/storage"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/testdocument"
)

// Only untouched snapshots from the old seeder are eligible. In particular, never
// restart a real collection, an answered question, an accepted offer or staff work.
func (r *ApplicationRepository) enqueueLegacyTestApplication(ctx context.Context, tx pgx.Tx, seed testApplication, id string, now time.Time, files ...*storage.Local) error {
	if seed.Status == domain.ApplicationDraft || seed.Status == domain.ApplicationSubmitted {
		return nil
	}
	var form []byte
	var creator string
	expectedCreator := testCompanies[seed.Company].DirectorID
	if seed.CreatorID != "" {
		expectedCreator = seed.CreatorID
	}
	err := tx.QueryRow(ctx, `SELECT a.form, a.created_by FROM applications a JOIN companies c ON c.id=a.company_id
 WHERE a.id=$1 AND a.status=$2 AND c.inn=$3 AND a.form #>> '{questionnaire,id}'=$4 AND a.created_by=$9
 AND coalesce(a.assignee_id,'')=$5
 AND EXISTS (SELECT 1 FROM decisions d WHERE d.application_id=a.id AND d.id=$6 AND d.details->>'test_data'='true')
 AND NOT EXISTS (SELECT 1 FROM decisions d WHERE d.application_id=a.id AND d.id<>$6)
 AND NOT EXISTS (SELECT 1 FROM collection_tasks t WHERE t.application_id=a.id)
 AND NOT EXISTS (SELECT 1 FROM documents d WHERE d.application_id=a.id)
 AND NOT EXISTS (SELECT 1 FROM signings s WHERE s.application_id=a.id)
 AND NOT EXISTS (SELECT 1 FROM info_requests q WHERE q.application_id=a.id AND (q.status<>'OPEN' OR q.id<>ALL($7::text[])))
 AND NOT EXISTS (SELECT 1 FROM offers o WHERE o.application_id=a.id AND (o.status<>'ACTIVE' OR o.id<>$8))
 AND NOT EXISTS (SELECT 1 FROM application_events e WHERE e.application_id=a.id
                 AND e.type NOT IN ('application.created','application.submitted','decision.ready'))
 FOR UPDATE OF a`, id, string(seed.Status), testCompanies[seed.Company].INN, strings.Replace(id, "app_", "qst_", 1),
		seed.AssigneeID, strings.Replace(id, "app_", "dcs_", 1),
		[]string{"ir_" + strings.TrimPrefix(id, "app_") + "_0", "ir_" + strings.TrimPrefix(id, "app_") + "_1"},
		strings.Replace(id, "app_", "ofr_", 1), expectedCreator).Scan(&form, &creator)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("select legacy test application: %w", err)
	}
	for _, query := range []string{
		`DELETE FROM offers WHERE application_id=$1`,
		`DELETE FROM info_requests WHERE application_id=$1`,
		`DELETE FROM decisions WHERE application_id=$1`,
	} {
		if _, err := tx.Exec(ctx, query, id); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE applications SET status='SUBMITTED', deadline_at=NULL, version=version+1, updated_at=$2 WHERE id=$1`, id, now); err != nil {
		return err
	}
	if err := insertEvent(ctx, tx, appEvent{ApplicationID: id, Type: "test_data.recalculation_requested", From: seed.Status,
		To: domain.ApplicationSubmitted, Message: "Тестовая заявка отправлена на сбор данных и расчёт скоринга"}); err != nil {
		return err
	}
	return r.enqueueTestApplication(ctx, tx, id, creator, form, now, files...)
}

func (r *ApplicationRepository) enqueueTestApplication(ctx context.Context, tx pgx.Tx, id, creator string, form []byte, now time.Time, files ...*storage.Local) error {
	var record formRecord
	if err := json.Unmarshal(form, &record); err != nil {
		return err
	}
	task := domain.CollectionTask{ID: strings.Replace(id, "app_", "tsk_", 1), ApplicationID: id,
		QuestionnaireID: record.Questionnaire.ID, Input: fromRequestRecord(record.Questionnaire.Collect, nil), CreatedAt: now}
	if len(files) > 0 && files[0] != nil {
		docID, err := seedTestDocument(ctx, tx, files[0], id, creator, now)
		if err != nil {
			return err
		}
		task.DocumentIDs = []string{docID}
	}
	skipped := domain.SkippedSystems(task.Input)
	for _, system := range domain.Systems {
		if _, ok := skipped[system]; ok {
			task.Skipped = append(task.Skipped, system)
		}
	}
	return insertTask(ctx, tx, task, r.tasksTopic)
}

func seedTestDocument(ctx context.Context, tx pgx.Tx, files *storage.Local, appID, creator string, now time.Time) (string, error) {
	id := strings.Replace(appID, "app_", "doc_", 1)
	key := appID + "/" + id + ".pdf"
	const name = "test-financial-document.pdf"
	staged, err := files.Stage(name, bytes.NewReader(testdocument.PDF()))
	if err != nil {
		return "", err
	}
	defer files.Discard(staged)
	// A rolled-back seed can leave these exact synthetic bytes behind; retrying
	// writes the same deterministic key. No client-supplied path is used here.
	if err := files.Commit(staged, key); err != nil {
		return "", err
	}
	_, err = tx.Exec(ctx, `INSERT INTO documents
 (id,application_id,type,file_name,mime_type,size_bytes,sha256,storage_key,uploaded_by,recognition,signature_state,uploaded_at)
 VALUES ($1,$2,'BANK_STATEMENT',$3,'application/pdf',$4,$5,$6,$7,'{"state":"NOT_APPLICABLE"}','NOT_SIGNED',$8)`,
		id, appID, name, staged.Size, staged.SHA256, key, creator, now)
	return id, err
}
