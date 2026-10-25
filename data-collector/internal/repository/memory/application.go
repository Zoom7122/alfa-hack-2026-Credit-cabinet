package memory

import (
	"context"
	"fmt"
	"time"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/domain"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/events"
)

// ApplicationRepository хранит заявки, пользователей, компании и документы в памяти — в той же
// «БД», что и задачи tasks. Семантика как у Postgres: пользователь и компания находятся по ИНН,
// черновик переиспользуется, отправка пишет документы, задачу и события вместе. Для тестов.
type ApplicationRepository struct {
	s              *store
	tasksTopic     string
	documentsTopic string
}

func NewApplicationRepository(tasks *CollectionTaskRepository) *ApplicationRepository {
	return &ApplicationRepository{s: tasks.s, tasksTopic: events.DefaultTopic, documentsTopic: events.DocumentsTopic}
}

// Register пишет или находит пользователя и компанию, их связь и черновик заявки.
func (r *ApplicationRepository) Register(_ context.Context, reg domain.Registration) (domain.Application, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()

	rep := reg.Representative
	userID := reg.UserID
	switch {
	case reg.ExistingUser:
		// вошёл по токену: пользователь уже есть, по ИНН его не ищем (в Postgres — FK на users)
		u, ok := r.s.users[userID]
		if !ok {
			return domain.Application{}, fmt.Errorf("user %s is not found", userID)
		}
		rep.FullName, rep.INN = u.FullName, u.INN
	case rep.INN != "":
		if id, ok := r.s.userByINN[rep.INN]; ok {
			userID = id
		} else {
			r.s.userByINN[rep.INN] = userID
		}
	}
	if !reg.ExistingUser {
		r.s.users[userID] = domain.Representative{UserID: userID, FullName: rep.FullName, INN: rep.INN}
	}

	companyID, known := r.s.companyByINN[reg.Company.INN]
	if !known {
		companyID = reg.CompanyID
		c := reg.Company
		c.ID = companyID
		r.s.companies[companyID] = c
		r.s.companyByINN[c.INN] = companyID
	}
	rep.UserID = userID
	// вошедший по токену: уже записанную роль в компании не переписываем (как в Postgres)
	if _, member := r.s.members[[2]string{userID, companyID}]; !member || !reg.ExistingUser {
		r.s.members[[2]string{userID, companyID}] = rep
	}

	appID := ""
	for id, a := range r.s.applications {
		if a.companyID == companyID && a.userID == userID && a.app.Status == domain.ApplicationDraft {
			appID = id
		}
	}
	if appID == "" {
		appID = reg.ApplicationID
		r.s.appSeq++
		r.s.applications[appID] = &application{
			app: domain.Application{ID: appID, Number: fmt.Sprintf("A-%d-%06d", reg.CreatedAt.Year(), r.s.appSeq),
				Status: domain.ApplicationDraft, CreatedAt: reg.CreatedAt, UpdatedAt: reg.CreatedAt},
			companyID: companyID,
			userID:    userID,
		}
		r.s.addEvent(appID, "application.created")
	}
	app := r.s.application(appID)
	app.Company.AlreadyKnown = known
	return app, nil
}

// Get возвращает заявку с компанией, представителем и документами.
func (r *ApplicationRepository) Get(_ context.Context, id string) (domain.Application, bool, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	if _, ok := r.s.applications[id]; !ok {
		return domain.Application{}, false, nil
	}
	return r.s.application(id), true, nil
}

// Submit отправляет заявку: документы, анкета, задача на сбор, события в outbox.
// domain.ErrApplicationNotDraft — заявка уже не черновик.
func (r *ApplicationRepository) Submit(_ context.Context, sub domain.Submission) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	a, ok := r.s.applications[sub.ApplicationID]
	if !ok {
		return fmt.Errorf("application %s is not found", sub.ApplicationID)
	}
	if a.app.Status != domain.ApplicationDraft {
		return domain.ErrApplicationNotDraft
	}
	if err := r.s.createTask(sub.Task, r.tasksTopic); err != nil {
		return err
	}
	if len(sub.Documents) > 0 {
		if err := r.s.addOutbox(r.documentsTopic, sub.ApplicationID,
			events.NewDocumentsSubmitted(sub.ApplicationID, sub.Task.Input.INN, sub.Documents, sub.SubmittedAt)); err != nil {
			return err
		}
	}
	a.app.Status, a.app.SubmittedAt, a.app.UpdatedAt = domain.ApplicationSubmitted, sub.SubmittedAt, sub.SubmittedAt
	a.app.ProductCode, a.app.Requested, a.app.Purpose = sub.ProductCode, sub.Requested, sub.Purpose
	a.app.Documents = append(a.app.Documents, sub.Documents...)
	a.app.CollectionTaskID = sub.Task.ID
	a.submission = &sub
	r.s.addEvent(sub.ApplicationID, "application.submitted")
	return nil
}

// Submission — последняя отправка анкеты по заявке: что записано в applications.
func (r *ApplicationRepository) Submission(appID string) (domain.Submission, bool) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	a, ok := r.s.applications[appID]
	if !ok || a.submission == nil {
		return domain.Submission{}, false
	}
	return *a.submission, true
}

// Events — типы событий журнала заявки по порядку.
func (r *ApplicationRepository) Events(appID string) []string {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	return append([]string(nil), r.s.events[appID]...)
}

// application собирает заявку с компанией и представителем; блокировка уже взята.
func (s *store) application(id string) domain.Application {
	a := s.applications[id]
	app := a.app
	app.Company = s.companies[a.companyID]
	app.Representative = s.members[[2]string{a.userID, a.companyID}]
	app.Representative.FullName = s.users[a.userID].FullName
	app.Documents = append([]domain.Document(nil), a.app.Documents...)
	if app.UpdatedAt.IsZero() {
		app.UpdatedAt = time.Now().UTC()
	}
	return app
}
