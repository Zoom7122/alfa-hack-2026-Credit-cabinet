// Package postgres — хранилище data-collector в PostgreSQL (pgx).
// Схема — в модуле db: миграции Goose накатывает сервис migrate до старта.
package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/domain"
)

// Connect открывает пул соединений и проверяет, что база доступна.
func Connect(ctx context.Context, url string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("open pool: %w", err)
	}
	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping: %w", err)
	}
	return pool, nil
}

// SystemColumns — в какую колонку raw_collections ложатся сырые ответы системы.
var SystemColumns = map[domain.System]string{
	domain.SystemCoreBanking:    "core_banking",
	domain.SystemFNS:            "fns",
	domain.SystemBKI:            "bki",
	domain.SystemCompliance:     "compliance",
	domain.SystemFSSP:           "fssp",
	domain.SystemKAD:            "kad",
	domain.SystemFedresurs:      "fedresurs",
	domain.SystemM4D:            "m4d",
	domain.SystemTaxReports:     "tax_reports",
	domain.SystemPledgeRegistry: "pledge_registry",
	domain.SystemEGRN:           "egrn",
	domain.SystemGosuslugi:      "gosuslugi",
	domain.SystemOpenBanking:    "open_banking",
	domain.SystemMarketplaces:   "marketplaces",
	domain.SystemOFD:            "ofd",
	domain.SystemZakupki:        "zakupki",
	domain.SystemSignals:        "signals",
	domain.SystemCBR:            "cbr",
	domain.SystemHH:             "hh",
}

// fixedColumns — служебные колонки raw_collections перед колонками систем.
var fixedColumns = []string{"id", "task_id", "questionnaire_id", "inn", "director_inn", "request", "sources",
	"started_at", "finished_at"}

var insertRawCollection = buildInsert()

// buildInsert собирает INSERT по domain.Systems. Система без колонки — паника при старте.
func buildInsert() string {
	cols := append([]string{}, fixedColumns...)
	for _, sys := range domain.Systems {
		col, ok := SystemColumns[sys]
		if !ok {
			panic("postgres: no raw_collections column for system " + string(sys))
		}
		cols = append(cols, col)
	}
	params := make([]string, len(cols))
	for i := range cols {
		params[i] = fmt.Sprintf("$%d", i+1)
	}
	return "INSERT INTO raw_collections (" + strings.Join(cols, ", ") + ") VALUES (" + strings.Join(params, ", ") + ")"
}

// insertArgs — значения в порядке колонок insertRawCollection.
// jsonb передаётся строкой; nil — NULL (система не дала данных).
func insertArgs(rc domain.RawCollection) ([]any, error) {
	request, err := json.Marshal(toRequestRecord(rc.Input))
	if err != nil {
		return nil, fmt.Errorf("encode request: %w", err)
	}
	sources, err := json.Marshal(toSourceRecords(rc.Systems))
	if err != nil {
		return nil, fmt.Errorf("encode sources: %w", err)
	}
	var directorINN any
	if rc.Input.Director != nil {
		directorINN = rc.Input.Director.INN
	}
	args := []any{rc.ID, rc.TaskID, rc.QuestionnaireID, rc.Input.INN, directorINN, string(request), string(sources),
		rc.StartedAt, rc.FinishedAt}

	data := make(map[domain.System]json.RawMessage, len(rc.Systems))
	for _, res := range rc.Systems {
		data[res.System] = res.Data
	}
	for _, sys := range domain.Systems {
		if raw := data[sys]; len(raw) > 0 {
			args = append(args, string(raw))
		} else {
			args = append(args, nil)
		}
	}
	return args, nil
}

// requestRecord — входные данные сбора в колонке request. Токены не храним.
type requestRecord struct {
	INN               string           `json:"inn"`
	Director          *personRecord    `json:"director,omitempty"`
	Consents          consentsRecord   `json:"consents"`
	OtherBankBIKs     []string         `json:"other_bank_biks"`
	Marketplaces      []string         `json:"marketplaces"` // площадки, на которые передан токен
	OFDTokenProvided  bool             `json:"ofd_token_provided"`
	Collateral        collateralRecord `json:"collateral"`
	POANumber         string           `json:"poa_number,omitempty"`
	RepresentativeINN string           `json:"representative_inn,omitempty"`
}

type personRecord struct {
	INN        string `json:"inn"`
	LastName   string `json:"last_name"`
	FirstName  string `json:"first_name"`
	MiddleName string `json:"middle_name,omitempty"`
	BirthDate  string `json:"birth_date"`
}

type consentRecord struct {
	ID         string   `json:"id"`
	SignedAt   string   `json:"signed_at,omitempty"`
	ValidUntil string   `json:"valid_until"`
	Scope      []string `json:"scope,omitempty"`
}

type consentsRecord struct {
	BKICompany *consentRecord `json:"bki_company,omitempty"`
	BKIPerson  *consentRecord `json:"bki_person,omitempty"`
	Tax        *consentRecord `json:"tax,omitempty"`
}

type collateralRecord struct {
	CadastralNumbers []string `json:"cadastral_numbers"`
	VINs             []string `json:"vins"`
	Serials          []string `json:"serials"`
}

