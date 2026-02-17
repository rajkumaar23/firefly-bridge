package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"time"

	"github.com/rajkumaar23/firefly-bridge/internal/chromedp"
	"github.com/rajkumaar23/firefly-bridge/internal/config"
	"github.com/rajkumaar23/firefly-bridge/internal/firefly"
	"github.com/rajkumaar23/firefly-bridge/internal/institution"
	"github.com/rajkumaar23/firefly-bridge/internal/secrets"
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

	secretManager, err := secrets.NewManagerFromConfig(ctx, cfg.Secrets)
	if err != nil {
		logger.Panicf("failed to create secret manager: %s", err.Error())
	}
	logger.Debug("initialized secret manager")

	ff, err := firefly.NewAPIClient(ctx, cfg.Firefly.Host, cfg.Firefly.Token)
	if err != nil {
		logger.Panicf("failed to create firefly client: %s", err.Error())
	}
	logger.Debug("verified connection to firefly")

	cdp, err := chromedp.NewChromeDP(ctx, logger, cfg.BrowserExecPath, cfg.GetDownloadCount(), *cdpDebug, secretManager)
	if err != nil {
		logger.Panicf("failed to setup chromedp: %s", err.Error())
	}
	defer cdp.Close()
	logger.Debug("chromedp setup complete")

	fireflyTag := fmt.Sprintf("firefly-bridge-%s", time.Now().Format(time.RFC3339))
	totalUploadCount := 0
	defer func() {
		if totalUploadCount > 0 {
			logrus.Infof("%d transactions uploaded at %s/tags/show/%s", totalUploadCount, strings.TrimSuffix(cfg.Firefly.Host, "/"), strings.ReplaceAll(fireflyTag, " ", "%20"))
		}
	}()

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	go func() {
		for sig := range c {
			logrus.Panicf("SIGINT received %s", sig.String())
		}
	}()

	for _, i := range cfg.Institutions {
		if err = i.Login(cdp); err != nil {
			logger.Panicf("failed to login to %s: %s", i.Name, err.Error())
		}
		logger.Debugf("logged in to '%s' successfully", i.Name)
		for _, a := range i.Accounts {
			if a.AccountType == institution.AccountTypeInvestment {
				if err := processInvestmentAccount(ctx, logger, cdp, ff, i.Name, &a); err != nil {
					logger.Panicf("failed to process investment account '%s - %s': %s", i.Name, a.Name, err.Error())
				}
			} else {
				if err := processRegularAccount(ctx, logger, cdp, ff, i.Name, &a, &totalUploadCount, fireflyTag); err != nil {
					logger.Panicf("failed to process regular account '%s - %s': %s", i.Name, a.Name, err.Error())
				}
			}
		}
	}
}

// processInvestmentAccount handles investment account synchronization
func processInvestmentAccount(ctx context.Context, logger *logrus.Logger, cdp *chromedp.ChromeDP, ff *firefly.ClientWithResponses, institutionName string, account *institution.Account) error {
	holdings, err := account.GetHoldings(cdp)
	if err != nil {
		return fmt.Errorf("failed to get holdings: %w", err)
	}
	logger.Infof("got %d holdings for '%s - %s'", len(*holdings), institutionName, account.Name)

	for symbol, qty := range *holdings {
		logger.Debugf("  %s = %.8f", symbol, qty)
	}

	accountIDStr := strconv.Itoa(account.FireflyAccountID)
	res, err := ff.GetAccountWithResponse(ctx, accountIDStr, &firefly.GetAccountParams{})
	if err != nil {
		return fmt.Errorf("failed to get firefly account: %w", err)
	}
	if res.ApplicationvndApiJSON200 == nil {
		return fmt.Errorf("unexpected status code: (%s) %s", res.Status(), res.Body)
	}

	currentHoldings, err := res.ApplicationvndApiJSON200.Data.GetHoldings()
	if err != nil {
		return fmt.Errorf("failed to parse current holdings: %w", err)
	}

	if holdings.Equal(currentHoldings) {
		logger.Infof("holdings unchanged for '%s - %s', skipping update", institutionName, account.Name)
		return nil
	}

	logger.Infof("holdings changed for '%s - %s':", institutionName, account.Name)
	for symbol, newQty := range *holdings {
		oldQty := float64(0)
		if currentHoldings != nil {
			oldQty = (*currentHoldings)[symbol]
		}
		if oldQty == 0 {
			logger.Infof("  %s: new holding %.8f", symbol, newQty)
		} else if math.Abs(oldQty-newQty) > 0.00000001 {
			logger.Infof("  %s: %.8f → %.8f (Δ %.8f)", symbol, oldQty, newQty, newQty-oldQty)
		}
	}
	if currentHoldings != nil {
		for symbol, oldQty := range *currentHoldings {
			if _, exists := (*holdings)[symbol]; !exists {
				logger.Infof("  %s: %.8f → removed", symbol, oldQty)
			}
		}
	}

	if err := ff.UpdateAccountHoldings(ctx, account.FireflyAccountID, holdings); err != nil {
		return fmt.Errorf("failed to update holdings: %w", err)
	}
	logger.Infof("updated holdings for '%s - %s'", institutionName, account.Name)

	return nil
}

