package main

import (
	"context"
	"flag"
	"github.com/rajkumaar23/firefly-bridge/internal/config"
	"github.com/rajkumaar23/firefly-bridge/internal/firefly"
	"github.com/sirupsen/logrus"
)

func main() {
	var debugMode = flag.Bool("debug", false, "enable debug logs")
	var configPath = flag.String("config", "config.yaml", "path to the configuration file")
	flag.Parse()

	// Set up logger
	logger := logrus.New()
	if *debugMode {
		logger.SetLevel(logrus.DebugLevel)
		logger.Debugf("log level set to debug")
	}

	// Load configuration
	cfg, err := config.NewConfig(*configPath)
	if err != nil {
		logger.Fatalf("failed to load config: %v", err)
	}
	logger.Debugf("loaded config: %+v", cfg)

	// Initialize Firefly client
	ff, err := firefly.NewFireflyClient(cfg.Firefly.BaseURL, cfg.Firefly.Token)
	if err != nil {
		logger.Fatalf("failed to create firefly client: %v", err)
	}

	// Verify connection to Firefly
	ffSysInfo, err := ff.GetAboutWithResponse(context.Background(), &firefly.GetAboutParams{})
	if err != nil {
		logger.Fatalf("failed to get firefly system info: %v", err)
	}
	if ffSysInfo.JSON200 == nil {
		logger.Fatalf("failed to connect to firefly: %s", ffSysInfo.Status())
	}
	logger.Debugf("connected to firefly: %s (os: %s, version: %s)", cfg.Firefly.BaseURL, *ffSysInfo.JSON200.Data.Os, *ffSysInfo.JSON200.Data.ApiVersion)
}
