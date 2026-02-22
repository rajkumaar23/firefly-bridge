package firefly

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/oapi-codegen/oapi-codegen/v2/pkg/securityprovider"
	"github.com/rajkumaar23/firefly-bridge/internal/market"
)

func NewAPIClient(ctx context.Context, host, token string) (*ClientWithResponses, error) {
	ffToken, err := securityprovider.NewSecurityProviderBearerToken(token)
	if err != nil {
		return nil, fmt.Errorf("failed to create security provider: %w", err)
	}
	client := &http.Client{
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	ff, err := NewClientWithResponses(
		fmt.Sprintf("%s/api", strings.TrimSuffix(host, "/")),
		WithHTTPClient(client),
		WithRequestEditorFn(ffToken.Intercept),
		WithRequestEditorFn(func(_ context.Context, req *http.Request) error {
			req.Header.Set("Accept", "application/json")
			return nil
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create firefly client: %v", err)
	}

	err = ff.VerifyConnection(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to verify connection to firefly: %w", err)
	}

	return ff, nil
}

func (ff *ClientWithResponses) VerifyConnection(ctx context.Context) error {
	ffSysInfo, err := ff.GetAboutWithResponse(ctx, &GetAboutParams{})
	if err != nil {
		return err
	}
	if ffSysInfo.JSON200 == nil {
		return fmt.Errorf("got unexpected status code: %s", ffSysInfo.Status())
	}
	return nil
}

type FireflyHoldings map[string]float64

// GetHoldings parses the holdings from the account's notes field and returns them as a map of symbol to quantity
// The notes field is expected to be in the format "{prefix:}symbol=quantity,{prefix:}symbol2=quantity2,...". If the notes field is empty or nil, this returns nil with no error.
// Each symbol is expected to be a string and each quantity is expected to be a float. If any quantity cannot be parsed as a float, this returns an error.
func (acc *AccountRead) GetHoldings() (*FireflyHoldings, error) {
	notes := acc.Attributes.Notes
	if notes == nil || *notes == "" {
		return nil, nil
	}

	holdings := make(FireflyHoldings)
	holdingsStr := strings.Split(*notes, ",")
	for _, holding := range holdingsStr {
		holdingSplit := strings.Split(holding, "=")
		symbol := strings.TrimSpace(holdingSplit[0])
		qty, err := strconv.ParseFloat(holdingSplit[1], 64)
		if err != nil {
			return nil, fmt.Errorf("error parsing quantity for %s: %w", symbol, err)
		}
		holdings[symbol] = qty
	}

	return &holdings, nil
}

func (h *FireflyHoldings) GetTotalValue(market *market.Market) (float64, error) {
	var total float64
	for symbolWithPrefix, qty := range *h {
		symbolSplit := strings.SplitN(symbolWithPrefix, ":", 2)
		var marketID, symbol string
		if len(symbolSplit) != 2 {
			symbol = symbolWithPrefix
		} else {
			marketID, symbol = symbolSplit[0], symbolSplit[1]
		}
		price, err := market.GetPrice(marketID, symbol)
		if err != nil {
			return 0, fmt.Errorf("failed to get market (%s) price for %s: %w", marketID, symbol, err)
		}

		total += price * qty
	}

	return total, nil
}

// Format converts holdings to the format expected in Firefly notes field
// Format: "symbol=qty,symbol2=qty2,..." (sorted for consistent comparison)
func (h *FireflyHoldings) Format() string {
	if h == nil || len(*h) == 0 {
		return ""
	}

	var parts []string
	for symbol, qty := range *h {
		parts = append(parts, fmt.Sprintf("%s=%.8f", symbol, qty))
	}

	sort.Strings(parts) // Deterministic ordering
	return strings.Join(parts, ",")
}

// Equal compares two holdings for equality (with float epsilon)
func (h *FireflyHoldings) Equal(other *FireflyHoldings) bool {
	if h == nil && other == nil {
		return true
	}
	if h == nil || other == nil {
		return false
	}
	if len(*h) != len(*other) {
		return false
	}

	for symbol, qtyA := range *h {
		qtyB, exists := (*other)[symbol]
		if !exists {
			return false
		}
		// Use epsilon for float comparison
		if math.Abs(qtyA-qtyB) > 0.00000001 {
			return false
		}
	}

	return true
}

// GetBalance returns the current balance for the given Firefly account ID.
func (ff *ClientWithResponses) GetBalance(ctx context.Context, accountID int) (float64, error) {
	res, err := ff.GetAccountWithResponse(ctx, strconv.Itoa(accountID), &GetAccountParams{})
	if err != nil {
		return 0, err
	}
	if res.ApplicationvndApiJSON200 == nil {
		return 0, fmt.Errorf("unexpected status code: (%s) %s", res.Status(), res.Body)
	}
	balance, err := strconv.ParseFloat(*res.ApplicationvndApiJSON200.Data.Attributes.CurrentBalance, 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse balance: %w", err)
	}
	return balance, nil
}

// UpdateAccountHoldings updates the holdings (notes field) for a Firefly account
func (ff *ClientWithResponses) UpdateAccountHoldings(ctx context.Context, accountID int, holdings *FireflyHoldings) error {
	accountIDStr := strconv.Itoa(accountID)
	notesStr := holdings.Format()

	// Prepare update request
	accountUpdate := map[string]interface{}{
		"notes": &notesStr,
	}

	body, err := json.Marshal(accountUpdate)
	if err != nil {
		return fmt.Errorf("failed to marshal account update: %w", err)
	}

	res, err := ff.UpdateAccountWithBodyWithResponse(ctx, accountIDStr, &UpdateAccountParams{}, "application/json", bytes.NewBuffer(body))
	if err != nil {
		return fmt.Errorf("failed to update account: %w", err)
	}

	if res.ApplicationvndApiJSON200 == nil {
		return fmt.Errorf("unexpected status code: (%s) %s", res.Status(), res.Body)
	}

	return nil
}

// computeHashV2 is the shared implementation of the v2 transaction hash.
// Format: SHA256(lowercase("{date};{amount};{type};{description};account={accountID}"))
func computeHashV2(date time.Time, amount string, txType TransactionTypeProperty, description, accountID string) string {
	h := sha256.New()
	payload := strings.ToLower(fmt.Sprintf("%s;%s;%s;%s;account=%s", date.Format(time.DateOnly), amount, txType, description, accountID))
	h.Write([]byte(payload))
	return fmt.Sprintf("v2:%s", hex.EncodeToString(h.Sum(nil)[:]))
}

// HashV2 generates a duplicate-detection hash for an existing transaction split
// read from the Firefly API. Returns an error if the relevant account ID field is nil.
func (t *TransactionSplit) HashV2() (string, error) {
	var accountID *string
	if t.Type == Withdrawal {
		accountID = t.SourceId
	} else {
		accountID = t.DestinationId
	}
	if accountID == nil {
		return "", fmt.Errorf("account ID is nil for split type %s (description: %q)", t.Type, t.Description)
	}
	return computeHashV2(t.Date, t.Amount, t.Type, t.Description, *accountID), nil
}

// HashV2 generates a duplicate-detection hash for a transaction split being stored.
// Used before uploading to check whether the transaction already exists in Firefly.
func (t *TransactionSplitStore) HashV2() string {
	var accountID *string
	if t.Type == Withdrawal {
		accountID = t.SourceId
	} else {
		accountID = t.DestinationId
	}
	return computeHashV2(t.Date, t.Amount, t.Type, t.Description, *accountID)
}

// TransactionExists checks if a transaction with the same hash already exists in Firefly.
// This is used to avoid creating duplicate transactions in Firefly.
func (ff *ClientWithResponses) TransactionExists(ctx context.Context, transaction *TransactionSplitStore) (bool, error) {
	res, err := ff.SearchTransactionsWithResponse(ctx, &SearchTransactionsParams{Query: fmt.Sprintf("internal_reference_is:\"%s\"", *transaction.InternalReference)})
	if err != nil {
		return false, fmt.Errorf("failed to check if transaction exists: %w", err)
	}
	if res.ApplicationvndApiJSON200 == nil {
		return false, fmt.Errorf("got unexpected status code: %s", res.Status())
	}
	return len(res.ApplicationvndApiJSON200.Data) > 0, nil
}
