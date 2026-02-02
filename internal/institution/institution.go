package institution

import "github.com/rajkumaar23/firefly-bridge/internal/chromedp"

type AccountType string

const (
	AccountTypeRegular    AccountType = "regular"
	AccountTypeInvestment AccountType = "investment"
)

type Account struct {
	Name             string      `yaml:"name" validate:"required"`
	FireflyAccountID int         `yaml:"firefly_account_id" validate:"required"`
	AccountType      AccountType `yaml:"account_type" validate:"oneof=regular investment"`
}

type Institution struct {
	Name      string                 `yaml:"name" validate:"required"`
	Downloads uint8                  `yaml:"downloads"`
	LoginFlow []chromedp.BrowserStep `yaml:"login" validate:"min=1,dive"`
	Accounts  []Account              `yaml:"accounts" validate:"min=1,dive"`
}
