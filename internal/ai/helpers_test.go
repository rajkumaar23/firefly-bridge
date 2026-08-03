package ai

import "testing"

func TestKeyword(t *testing.T) {
	cases := map[string]string{
		"POS PURCHASE COFFEEHOUSE 1234": "COFFEEHOUSE", // longest alpha token
		"MEGASTORE":                     "MEGASTORE",
		"12345 99":                      "12345 99", // no usable alpha token -> whole desc
		"UPI/wallet/9876":               "wallet",
	}
	for in, want := range cases {
		if got := keyword(in); got != want {
			t.Errorf("keyword(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestConsensus(t *testing.T) {
	tests := []struct {
		name   string
		values []string
		want   string
		ok     bool
	}{
		{"empty", nil, "", false},
		{"single", []string{"Groceries"}, "Groceries", true},
		{"unanimous", []string{"Groceries", "Groceries"}, "Groceries", true},
		{"majority", []string{"Groceries", "Groceries", "Dining"}, "Groceries", true},
		{"tie counts as half", []string{"Groceries", "Dining"}, "", false},
		{"no majority", []string{"A", "B", "C"}, "", false},
	}
	for _, tt := range tests {
		got, ok := consensus(tt.values)
		if ok != tt.ok || (ok && got != tt.want) {
			t.Errorf("%s: consensus(%v) = (%q, %v), want (%q, %v)", tt.name, tt.values, got, ok, tt.want, tt.ok)
		}
	}
}

func TestMatch(t *testing.T) {
	allowed := []string{"Groceries", "Dining Out"}
	if got, ok := match("groceries", allowed); !ok || got != "Groceries" {
		t.Errorf("case-insensitive match failed: got %q, %v", got, ok)
	}
	if _, ok := match("Unknown", allowed); ok {
		t.Error("expected no match for unknown value")
	}
	if _, ok := match("  ", allowed); ok {
		t.Error("expected no match for blank value")
	}
}

func TestExtractJSON(t *testing.T) {
	cases := map[string]string{
		`{"category":"X"}`:                          `{"category":"X"}`,
		"```json\n{\"category\":\"X\"}\n```":        `{"category":"X"}`,
		`Here you go: {"budget":"Y"} hope it helps`: `{"budget":"Y"}`,
		`no json here`:                              `no json here`,
	}
	for in, want := range cases {
		if got := extractJSON(in); got != want {
			t.Errorf("extractJSON(%q) = %q, want %q", in, got, want)
		}
	}
}
