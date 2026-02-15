package main

import (
	"context"
	"flag"
	"os"

	"github.com/rajkumaar23/firefly-bridge/internal/firefly"
	"github.com/rajkumaar23/firefly-bridge/internal/utils"
	"github.com/sirupsen/logrus"
)

func main() {
	var debug = flag.Bool("debug", false, "enable debug logs")
	var baseURL = flag.String("base-url", "", "firefly base url (eg: http://firefly.lan.example.com/api), alternative to setting it via environment $FIREFLY_BASE_URL")
	var token = flag.String("token", "", "firefly access token, alternative to setting it via environment $FIREFLY_TOKEN")
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

	accounts, err := ff.GetAccountsACWithResponse(ctx, &firefly.GetAccountsACParams{})
	if err != nil {
		logger.Panicf("failed to get accounts: %s", err.Error())
	}
	if accounts.JSON200 == nil {
		logger.Panicf("failed to get accounts: %s", accounts.Status())
	}
	logger.Debugf("got %d accounts", len(*accounts.JSON200))
}
