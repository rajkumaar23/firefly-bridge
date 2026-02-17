package institution

import (
	"fmt"
	"strconv"

	"github.com/rajkumaar23/firefly-bridge/internal/chromedp"
	"github.com/rajkumaar23/firefly-bridge/internal/firefly"
	"github.com/rajkumaar23/firefly-bridge/internal/utils"
)

type AccountType string

const (
	AccountTypeRegular    AccountType = "regular"
	AccountTypeInvestment AccountType = "investment"
)

type Account struct {
	Name             string      `yaml:"name" validate:"required"`
	FireflyAccountID int         `yaml:"firefly_account_id" validate:"required"`
	AccountType      AccountType `yaml:"account_type" validate:"oneof=regular investment"`
	BalanceFlow      []chromedp.BrowserStep `yaml:"balance" validate:"dive"`
	TransactionsFlow []chromedp.BrowserStep `yaml:"transactions" validate:"dive"`
	HoldingsFlow     []chromedp.BrowserStep `yaml:"holdings" validate:"dive"`
}

func (a *Account) GetBalance(cdp *chromedp.ChromeDP) (float64, error) {
	results, err := cdp.RunSteps(a.BalanceFlow)
	if err != nil {
		return 0, err
	}

	balanceStr, ok := results[chromedp.StepGetBalance].(string)
	if !ok {
		return 0, fmt.Errorf("failed to retrieve balance")
	}

	return utils.ParseAmountFromString(balanceStr)
}

func (a *Account) GetTransactions(cdp *chromedp.ChromeDP) ([]*firefly.TransactionSplitStore, error) {
	results, err := cdp.RunSteps(a.TransactionsFlow)
	if err != nil {
		return nil, err
	}

	txns, ok := results[chromedp.StepGetTransactions].([]*firefly.TransactionSplitStore)
	if !ok {
		return nil, fmt.Errorf("failed to retrieve transactions")
	}

	accountIDStr := strconv.Itoa(a.FireflyAccountID)
	for _, t := range txns {
		// Based on each transaction's type, set the source/destination ID with FireflyAccountID from config.
		if t.Type == firefly.Withdrawal {
			t.SourceId = &accountIDStr
		} else {
			t.DestinationId = &accountIDStr
		}

		// Set the internal reference to the hash of the transaction to lateravoid duplicates in Firefly.
		hash := t.HashV2()
		t.InternalReference = &hash

		var order int32 = 0
		t.Order = &order
	}

	return txns, nil
}

func (a *Account) GetHoldings(cdp *chromedp.ChromeDP) (*firefly.FireflyHoldings, error) {
	results, err := cdp.RunSteps(a.HoldingsFlow)
	if err != nil {
		return nil, err
	}

	holdings, ok := results[chromedp.StepGetHoldings].(*firefly.FireflyHoldings)
	if !ok {
		return nil, fmt.Errorf("failed to retrieve holdings")
	}

	return holdings, nil
}

type Institution struct {
	Name      string                 `yaml:"name" validate:"required"`
	LoginFlow []chromedp.BrowserStep `yaml:"login" validate:"min=1,dive"`
	Accounts  []Account              `yaml:"accounts" validate:"min=1,dive"`
}

func (i *Institution) Login(cdp *chromedp.ChromeDP) error {
	if _, err := cdp.RunSteps(i.LoginFlow); err != nil {
		return err
	}

	return nil
}
