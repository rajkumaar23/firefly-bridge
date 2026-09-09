package utils

import (
	"fmt"
	"time"
)

// SubtractDays subtracts given number of days from the current date and formats the result.
func SubtractDays(days int, format string) (string, error) {
	if format == "" {
		return "", fmt.Errorf("format cannot be empty")
	}

	subtractedTime := time.Now().AddDate(0, 0, -days)
	return subtractedTime.Format(format), nil
}

// Today returns the current date formatted according to the provided format string.
func Today(format string) (string, error) {
	if format == "" {
		return "", fmt.Errorf("format cannot be empty")
	}

	return time.Now().Format(format), nil
}

// ParseLocalDateFromString parses a date string using the specified layout,
// anchoring the result in loc (the caller's date-anchoring zone, e.g. from
// config). The zone matters: a date-only string parsed into UTC instead of the
// local zone is off by a full day once Firefly displays it in the local zone.
func ParseLocalDateFromString(layout string, s string, loc *time.Location) (time.Time, error) {
	if loc == nil {
		loc = time.Local
	}
	transactionDate, err := time.ParseInLocation(layout, s, loc)
	if err != nil {
		return time.Time{}, err
	}
	return transactionDate, nil
}
