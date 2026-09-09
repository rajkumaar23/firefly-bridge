package utils

import (
	"context"
	"time"

	"github.com/sirupsen/logrus"
)

const loggerKey = "logger"

// timeLocationKey carries the zone used to anchor date-only values parsed from
// institution CSVs. It is a *time.Location so the zone (and its DST rules)
// travel with the context.
type timeLocationKey struct{}

var timeLocationKeyVal = timeLocationKey{}

// WithTimeLocation attaches the date-anchoring zone to the context.
func WithTimeLocation(ctx context.Context, loc *time.Location) context.Context {
	return context.WithValue(ctx, timeLocationKeyVal, loc)
}

// TimeLocationFromContext returns the date-anchoring zone, or time.Local when
// none was set so code that builds a context by hand (tests, scratch tools)
// keeps working.
func TimeLocationFromContext(ctx context.Context) *time.Location {
	if loc, ok := ctx.Value(timeLocationKeyVal).(*time.Location); ok && loc != nil {
		return loc
	}
	return time.Local
}

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
