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
	"github.com/rajkumaar23/firefly-bridge/internal/state"
	"github.com/rajkumaar23/firefly-bridge/internal/utils"
	"github.com/sirupsen/logrus"
)

func main() {
	os.Exit(run())
}

func run() int {
	var cdpDebug = flag.Bool("cdp-debug", false, "enable chromedp debug logs")
	var ffBridgeDebug = flag.Bool("debug", false, "enable firefly-bridge debug logs")
	var configPath = flag.String("config", "config.yaml", "path to the configuration file")
	var statePath = flag.String("state", ".state.json", "path to the file used to track last successful run per institution")
	var force = flag.Bool("force", false, "bypass the per-institution cooldown and the per-account balance-unchanged skip, forcing a full sync of every institution and account")
	var forceSyncDays = flag.Int("sync-days", 10, "force a full transaction CSV sync for an account after this many days, even if its scraped balance matches the Firefly balance")
	var onlyInstitution = flag.String("institution", "", "run only the institution with this name, skipping all others; also bypasses cooldown and balance-unchanged checks for that institution")
	var csvDebug = flag.Bool("csv-debug", false, "log every parsed CSV row with its row number to help diagnose parsing issues")
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
	logger.Debug("loaded config")

	secretManager, err := secrets.NewManagerFromConfig(ctx, cfg.Secrets)
	if err != nil {
		logger.Panicf("failed to create secret manager: %s", err.Error())
	}
	logger.Debug("initialized secret manager")

	ff, err := firefly.NewAPIClient(ctx, cfg.Firefly.Host, cfg.Firefly.Token)
	if err != nil {
		logger.Panicf("failed to create firefly client: %s", err.Error())
	}
	logger.Info("verified connection to firefly")

	cdp, err := chromedp.NewChromeDP(ctx, logger, cfg.BrowserExecPath, cfg.GetDownloadCount(), *cdpDebug, secretManager)
	cdp.CSVDebug = *csvDebug
	if err != nil {
		logger.Panicf("failed to setup chromedp: %s", err.Error())
	}
	defer cdp.Close()
	logger.Debug("chromedp setup complete")

	runState, err := state.Load(*statePath)
	if err != nil {
		logger.Panicf("failed to load state file: %s", err.Error())
	}

	syncThreshold := time.Duration(*forceSyncDays) * 24 * time.Hour

	fireflyTag := fmt.Sprintf("firefly-bridge-%s", time.Now().Format(time.RFC3339))
	totalUploadCount := 0
	logUploadSummary := func() {
		if totalUploadCount > 0 {
			logger.Infof("%d transactions uploaded at %s/tags/show/%s", totalUploadCount, strings.TrimSuffix(cfg.Firefly.Host, "/"), strings.ReplaceAll(fireflyTag, " ", "%20"))
		}
	}

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	go func() {
		for sig := range c {
			logUploadSummary()
			logger.Panicf("SIGINT received %s", sig.String())
		}
	}()

	var errs []error

	for _, i := range cfg.Institutions {
		iLog := logger.WithField("institution", i.Name)

		if *onlyInstitution != "" && i.Name != *onlyInstitution {
			continue
		}

		forceThis := *force || *onlyInstitution == i.Name
		if !forceThis {
			if lastRun, ok := runState.Institutions[i.Name]; ok {
				if age := time.Since(lastRun); age < state.SkipWindow {
					iLog.Infof("skipping, last processed %s ago", age.Round(time.Second))
					continue
				}
			}
		}

		if err = i.Login(cdp); err != nil {
			iLog.Errorf("failed to login: %s", err.Error())
			errs = append(errs, fmt.Errorf("institution %s: failed to login: %w", i.Name, err))
			continue
		}
		iLog.Info("logged in successfully")

		institutionFailed := false
		for _, a := range i.Accounts {
			aLog := iLog.WithField("account", a.Name)
			if a.AccountType == institution.AccountTypeInvestment {
				if err := processInvestmentAccount(ctx, aLog, cdp, ff, &a); err != nil {
					aLog.Errorf("failed to process investment account: %s", err.Error())
					errs = append(errs, fmt.Errorf("institution %s, account %s: failed to process investment account: %w", i.Name, a.Name, err))
					institutionFailed = true
					continue
				}
			} else {
				// When forcing, pass a zero lastSync so the balance-unchanged
				// skip check never triggers inside processRegularAccount.
				lastSync := runState.LastAccountSync(i.Name, a.Name)
				if forceThis {
					lastSync = time.Time{}
				}
				skipped, err := processRegularAccount(ctx, aLog, cdp, ff, &a, &totalUploadCount, fireflyTag, lastSync, syncThreshold)
				if err != nil {
					aLog.Errorf("failed to process regular account: %s", err.Error())
					errs = append(errs, fmt.Errorf("institution %s, account %s: failed to process regular account: %w", i.Name, a.Name, err))
					institutionFailed = true
					continue
				}
				if !skipped {
					runState.RecordAccountSync(i.Name, a.Name)
					if err := runState.Save(*statePath); err != nil {
						aLog.Warnf("failed to save state: %s", err.Error())
					}
				}
			}
		}

		if !institutionFailed {
			runState.Institutions[i.Name] = time.Now()
			if err := runState.Save(*statePath); err != nil {
				iLog.Warnf("failed to save state: %s", err.Error())
			}
		}
	}

	logUploadSummary()

	if len(errs) > 0 {
		logger.Errorf("%d error(s) occurred:", len(errs))
		for idx, e := range errs {
			logger.Errorf("  [%d] %s", idx+1, e.Error())
		}
		return 1
	}
	return 0
}

