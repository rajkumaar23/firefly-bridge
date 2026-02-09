package utils

import (
	"bytes"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"text/template"
)

func ParseAmountFromString(s string) (float64, error) {
	if s == "" {
		return 0, nil
	}

	amtRegex := regexp.MustCompile(`[^0-9.-]+`)
	amtStr := amtRegex.ReplaceAllString(s, "")
	return strconv.ParseFloat(amtStr, 64)
}

// isTemplateString checks if the given string contains template syntax.
func isTemplateString(s string) bool {
	return strings.Contains(s, "{{") && strings.Contains(s, "}}")
}

// ParseTemplate parses a text template with the predefined FuncMap and returns the resulting string.
func ParseTemplate(tmpl string) (string, error) {
	if !isTemplateString(tmpl) {
		return tmpl, nil
	}

	funcs := template.FuncMap{
		"Today":        Today,
		"SubtractDays": SubtractDays,
	}

	t, err := template.New("tmpl").Funcs(funcs).Parse(tmpl)
	if err != nil {
		return "", fmt.Errorf("error parsing template: %w", err)
	}

	var buf bytes.Buffer
	if err := t.Execute(&buf, nil); err != nil {
		return "", fmt.Errorf("error executing template: %w", err)
	}

	return buf.String(), nil
}