func toRequestRecord(in domain.CollectInput) requestRecord {
	rec := requestRecord{
		INN:              in.INN,
		OtherBankBIKs:    nonNil(in.OtherBankBIKs),
		Marketplaces:     []string{},
		OFDTokenProvided: in.OFDToken != "",
		Consents: consentsRecord{
			BKICompany: toConsentRecord(in.BKICompanyConsent),
			BKIPerson:  toConsentRecord(in.BKIPersonConsent),
			Tax:        toConsentRecord(in.TaxConsent),
		},
		Collateral: collateralRecord{
			CadastralNumbers: nonNil(in.Collateral.CadastralNumbers),
			VINs:             nonNil(in.Collateral.VINs),
			Serials:          nonNil(in.Collateral.Serials),
		},
		POANumber:         in.POANumber,
		RepresentativeINN: in.RepresentativeINN,
	}
	if d := in.Director; d != nil {
		rec.Director = &personRecord{INN: d.INN, LastName: d.LastName, FirstName: d.FirstName,
			MiddleName: d.MiddleName, BirthDate: d.BirthDate}
	}
	for _, mp := range []string{domain.MarketplaceWildberries, domain.MarketplaceOzon} {
		if in.MarketplaceTokens[mp] != "" {
			rec.Marketplaces = append(rec.Marketplaces, mp)
		}
	}
	return rec
}

// fromRequestRecord — входные данные сбора из колонки request, токены — из secrets
// (nil — токенов нет или они уже стёрты).
func fromRequestRecord(rec requestRecord, sec *secretsRecord) domain.CollectInput {
	in := domain.CollectInput{
		INN:               rec.INN,
		BKICompanyConsent: rec.Consents.BKICompany.toDomain(),
		BKIPersonConsent:  rec.Consents.BKIPerson.toDomain(),
		TaxConsent:        rec.Consents.Tax.toDomain(),
		OtherBankBIKs:     nilIfEmpty(rec.OtherBankBIKs),
		Collateral: domain.Collateral{
			CadastralNumbers: nilIfEmpty(rec.Collateral.CadastralNumbers),
			VINs:             nilIfEmpty(rec.Collateral.VINs),
			Serials:          nilIfEmpty(rec.Collateral.Serials),
		},
		POANumber:         rec.POANumber,
		RepresentativeINN: rec.RepresentativeINN,
	}
	if d := rec.Director; d != nil {
		in.Director = &domain.Person{INN: d.INN, LastName: d.LastName, FirstName: d.FirstName,
			MiddleName: d.MiddleName, BirthDate: d.BirthDate}
	}
	if sec != nil {
		if len(sec.MarketplaceTokens) > 0 {
			in.MarketplaceTokens = sec.MarketplaceTokens
		}
		in.OFDToken = sec.OFDToken
	}
	return in
}

func toConsentRecord(c *domain.Consent) *consentRecord {
	if c == nil {
		return nil
	}
	return &consentRecord{ID: c.ID, SignedAt: c.SignedAt, ValidUntil: c.ValidUntil, Scope: c.Scope}
}

func (c *consentRecord) toDomain() *domain.Consent {
	if c == nil {
		return nil
	}
	return &domain.Consent{ID: c.ID, SignedAt: c.SignedAt, ValidUntil: c.ValidUntil, Scope: c.Scope}
}

// secretsRecord — токены в collection_tasks.secrets: нужны worker'у для запросов
// к маркетплейсам и ОФД, после завершения задачи стираются.
type secretsRecord struct {
	MarketplaceTokens map[string]string `json:"marketplace_tokens,omitempty"`
	OFDToken          string            `json:"ofd_token,omitempty"`
}

// encodeSecrets — значение колонки secrets: nil (NULL), если токенов нет.
func encodeSecrets(in domain.CollectInput) (any, error) {
	if len(in.MarketplaceTokens) == 0 && in.OFDToken == "" {
		return nil, nil
	}
	raw, err := json.Marshal(secretsRecord{MarketplaceTokens: in.MarketplaceTokens, OFDToken: in.OFDToken})
	if err != nil {
		return nil, fmt.Errorf("encode secrets: %w", err)
	}
	return string(raw), nil
}

// sourceRecord — итог по системе в колонке sources: состояние, причина и все запросы.
type sourceRecord struct {
	Source     string       `json:"source"`
	State      string       `json:"state"`
	Reason     string       `json:"reason,omitempty"`
	DurationMs int64        `json:"duration_ms"`
	Calls      []callRecord `json:"calls"`
}

type callRecord struct {
	Key        string `json:"key"`
	Method     string `json:"method"`
	Path       string `json:"path"`
	Status     int    `json:"status"`
	DurationMs int64  `json:"duration_ms"`
	Error      string `json:"error,omitempty"`
}

func toSourceRecords(results []domain.SystemResult) []sourceRecord {
	out := make([]sourceRecord, 0, len(results))
	for _, res := range results {
		calls := make([]callRecord, 0, len(res.Calls))
		for _, c := range res.Calls {
			calls = append(calls, callRecord{Key: c.Key, Method: c.Method, Path: c.Path, Status: c.Status,
				DurationMs: c.Duration.Milliseconds(), Error: c.Error})
		}
		out = append(out, sourceRecord{Source: string(res.System), State: string(res.State), Reason: res.Reason,
			DurationMs: res.Duration.Milliseconds(), Calls: calls})
	}
	return out
}

// fromSourceRecords — итог по системам из колонки sources, без сырых данных.
func fromSourceRecords(records []sourceRecord) []domain.SystemResult {
	out := make([]domain.SystemResult, 0, len(records))
	for _, rec := range records {
		res := domain.SystemResult{System: domain.System(rec.Source), State: domain.CollectState(rec.State), Reason: rec.Reason,
			Duration: time.Duration(rec.DurationMs) * time.Millisecond}
		for _, c := range rec.Calls {
			res.Calls = append(res.Calls, domain.Call{Key: c.Key, Method: c.Method, Path: c.Path, Status: c.Status,
				Duration: time.Duration(c.DurationMs) * time.Millisecond, Error: c.Error})
		}
		out = append(out, res)
	}
	return out
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func nilIfEmpty(s []string) []string {
	if len(s) == 0 {
		return nil
	}
	return s
}
