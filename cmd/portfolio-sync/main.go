package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/rajkumaar23/firefly-bridge/internal/firefly"
	"github.com/rajkumaar23/firefly-bridge/internal/market"
	"github.com/rajkumaar23/firefly-bridge/internal/utils"
	"github.com/sirupsen/logrus"
)

func main() {
	var debug = flag.Bool("debug", false, "enable debug logs")
	var baseURL = flag.String("base-url", "", "firefly base url (eg: http://firefly.lan.example.com/api), alternative to setting it via environment $FIREFLY_BASE_URL")
	var token = flag.String("token", "", "firefly access token, alternative to setting it via environment $FIREFLY_TOKEN")
	var defaultCategory = flag.String("category", "Savings & Investments", "default category for transactions created by this tool")
	flag.Parse()

	logger := logrus.New()
	logger.SetFormatter(&logrus.TextFormatter{FullTimestamp: true, ForceColors: true})
	if *debug {
		logger.SetLevel(logrus.DebugLevel)
		logger.Debugf("log level set to debug")
	}

	ctx := context.Background()
	ctx = utils.WithLogger(ctx, logger)

	if baseURL == nil || *baseURL == "" {
		if envBaseURL := os.Getenv("FIREFLY_BASE_URL"); envBaseURL != "" {
			baseURL = &envBaseURL
			logger.Debugf("using firefly base url from environment variable: %s", *baseURL)
		} else {
			logger.Panicf("firefly base url not provided, set it via --base-url flag or $FIREFLY_BASE_URL environment variable")
		}
	}
	if token == nil || *token == "" {
		if envToken := os.Getenv("FIREFLY_TOKEN"); envToken != "" {
			token = &envToken
			logger.Debug("using firefly token from environment variable")
		} else {
			logger.Panicf("firefly token not provided, set it via --token flag or $FIREFLY_TOKEN environment variable")
		}
	}

	ff, err := firefly.NewFireflyClient(ctx, *baseURL, *token)
	if err != nil {
		logger.Panicf("failed to create firefly client: %s", err.Error())
	}
	logger.Debug("verified connection to firefly")

	accountTypeFilter := firefly.AccountTypeFilterAsset
	accounts, err := ff.ListAccountWithResponse(ctx, &firefly.ListAccountParams{Type: &accountTypeFilter})
	if err != nil {
		logger.Panicf("failed to get accounts: %s", err.Error())
	}
	if accounts.ApplicationvndApiJSON200 == nil {
		logger.Panicf("failed to get accounts: %s", accounts.Status())
	}
	logger.Debugf("got %d accounts", len(accounts.ApplicationvndApiJSON200.Data))

	market := market.NewMarket()
	errors := make([]error, 0)

	slices.SortFunc(accounts.ApplicationvndApiJSON200.Data, func(a firefly.AccountRead, b firefly.AccountRead) int {
		return strings.Compare(a.Attributes.Name, b.Attributes.Name)
	})

	for _, account := range accounts.ApplicationvndApiJSON200.Data {
		notes := account.Attributes.Notes
		if notes == nil || *notes == "" {
			logger.Debugf("skipping account %s since it has no notes", account.Attributes.Name)
			continue
		}

		logger.Debugf("found account %s with notes: %s", account.Attributes.Name, *notes)
		holdings, err := account.GetHoldings()
		if err != nil {
			err = fmt.Errorf("failed to get holdings for account %s: %w", account.Attributes.Name, err)
			logger.Error(err.Error())
			errors = append(errors, err)
			continue
		}
		if holdings == nil {
			err = fmt.Errorf("no holdings found for account %s", account.Attributes.Name)
			logger.Error(err.Error())
			errors = append(errors, err)
			continue
		}

		totalValue, err := holdings.GetTotalValue(market)
		if err != nil {
			err = fmt.Errorf("failed to get total value for account %s: %w", account.Attributes.Name, err)
			logger.Error(err.Error())
			errors = append(errors, err)
			continue
		}

		currentBalance, err := strconv.ParseFloat(*account.Attributes.CurrentBalance, 64)
		if err != nil {
			err = fmt.Errorf("failed to parse current balance for account %s: %w", account.Attributes.Name, err)
			logger.Error(err.Error())
			errors = append(errors, err)
			continue
		}

		logger.Infof("account: %s, real-time value: %.2f, firefly value: %.2f", account.Attributes.Name, totalValue, currentBalance)

		difference := totalValue - currentBalance

		if math.Abs(difference) >= 0.01 {
			transaction := firefly.TransactionSplitStore{
				Amount:       strconv.FormatFloat(math.Abs(difference), 'f', 2, 64),
				Date:         time.Now(),
				CategoryName: defaultCategory,
				Order:        new(int32),
			}
			if difference < 0 {
				transaction.Type = firefly.Withdrawal
				transaction.SourceId = &account.Id
				transaction.Description = "Loss"
			} else {
				transaction.Type = firefly.Deposit
				transaction.DestinationId = &account.Id
				transaction.Description = "Profit"
			}

			res, err := ff.StoreTransaction(ctx, &firefly.StoreTransactionParams{}, firefly.StoreTransactionJSONRequestBody{
				Transactions: []firefly.TransactionSplitStore{transaction},
			})
			if err != nil {
				err = fmt.Errorf("failed to store transaction for account %s: %w", account.Attributes.Name, err)
				logger.Error(err.Error())
				errors = append(errors, err)
				continue
			}
			if res.StatusCode != http.StatusOK {
				body, _ := io.ReadAll(res.Body)
				err = fmt.Errorf("got unexpected status code when storing transaction for account %s: (%s) %s", account.Attributes.Name, res.Status, body)
				logger.Error(err.Error())
				errors = append(errors, err)
				continue
			}
			logger.Infof("added transaction to account %s for %.2f to sync the balance", account.Attributes.Name, difference)
		}
	}
}
