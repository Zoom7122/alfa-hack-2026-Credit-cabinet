package memory

import (
	"cmp"
	"context"
	"slices"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/domain"
)

// UserRepository — пользователи и их компании в памяти, в той же «БД», что и заявки. Для тестов.
type UserRepository struct {
	s *store
}

func NewUserRepository(tasks *CollectionTaskRepository) *UserRepository {
	return &UserRepository{s: tasks.s}
}

// SeedTestUsers заводит тестовых пользователей; компания с тем же ИНН остаётся со своим id.
func (r *UserRepository) SeedTestUsers(_ context.Context, users []domain.TestUserSeed) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	// как в Postgres: порядок — как в списке, убранные из списка — больше не тестовые
	r.s.testUsers = r.s.testUsers[:0]
	for id := range r.s.personas {
		delete(r.s.personas, id)
	}
	for _, u := range users {
		r.s.testUsers = append(r.s.testUsers, u.ID)
		r.s.users[u.ID] = domain.Representative{UserID: u.ID, FullName: u.FullName}
		r.s.roles[u.ID] = u.Role
		r.s.personas[u.ID] = u.Persona
		for _, c := range u.Companies {
			companyID, ok := r.s.companyByINN[c.INN]
			if !ok {
				companyID = c.ID
				t := domain.CompanyTypeByINN(c.INN)
				r.s.companies[companyID] = domain.Company{ID: companyID, INN: c.INN, Type: t, Name: c.Name}
				r.s.companyByINN[c.INN] = companyID
			}
			r.s.members[[2]string{u.ID, companyID}] = domain.Representative{UserID: u.ID, FullName: u.FullName, Role: c.Role,
				CanSign: domain.CanSign(c.Role, r.s.companies[companyID].Type)}
		}
	}
	return nil
}

func (r *UserRepository) TestUsers(_ context.Context) ([]domain.User, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	out := make([]domain.User, 0, len(r.s.testUsers))
	for _, id := range r.s.testUsers {
		out = append(out, r.user(id))
	}
	return out, nil
}

func (r *UserRepository) User(_ context.Context, id string) (domain.User, bool, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	if _, ok := r.s.users[id]; !ok {
		return domain.User{}, false, nil
	}
	return r.user(id), true, nil
}

// user собирает пользователя с компаниями; вызывается под блокировкой.
func (r *UserRepository) user(id string) domain.User {
	role := r.s.roles[id]
	if role == "" {
		role = domain.UserClient
	}
	u := domain.User{ID: id, FullName: r.s.users[id].FullName, Role: role, Persona: r.s.personas[id]}
	for key, m := range r.s.members {
		if key[0] != id {
			continue
		}
		c := r.s.companies[key[1]]
		// после сбора Postgres пишет в companies.name название из ЕГРЮЛ; здесь оно — в cards
		name := c.Name
		if card, ok := r.s.cards[c.INN]; ok && card.Name() != "" {
			name = card.Name()
		}
		u.Companies = append(u.Companies, domain.CompanyAccess{CompanyID: c.ID, INN: c.INN, Name: name, Role: m.Role, CanSign: m.CanSign})
	}
	// как ORDER BY c.name, c.id в Postgres; основную компанию ставит первой сервис
	slices.SortFunc(u.Companies, func(a, b domain.CompanyAccess) int {
		return cmp.Or(cmp.Compare(a.Name, b.Name), cmp.Compare(a.CompanyID, b.CompanyID))
	})
	return u
}
