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
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/rajkumaar23/firefly-bridge/internal/ai"
	"github.com/rajkumaar23/firefly-bridge/internal/chromedp"
	"github.com/rajkumaar23/firefly-bridge/internal/config"
	"github.com/rajkumaar23/firefly-bridge/internal/datadir"
	"github.com/rajkumaar23/firefly-bridge/internal/firefly"
	"github.com/rajkumaar23/firefly-bridge/internal/institution"
	"github.com/rajkumaar23/firefly-bridge/internal/secrets"
	"github.com/rajkumaar23/firefly-bridge/internal/state"
	"github.com/rajkumaar23/firefly-bridge/internal/utils"
	"github.com/rajkumaar23/firefly-bridge/internal/vendor"
	"github.com/rajkumaar23/firefly-bridge/internal/versioncheck"
	"github.com/sirupsen/logrus"
)

// Set by the release workflow via -ldflags; "dev" for local builds.
var (
	version   = "dev"
	buildTime = ""
)

func main() {
	os.Exit(run())
}

// orLocalBuild reports a human-friendly label for an empty build time.
func orLocalBuild(buildTime string) string {
	if buildTime == "" {
		return "local build"
	}
	return buildTime
}

func checkForUpdate(logger *logrus.Logger) {
	// Dev/local builds: no nudge — just a single info line so the user
	// knows they're not on a release binary (per plan: "dev builds ok, do
	// NOT nudge").
	if version == "" || version == "dev" {
		logger.Info("running a local/dev build — release binaries are available on GitHub; no update check performed")
		return
	}
	c, err := versioncheck.New(versioncheck.Options{
		Version:   version,
		BuildTime: buildTime,
		CachePath: datadir.UpdateCacheFile(),
	})
	if err != nil {
		return // versioncheck.New only errors on a bad cache path — stay silent
	}
	res, _ := c.Check() // Check never errors by design
	if !res.Updated {
		return
	}
	logger.Warnf("firefly-bridge %s is out of date — %s is available (download: %s)", version, res.LatestTag, res.DownloadURL)
	logger.Warnf("to update: %s", installOneLiner(res.DownloadURL))
}

// installOneLiner prints a single copy-paste command that downloads the
// latest release, verifies its checksum against the release's
// checksums.txt, and installs it. This is the user-facing update path for
// v1 — see the TODO on autoSelfUpdate below for the in-binary follow-up.
func installOneLiner(downloadURL string) string {
	repo := versioncheck.RepoPath()
	asset := versioncheck.BinaryAssetName()
	return fmt.Sprintf(
		"a=%s && curl -fsSL -o /tmp/$a '%s' && curl -fsSL 'https://github.com/%s/releases/latest/download/checksums.txt' | grep \" $a$\" | awk -v d=/tmp '{print $1\"  \"d\"/\"$2}' | shasum -a 256 -c - --status && install -m 755 /tmp/$a ~/.local/bin/firefly-bridge",
		asset, downloadURL, repo,
	)
}

// TODO(autoSelfUpdate): with the user's explicit permission (a `-y` or an
// interactive "Install update now? [y/N]"), download the asset to a temp
// file, verify it against the release's checksums.txt (crypto/sha256 —
// the dependency is stdlib; checksums.txt is added by Task 1), then
// os.Rename() it over os.Executable(). On Unix the running process keeps
// executing from the old inode after the rename (verified: the kernel
// keeps the inode alive until exit), so the current run is unaffected and
// the new binary is in place on the next invocation. Also verify the
// target path is writable and warn if it isn't (e.g. /usr/local/bin under
// sudo). Deferred to a follow-up task: it needs an interactive prompt, a
// sudo/sudoers story for system-wide installs, and a backup of the
// previous binary (rename-aside to <binary>.bak) in case the new one is
// broken. v1 ships the one-liner instead.

