//go:build integration

package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/domain"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/storage"
)

func TestIntegrationUpgradeLegacyTestApplications(t *testing.T) {
	pool := testApplicationsPool(t)
	ctx := context.Background()
	r := NewApplicationRepository(pool, "test.seed.tasks", "test.seed.documents")
	files, err := storage.NewLocal(t.TempDir(), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct{ name, protect string }{
		{"untouched", ""},
		{"changed status", `UPDATE applications SET status='COMPLETED' WHERE id=$1`},
		{"answered question", `INSERT INTO info_requests(id,application_id,required,question,status,answer) VALUES('ir_answered',$1,true,'{}','ANSWERED','{}')`},
		{"staff assignment", `UPDATE applications SET assignee_id='usr_underwriter_orlov' WHERE id=$1`},
		{"accepted offer", `INSERT INTO offers(id,application_id,kind,amount,term_months,rate,monthly_payment,valid_until,status) VALUES('ofr_accepted',$1,'REQUESTED',1000000,12,20,100000,now()+interval '1 day','ACCEPTED')`},
		{"uploaded document", `INSERT INTO documents(id,application_id,type,file_name,mime_type,size_bytes,sha256,storage_key) VALUES('doc_uploaded',$1,'OTHER','client.pdf','application/pdf',1,'abc','client.pdf')`},
		{"staff event", `INSERT INTO application_events(id,application_id,type) VALUES('evt_staff',$1,'underwriter.assigned')`},
		{"real collection", `INSERT INTO collection_tasks(id,questionnaire_id,inn,request,skipped_sources,application_id) VALUES('tsk_real','qst_real','7799000012','{}','{}',$1)`},
		{"new decision", `INSERT INTO decisions(id,application_id,details) VALUES('dcs_real',$1,'{}')`},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			seed := testApplication{ID: 700 + i, Company: "north", Status: domain.ApplicationOffered,
				Product: "WORKING_CAPITAL", Amount: 5_000_000, Term: 24}
			id := testApplicationID(seed.ID)
			if _, err := r.seedTestApplications(ctx, []testApplication{seed}, time.Now().UTC()); err != nil {
				t.Fatal(err)
			}
			// Reproduce the previous seeder's fixed outcome, with no source collection.
			for _, query := range []string{
				`DELETE FROM outbox WHERE key=replace($1,'app_','tsk_')`,
				`DELETE FROM collection_tasks WHERE application_id=$1`,
				`UPDATE applications SET status='OFFERED' WHERE id=$1`,
				`INSERT INTO decisions(id,application_id,grade,score,details) VALUES(replace($1,'app_','dcs_'),$1,'A',820,'{"test_data":true}')`,
			} {
				if _, err := pool.Exec(ctx, query, id); err != nil {
					t.Fatal(err)
				}
			}
			if tc.protect != "" {
				if _, err := pool.Exec(ctx, tc.protect, id); err != nil {
					t.Fatal(err)
				}
			}
			before := testApplicationSnapshot(t, pool)
			if count, err := r.seedTestApplications(ctx, []testApplication{seed}, time.Now().UTC(), files); err != nil || count != 0 {
				t.Fatalf("upgrade: %d %v", count, err)
			}
			if tc.protect != "" {
				for table, actual := range testApplicationSnapshot(t, pool) {
					if actual != before[table] {
						t.Errorf("protected application changed %s", table)
					}
				}
				return
			}
			app, ok, err := r.Get(ctx, id)
			if err != nil || !ok || app.Status != domain.ApplicationSubmitted || len(app.Documents) != 1 || !app.Documents[0].IsTest() {
				t.Fatalf("upgraded application: %+v %v", app, err)
			}
			var jobs, decisions int
			if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM collection_tasks WHERE application_id=$1),
 (SELECT count(*) FROM decisions WHERE application_id=$1)`, id).Scan(&jobs, &decisions); err != nil || jobs != 1 || decisions != 0 {
				t.Fatalf("upgrade pipeline: jobs=%d decisions=%d err=%v", jobs, decisions, err)
			}
			if !strings.Contains(testApplicationSnapshot(t, pool)["application_events"], "test_data.recalculation_requested") {
				t.Fatal("upgrade missing from timeline")
			}
			before = testApplicationSnapshot(t, pool)
			if _, err := r.seedTestApplications(ctx, []testApplication{seed}, time.Now().UTC(), files); err != nil {
				t.Fatal(err)
			}
			for table, actual := range testApplicationSnapshot(t, pool) {
				if actual != before[table] {
					t.Errorf("restart changed %s", table)
				}
			}
		})
	}
}
