package mockkit

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
)

// ID — детерминированный идентификатор: prefix + 6 hex от parts.
// Один и тот же запрос получает один и тот же id.
func ID(prefix string, parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return prefix + strings.ToUpper(hex.EncodeToString(sum[:3]))
}

// Store хранит асинхронные запросы (налоговая отчётность, Госуслуги) в памяти процесса.
// После перезапуска мока запросы теряются — для эмулятора этого достаточно.
type Store[T any] struct {
	mu sync.RWMutex
	m  map[string]T
}

func NewStore[T any]() *Store[T] { return &Store[T]{m: map[string]T{}} }

func (s *Store[T]) Put(id string, v T) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[id] = v
}

func (s *Store[T]) Get(id string) (T, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.m[id]
	return v, ok
}

// RequireToken проверяет токен доступа клиента в заголовке header.
// Пустой токен или начинающийся с "invalid" — 401 с кодом code.
func RequireToken(c *gin.Context, header, code string) bool {
	t := strings.TrimSpace(c.GetHeader(header))
	if t == "" || strings.HasPrefix(t, "invalid") {
		Error(c, http.StatusUnauthorized, code, header+" is missing or invalid")
		return false
	}
	return true
}
