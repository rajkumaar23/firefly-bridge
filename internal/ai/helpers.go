package ai

import (
	"fmt"
	"strings"

	"github.com/rajkumaar23/firefly-bridge/internal/firefly"
)

func isEmpty(s *string) bool {
	return s == nil || strings.TrimSpace(*s) == ""
}

// keyword derives a single search token from a transaction description to find
// similar past transactions. It picks the longest alphabetic word (usually the
// merchant name), ignoring short and numeric tokens.
func keyword(description string) string {
	fields := strings.FieldsFunc(description, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z')
	})
	best := ""
	for _, f := range fields {
		if len(f) > len(best) {
			best = f
		}
	}
	if len(best) < 3 {
		return strings.TrimSpace(description)
	}
	return best
}

// exampleValues extracts the non-empty value produced by pick from each example.
func exampleValues(examples []firefly.TransactionSplit, pick func(firefly.TransactionSplit) *string) []string {
	var vals []string
	for _, e := range examples {
		if v := pick(e); v != nil && strings.TrimSpace(*v) != "" {
			vals = append(vals, strings.TrimSpace(*v))
		}
	}
	return vals
}

// consensus returns the most common value and whether it represents a strict
// majority of the value-bearing examples. A lone same-merchant precedent counts
// (the description match is itself a strong signal), but an even split between
// two values does not — that ambiguity is left for the model to resolve.
func consensus(values []string) (string, bool) {
	if len(values) == 0 {
		return "", false
	}
	counts := make(map[string]int)
	for _, v := range values {
		counts[v]++
	}
	top, topCount := "", 0
	for v, n := range counts {
		if n > topCount {
			top, topCount = v, n
		}
	}
	if topCount*2 > len(values) {
		return top, true
	}
	return "", false
}

// match resolves value against the allowed list case-insensitively and returns
// the canonical (as-stored) name. An empty value is never a match.
func match(value string, allowed []string) (string, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", false
	}
	for _, a := range allowed {
		if strings.EqualFold(a, value) {
			return a, true
		}
	}
	return "", false
}

// formatExamples renders a compact few-shot block. Only the fields being
// requested are shown, and duplicates are collapsed to keep the prompt small.
func formatExamples(examples []firefly.TransactionSplit, wantCategory, wantBudget bool) string {
	var b strings.Builder
	seen := make(map[string]bool)
	for _, e := range examples {
		var parts []string
		if wantCategory && e.CategoryName != nil && strings.TrimSpace(*e.CategoryName) != "" {
			parts = append(parts, fmt.Sprintf("category %q", strings.TrimSpace(*e.CategoryName)))
		}
		if wantBudget && e.BudgetName != nil && strings.TrimSpace(*e.BudgetName) != "" {
			parts = append(parts, fmt.Sprintf("budget %q", strings.TrimSpace(*e.BudgetName)))
		}
		if len(parts) == 0 {
			continue
		}
		line := fmt.Sprintf("- %q -> %s\n", e.Description, strings.Join(parts, ", "))
		if seen[line] {
			continue
		}
		seen[line] = true
		b.WriteString(line)
	}
	return b.String()
}

// extractJSON pulls the first {...} object out of raw model text, tolerating
// models that wrap JSON in prose or code fences.
func extractJSON(raw string) string {
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start >= 0 && end > start {
		return raw[start : end+1]
	}
	return raw
}
