package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/app"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/config"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/httpapi"
	"github.com/gin-gonic/gin"
)

func main() {
	if err := run(); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}

func run() error {
	if os.Getenv("DATABASE_URL") == "" {
		return fmt.Errorf("DATABASE_URL is required for durable runtime")
	}
	cfg, err := config.LoadScoring(os.LookupEnv)
	if err != nil {
		return err
	}
	gin.SetMode(gin.ReleaseMode)
	service, err := app.NewScoring(cfg, nil)
	if err != nil {
		return err
	}
	if service.Database != nil {
		defer service.Database.Close()
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	if service.ModelsDatabase != nil {
		defer service.ModelsDatabase.Close()
	}
	defer stop()
	return httpapi.Serve(ctx, service.Server)
}
