package utils

import (
	"bytes"
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"text/template"
)

func ParseAmountFromString(s string) (float64, error) {
	if strings.TrimSpace(s) == "" {
		return 0, nil
	}

	amtRegex := regexp.MustCompile(`-?[\d,]+\.?\d*`)
	amtStr := strings.ReplaceAll(amtRegex.FindString(s), ",", "")
	return strconv.ParseFloat(amtStr, 64)
}

// isTemplateString checks if the given string contains template syntax.
func isTemplateString(s string) bool {
	return strings.Contains(s, "{{") && strings.Contains(s, "}}")
}

// SecretResolver interface for resolving secret references
type SecretResolver interface {
	Resolve(ctx context.Context, value string) (string, error)
}

// ParseTemplate parses a text template with the predefined FuncMap and returns the resulting string.
// It also resolves secret references if a SecretResolver is provided.
func ParseTemplate(ctx context.Context, tmpl string, resolver SecretResolver) (string, error) {
	// First, try to resolve as a secret reference if resolver is provided
	if resolver != nil && strings.Contains(tmpl, "://") {
		resolved, err := resolver.Resolve(ctx, tmpl)
		if err != nil {
			return "", err
		}
		tmpl = resolved
	}

	// Then, parse as a template if it contains template syntax
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
