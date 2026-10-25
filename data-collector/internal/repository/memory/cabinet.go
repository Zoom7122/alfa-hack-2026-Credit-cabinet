package memory

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/domain"
)

// CabinetRepository — чтение для кабинета клиента в памяти, поверх той же «БД», что заявки. Для тестов.
// Журнал хранит только типы событий: время — время заявки, тексты подставляет сервис.
type CabinetRepository struct {
	s       *store
	reports map[string]json.RawMessage // отчёт о подписи по id документа
}

func NewCabinetRepository(tasks *CollectionTaskRepository) *CabinetRepository {
	return &CabinetRepository{s: tasks.s, reports: map[string]json.RawMessage{}}
}

// PutSignatureReport — отчёт о подписи документа (в Postgres его пишет worker подписи).
func (r *CabinetRepository) PutSignatureReport(documentID string, report json.RawMessage) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	r.reports[documentID] = report
}

func (r *CabinetRepository) CompaniesOf(_ context.Context, userID string) ([]domain.CompanyProfile, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	var out []domain.CompanyProfile
	for key, m := range r.s.members {
		if key[0] != userID {
			continue
		}
		c := r.s.companies[key[1]]
		p := domain.CompanyProfile{ID: c.ID, INN: c.INN, Type: c.Type, Name: c.Name, OGRN: c.OGRN, Role: m.Role, CanSign: m.CanSign}
		if card, ok := r.s.cards[c.INN]; ok {
			p.Card = &card
			p.Name, p.OGRN = card.Name(), card.OGRN
		}
		out = append(out, p)
	}
	slices.SortFunc(out, func(a, b domain.CompanyProfile) int {
		return cmp.Or(cmp.Compare(a.Name, b.Name), cmp.Compare(a.ID, b.ID))
	})
	return out, nil
}

func (r *CabinetRepository) ApplicationIDs(_ context.Context, f domain.ApplicationFilter) ([]string, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	type row struct {
		id  string
		app domain.Application
	}
	var rows []row
	for id, a := range r.s.applications {
		if _, member := r.s.members[[2]string{f.UserID, a.companyID}]; !member {
			continue
		}
		mine := a.userID == f.UserID
		switch {
		case f.CompanyID != "" && a.companyID != f.CompanyID,
			len(f.Statuses) > 0 && !slices.Contains(f.Statuses, a.app.Status),
			a.app.Status == domain.ApplicationDraft && !mine,
			f.OnlyMine && !mine:
			continue
		}
		rows = append(rows, row{id, a.app})
	}
	slices.SortFunc(rows, func(a, b row) int { return cmp.Or(b.app.CreatedAt.Compare(a.app.CreatedAt), cmp.Compare(b.id, a.id)) })
	if f.Limit > 0 && len(rows) > f.Limit {
		rows = rows[:f.Limit]
	}
	ids := make([]string, len(rows))
	for i, x := range rows {
		ids[i] = x.id
	}
	return ids, nil
}

func (r *CabinetRepository) Events(_ context.Context, applicationID string) ([]domain.ApplicationEvent, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	a, ok := r.s.applications[applicationID]
	if !ok {
		return nil, nil
	}
	out := make([]domain.ApplicationEvent, 0, len(r.s.events[applicationID]))
	for i, t := range r.s.events[applicationID] {
		out = append(out, domain.ApplicationEvent{ID: fmt.Sprintf("evt_%03d", i+1), At: a.app.CreatedAt, Type: t})
	}
	return out, nil
}

func (r *CabinetRepository) LatestSignatureReport(_ context.Context, _, documentID string) (json.RawMessage, bool, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	rep, ok := r.reports[documentID]
	return rep, ok, nil
}

func (r *CabinetRepository) CompanyKnown(_ context.Context, inn string) (bool, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	_, ok := r.s.companyByINN[inn]
	return ok, nil
}
