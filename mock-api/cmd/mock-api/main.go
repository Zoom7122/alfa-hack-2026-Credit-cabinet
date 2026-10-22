// mock-api — эмулятор внешних систем для локальной разработки и демо.
// Каждая система — отдельный пакет в корне модуля со своим префиксом пути.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"

	bki "github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/mock-api/BKI"
	bank "github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/mock-api/Bank"
	compliance "github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/mock-api/Compliance"
	corebanking "github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/mock-api/CoreBanking"
	fns "github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/mock-api/FNS"
	fssp "github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/mock-api/FSSP"
	fedresurs "github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/mock-api/Fedresurs"
	kad "github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/mock-api/KAD"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/mock-api/internal/mockkit"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(log); err != nil {
		log.Error("mock-api stopped", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	addr := envOr("HTTP_ADDR", ":8090")
	faultTimeout, err := time.ParseDuration(envOr("MOCK_FAULT_TIMEOUT", "30s"))
	if err != nil {
		return errors.New("MOCK_FAULT_TIMEOUT: " + err.Error())
	}

	router := newRouter(faultTimeout, gin.Logger(), gin.Recovery())

	srv := &http.Server{
		Addr:              addr,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Info("mock-api listening", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}

// newRouter собирает все системы. faultTimeout — сколько «висеть» на X-Mock-Fault: timeout;
// middleware (логи, recovery) ставятся до маршрутов, иначе gin их к ним не применит.
func newRouter(faultTimeout time.Duration, middleware ...gin.HandlerFunc) *gin.Engine {
	router := gin.New()
	router.Use(middleware...)
	router.GET("/healthz", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok"}) })

	// Bank — старый мок, его вызывает data-collector; X-Mock-Fault на него не действует.
	bank.Register(router.Group("/bank/v1"))

	faults := mockkit.Middleware(faultTimeout, nil)
	corebanking.Register(router.Group("/core-banking/v1", faults))
	fns.Register(router.Group("/fns", mockkit.Middleware(faultTimeout, fns.Captcha)))
	bki.Register(router.Group("/bki/v1", faults))
	compliance.Register(router.Group("/compliance/v1", faults))
	fssp.Register(router.Group("/fssp/v1", faults))
	kad.Register(router.Group("/kad/v1", faults))
	fedresurs.Register(router.Group("/fedresurs/v1", faults))
	return router
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
