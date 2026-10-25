package domain

import (
	"encoding/json"
	"time"
)

// Проверка электронной подписи документов заёмщика (worker подписи, docs/e-signature.md).
// Результат — строка signature_reports: статус каждого этапа, итог по правилам банка и подробный
// отчёт. Верная подпись подтверждает только, что файл не изменён и кто его подписал, — не
// достоверность цифр в документе и не платёжеспособность заёмщика.

// VerificationStatus — итог этапа проверки подписи и итоговое решение.
type VerificationStatus string

const (
	VerificationVerified      VerificationStatus = "VERIFIED"      // этап пройден
	VerificationInvalid       VerificationStatus = "INVALID"       // этап не пройден: подпись не годится для этого документа
	VerificationIndeterminate VerificationStatus = "INDETERMINATE" // вывод сделать нельзя: формат не поддержан, нет данных или файла
	VerificationNotRequired   VerificationStatus = "NOT_REQUIRED"  // этап не нужен по правилам банка для этого типа документа
)

// VerificationStage — этап проверки подписи.
type VerificationStage string

const (
	StageIntegrity   VerificationStage = "INTEGRITY"   // целостность и соответствие подписи исходным байтам
	StageFormat      VerificationStage = "FORMAT"      // формат подписи и охват подписанных данных
	StageCertificate VerificationStage = "CERTIFICATE" // цепочка доверия, срок действия, назначение ключа, отзыв
	StageIdentity    VerificationStage = "IDENTITY"    // личность подписанта и её связь с заявителем
	StageAuthority   VerificationStage = "AUTHORITY"   // полномочия подписанта для заявителя и типа документа
	StageDecision    VerificationStage = "DECISION"    // итог по правилам банка
)

// SignatureReport — отчёт проверки одной подписи: строка signature_reports.
type SignatureReport struct {
	ID                  string // sigr_<ULID>
	ApplicationID       string
	DocumentID          string // подписанный документ; у присоединённой подписи — сам контейнер
	SignatureDocumentID string // файл открепленной подписи; пусто — подпись внутри документа
	DocumentSHA256      string // sha256 проверенных байтов документа, посчитан до проверки
	SignatureSHA256     string // sha256 файла подписи; пусто — отдельного файла подписи нет
	Profile             string // профиль проверки: TEST
	TrustMode           string // MOCK — тестовые корни и моки реестров
	Policy              string // GENERIC, FINANCIAL_STATEMENTS, POWER_OF_ATTORNEY, LOAN_CONTRACT
	Verdict             string // VALID_WITH_WARNINGS, INVALID, INDETERMINATE — для списков и старой колонки verdict

	Integrity   VerificationStatus
	Format      VerificationStatus
	Certificate VerificationStatus
	Identity    VerificationStatus
	Authority   VerificationStatus
	Decision    VerificationStatus // итог по правилам банка

	FailureReason  string          // почему не VERIFIED: причина первого непройденного этапа
	IdempotencyKey string          // один автоматический отчёт на версию документа и подписи
	DocumentState  SignatureState  // signature_state документа и подписи после проверки: DONE или FAILED
	Report         json.RawMessage // подробный отчёт (report jsonb)
	VerifiedAt     time.Time
}

// SignatureContext — что известно о заявке для проверки подписанта: компания-заявитель,
// кто подал заявку и последний сбор данных (анкета, ответы ЕГРЮЛ и реестра МЧД).
type SignatureContext struct {
	ApplicationID  string
	Company        Company              // ИНН, тип, имя; ОГРН — после сбора, из ЕГРЮЛ
	Representative Representative       // кто подал заявку: ИНН физлица и роль
	Collection     *SignatureCollection // последняя задача на сбор; nil — сбора по заявке нет
}

// SignatureCollection — последняя задача на сбор по заявке и её результат по ЕГРЮЛ и МЧД.
type SignatureCollection struct {
	TaskID       string
	Status       TaskStatus
	Input        CollectInput    // данные анкеты для сбора: руководитель, номер МЧД, ИНН представителя
	CollectionID string          // raw_collections.id; пусто — результата ещё нет
	FNS          json.RawMessage // ответы ФНС как есть (колонка fns); nil — данных нет
	M4D          json.RawMessage // ответы реестра МЧД как есть (колонка m4d); nil — данных нет
	FNSState     CollectState    // итог опроса ФНС; пусто — результата нет
	M4DState     CollectState
}

// Finished сообщает, что сбор завершён (успешно или нет) и ждать его больше незачем.
func (c *SignatureCollection) Finished() bool {
	return c == nil || c.Status == TaskCompleted || c.Status == TaskFailed
}

// RegistryHead — руководитель компании по ЕГРЮЛ; у ИП — сам предприниматель по ЕГРИП.
type RegistryHead struct {
	INN      string // ИНН физлица; пусто — в выписке только ФИО
	FullName string
	Position string
}

// PowerOfAttorney — машиночитаемая доверенность (МЧД) из реестра.
type PowerOfAttorney struct {
	Number              string
	Status              string // ACTIVE, EXPIRED, REVOKED
	ValidFrom           string // YYYY-MM-DD
	ValidUntil          string // YYYY-MM-DD
	RevokedAt           string // YYYY-MM-DD; пусто — не отозвана
	PrincipalINN        string // доверитель — компания или ИП
	RepresentativeINN   string
	RepresentativeSNILS string
	RepresentativeName  string
	Powers              []string // коды полномочий
}

// Коды полномочий МЧД — справочник mock-api (классификатор ФНС мапится в адаптере).
const (
	POAPowerLoanApplication = "LOAN_APPLICATION_SUBMIT" // подавать заявки на кредит и сопутствующие документы
	POAPowerFinancialDocs   = "FINANCIAL_DOCS_SIGN"     // подписывать бухгалтерскую и налоговую отчётность, выписки
)
