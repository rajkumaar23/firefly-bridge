package config

import (
	"fmt"
	"os"
	"reflect"

	"github.com/go-playground/validator/v10"
	"github.com/rajkumaar23/firefly-bridge/internal/chromedp"
	"github.com/rajkumaar23/firefly-bridge/internal/institution"
	"github.com/rajkumaar23/firefly-bridge/internal/secrets"
	"gopkg.in/yaml.v3"
)

type FireflyConfig struct {
	Host  string `yaml:"host" validate:"http_url"`
	Token string `yaml:"token" validate:"jwt"`
}

type Config struct {
	Firefly         FireflyConfig             `yaml:"firefly" validate:"required"`
	Secrets         *secrets.SecretsConfig    `yaml:"secrets,omitempty"`
	BrowserExecPath string                    `yaml:"browser_exec_path" validate:"file"`
	Institutions    []institution.Institution `yaml:"institutions" validate:"min=1,dive"`
}

func (c *Config) Validate() error {
	validate := validator.New(validator.WithRequiredStructEnabled())

	validate.RegisterCustomTypeFunc(func(field reflect.Value) interface{} {
		if step, ok := field.Interface().(chromedp.BrowserStep); ok {
			return step.Step
		}
		return nil
	}, chromedp.BrowserStep{})

	// Register custom validation for accounts
	validate.RegisterStructValidation(accountStructLevelValidation, institution.Account{})

	if err := validate.Struct(c); err != nil {
		return err
	}
	return nil
}

func (c *Config) GetDownloadCount() int {
	count := 0
	for _, i := range c.Institutions {
		for _, a := range i.Accounts {
			for _, s := range a.TransactionsFlow {
				if s.Step.Type() == chromedp.StepGetTransactions {
					count++
				}
			}
		}
	}
	return count
}

func NewConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal yaml: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("failed to validate: %w", err)
	}

	return &cfg, nil
}

func accountStructLevelValidation(sl validator.StructLevel) {
	account := sl.Current().Interface().(institution.Account)

	switch account.AccountType {
	case institution.AccountTypeRegular:
		if len(account.BalanceFlow) == 0 {
			sl.ReportError(account.BalanceFlow, "balance", "BalanceFlow", "required_for_regular", "")
		} else {
			// Validate that balance flow ends with a balance step
			lastStep := account.BalanceFlow[len(account.BalanceFlow)-1]
			if lastStep.Step.Type() != chromedp.StepGetBalance {
				sl.ReportError(account.BalanceFlow, "balance", "BalanceFlow", "must_end_with_balance_step", "")
			}
		}

		if len(account.TransactionsFlow) == 0 {
			sl.ReportError(account.TransactionsFlow, "transactions", "TransactionsFlow", "required_for_regular", "")
		} else {
			// Validate that transactions flow has at least 1 transaction step
			hasTransactionStep := false
			for _, step := range account.TransactionsFlow {
				if step.Step.Type() == chromedp.StepGetTransactions {
					hasTransactionStep = true
					break
				}
			}
			if !hasTransactionStep {
				sl.ReportError(account.TransactionsFlow, "transactions", "TransactionsFlow", "must_have_transaction_step", "")
			}
		}

	case institution.AccountTypeInvestment:
		if len(account.HoldingsFlow) == 0 {
			sl.ReportError(account.HoldingsFlow, "holdings", "HoldingsFlow", "required_for_investment", "")
		} else {
			// Validate that holdings flow has at least 1 holding step
			hasHoldingStep := false
			for _, step := range account.HoldingsFlow {
				if step.Step.Type() == chromedp.StepGetHoldings {
					hasHoldingStep = true
					break
				}
			}
			if !hasHoldingStep {
				sl.ReportError(account.HoldingsFlow, "holdings", "HoldingsFlow", "must_have_holding_step", "")
			}
		}
	}
}
