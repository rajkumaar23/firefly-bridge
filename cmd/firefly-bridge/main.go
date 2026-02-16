package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/rajkumaar23/firefly-bridge/internal/chromedp"
	"github.com/rajkumaar23/firefly-bridge/internal/config"
	"github.com/rajkumaar23/firefly-bridge/internal/firefly"
	"github.com/rajkumaar23/firefly-bridge/internal/utils"
	"github.com/sirupsen/logrus"
)

func main() {
	var cdpDebug = flag.Bool("cdp-debug", false, "enable chromedp debug logs")
	var ffBridgeDebug = flag.Bool("debug", false, "enable firefly-bridge debug logs")
	var configPath = flag.String("config", "config.yaml", "path to the configuration file")
	flag.Parse()

	ctx := context.Background()

	logger := logrus.New()
	logger.SetFormatter(&logrus.TextFormatter{FullTimestamp: true, ForceColors: true})
	if *ffBridgeDebug {
		logger.SetLevel(logrus.DebugLevel)
		logger.Debugf("log level set to debug")
	}

	ctx = utils.WithLogger(ctx, logger)

	cfg, err := config.NewConfig(*configPath)
	if err != nil {
		logger.Panicf("failed to load config: %s", err.Error())
	}
	logger.Debugf("loaded config")

	ff, err := firefly.NewFireflyClient(ctx, cfg.Firefly.BaseURL, cfg.Firefly.Token)
	if err != nil {
		logger.Panicf("failed to create firefly client: %s", err.Error())
	}
	logger.Debug("verified connection to firefly")

	cdp, err := chromedp.NewChromeDP(ctx, logger, cfg.BrowserExecPath, *cdpDebug)
	if err != nil {
		logger.Panicf("failed to setup chromedp: %s", err.Error())
	}
	defer cdp.Close()
	logger.Debug("chromedp setup complete")

	fireflyTag := fmt.Sprintf("firefly-bridge-%s", time.Now().Format(time.RFC3339))

	for _, i := range cfg.Institutions {
		if err = i.Login(cdp); err != nil {
			logger.Panicf("failed to login to %s: %s", i.Name, err.Error())
		}
		logger.Debugf("logged in to '%s' successfully", i.Name)
		for _, a := range i.Accounts {
			balance, err := a.GetBalance(cdp)
			if err != nil {
				logger.Panicf("failed to get balance for '%s - %s': %s", i.Name, a.Name, err.Error())
			}
			logger.Debugf("balance for '%s - %s': %.2f", i.Name, a.Name, balance)
			txns, err := a.GetTransactions(cdp)
			if err != nil {
				logger.Panicf("failed to get transactions for '%s - %s': %s", i.Name, a.Name, err.Error())
			}
			logger.Debugf("got %d transactions for '%s - %s'", len(txns), i.Name, a.Name)

			// All transactions are filtered at once before starting upload because we DO want to allow duplicates within the transactions
			// retrieved from the institution in this current run.
			var filtered []*firefly.TransactionSplitStore
			for _, t := range txns {
				exists, err := ff.TransactionExists(ctx, t)
				if err != nil {
					logger.Panicf("failed to check if transaction exists in firefly for '%s - %s': (%s, %s, %s, %s): %s", i.Name, a.Name, t.Date.Format(time.DateOnly), t.Description, t.Amount, t.Type, err.Error())
				}
				logger.Debugf("transaction for '%s - %s': (%s, %s, %s, %s)", i.Name, a.Name, t.Date.Format(time.DateOnly), t.Description, t.Amount, t.Type)
				if !exists {
					filtered = append(filtered, t)
				} else {
					logger.Debugf("transaction for '%s - %s' already exists: (%s, %s, %s, %s)", i.Name, a.Name, t.Date.Format(time.DateOnly), t.Description, t.Amount, t.Type)
				}
			}

			logger.Debugf("got %d filtered transactions for '%s - %s'", len(filtered), i.Name, a.Name)

			for _, t := range filtered {
				t.Tags = &[]string{fireflyTag}
				//TODO: optionally use ollama here to identify category of transaction
				res, err := ff.StoreTransaction(ctx, &firefly.StoreTransactionParams{}, firefly.StoreTransactionJSONRequestBody{Transactions: []firefly.TransactionSplitStore{*t}})
				if err != nil {
					logger.Panicf("failed to store transaction in firefly for '%s - %s': (%s, %s, %s, %s): %s", i.Name, a.Name, t.Date.Format(time.DateOnly), t.Description, t.Amount, t.Type, err.Error())
				}
				if res.StatusCode != http.StatusOK {
					body, _ := io.ReadAll(res.Body)
					logger.Panicf("got expected status code when storing transaction in firefly for '%s - %s': (%s, %s, %s, %s): (%s) %s", i.Name, a.Name, t.Date.Format(time.DateOnly), t.Description, t.Amount, t.Type, res.Status, body)
				}
				logger.Debugf("stored transaction in firefly for '%s - %s': (%s, %s, %s, %s)", i.Name, a.Name, t.Date.Format(time.DateOnly), t.Description, t.Amount, t.Type)
			}
			
			// Verify the (absolute) balances are equal after syncing transactions for this account
			res, err := ff.GetAccountWithResponse(ctx, strconv.Itoa(a.FireflyAccountID), &firefly.GetAccountParams{})
			if err != nil {
				logger.Panicf("failed to check updated firefly balance for %s - %s: %s", i.Name, a.Name, err.Error())
			}
			if res.ApplicationvndApiJSON200 == nil {
				logger.Panicf("unexpected status code in checking updated firefly balance for %s - %s: (%s) %s", i.Name, a.Name, res.Status(), res.Body)
			}
			updatedFireflyBalanceStr := res.ApplicationvndApiJSON200.Data.Attributes.CurrentBalance
			updatedFireflyBalance, err := strconv.ParseFloat(*updatedFireflyBalanceStr, 64)
			if err != nil {
				logger.Panicf("failed to parse updated firefly balance for %s - %s: %s", i.Name, a.Name, err.Error())
			}
			if math.Abs(balance) != math.Abs(updatedFireflyBalance) {
				logger.Warnf("balance mismatch: firefly: %f, bank: %f", updatedFireflyBalance, balance)
			}
		}
	}
}
