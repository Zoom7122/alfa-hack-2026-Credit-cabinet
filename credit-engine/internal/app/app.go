// Package app assembles startup dependencies without binding ports or contacting
// external systems. Business operations are added in subsequent implementation stages.
package app

import (
	"net/http"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/contracts"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/bank"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/clock"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/config"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/economics"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/httpapi"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/optimizer"
)

type Scoring struct {
	Settings       config.Scoring
	Policy         config.Policy
	Economics      *economics.Calculator
	Optimizer      *optimizer.Selector
	Clock          clock.Clock
	BankHTTP       *http.Client
	StatementsHTTP *http.Client
	Server         *http.Server
}

type Bank struct {
	Settings     config.Bank
	Policy       config.Policy
	Economics    *economics.Calculator
	InitialState contracts.BankState
	Operations   *bank.Service
	Clock        clock.Clock
	AdminHTTP    *http.Client
	Server       *http.Server
}

func NewScoring(cfg config.Scoring, source clock.Clock) (*Scoring, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	policy, err := config.LoadPolicy(cfg.PolicyFile)
	if err != nil {
		return nil, err
	}
	calculator, err := economics.NewCalculator(policy.Economics)
	if err != nil {
		return nil, err
	}
	selector, err := optimizer.New(policy)
	if err != nil {
		return nil, err
	}
	if source == nil {
		source = clock.System{}
	}
	if cfg.EvaluationDate != nil {
		source = clock.Fixed{Time: *cfg.EvaluationDate}
	}
	return &Scoring{
		Settings: cfg, Policy: policy, Economics: calculator, Optimizer: selector, Clock: source,
		BankHTTP:       &http.Client{Timeout: cfg.BankTimeout},
		StatementsHTTP: &http.Client{Timeout: cfg.StatementsTimeout},
		Server:         httpapi.NewServer(cfg.Addr, cfg.WriteTimeout, httpapi.NewRouter()),
	}, nil
}

func NewBank(cfg config.Bank, source clock.Clock) (*Bank, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	policy, err := config.LoadPolicy(cfg.PolicyFile)
	if err != nil {
		return nil, err
	}
	state, err := config.LoadBankState(cfg.StateFile)
	if err != nil {
		return nil, err
	}
	calculator, err := economics.NewCalculator(policy.Economics)
	if err != nil {
		return nil, err
	}
	if source == nil {
		source = clock.System{}
	}
	operations, err := bank.NewService(policy, state, source, nil)
	if err != nil {
		return nil, err
	}
	return &Bank{
		Settings: cfg, Policy: policy, Economics: calculator, InitialState: state, Operations: operations, Clock: source,
		AdminHTTP: &http.Client{Timeout: cfg.AdminTimeout},
		Server:    httpapi.NewServer(cfg.Addr, cfg.WriteTimeout, httpapi.NewRouter()),
	}, nil
}
