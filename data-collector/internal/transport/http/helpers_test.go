package httptransport_test

import (
	"context"
	"io"
	"log/slog"
	"os"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/domain"
)

var discardLog = slog.New(slog.NewTextHandler(io.Discard, nil))

func TestMain(m *testing.M) {
	gin.SetMode(gin.TestMode)
	gin.DefaultWriter = io.Discard // без строк access-лога в выводе тестов
	os.Exit(m.Run())
}

// stubInternalData — сервис внутренних данных с заранее заданным ответом.
type stubInternalData struct {
	data domain.InternalBankData
	err  error
}

func (s stubInternalData) Collect(_ context.Context, inn string) (domain.InternalBankData, error) {
	if s.err != nil {
		return domain.InternalBankData{}, s.err
	}
	d := s.data
	d.INN = inn
	return d, nil
}
