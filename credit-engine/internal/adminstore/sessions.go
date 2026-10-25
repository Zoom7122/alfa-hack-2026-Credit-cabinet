package adminstore

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"strings"
	"time"
)

type Session struct {
	TokenSHA256 string    `json:"-"`
	Username    string    `json:"username"`
	CSRFSHA256  string    `json:"-"`
	CreatedAt   time.Time `json:"created_at"`
	ExpiresAt   time.Time `json:"expires_at"`
}

var sessionHashPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

// CreateSession bounds outstanding sessions for the single administrative user.
// The per-user transaction lock also serializes concurrent successful logins.
func (r *Repository) CreateSession(ctx context.Context, s Session, limit int) error {
	if !sessionHashPattern.MatchString(s.TokenSHA256) || !sessionHashPattern.MatchString(s.CSRFSHA256) || strings.TrimSpace(s.Username) == "" || len(s.Username) > 256 || !s.ExpiresAt.After(s.CreatedAt) || s.CreatedAt.IsZero() || limit < 1 || limit > 100 {
		return ErrInvalid
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return databaseError("create session", err)
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1, 728564))", s.Username); err != nil {
		return databaseError("create session", err)
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM credit_engine_admin.admin_sessions WHERE expires_at <= $1", s.CreatedAt); err != nil {
		return databaseError("create session", err)
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM credit_engine_admin.admin_sessions WHERE token_sha256 IN (SELECT token_sha256 FROM credit_engine_admin.admin_sessions WHERE username=$1 ORDER BY created_at DESC,token_sha256 OFFSET $2)", s.Username, limit-1); err != nil {
		return databaseError("create session", err)
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO credit_engine_admin.admin_sessions(token_sha256,username,csrf_sha256,created_at,expires_at) VALUES($1,$2,$3,$4,$5)", s.TokenSHA256, s.Username, s.CSRFSHA256, s.CreatedAt, s.ExpiresAt); err != nil {
		return databaseError("create session", err)
	}
	return databaseError("create session", tx.Commit())
}
func (r *Repository) GetSession(ctx context.Context, hash string, now time.Time) (Session, error) {
	if !sessionHashPattern.MatchString(hash) {
		return Session{}, ErrNotFound
	}
	var s Session
	err := r.db.QueryRowContext(ctx, "SELECT token_sha256,username,csrf_sha256,created_at,expires_at FROM credit_engine_admin.admin_sessions WHERE token_sha256=$1 AND expires_at > $2", hash, now).Scan(&s.TokenSHA256, &s.Username, &s.CSRFSHA256, &s.CreatedAt, &s.ExpiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, ErrNotFound
	}
	return s, databaseError("read session", err)
}
func (r *Repository) DeleteSession(ctx context.Context, hash string) error {
	if !sessionHashPattern.MatchString(hash) {
		return ErrInvalid
	}
	_, err := r.db.ExecContext(ctx, "DELETE FROM credit_engine_admin.admin_sessions WHERE token_sha256=$1", hash)
	return databaseError("delete session", err)
}
