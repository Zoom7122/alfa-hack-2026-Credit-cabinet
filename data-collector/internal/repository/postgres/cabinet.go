package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/domain"
)

const (
	selectCompaniesOf = `
SELECT c.id, c.inn, c.type, c.name, coalesce(c.ogrn, ''), c.registry, m.role, m.can_sign
FROM company_members m JOIN companies c ON c.id = m.company_id
WHERE m.user_id = $1
ORDER BY c.name, c.id`

	// заявки компаний пользователя — его и коллег по компании; черновики — только свои
	selectApplicationIDs = `
SELECT a.id FROM applications a
JOIN company_members m ON m.company_id = a.company_id AND m.user_id = $1
WHERE ($2 = '' OR a.company_id = $2) AND (cardinality($3::text[]) = 0 OR a.status = ANY($3))
  AND (a.status <> 'DRAFT' OR a.created_by = $1) AND (NOT $4 OR a.created_by = $1)
ORDER BY a.created_at DESC, a.id DESC
LIMIT $5`

	selectEvents = `
SELECT e.id, e.created_at, e.type, coalesce(e.from_status, ''), coalesce(e.to_status, ''), coalesce(e.actor_id, ''),
       coalesce(u.role, ''), coalesce(e.message, ''), e.payload
FROM application_events e LEFT JOIN users u ON u.id = e.actor_id
WHERE e.application_id = $1
ORDER BY e.id`

	// отчёт по документу или по его открепленной подписи, последний по времени проверки
	selectLatestReport = `
SELECT r.report FROM signature_reports r
JOIN documents d ON d.id = r.document_id
WHERE d.application_id = $1 AND ($2 = r.document_id OR $2 = r.signature_document_id)
ORDER BY r.verified_at DESC, r.id DESC
LIMIT 1`

	selectCompanyKnown = `SELECT EXISTS (SELECT 1 FROM companies WHERE inn = $1)`
)

// CabinetRepository — чтение для кабинета клиента: компании пользователя, список и журнал заявок, отчёты о подписи.
type CabinetRepository struct {
	pool *pgxpool.Pool
}

func NewCabinetRepository(pool *pgxpool.Pool) *CabinetRepository {
	return &CabinetRepository{pool: pool}
}

func (r *CabinetRepository) CompaniesOf(ctx context.Context, userID string) ([]domain.CompanyProfile, error) {
	rows, err := r.pool.Query(ctx, selectCompaniesOf, userID)
	if err != nil {
		return nil, fmt.Errorf("select companies: %w", err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.CompanyProfile, error) {
		var (
			p                 domain.CompanyProfile
			companyType, role string
			registry          []byte
		)
		if err := row.Scan(&p.ID, &p.INN, &companyType, &p.Name, &p.OGRN, &registry, &role, &p.CanSign); err != nil {
			return p, err
		}
		p.Type, p.Role = domain.CompanyType(companyType), domain.RepresentativeRole(role)
		if len(registry) > 0 {
			var rec registryRecord
			if err := json.Unmarshal(registry, &rec); err != nil {
				return p, fmt.Errorf("decode registry %s: %w", p.INN, err)
			}
			p.Card = &domain.CompanyCard{INN: rec.INN, Type: domain.CompanyType(rec.ApplicantType), OGRN: rec.OGRN, KPP: rec.KPP,
				FullName: rec.FullName, ShortName: rec.ShortName, RegistrationDate: rec.RegistrationDate,
				DirectorName: rec.DirectorName, Region: rec.Region, TerminatedOn: rec.TerminatedOn}
		}
		return p, nil
	})
}

func (r *CabinetRepository) ApplicationIDs(ctx context.Context, f domain.ApplicationFilter) ([]string, error) {
	st := make([]string, len(f.Statuses))
	for i, s := range f.Statuses {
		st[i] = string(s)
	}
	rows, err := r.pool.Query(ctx, selectApplicationIDs, f.UserID, f.CompanyID, st, f.OnlyMine, f.Limit)
	if err != nil {
		return nil, fmt.Errorf("select applications: %w", err)
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}

func (r *CabinetRepository) Events(ctx context.Context, applicationID string) ([]domain.ApplicationEvent, error) {
	rows, err := r.pool.Query(ctx, selectEvents, applicationID)
	if err != nil {
		return nil, fmt.Errorf("select events: %w", err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.ApplicationEvent, error) {
		var (
			e              domain.ApplicationEvent
			from, to, role string
			at             time.Time
		)
		err := row.Scan(&e.ID, &at, &e.Type, &from, &to, &e.ActorID, &role, &e.Message, &e.Payload)
		e.At, e.From, e.To, e.ActorRole = at.UTC(), domain.ApplicationStatus(from), domain.ApplicationStatus(to), domain.UserRole(role)
		return e, err
	})
}

func (r *CabinetRepository) LatestSignatureReport(ctx context.Context, applicationID, documentID string) (json.RawMessage, bool, error) {
	var report []byte
	err := r.pool.QueryRow(ctx, selectLatestReport, applicationID, documentID).Scan(&report)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("select signature report: %w", err)
	}
	return report, true, nil
}

func (r *CabinetRepository) CompanyKnown(ctx context.Context, inn string) (bool, error) {
	var known bool
	if err := r.pool.QueryRow(ctx, selectCompanyKnown, inn).Scan(&known); err != nil {
		return false, fmt.Errorf("select company: %w", err)
	}
	return known, nil
}
