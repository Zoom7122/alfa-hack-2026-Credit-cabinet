// Package domain содержит сущности модуля сбора данных для скоринга.
package domain

import "time"

// MaritalStatus — семейное положение заёмщика.
type MaritalStatus string

const (
	MaritalSingle           MaritalStatus = "SINGLE"            // не замужем / не женат
	MaritalMarried          MaritalStatus = "MARRIED"           // женаты / замужем
	MaritalDivorced         MaritalStatus = "DIVORCED"          // официально разведены
	MaritalDivorcedMultiple MaritalStatus = "DIVORCED_MULTIPLE" // официально разведены 2 и более раз
)

// Valid сообщает, входит ли значение в допустимый перечень.
func (m MaritalStatus) Valid() bool {
	switch m {
	case MaritalSingle, MaritalMarried, MaritalDivorced, MaritalDivorcedMultiple:
		return true
	}
	return false
}

// Questionnaire — анкета заёмщика, заполненная пользователем.
// Возраст хранится числом лет: в бакеты его раскладывает скоринг.
type Questionnaire struct {
	ID            string
	Age           int
	MaritalStatus MaritalStatus
	ChildrenCount int // дети и/или иждивенцы
	CreatedAt     time.Time
}
