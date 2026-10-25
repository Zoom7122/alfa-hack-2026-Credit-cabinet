package domain

import (
	"errors"
	"time"
)

// Сбор данных идёт асинхронно: POST /questionnaires создаёт задачу (и событие о ней),
// worker по событию из Kafka опрашивает системы и сохраняет результат.

// TaskStatus — состояние задачи на сбор.
type TaskStatus string

const (
	TaskPending   TaskStatus = "PENDING"   // создана, ждёт worker
	TaskRunning   TaskStatus = "RUNNING"   // worker опрашивает системы
	TaskCompleted TaskStatus = "COMPLETED" // сбор сохранён в raw_collections
	TaskFailed    TaskStatus = "FAILED"    // не выполнена: кончились попытки или сбор упал
)

var (
	// ErrTaskNotRunning — задача уже не выполняется: результат записан при повторной
	// доставке сообщения или задача снята. Сохранять второй результат не нужно.
	ErrTaskNotRunning = errors.New("collection task is not running")
	// ErrAttemptsExhausted — worker брал задачу слишком много раз (падал посреди сбора).
	ErrAttemptsExhausted = errors.New("превышено число попыток сбора")
)

// CollectionTask — задача на сбор данных о компании по анкете.
type CollectionTask struct {
	ID              string // tsk_<ULID>
	ApplicationID   string // app_…; пусто у задач, созданных до заявок
	QuestionnaireID string
	DocumentIDs     []string     // документы, пришедшие с анкетой
	Input           CollectInput // нормализованные данные для сбора; токены — только пока задача не завершена
	Status          TaskStatus
	Skipped         []System // системы без входных данных, в порядке Systems: не опрашиваются
	Attempts        int      // сколько раз worker брал задачу
	Error           string   // почему FAILED
	CreatedAt       time.Time
	StartedAt       time.Time      // нулевое — worker ещё не брал
	FinishedAt      time.Time      // нулевое — не завершена
	Result          *RawCollection // итог сбора: только у COMPLETED и только при чтении задачи
}
