package firefly

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/oapi-codegen/oapi-codegen/v2/pkg/securityprovider"
	"github.com/rajkumaar23/firefly-bridge/internal/market"
)

func NewFireflyClient(ctx context.Context, host, token string) (*ClientWithResponses, error) {
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
		host,
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

// HashV2 generates a hash of the transaction using its date, description, amount, type and account ID (source for withdrawals and destination for deposits).
// This is used to check if a transaction already exists in Firefly before creating it, to avoid duplicates.
func (t *TransactionSplit) HashV2() string {
	h := sha256.New()
	var accountID *string
	if t.Type == Withdrawal {
		accountID = t.SourceId
	} else {
		accountID = t.DestinationId
	}
	payload := strings.ToLower(fmt.Sprintf("%s;%s;%s;%s;account=%s", t.Date.Format(time.DateOnly), t.Amount, t.Type, t.Description, *accountID))
	h.Write([]byte(payload))
	return fmt.Sprintf("v2:%s", hex.EncodeToString(h.Sum(nil)[:]))
}

// TransactionExists checks if a transaction with the same hash already exists in Firefly. 
// This is used to avoid creating duplicate transactions in Firefly.
func (ff *ClientWithResponses) TransactionExists(ctx context.Context, transaction *TransactionSplit) (bool, error) {
	res, err := ff.SearchTransactionsWithResponse(ctx, &SearchTransactionsParams{Query: fmt.Sprintf("internal_reference_is:\"%s\"", transaction.HashV2())})
	if err != nil {
		return false, fmt.Errorf("failed to check if transaction exists: %w", err)
	}
	if res.ApplicationvndApiJSON200 == nil {
		return false, fmt.Errorf("got unexpected status code: %s", res.Status())
	}
	return len(res.ApplicationvndApiJSON200.Data) > 0, nil
}
