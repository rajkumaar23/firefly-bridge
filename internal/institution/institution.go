package institution

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
	Name     string    `yaml:"name" validate:"required"`
	Accounts []Account `yaml:"accounts" validate:"min=1,dive"`
}
