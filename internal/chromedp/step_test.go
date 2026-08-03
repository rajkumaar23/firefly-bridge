package chromedp

import "testing"

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
