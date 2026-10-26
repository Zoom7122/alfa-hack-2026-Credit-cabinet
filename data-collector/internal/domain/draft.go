package domain

import (
	"encoding/json"
	"time"
)

// SavedDraft — сохранённые поля ещё не отправленной анкеты.
// Отсутствующее поле означает отсутствие данных, а не значение по умолчанию.
type SavedDraft struct {
	Data      json.RawMessage
	Version   int
	UpdatedAt time.Time
}
