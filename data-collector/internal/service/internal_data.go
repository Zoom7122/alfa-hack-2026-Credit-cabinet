package service

import (
	"context"
	"regexp"
	"sync"
	"time"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/domain"
)

// BankClient — доступ к внутренним системам банка. Объявлен у потребителя:
// сервису всё равно, эмулятор это или настоящий банк.
type BankClient interface {
	Accounts(ctx context.Context, inn string) (domain.AccountsReport, error)
	Credits(ctx context.Context, inn string) (domain.CreditReport, error)
	Acquiring(ctx context.Context, inn string) (domain.AcquiringReport, error)
	SalaryProject(ctx context.Context, inn string) (domain.SalaryProjectReport, error)
	CardIndex(ctx context.Context, inn string) (domain.CardIndexReport, error)
}

var innRe = regexp.MustCompile(`^(\d{10}|\d{12})$`)

// InternalDataService собирает данные компании из внутренних систем банка.
// Ничего не хранит: только запрашивает и отдаёт.
type InternalDataService struct {
	bank BankClient
}

func NewInternalDataService(bank BankClient) *InternalDataService {
	return &InternalDataService{bank: bank}
}

// Collect опрашивает все системы банка параллельно.
// Отказ одной системы не роняет сбор: её поле остаётся nil, причина — в Sources.
// Ошибку возвращает только на некорректный ИНН (*ValidationError).
func (s *InternalDataService) Collect(ctx context.Context, inn string) (domain.InternalBankData, error) {
	if !innRe.MatchString(inn) {
		return domain.InternalBankData{}, &ValidationError{Fields: []FieldError{{
			Field:   "inn",
			Code:    "invalid_format",
			Message: "ИНН должен состоять из 10 (ЮЛ) или 12 (ИП) цифр",
		}}}
	}

	data := domain.InternalBankData{INN: inn}
	tasks := []struct {
		source domain.BankSource
		fetch  func(context.Context) error
	}{
		{domain.SourceAccounts, into(s.bank.Accounts, inn, &data.Accounts)},
		{domain.SourceCredits, into(s.bank.Credits, inn, &data.Credits)},
		{domain.SourceAcquiring, into(s.bank.Acquiring, inn, &data.Acquiring)},
		{domain.SourceSalaryProject, into(s.bank.SalaryProject, inn, &data.SalaryProject)},
		{domain.SourceCardIndex, into(s.bank.CardIndex, inn, &data.CardIndex)},
	}

	// Каждая горутина пишет только в своё поле data и свой элемент results.
	results := make([]domain.SourceResult, len(tasks))
	var wg sync.WaitGroup
	for i, t := range tasks {
		wg.Go(func() {
			start := time.Now()
			err := t.fetch(ctx)
			res := domain.SourceResult{Source: t.source, State: domain.SourceDone, Duration: time.Since(start)}
			if err != nil {
				res.State = domain.SourceFailed
				res.Error = err.Error()
			}
			results[i] = res
		})
	}
	wg.Wait()

	data.Sources = results
	return data, nil
}

// into превращает метод клиента в задачу, которая кладёт результат в dst.
func into[T any](fetch func(context.Context, string) (T, error), inn string, dst **T) func(context.Context) error {
	return func(ctx context.Context) error {
		v, err := fetch(ctx, inn)
		if err != nil {
			return err
		}
		*dst = &v
		return nil
	}
}
