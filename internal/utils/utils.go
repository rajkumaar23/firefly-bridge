package utils

import (
	"regexp"
	"strconv"
)

func ParseAmountFromString(s string) (float64, error) {
	if s == "" {
		return 0, nil
	}

	amtRegex := regexp.MustCompile(`[^0-9.-]+`)
	amtStr := amtRegex.ReplaceAllString(s, "")
	return strconv.ParseFloat(amtStr, 64)
}
