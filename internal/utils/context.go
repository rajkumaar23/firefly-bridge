package utils

import (
	"context"

	"github.com/sirupsen/logrus"
)

const loggerKey = "logger"

// GetLogger retrieves the logger from the context.
func GetLogger(ctx context.Context) *logrus.Logger {
	logger, ok := ctx.Value(loggerKey).(*logrus.Logger)
	if !ok {
		panic("logger not found in context")
	}
	return logger
}

// WithLogger attaches a logger to the context.
func WithLogger(ctx context.Context, logger *logrus.Logger) context.Context {
	return context.WithValue(ctx, loggerKey, logger)
}
