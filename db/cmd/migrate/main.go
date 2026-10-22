// migrate накатывает миграции общей схемы БД и завершается.
//
//	migrate [up|down|status]   (по умолчанию up)
//
// Подключение берётся из DATABASE_URL.
package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/db/migrations"
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if err := run(log); err != nil {
		log.Error("migrate failed", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cmd := "up"
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return errors.New("DATABASE_URL is not set")
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer db.Close()

	if err := waitForDB(ctx, db, 30*time.Second); err != nil {
		return err
	}

	// Advisory lock: если migrate запустят параллельно, второй дождётся первого.
	locker, err := lock.NewPostgresSessionLocker()
	if err != nil {
		return fmt.Errorf("create locker: %w", err)
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, db, migrations.FS, goose.WithSessionLocker(locker))
	if err != nil {
		return fmt.Errorf("create goose provider: %w", err)
	}

	switch cmd {
	case "up":
		results, err := provider.Up(ctx)
		for _, r := range results {
			log.Info("applied", "version", r.Source.Version, "file", r.Source.Path, "duration", r.Duration)
		}
		if err != nil {
			return fmt.Errorf("up: %w", err)
		}
		if len(results) == 0 {
			log.Info("schema is up to date")
		}
	case "down":
		r, err := provider.Down(ctx)
		if err != nil {
			return fmt.Errorf("down: %w", err)
		}
		log.Info("rolled back", "version", r.Source.Version, "file", r.Source.Path)
	case "status":
		statuses, err := provider.Status(ctx)
		if err != nil {
			return fmt.Errorf("status: %w", err)
		}
		for _, s := range statuses {
			fmt.Printf("%-8s %-30s %s\n", s.State, s.Source.Path, appliedAt(s.AppliedAt))
		}
	default:
		return fmt.Errorf("unknown command %q (want up, down or status)", cmd)
	}
	return nil
}

// waitForDB ждёт, пока Postgres начнёт принимать соединения (контейнер мог ещё стартовать).
func waitForDB(ctx context.Context, db *sql.DB, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	for {
		err := db.PingContext(ctx)
		if err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("database is not reachable: %w", err)
		case <-time.After(time.Second):
		}
	}
}

func appliedAt(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}
