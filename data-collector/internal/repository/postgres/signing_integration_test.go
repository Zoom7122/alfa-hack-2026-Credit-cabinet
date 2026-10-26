//go:build integration

package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/domain"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/service"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/storage"
)

type signingFixture struct {
	pool                                           *pgxpool.Pool
	svc                                            *service.SigningService
	repo                                           *SigningRepository
	root, appID, directorID, accountantID, offerID string
}

func newSigningFixture(t *testing.T) signingFixture {
	t.Helper()
	ctx := context.Background()
	pool := testPool(t)
	id := ulid.Make().String()
	f := signingFixture{pool: pool, root: t.TempDir(), appID: "app_" + id, directorID: "usr_director_" + id,
		accountantID: "usr_accountant_" + id, offerID: "ofr_" + id}
	companyID := "apl_" + id
	companyINN := fmt.Sprintf("88%08d", time.Now().UnixNano()%1e8)
	apps := NewApplicationRepository(pool, "it.signing.tasks", "it.signing.documents")
	_, err := apps.Register(ctx, domain.Registration{UserID: f.directorID, CompanyID: companyID, ApplicationID: f.appID,
		Representative: domain.Representative{FullName: "Директор", Role: domain.RoleDirector, CanSign: true},
		Company:        domain.Company{INN: companyINN, Type: domain.CompanyLegalEntity, Name: "ООО Тест"}, CreatedAt: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, q := range []string{
			`DELETE FROM signings WHERE application_id = $1`, `DELETE FROM documents WHERE application_id = $1`,
			`DELETE FROM offers WHERE application_id = $1`, `DELETE FROM application_events WHERE application_id = $1`,
			`DELETE FROM applications WHERE id = $1`,
		} {
			if _, err := pool.Exec(ctx, q, f.appID); err != nil {
				t.Error(err)
			}
		}
		for _, q := range []string{`DELETE FROM company_members WHERE company_id = $1`, `DELETE FROM companies WHERE id = $1`} {
			if _, err := pool.Exec(ctx, q, companyID); err != nil {
				t.Error(err)
			}
		}
		if _, err := pool.Exec(ctx, `DELETE FROM users WHERE id = ANY($1)`, []string{f.directorID, f.accountantID}); err != nil {
			t.Error(err)
		}
	})
	for _, cmd := range []struct {
		sql  string
		args []any
	}{
		{`UPDATE applications SET status = 'OFFERED', product_code = 'WORKING_CAPITAL', amount = 1000000, term_months = 24 WHERE id = $1`, []any{f.appID}},
		{`INSERT INTO offers (id, application_id, kind, amount, term_months, rate, monthly_payment, valid_until)
          VALUES ($1, $2, 'REQUESTED', 1000000, 24, 20, 51000, now() + interval '1 day')`, []any{f.offerID, f.appID}},
		{`INSERT INTO users (id, full_name, role) VALUES ($1, 'Бухгалтер', 'CLIENT')`, []any{f.accountantID}},
		{`INSERT INTO company_members (user_id, company_id, role, can_sign) VALUES ($1, $2, 'ACCOUNTANT', false)`, []any{f.accountantID, companyID}},
	} {
		if _, err := pool.Exec(ctx, cmd.sql, cmd.args...); err != nil {
			t.Fatal(err)
		}
	}
	files, err := storage.NewLocal(f.root, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	f.repo = NewSigningRepository(pool)
	f.svc = service.NewSigningService(f.repo, files, func(key string) (io.ReadCloser, error) { return files.Open(key) })
	return f
}

func TestIntegrationMockSigning(t *testing.T) {
	t.Run("persistent contract, signer access and concurrent replay", func(t *testing.T) {
		f := newSigningFixture(t)
		ctx := context.Background()
		requested, err := f.svc.Accept(ctx, f.accountantID, f.appID, f.offerID)
		if err != nil || requested.Code != "" || requested.Signer.ID != f.directorID {
			t.Fatalf("request by accountant: %+v %v", requested, err)
		}
		if _, err := f.svc.Signing(ctx, "usr_outside", f.appID); !errors.Is(err, service.ErrForbidden) {
			t.Fatalf("outsider: %v", err)
		}
		s, err := NewSigningRepository(f.pool).Signing(ctx, f.directorID, f.appID)
		if err != nil || len(s.Code) != 6 || s.AttemptsLeft != 3 {
			t.Fatalf("persisted session: %+v %v", s, err)
		}
		replayed, err := f.svc.Accept(ctx, f.directorID, f.appID, f.offerID)
		if err != nil || replayed.ID != s.ID || replayed.Code != s.Code {
			t.Fatalf("accept replay: %+v %v", replayed, err)
		}
		data, err := os.ReadFile(filepath.Join(f.root, s.Document.StorageKey))
		hash := sha256.Sum256(data)
		if err != nil || !strings.Contains(string(data), "1000000 RUB") || hex.EncodeToString(hash[:]) != s.DocumentSHA256 {
			t.Fatalf("contract: %s %v", data, err)
		}
		if _, err := f.svc.Confirm(ctx, f.accountantID, f.appID, s.Code); !errors.Is(err, domain.ErrNotAuthorizedSigner) {
			t.Fatalf("accountant signed: %v", err)
		}
		u, _, err := NewUserRepository(f.pool).User(ctx, f.directorID)
		if err != nil || !slices.Contains(u.PendingSignatures, f.appID) {
			t.Fatalf("pending: %+v %v", u, err)
		}
		var wg sync.WaitGroup
		for range 2 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				done, err := f.svc.Confirm(ctx, f.directorID, f.appID, s.Code)
				if err != nil || done.State != domain.SigningSigned || done.Code != "" || done.Disbursement == nil || done.Disbursement.Amount != 1_000_000 {
					t.Errorf("confirm: %+v %v", done, err)
				}
			}()
		}
		wg.Wait()
		var status string
		var docs, signed, paid int
		err = f.pool.QueryRow(ctx, `SELECT status,
            (SELECT count(*) FROM documents WHERE application_id = a.id),
            (SELECT count(*) FROM application_events WHERE application_id = a.id AND type = 'sign.confirmed'),
            (SELECT count(*) FROM application_events WHERE application_id = a.id AND type = 'disbursement.simulated')
            FROM applications a WHERE id = $1`, f.appID).Scan(&status, &docs, &signed, &paid)
		if err != nil || status != "COMPLETED" || docs != 1 || signed != 1 || paid != 1 {
			t.Fatalf("completion: %s docs=%d signed=%d paid=%d err=%v", status, docs, signed, paid, err)
		}
		u, _, err = NewUserRepository(f.pool).User(ctx, f.directorID)
		if err != nil || slices.Contains(u.PendingSignatures, f.appID) {
			t.Fatalf("completed pending: %+v %v", u, err)
		}
	})
	t.Run("wrong code persists attempts and exhaustion permits a new session", func(t *testing.T) {
		f := newSigningFixture(t)
		ctx := context.Background()
		s, err := f.svc.Accept(ctx, f.directorID, f.appID, f.offerID)
		if err != nil {
			t.Fatal(err)
		}
		wrong := "000000"
		if s.Code == wrong {
			wrong = "111111"
		}
		for left := 2; left >= 0; left-- {
			_, err := f.svc.Confirm(ctx, f.directorID, f.appID, wrong)
			var codeErr *domain.SigningCodeError
			if !errors.As(err, &codeErr) || codeErr.AttemptsLeft != left {
				t.Fatalf("attempt %d: %v", left, err)
			}
			stored, err := f.svc.Signing(ctx, f.directorID, f.appID)
			if err != nil || stored.AttemptsLeft != left {
				t.Fatalf("persisted attempts: %+v %v", stored, err)
			}
		}
		if _, err := f.svc.Confirm(ctx, f.directorID, f.appID, s.Code); !errors.Is(err, service.ErrInvalidTransition) {
			t.Fatalf("exhausted code: %v", err)
		}
		fresh, err := f.svc.Accept(ctx, f.directorID, f.appID, f.offerID)
		if err != nil || fresh.ID == s.ID || fresh.Document.ID == s.Document.ID || fresh.AttemptsLeft != 3 {
			t.Fatalf("new session: %+v %v", fresh, err)
		}
		if _, err := f.repo.Confirm(ctx, f.directorID, f.appID, s.ID, fresh.Code, time.Now()); !errors.Is(err, service.ErrInvalidTransition) {
			t.Fatalf("stale session: %v", err)
		}
	})
	t.Run("expired code is rejected", func(t *testing.T) {
		f := newSigningFixture(t)
		ctx := context.Background()
		s, err := f.svc.Accept(ctx, f.directorID, f.appID, f.offerID)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.pool.Exec(ctx, `UPDATE signings SET code_expires_at = now() - interval '1 second' WHERE id = $1`, s.ID); err != nil {
			t.Fatal(err)
		}
		_, err = f.svc.Confirm(ctx, f.directorID, f.appID, s.Code)
		var codeErr *domain.SigningCodeError
		if !errors.As(err, &codeErr) || !codeErr.Expired || codeErr.AttemptsLeft != 0 {
			t.Fatalf("expired: %v", err)
		}
	})
	t.Run("changed document and expired offer cannot be signed", func(t *testing.T) {
		f := newSigningFixture(t)
		ctx := context.Background()
		if _, err := f.pool.Exec(ctx, `UPDATE offers SET valid_until = now() - interval '1 second' WHERE id = $1`, f.offerID); err != nil {
			t.Fatal(err)
		}
		if _, err := f.svc.Accept(ctx, f.directorID, f.appID, f.offerID); !errors.Is(err, domain.ErrOfferExpired) {
			t.Fatalf("expired offer: %v", err)
		}
		if _, err := f.pool.Exec(ctx, `UPDATE offers SET valid_until = now() + interval '1 day' WHERE id = $1`, f.offerID); err != nil {
			t.Fatal(err)
		}
		s, err := f.svc.Accept(ctx, f.directorID, f.appID, f.offerID)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(f.root, s.Document.StorageKey), []byte("changed"), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := f.svc.Confirm(ctx, f.directorID, f.appID, s.Code); err == nil {
			t.Fatal("changed contract was signed")
		}
		stored, err := f.svc.Signing(ctx, f.directorID, f.appID)
		if err != nil || stored.State != domain.SigningAwaiting || stored.AttemptsLeft != 3 {
			t.Fatalf("changed contract affected state: %+v %v", stored, err)
		}
	})
}
