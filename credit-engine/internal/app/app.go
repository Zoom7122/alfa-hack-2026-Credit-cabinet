// Package app assembles startup dependencies without binding ports or contacting
// external systems.
package app

import (
	"fmt"
	"net/http"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/contracts"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/bank"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/clients"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/clock"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/config"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/decision"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/economics"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/httpapi"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/modelbindings"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/optimizer"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/scoring"
)

type Scoring struct {
	Settings         config.Scoring
	Policy           config.Policy
	Economics        *economics.Calculator
	Optimizer        *optimizer.Selector
	Clock            clock.Clock
	BankHTTP         *http.Client
	StatementsHTTP   *http.Client
	FactsHTTP        *http.Client
	BankClient       *clients.BankClient
	StatementsClient *clients.StatementsClient
	FactsClient      *clients.FactsClient
	Models           *modelbindings.Catalog
	Evaluator        *scoring.Evaluator
	Decisions        *decision.Service
	Server           *http.Server
}

type Bank struct {
	Settings     config.Bank
	Policy       config.Policy
	Economics    *economics.Calculator
	InitialState contracts.BankState
	Operations   *bank.Service
	Clock        clock.Clock
	AdminHTTP    *http.Client
	AdminClient  *clients.AdminClient
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
	bankHTTP := &http.Client{Timeout: cfg.BankTimeout}
	statementsHTTP := &http.Client{Timeout: cfg.StatementsTimeout}
	bankClient, err := clients.NewBank(cfg.BankURL, bankHTTP)
	if err != nil {
		return nil, err
	}
	statementsClient, err := clients.NewStatements(cfg.StatementsURL, statementsHTTP)
	if err != nil {
		return nil, err
	}
	catalog, err := modelbindings.Default()
	if err != nil {
		return nil, err
	}
	usesFacts := false
	for _, metadata := range catalog.List() {
		usesFacts = usesFacts || metadata.UsesFacts
	}
	var factsHTTP *http.Client
	var factsClient *clients.FactsClient
	var factsSource scoring.FactsSource
	if usesFacts {
		if cfg.FactsURL == "" {
			return nil, fmt.Errorf("FACTS_URL is required by compiled models")
		}
		factsHTTP = &http.Client{Timeout: cfg.FactsTimeout}
		factsClient, err = clients.NewFacts(cfg.FactsURL, factsHTTP)
		if err != nil {
			return nil, err
		}
		factsSource = factsClient
	}
	evaluator, err := scoring.NewEvaluatorWithCatalog(statementsClient, factsSource, source, catalog)
	if err != nil {
		return nil, err
	}
	decisions, err := decision.New(policy.ID, evaluator, selector, bankClient)
	if err != nil {
		return nil, err
	}
	router := httpapi.NewScoringRouter(decisions)
	httpapi.RegisterScoringModels(router, catalog)
	return &Scoring{
		Settings: cfg, Policy: policy, Economics: calculator, Optimizer: selector, Clock: source,
		BankHTTP: bankHTTP, StatementsHTTP: statementsHTTP, BankClient: bankClient, StatementsClient: statementsClient, Evaluator: evaluator,
		FactsHTTP: factsHTTP, FactsClient: factsClient, Models: catalog,
		Decisions: decisions,
		Server:    httpapi.NewServer(cfg.Addr, cfg.WriteTimeout, router),
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
	adminHTTP := &http.Client{Timeout: cfg.AdminTimeout}
	adminClient, err := clients.NewAdmin(cfg.AdminURL, adminHTTP)
	if cfg.AdminEventsToken != "" {
		adminClient, err = clients.NewAdminWithToken(cfg.AdminURL, cfg.AdminEventsToken, adminHTTP)
	}
	if err != nil {
		return nil, err
	}
	operations, err := bank.NewService(policy, state, source, adminClient)
	if err != nil {
		return nil, err
	}
	return &Bank{
		Settings: cfg, Policy: policy, Economics: calculator, InitialState: state, Operations: operations, Clock: source,
		AdminHTTP: adminHTTP, AdminClient: adminClient,
		Server: httpapi.NewServer(cfg.Addr, cfg.WriteTimeout, httpapi.NewBankRouter(operations)),
	}, nil
}
