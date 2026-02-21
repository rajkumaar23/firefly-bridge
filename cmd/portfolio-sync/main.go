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
	var host = flag.String("host", "", "firefly host (eg: http://firefly.lan.example.com), alternative to setting it via environment $FIREFLY_HOST")
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

	if host == nil || *host == "" {
		if envHost := os.Getenv("FIREFLY_HOST"); envHost != "" {
			host = &envHost
			logger.Debugf("using firefly host from environment variable: %s", *host)
		} else {
			logger.Panicf("firefly host not provided, set it via --host flag or $FIREFLY_HOST environment variable")
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

	ff, err := firefly.NewAPIClient(ctx, *host, *token)
	if err != nil {
		logger.Panicf("failed to create firefly client: %s", err.Error())
	}
	logger.Info("verified connection to firefly")

	accountTypeFilter := firefly.AccountTypeFilterAsset
	accounts, err := ff.ListAccountWithResponse(ctx, &firefly.ListAccountParams{Type: &accountTypeFilter})
	if err != nil {
		logger.Panicf("failed to get accounts: %s", err.Error())
	}
	if accounts.ApplicationvndApiJSON200 == nil {
		logger.Panicf("failed to get accounts: %s", accounts.Status())
	}
	logger.Infof("got %d accounts", len(accounts.ApplicationvndApiJSON200.Data))

	market := market.NewMarket()
	errors := make([]error, 0)

	slices.SortFunc(accounts.ApplicationvndApiJSON200.Data, func(a firefly.AccountRead, b firefly.AccountRead) int {
		return strings.Compare(a.Attributes.Name, b.Attributes.Name)
	})

	for _, account := range accounts.ApplicationvndApiJSON200.Data {
		aLog := logger.WithField("account", account.Attributes.Name)
		notes := account.Attributes.Notes
		if notes == nil || *notes == "" {
			aLog.Debug("skipping, no notes")
			continue
		}

		aLog.Debugf("notes: %s", *notes)
		holdings, err := account.GetHoldings()
		if err != nil {
			err = fmt.Errorf("failed to get holdings: %w", err)
			aLog.Error(err.Error())
			errors = append(errors, err)
			continue
		}
		if holdings == nil {
			err = fmt.Errorf("no holdings found")
			aLog.Error(err.Error())
			errors = append(errors, err)
			continue
		}

		totalValue, err := holdings.GetTotalValue(market)
		if err != nil {
			err = fmt.Errorf("failed to get total value: %w", err)
			aLog.Error(err.Error())
			errors = append(errors, err)
			continue
		}

		currentBalance, err := strconv.ParseFloat(*account.Attributes.CurrentBalance, 64)
		if err != nil {
			err = fmt.Errorf("failed to parse current balance: %w", err)
			aLog.Error(err.Error())
			errors = append(errors, err)
			continue
		}

		aLog.Infof("real-time value: %.2f, firefly balance: %.2f", totalValue, currentBalance)

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
				err = fmt.Errorf("failed to store transaction: %w", err)
				aLog.Error(err.Error())
				errors = append(errors, err)
				continue
			}
			if res.StatusCode != http.StatusOK {
				body, _ := io.ReadAll(res.Body)
				err = fmt.Errorf("unexpected status code storing transaction: (%s) %s", res.Status, body)
				aLog.Error(err.Error())
				errors = append(errors, err)
				continue
			}
			aLog.Infof("stored '%.2f %s' transaction to sync balance", math.Abs(difference), transaction.Description)
		}
	}
}
