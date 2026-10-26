package adminstore_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/adminstore"
)

func TestDatabaseConfigurationAndSafeErrors(t *testing.T) {
	for _, dsn := range []string{"", " ", "postgres://reader:private-password@localhost/%zz"} {
		db, err := adminstore.OpenDatabase(dsn)
		if err == nil {
			db.Close()
			t.Fatal("invalid configuration accepted")
		}
		if strings.Contains(err.Error(), "private-password") {
			t.Fatal("password leaked")
		}
		var safe *adminstore.DatabaseError
		if !errors.As(err, &safe) {
			t.Fatal("untyped configuration error")
		}
	}
	db, err := adminstore.OpenDatabase("postgres://reader:fake-password@db.invalid/credit_engine_admin_test?sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if db.Stats().OpenConnections != 0 || db.Stats().MaxOpenConnections != 5 {
		t.Fatal("opening database connected eagerly")
	}
	migrator, err := adminstore.NewMigrator(db)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := migrator.Status(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal("context cause lost", err)
	}
	if db.Stats().OpenConnections != 0 {
		t.Fatal("cancelled status contacted database")
	}
	if _, err := adminstore.NewMigrator(nil); err == nil {
		t.Fatal("nil DB accepted")
	}
}

func TestMigrationCommandValidatesBeforeConnecting(t *testing.T) {
	for _, test := range []struct {
		args []string
		bad  bool
	}{{[]string{"help"}, false}, {[]string{"up", "-h"}, false}, {[]string{"unknown"}, true}, {[]string{"up", "-timeout", "0s"}, true}, {[]string{"up", "extra"}, true}, {[]string{"up", "-unknown"}, true}, {nil, true}} {
		var out, errOut bytes.Buffer
		err := adminstore.RunMigrations(context.Background(), test.args, func(string) (string, bool) { return "", false }, &out, &errOut)
		if (err != nil) != test.bad {
			t.Fatal(test.args, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var out, errOut bytes.Buffer
	if err := adminstore.RunMigrations(ctx, []string{"up"}, func(string) (string, bool) { t.Fatal("cancelled command read DSN"); return "", false }, &out, &errOut); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := adminstore.RunMigrations(context.Background(), []string{"up"}, func(string) (string, bool) { return "postgres://x:private-password@host/%zz", true }, &out, &errOut); err == nil || strings.Contains(err.Error(), "private-password") {
		t.Fatal(err)
	}
}
