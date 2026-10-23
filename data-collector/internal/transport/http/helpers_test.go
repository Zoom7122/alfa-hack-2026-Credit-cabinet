package httptransport_test

import (
	"context"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/domain"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/repository/memory"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/service"
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

// stubSystems — внешние системы: маркетплейсы без токена пропущены, ФССП не ответила, остальные DONE.
type stubSystems struct{}

func (stubSystems) CollectAll(_ context.Context, _ string, in domain.CollectInput) []domain.SystemResult {
	out := make([]domain.SystemResult, 0, len(domain.Systems))
	for _, sys := range domain.Systems {
		res := domain.SystemResult{System: sys, State: domain.CollectDone, Duration: 3 * time.Millisecond,
			Calls: []domain.Call{{Key: "k", Status: 200}}, Data: []byte(`{"inn":"` + in.INN + `"}`)}
		switch sys {
		case domain.SystemMarketplaces:
			res = domain.SystemResult{System: sys, State: domain.CollectSkipped, Reason: "нет токенов продавца marketplace_tokens"}
		case domain.SystemFSSP:
			res = domain.SystemResult{System: sys, State: domain.CollectFailed, Reason: "company: status 503",
				Calls: []domain.Call{{Key: "company", Status: 503, Error: "status 503"}}}
		}
		out = append(out, res)
	}
	return out
}

// newQuestionnaireService — сервис анкет со сбором через stubSystems и хранилищами в памяти.
func newQuestionnaireService(repo *memory.QuestionnaireRepository, raw *memory.RawCollectionRepository, opts ...service.Option) *service.QuestionnaireService {
	collection := service.NewCollectionService(stubSystems{}, raw, time.Second,
		service.WithCollectionClock(func() time.Time { return time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC) }),
		service.WithCollectionIDGenerator(func() string { return "col_test" }),
	)
	return service.NewQuestionnaireService(repo, collection, opts...)
}
