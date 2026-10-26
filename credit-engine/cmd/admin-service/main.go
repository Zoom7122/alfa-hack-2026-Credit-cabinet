package main

import (
	"context"
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
	cfg, err := config.LoadAdmin(os.LookupEnv)
	if err != nil {
		return err
	}
	gin.SetMode(gin.ReleaseMode)
	service, err := app.NewAdmin(cfg, nil)
	if err != nil {
		return err
	}
	defer service.Close()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err = service.Initialize(ctx); err != nil {
		return err
	}
	return httpapi.Serve(ctx, service.Server)
}
