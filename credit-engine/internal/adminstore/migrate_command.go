package adminstore

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"time"
)

type MigrationState struct {
	Version   int64      `json:"version"`
	File      string     `json:"file"`
	State     string     `json:"state"`
	AppliedAt *time.Time `json:"applied_at,omitempty"`
}

func RunMigrations(ctx context.Context, args []string, lookup func(string) (string, bool), stdout, stderr io.Writer) error {
	command := "up"
	if len(args) > 0 {
		command = args[0]
		args = args[1:]
	}
	if command == "help" || command == "-h" || command == "--help" {
		fmt.Fprintln(stdout, "Usage: admin-migrate [up|down|status] [-timeout 30s]; ADMIN_DATABASE_URL is required")
		return nil
	}
	if command != "up" && command != "down" && command != "status" {
		return fmt.Errorf("admin migrations: expected up, down or status")
	}
	flags := flag.NewFlagSet("admin-migrate "+command, flag.ContinueOnError)
	flags.SetOutput(stderr)
	timeout := flags.Duration("timeout", 30*time.Second, "overall connection/migration timeout")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if *timeout <= 0 || len(flags.Args()) != 0 {
		return fmt.Errorf("admin migrations: invalid arguments or timeout")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	dsn, ok := lookup("ADMIN_DATABASE_URL")
	if !ok || dsn == "" {
		return fmt.Errorf("ADMIN_DATABASE_URL is required")
	}
	db, err := OpenDatabase(dsn)
	if err != nil {
		return err
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		return databaseError("connection", err)
	}
	migrator, err := NewMigrator(db)
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	switch command {
	case "up":
		results, err := migrator.Up(ctx)
		if err != nil {
			return err
		}
		versions := []int64{}
		for _, result := range results {
			versions = append(versions, result.Source.Version)
		}
		return encoder.Encode(struct {
			Command string  `json:"command"`
			Applied []int64 `json:"applied"`
		}{command, versions})
	case "down":
		result, err := migrator.Down(ctx)
		if err != nil {
			return err
		}
		versions := []int64{}
		if result != nil {
			versions = append(versions, result.Source.Version)
		}
		return encoder.Encode(struct {
			Command    string  `json:"command"`
			RolledBack []int64 `json:"rolled_back"`
		}{command, versions})
	case "status":
		status, err := migrator.Status(ctx)
		if err != nil {
			return err
		}
		states := []MigrationState{}
		for _, entry := range status {
			state := MigrationState{Version: entry.Source.Version, File: entry.Source.Path, State: string(entry.State)}
			if !entry.AppliedAt.IsZero() {
				value := entry.AppliedAt
				state.AppliedAt = &value
			}
			states = append(states, state)
		}
		return encoder.Encode(states)
	}
	return nil
}
