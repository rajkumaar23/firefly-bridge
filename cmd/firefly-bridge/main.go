package main

import (
	"context"
	"flag"

	"github.com/rajkumaar23/firefly-bridge/internal/chromedp"
	"github.com/rajkumaar23/firefly-bridge/internal/config"
	"github.com/rajkumaar23/firefly-bridge/internal/firefly"
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

	cfg, err := config.NewConfig(*configPath)
	if err != nil {
		logger.Fatalf("failed to load config: %s", err.Error())
	}
	logger.Debugf("loaded config")

	_, err = firefly.NewFireflyClient(ctx, cfg.Firefly.BaseURL, cfg.Firefly.Token)
	if err != nil {
		logger.Fatalf("failed to create firefly client: %s", err.Error())
	}
	logger.Debug("verified connection to firefly")

	cdp, err := chromedp.NewChromeDP(ctx, logger, cfg.BrowserExecPath, cfg.GetDownloadCount(), *cdpDebug)
	if err != nil {
		logger.Fatalf("failed to setup chromedp: %s", err.Error())
	}
	defer cdp.Close()
	logger.Debug("chromedp setup complete")

	for _, i := range cfg.Institutions {
		if err = i.Login(cdp); err != nil {
			logger.Fatalf("failed to login to %s: %s", i.Name, err.Error())
		}
		logger.Debugf("logged in to '%s' successfully", i.Name)
		for _, a := range i.Accounts {
			balance, err := a.GetBalance(cdp)
			if err != nil {
				logger.Fatalf("failed to get balance for '%s - %s': %s", i.Name, a.Name, err.Error())
			}
			logger.Debugf("balance for '%s - %s': %.2f", i.Name, a.Name, balance)
		}
	}
}