func run() int {
	var cdpDebug = flag.Bool("cdp-debug", false, "enable chromedp debug logs")
	var ffBridgeDebug = flag.Bool("debug", false, "enable firefly-bridge debug logs")
	var configPath = flag.String("config", "config.yaml", "path to the configuration file")
	var statePath = flag.String("state", datadir.StateFile(), "path to the state file (default: data dir)")
	var force = flag.Bool("force", false, "bypass the per-institution cooldown and the per-account balance-unchanged skip, forcing a full sync of every institution and account")
	var forceSyncDays = flag.Int("sync-days", 10, "force a full transaction CSV sync for an account after this many days, even if its scraped balance matches the Firefly balance")
	var onlyInstitution = flag.String("institution", "", "run only the institution with this name, skipping all others; also bypasses cooldown and balance-unchanged checks for that institution")
	var skipInstitutions = flag.String("skip", "", "comma-separated list of institution names to skip; all other institutions run normally")
	var csvDebug = flag.Bool("csv-debug", false, "log every parsed CSV row with its row number to help diagnose parsing issues")
	var vendorsFlag = flag.String("vendors", "", "scrape configured vendors' order history to categorize their charges from what was actually bought; \"all\" or a comma-separated list of vendor names, empty disables vendor scraping")
	var listOrders = flag.Bool("list-orders", false, "log in to the configured vendors, print their scraped orders, and exit without syncing any institution; combine with -vendors to restrict which vendors run (default all)")
	var showVersion = flag.Bool("version", false, "print version information and exit")
	flag.Parse()

	if *showVersion {
		fmt.Printf("firefly-bridge %s (built %s, %s/%s)\n", version, orLocalBuild(buildTime), runtime.GOOS, runtime.GOARCH)
		return 0
	}

	ctx := context.Background()

	logger := logrus.New()
	logger.SetFormatter(&logrus.TextFormatter{FullTimestamp: true, ForceColors: true})
	if *ffBridgeDebug {
		logger.SetLevel(logrus.DebugLevel)
		logger.Debugf("log level set to debug")
	}

	checkForUpdate(logger)

	ctx = utils.WithLogger(ctx, logger)

	cfg, err := config.NewConfig(*configPath)
	if err != nil {
		logger.Panicf("failed to load config: %s", err.Error())
	}
	logger.Debug("loaded config")

	mustDataDir, err := datadir.Dir()
	if err != nil {
		// Degrade to legacy CWD-relative paths; everything still works.
		logger.Warnf("could not determine data dir (%v); using CWD-relative paths", err)
		mustDataDir, _ = os.Getwd()
	}

	// One-time migration: if the user is on the default state path and a
	// legacy CWD .state.json exists, copy it into the data dir once.
	if *statePath == datadir.StateFile() {
		legacy := ".state.json"
		if data, err := os.ReadFile(legacy); err == nil {
			dest := filepath.Join(mustDataDir, ".state.json")
			if _, err := os.Stat(dest); os.IsNotExist(err) {
				if err := os.WriteFile(dest, data, 0o644); err == nil {
					logger.Infof("migrated %s → %s (original left in place)", legacy, dest)
				}
			}
		}
		// Same for the browser profile: copy CWD chromedp-data/ if the
		// data-dir profile doesn't exist yet. Sessions survive so the
		// user is not re-logged-out of every institution.
		legacyProfile := "chromedp-data"
		if st, err := os.Stat(legacyProfile); err == nil && st.IsDir() {
			destProfile := filepath.Join(mustDataDir, "chromedp-data")
			if _, err := os.Stat(destProfile); os.IsNotExist(err) {
				if err := datadir.CopyDir(legacyProfile, destProfile); err == nil {
					logger.Infof("migrated browser profile %s → %s (original left in place)", legacyProfile, destProfile)
				}
			}
		}
	}

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

	var categorizer *ai.Categorizer
	if cfg.AI != nil && cfg.AI.Enabled {
		apiKey, err := secretManager.Resolve(ctx, cfg.AI.APIKey)
		if err != nil {
			logger.Panicf("failed to resolve AI api_key: %s", err.Error())
		}
		categorizer, err = ai.New(ctx, cfg.AI, ff, apiKey, logger.WithField("component", "ai"))
		if err != nil {
			logger.Panicf("failed to initialize AI categorizer: %s", err.Error())
		}
		logger.Info("AI categorizer enabled")
	}

	cdp, err := chromedp.NewChromeDP(ctx, logger, cfg.BrowserExecPath, cfg.GetDownloadCount(), *cdpDebug, secretManager, mustDataDir)
	cdp.CSVDebug = *csvDebug
	if err != nil {
		logger.Panicf("failed to setup chromedp: %s", err.Error())
	}
	defer cdp.Close()
	logger.Debug("chromedp setup complete")

	// -list-orders: scrape-and-print mode for inspecting what the vendor flows
	// return (e.g. while tuning the orders JS selectors). No institution sync.
	if *listOrders {
		sel := *vendorsFlag
		if sel == "" {
			sel = "all"
		}
		return listVendorOrders(logger, cdp, cfg, sel)
	}

	// On-demand vendor order scraping (-vendors): log into each requested
	// vendor, pull its recent orders, and hand the resulting index to the
	// categorizer so ambiguous all-in-one charges are categorized from their
	// actual contents. Everything here is best-effort — a vendor that fails to
	// scrape is simply left out of the index and its transactions enrich the
	// normal way.
	scrapeVendors(logger, cdp, cfg, categorizer, *vendorsFlag)

	runState, err := state.Load(*statePath)
	if err != nil {
		logger.Panicf("failed to load state file: %s", err.Error())
	}

	syncThreshold := time.Duration(*forceSyncDays) * 24 * time.Hour

	fireflyTag := fmt.Sprintf("bridge-%s", time.Now().Format("Jan-2T15:04"))
	if err := ff.CreateTag(ctx, fireflyTag); err != nil {
		logger.Warnf("failed to create tag %q: %s", fireflyTag, err.Error())
	}
	totalUploadCount := 0
	cleanup := func() {
		if totalUploadCount == 0 {
			if err := ff.RemoveTag(ctx, fireflyTag); err != nil {
				logger.Warnf("failed to delete unused tag %q: %s", fireflyTag, err.Error())
			}
		} else {
			logger.Infof("%d transactions uploaded at %s/tags/show/%s", totalUploadCount, strings.TrimSuffix(cfg.Firefly.Host, "/"), strings.ReplaceAll(fireflyTag, " ", "%20"))
		}
	}
	defer cleanup()

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	go func() {
		for sig := range c {
			cleanup()
			logger.Panicf("SIGINT received %s", sig.String())
		}
	}()

	var errs []error

	skipSet := make(map[string]bool)
	if *skipInstitutions != "" {
		for _, name := range strings.Split(*skipInstitutions, ",") {
			skipSet[strings.TrimSpace(name)] = true
		}
	}

	for _, i := range cfg.Institutions {
		iLog := logger.WithField("institution", i.Name)

		if *onlyInstitution != "" && i.Name != *onlyInstitution {
			continue
		}

		if skipSet[i.Name] {
			iLog.Infof("skipping (excluded via -skip flag)")
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
				skipped, err := processRegularAccount(ctx, aLog, cdp, ff, categorizer, &a, &totalUploadCount, fireflyTag, lastSync, syncThreshold)
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

		// Best-effort: a failed logout never undoes the accounts that were
		// already synced, so it is logged as a warning, not a run error.
		if len(i.LogoutFlow) > 0 {
			if err := i.Logout(cdp); err != nil {
				iLog.Warnf("failed to logout: %s", err.Error())
			} else {
				iLog.Info("logged out")
			}
		}

		if !institutionFailed {
			runState.Institutions[i.Name] = time.Now()
			if err := runState.Save(*statePath); err != nil {
				iLog.Warnf("failed to save state: %s", err.Error())
			}
		}
	}

	if len(errs) > 0 {
		logger.Errorf("%d error(s) occurred:", len(errs))
		for idx, e := range errs {
			logger.Errorf("  [%d] %s", idx+1, e.Error())
		}
		return 1
	}
	return 0
}

// forEachRequestedVendor runs the login and orders flows of every vendor
// selected by vendorsFlag ("all" or a comma-separated list of names) and calls
// fn with the scraped orders. Failures are warnings, never run errors — a
// vendor that could not be scraped is simply skipped. It returns how many
// vendors were successfully scraped.
func forEachRequestedVendor(logger *logrus.Logger, cdp *chromedp.ChromeDP, cfg *config.Config, vendorsFlag string, fn func(v *vendor.Vendor, orders []vendor.Order)) int {
	all := vendorsFlag == "all"
	wanted := make(map[string]bool)
	if !all {
		for _, name := range strings.Split(vendorsFlag, ",") {
			wanted[strings.TrimSpace(name)] = true
		}
		for name := range wanted {
			found := false
			for i := range cfg.Vendors {
				if cfg.Vendors[i].Name == name {
					found = true
					break
				}
			}
			if !found {
				logger.Warnf("-vendors names %q but no such vendor is configured", name)
			}
		}
	}

	processed := 0
	for i := range cfg.Vendors {
		v := &cfg.Vendors[i]
		if !all && !wanted[v.Name] {
			continue
		}
		vLog := logger.WithField("vendor", v.Name)
		if err := v.Login(cdp); err != nil {
			vLog.Warnf("failed to login, skipping vendor: %s", err.Error())
			continue
		}
		vLog.Info("logged in successfully")
		orders, err := v.GetOrders(cdp)
		if err != nil {
			vLog.Warnf("failed to get orders, skipping vendor: %s", err.Error())
			continue
		}
		fn(v, orders)
		// Best-effort: a failed logout is logged as a warning and does not
		// count against the vendor's successful scrape.
		if len(v.LogoutFlow) > 0 {
			if err := v.Logout(cdp); err != nil {
				vLog.Warnf("failed to logout, ignoring: %s", err.Error())
			} else {
				vLog.Info("logged out")
			}
		}
		processed++
	}
	return processed
}

// scrapeVendors logs into each requested vendor and hands an order index to
// the categorizer. It is a no-op when vendor scraping is disabled or not
// applicable, so charges fall back to normal enrichment instead of being
// abstained on.
func scrapeVendors(logger *logrus.Logger, cdp *chromedp.ChromeDP, cfg *config.Config, categorizer *ai.Categorizer, vendorsFlag string) {
	if vendorsFlag == "" {
		return
	}
	if categorizer == nil {
		logger.Warn("-vendors specified but AI enrichment is disabled; skipping vendor scraping")
		return
	}
	if len(cfg.Vendors) == 0 {
		logger.Warn("-vendors specified but no vendors are configured")
		return
	}

	index := vendor.NewIndex()
	indexed := 0
	forEachRequestedVendor(logger, cdp, cfg, vendorsFlag, func(v *vendor.Vendor, orders []vendor.Order) {
		vLog := logger.WithField("vendor", v.Name)
		if err := index.Add(v, orders); err != nil {
			vLog.Warnf("skipping vendor: %s", err.Error())
			return
		}
		vLog.Infof("scraped %d orders", len(orders))
		indexed++
	})
	if indexed == 0 {
		return
	}
	categorizer.UseOrderResolver(index)
}

// listVendorOrders implements -list-orders: scrape the requested vendors and
// print every order, then exit without syncing anything. Meant for verifying
// login flows and tuning the orders JS selectors.
func listVendorOrders(logger *logrus.Logger, cdp *chromedp.ChromeDP, cfg *config.Config, vendorsFlag string) int {
	if len(cfg.Vendors) == 0 {
		logger.Error("-list-orders specified but no vendors are configured")
		return 1
	}

	processed := forEachRequestedVendor(logger, cdp, cfg, vendorsFlag, func(v *vendor.Vendor, orders []vendor.Order) {
		vLog := logger.WithField("vendor", v.Name)
		vLog.Infof("scraped %d orders:", len(orders))
		for _, o := range orders {
			line := fmt.Sprintf("  %s  %10.2f  %s", o.Date.Format(time.DateOnly), float64(o.Cents)/100, o.Items)
			if !o.EffectiveDate.IsZero() {
				line += fmt.Sprintf("  (effective %s)", o.EffectiveDate.Format(time.DateOnly))
			}
			if o.Category != "" {
				line += fmt.Sprintf("  [%s]", o.Category)
			}
			vLog.Info(line)
		}
	})
	if processed == 0 {
		logger.Error("no vendors could be scraped")
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
func processRegularAccount(ctx context.Context, logger *logrus.Entry, cdp *chromedp.ChromeDP, ff *firefly.ClientWithResponses, categorizer *ai.Categorizer, account *institution.Account, totalUploadCount *int, fireflyTag string, lastSync time.Time, syncThreshold time.Duration) (skipped bool, err error) {
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
		splits := []firefly.TransactionSplitStore{*t}
		if categorizer != nil {
			enriched, err := categorizer.Enrich(ctx, t)
			if err != nil {
				// Enrichment is best-effort; never block an upload on it.
				logger.Warnf("failed to enrich transaction (%s, %s): %s", t.Date.Format(time.DateOnly), t.Description, err.Error())
			}
			if len(enriched) > 0 {
				splits = enriched
			}
		}

		body := firefly.StoreTransactionJSONRequestBody{Transactions: splits}
		// A multi-split group needs a title; Firefly shows it in place of the
		// individual split descriptions.
		if len(splits) > 1 {
			groupTitle := t.Description
			body.GroupTitle = &groupTitle
			logger.Infof("storing %s as %d splits", t.Description, len(splits))
		}
		res, err := ff.StoreTransaction(ctx, &firefly.StoreTransactionParams{}, body)
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
