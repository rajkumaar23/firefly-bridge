package main

import (
	"flag"

	"github.com/rajkumaar23/firefly-bridge/internal/config"
	"github.com/sirupsen/logrus"
)

func main() {
	var logLevel = flag.String("log-level", "info", "set the logging level (debug, info, warn, error, fatal, panic)")
	var configPath = flag.String("config", "config.yaml", "path to the configuration file")
	flag.Parse()

	logger := logrus.New()
	logrusLevel, err := logrus.ParseLevel(*logLevel)
	if err != nil {
		logger.Fatalf("invalid log level: %v", err)
	}
	logger.SetLevel(logrusLevel)
	logger.Debugf("log level set to %s", logrusLevel)

	cfg, err := config.NewConfig(*configPath)
	if err != nil {
		logger.Fatalf("failed to load config: %v", err)
	}
	logger.Debugf("loaded config: %+v", cfg)
}