// processInvestmentAccount handles investment account synchronization
func processInvestmentAccount(ctx context.Context, logger *logrus.Entry, cdp *chromedp.ChromeDP, ff *firefly.ClientWithResponses, account *institution.Account) error {
	holdings, err := account.GetHoldings(cdp)
	if err != nil {
		return fmt.Errorf("failed to get holdings: %w", err)
	}
	logger.Infof("got %d holdings", len(*holdings))

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
		logger.Info("holdings unchanged, skipping update")
		return nil
	}

	logger.Info("holdings changed:")
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
	logger.Info("updated holdings")

	return nil
}

// processRegularAccount handles regular account synchronization. It returns
// (true, nil) when the account is skipped because its balance is unchanged and
// it was synced recently enough; the caller should not update the account's
// last-sync timestamp in that case.
func processRegularAccount(ctx context.Context, logger *logrus.Entry, cdp *chromedp.ChromeDP, ff *firefly.ClientWithResponses, account *institution.Account, totalUploadCount *int, fireflyTag string, lastSync time.Time, syncThreshold time.Duration) (skipped bool, err error) {
	balance, err := account.GetBalance(cdp)
	if err != nil {
		return false, fmt.Errorf("failed to get balance: %w", err)
	}
	logger.Infof("got balance: %.2f", balance)

	// Fetch the current Firefly balance up front so we can decide whether to
	// skip CSV parsing entirely, and reuse it for the final mismatch check if
	// no transactions end up being uploaded.
	fireflyBalance, err := ff.GetBalance(ctx, account.FireflyAccountID)
	if err != nil {
		return false, fmt.Errorf("failed to get firefly balance: %w", err)
	}

	// Skip CSV parsing if the balance is unchanged and the last sync is recent
	// enough. A zero lastSync (first run or --force) bypasses this check.
	if math.Abs(balance) == math.Abs(fireflyBalance) && !lastSync.IsZero() && time.Since(lastSync) < syncThreshold {
		logger.Infof("skipping, balance unchanged (%.2f) and last sync was %s ago", balance, time.Since(lastSync).Round(time.Second))
		return true, nil
	}

	txns, err := account.GetTransactions(cdp)
	if err != nil {
		return false, fmt.Errorf("failed to get transactions: %w", err)
	}
	logger.Infof("got %d transactions", len(txns))

	var filtered []*firefly.TransactionSplitStore
	for _, t := range txns {
		exists, err := ff.TransactionExists(ctx, t)
		if err != nil {
			return false, fmt.Errorf("failed to check if transaction exists: (%s, %s, %s, %s): %w", t.Date.Format(time.DateOnly), t.Description, t.Amount, t.Type, err)
		}
		alreadyExistsMsg := ""
		if !exists {
			filtered = append(filtered, t)
		} else {
			alreadyExistsMsg = "(already exists)"
		}
		logger.Debugf("transaction %s: (%s, %s, %s, %s)", alreadyExistsMsg, t.Date.Format(time.DateOnly), t.Description, t.Amount, t.Type)
	}

	logger.Infof("got %d new transactions", len(filtered))

	uploaded := 0
	for _, t := range filtered {
		t.Tags = &[]string{fireflyTag}
		//TODO: optionally use ollama here to identify category of transaction
		res, err := ff.StoreTransaction(ctx, &firefly.StoreTransactionParams{}, firefly.StoreTransactionJSONRequestBody{Transactions: []firefly.TransactionSplitStore{*t}})
		if err != nil {
			return false, fmt.Errorf("failed to store transaction: (%s, %s, %s, %s): %w", t.Date.Format(time.DateOnly), t.Description, t.Amount, t.Type, err)
		}
		if res.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(res.Body)
			return false, fmt.Errorf("unexpected status code: (%s, %s, %s, %s): (%s) %s", t.Date.Format(time.DateOnly), t.Description, t.Amount, t.Type, res.Status, body)
		}
		logger.Infof("stored transaction: (%s, %s, %s, %s)", t.Date.Format(time.DateOnly), t.Description, t.Amount, t.Type)
		*totalUploadCount++
		uploaded++
	}

	// Re-fetch the Firefly balance after uploads to verify it matches the
	// scraped balance. If nothing was uploaded the balance couldn't have
	// changed, so reuse the value fetched at the top of this function.
	if uploaded > 0 {
		fireflyBalance, err = ff.GetBalance(ctx, account.FireflyAccountID)
		if err != nil {
			return false, fmt.Errorf("failed to check updated firefly balance: %w", err)
		}
	}

	if math.Abs(balance) != math.Abs(fireflyBalance) {
		logger.Warnf("balance mismatch: firefly: %f, bank: %f", fireflyBalance, balance)
	}

	return false, nil
}
