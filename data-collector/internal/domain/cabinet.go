package domain

import (
	"encoding/json"
	"time"
)

// Что кабинет клиента читает о своих компаниях и заявках: профиль компании, журнал заявки.

// CompanyProfile — компания пользователя с его ролью и реквизитами из ЕГРЮЛ (после сбора или подсказки).
type CompanyProfile struct {
	ID      string // apl_…
	INN     string
	Type    CompanyType
	Name    string
	OGRN    string
	Card    *CompanyCard // companies.registry; нет — реквизиты ещё не собирали
	Role    RepresentativeRole
	CanSign bool
}

// ApplicationEvent — строка журнала заявки (application_events).
type ApplicationEvent struct {
	ID      string // evt_…
	At      time.Time
	Type    string // application.created, application.submitted, enrich.*, document.signatures_verified, …
	From    ApplicationStatus
	To      ApplicationStatus
	ActorID string // пусто — система
	// ActorRole — роль автора: CLIENT или UNDERWRITER; пусто — система.
	ActorRole UserRole
	Message   string          // пусто — текста для клиента нет
	Payload   json.RawMessage // подробности события (служебные: комментарий, поправка к скорингу)
}

// ApplicationFilter — какие заявки отдаёт список кабинета. Черновики коллег в список не попадают никогда:
// черновик видит и отправляет только его автор.
type ApplicationFilter struct {
	UserID    string
	CompanyID string // пусто — все компании пользователя
	Statuses  []ApplicationStatus
	OnlyMine  bool // только заявки, которые оформил сам пользователь
	Limit     int
}
