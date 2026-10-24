// Package httpapi provides the Gin transport shared by the two executables.
package httpapi

import (
	"context"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/contracts"
	"github.com/gin-gonic/gin"
)

func NewRouter() *gin.Engine {
	router := gin.New()
	// Gin's default recovery dump can contain request headers and borrower data.
	router.Use(gin.CustomRecoveryWithWriter(io.Discard, func(c *gin.Context, _ any) {
		c.AbortWithStatusJSON(http.StatusInternalServerError, contracts.Problem{
			Type: "about:blank", Title: "Internal Server Error", Status: http.StatusInternalServerError,
			Code: "INTERNAL_ERROR", Detail: "Internal server error", Fields: []string{},
		})
	}))
	router.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, contracts.Health{Status: "ok"})
	})
	return router
}

func NewServer(addr string, writeTimeout time.Duration, handler http.Handler) *http.Server {
	return &http.Server{
		Addr: addr, Handler: handler, WriteTimeout: writeTimeout,
		ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second,
	}
}

// Serve binds a listener only when explicitly called by an executable.
// Constructors and handler tests never start a server.
func Serve(ctx context.Context, server *http.Server) error {
	if ctx.Err() != nil {
		return nil
	}
	serveErr := make(chan error, 1)
	go func() { serveErr <- server.ListenAndServe() }()
	select {
	case err := <-serveErr:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		shutdownErr := server.Shutdown(shutdownCtx)
		if shutdownErr != nil {
			shutdownErr = errors.Join(shutdownErr, server.Close())
		}
		err := <-serveErr
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		return errors.Join(shutdownErr, err)
	}
}
