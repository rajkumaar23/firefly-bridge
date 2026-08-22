package chromedp

import (
	"encoding/json"
	"testing"
)

func TestIsTruthy(t *testing.T) {
	tests := []struct {
		in   interface{}
		want bool
	}{
		{nil, false},
		{false, false},
		{true, true},
		{"", false},
		{"yes", true},
		{float64(0), false}, // JS numbers arrive as float64
		{float64(1.5), true},
		{0, false},
		{42, true},
		{map[string]interface{}{}, true}, // objects are truthy
		{[]interface{}{}, true},          // arrays are truthy
	}
	for _, tt := range tests {
		if got := isTruthy(tt.in); got != tt.want {
			t.Errorf("isTruthy(%#v) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

// TestOrderUnmarshalEffectiveDate pins the scraper-to-bridge JSON contract for
// the optional effective_date field: the Amazon orders JS returns it as a
// snake_case key on each row, and it must round-trip into Order.EffectiveDate.
// Absent key stays empty (the classic matching path).
func TestOrderUnmarshalEffectiveDate(t *testing.T) {
	const payload = `[
	  { "date": "August 20, 2026", "amount": "$9.92", "items": "Liner", "line_items": [], "effective_date": "August 21" },
	  { "date": "August 19, 2026", "amount": "$27.41", "items": "Pickup" }
	]`
	var orders []Order
	if err := json.Unmarshal([]byte(payload), &orders); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(orders) != 2 {
		t.Fatalf("expected 2 orders, got %d", len(orders))
	}
	if orders[0].EffectiveDate != "August 21" {
		t.Errorf("order[0].EffectiveDate = %q, want %q", orders[0].EffectiveDate, "August 21")
	}
	if orders[1].EffectiveDate != "" {
		t.Errorf("order[1].EffectiveDate = %q, want empty (absent key)", orders[1].EffectiveDate)
	}
	if orders[0].Date != "August 20, 2026" || orders[0].Amount != "$9.92" {
		t.Errorf("existing fields clobbered: %+v", orders[0])
	}
}
