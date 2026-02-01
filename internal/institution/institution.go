package institution

type AccountType string

const (
	AccountTypeRegular    AccountType = "regular"
	AccountTypeInvestment AccountType = "investment"
)

type Account struct {
	Name             string
	FireflyAccountID int
	AccountType      AccountType
}

type Institution struct {
	Name     string
	Accounts []Account
}
