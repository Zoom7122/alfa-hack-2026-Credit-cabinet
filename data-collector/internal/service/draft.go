package service

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/domain"
)

type DraftRepository interface {
	Get(context.Context, string) (domain.Application, bool, error)
	Draft(context.Context, string) (domain.SavedDraft, bool, error)
	SaveDraft(context.Context, string, json.RawMessage, int) (domain.SavedDraft, error)
}

type DraftService struct{ repo DraftRepository }

func NewDraftService(repo DraftRepository) *DraftService { return &DraftService{repo: repo} }

func (s *DraftService) authorize(ctx context.Context, id, actor string) error {
	app, ok, err := s.repo.Get(ctx, id)
	if err != nil {
		return err
	}
	if !ok {
		return ErrNotFound
	}
	if actor == "" && app.Representative.HasLogin {
		return ErrUnauthorized
	}
	if actor != "" && actor != app.Representative.UserID {
		return ErrForbidden
	}
	if app.Status != domain.ApplicationDraft {
		return domain.ErrApplicationNotDraft
	}
	return nil
}

func (s *DraftService) Get(ctx context.Context, id, actor string) (domain.SavedDraft, error) {
	if err := s.authorize(ctx, id, actor); err != nil {
		return domain.SavedDraft{}, err
	}
	draft, ok, err := s.repo.Draft(ctx, id)
	if err != nil {
		return draft, fmt.Errorf("read draft: %w", err)
	}
	if !ok {
		return draft, ErrNotFound
	}
	return draft, nil
}

func (s *DraftService) Save(ctx context.Context, id, actor string, data json.RawMessage, version int) (domain.SavedDraft, error) {
	if err := s.authorize(ctx, id, actor); err != nil {
		return domain.SavedDraft{}, err
	}
	return s.repo.SaveDraft(ctx, id, data, version)
}
