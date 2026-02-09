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
