package main

import (
	"context"
	"flag"
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

	_, err = firefly.NewFireflyClient(ctx, cfg.Firefly.BaseURL, cfg.Firefly.Token)
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
			for _, t := range txns {
				logger.Debugf("transaction for '%s - %s': (%s, %s, %s, %s)", i.Name, a.Name, t.Date.Format(time.DateOnly), t.Description, t.Amount, t.Type)
			}
		}
	}
}
