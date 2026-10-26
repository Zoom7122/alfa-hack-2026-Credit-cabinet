package app

import (
	"context"
	"database/sql"
	"errors"
	"net/http"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/adminauth"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/adminstore"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/clients"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/clock"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/config"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/httpapi"
	"github.com/pressly/goose/v3"
)

type Admin struct {
	Settings   config.Admin
	Database   *sql.DB
	Repository *adminstore.Repository
	Access     *adminauth.Access
	Server     *http.Server
}

// NewAdmin assembles dependencies without opening connections or binding ports.
func NewAdmin(cfg config.Admin, source clock.Clock) (*Admin, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	db, err := adminstore.OpenDatabase(cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}
	repo, err := adminstore.NewRepository(db)
	if err != nil {
		db.Close()
		return nil, err
	}
	access, err := adminauth.New(cfg, repo, source)
	if err != nil {
		db.Close()
		return nil, err
	}
	router := httpapi.NewAdminRouter(access, cfg.RequestTimeout)
	httpapi.RegisterAdminModels(router, access, repo)
	httpapi.RegisterAdminActivation(router, access, repo)
	httpapi.RegisterAdminFields(router, access)
	httpapi.RegisterAdminEvents(router, access, repo)
	httpapi.RegisterAdminSimulation(router, access, repo)
	var metadataSource httpapi.ScoringMetadataSource
	if cfg.ScoringURL != "" {
		client, err := clients.NewModels(cfg.ScoringURL, &http.Client{Timeout: cfg.ScoringTimeout})
		if err != nil {
			db.Close()
			return nil, err
		}
		metadataSource = client
	}
	httpapi.RegisterAdminRuntime(router, access, metadataSource)
	httpapi.RegisterAdminUI(router)
	server := httpapi.NewServer(cfg.Addr, cfg.WriteTimeout, router)
	server.ReadTimeout = cfg.WriteTimeout
	server.MaxHeaderBytes = 32 << 10
	return &Admin{Settings: cfg, Database: db, Repository: repo, Access: access, Server: server}, nil
}

// Initialize verifies separately applied migrations and imports the immutable v2
// baseline idempotently. It never runs DDL or fetches a live model from MS1.
func (a *Admin) Initialize(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, a.Settings.DatabaseTimeout)
	defer cancel()
	if err := a.Database.PingContext(ctx); err != nil {
		return errors.New("admin database connection failed")
	}
	migrator, err := adminstore.NewMigrator(a.Database)
	if err != nil {
		return errors.New("admin migration status failed")
	}
	statuses, err := migrator.Status(ctx)
	if err != nil {
		return errors.New("admin migration status failed")
	}
	for _, status := range statuses {
		if status.State != goose.StateApplied {
			return errors.New("admin database migrations must be applied before startup")
		}
	}
	if err := a.Repository.SeedBaseline(ctx); err != nil {
		return err
	}
	return a.Repository.SeedActiveModels(ctx)
}
func (a *Admin) Close() error { return a.Database.Close() }