// processRegularAccount handles regular account synchronization
func processRegularAccount(ctx context.Context, logger *logrus.Logger, cdp *chromedp.ChromeDP, ff *firefly.ClientWithResponses, institutionName string, account *institution.Account, totalUploadCount *int, fireflyTag string) error {
	balance, err := account.GetBalance(cdp)
	if err != nil {
		return fmt.Errorf("failed to get balance: %w", err)
	}
	logger.Debugf("balance for '%s - %s': %.2f", institutionName, account.Name, balance)

	txns, err := account.GetTransactions(cdp)
	if err != nil {
		return fmt.Errorf("failed to get transactions: %w", err)
	}
	logger.Debugf("got %d transactions for '%s - %s'", len(txns), institutionName, account.Name)

	var filtered []*firefly.TransactionSplitStore
	for _, t := range txns {
		exists, err := ff.TransactionExists(ctx, t)
		if err != nil {
			return fmt.Errorf("failed to check if transaction exists: (%s, %s, %s, %s): %w", t.Date.Format(time.DateOnly), t.Description, t.Amount, t.Type, err)
		}
		alreadyExistsMsg := ""
		if !exists {
			filtered = append(filtered, t)
		} else {
			alreadyExistsMsg = "(already exists)"
		}
		logger.Debugf("transaction %s for '%s - %s': (%s, %s, %s, %s)", alreadyExistsMsg, institutionName, account.Name, t.Date.Format(time.DateOnly), t.Description, t.Amount, t.Type)
	}

	logger.Debugf("got %d filtered transactions for '%s - %s'", len(filtered), institutionName, account.Name)

	for _, t := range filtered {
		t.Tags = &[]string{fireflyTag}
		//TODO: optionally use ollama here to identify category of transaction
		res, err := ff.StoreTransaction(ctx, &firefly.StoreTransactionParams{}, firefly.StoreTransactionJSONRequestBody{Transactions: []firefly.TransactionSplitStore{*t}})
		if err != nil {
			return fmt.Errorf("failed to store transaction: (%s, %s, %s, %s): %w", t.Date.Format(time.DateOnly), t.Description, t.Amount, t.Type, err)
		}
		if res.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(res.Body)
			return fmt.Errorf("unexpected status code: (%s, %s, %s, %s): (%s) %s", t.Date.Format(time.DateOnly), t.Description, t.Amount, t.Type, res.Status, body)
		}
		logger.Debugf("stored transaction in firefly for '%s - %s': (%s, %s, %s, %s)", institutionName, account.Name, t.Date.Format(time.DateOnly), t.Description, t.Amount, t.Type)
		*totalUploadCount++
	}

	res, err := ff.GetAccountWithResponse(ctx, strconv.Itoa(account.FireflyAccountID), &firefly.GetAccountParams{})
	if err != nil {
		return fmt.Errorf("failed to check updated firefly balance: %w", err)
	}
	if res.ApplicationvndApiJSON200 == nil {
		return fmt.Errorf("unexpected status code: (%s) %s", res.Status(), res.Body)
	}
	updatedFireflyBalanceStr := res.ApplicationvndApiJSON200.Data.Attributes.CurrentBalance
	updatedFireflyBalance, err := strconv.ParseFloat(*updatedFireflyBalanceStr, 64)
	if err != nil {
		return fmt.Errorf("failed to parse updated firefly balance: %w", err)
	}
	if math.Abs(balance) != math.Abs(updatedFireflyBalance) {
		logger.Warnf("balance mismatch: firefly: %f, bank: %f", updatedFireflyBalance, balance)
	}

	return nil
}
