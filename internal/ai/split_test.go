package ai

import "testing"

func TestParseCents(t *testing.T) {
	tests := []struct {
		in      string
		want    int64
		wantErr bool
	}{
		{"142.37", 14237, false},
		{"45", 4500, false},
		{"0.05", 5, false},
		{"1234.5", 123450, false}, // single decimal digit
		{"-11.04", -1104, false},
		{" 199.95 ", 19995, false},
		{"abc", 0, true},
		{"", 0, true},
	}
	for _, tt := range tests {
		got, err := parseCents(tt.in)
		if (err != nil) != tt.wantErr {
			t.Errorf("parseCents(%q) err = %v, wantErr %v", tt.in, err, tt.wantErr)
			continue
		}
		if !tt.wantErr && got != tt.want {
			t.Errorf("parseCents(%q) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

func TestFormatCents(t *testing.T) {
	for in, want := range map[int64]string{
		14237: "142.37",
		4500:  "45.00",
		5:     "0.05",
		0:     "0.00",
		-1104: "11.04", // Firefly amounts are unsigned; type carries direction
	} {
		if got := formatCents(in); got != want {
			t.Errorf("formatCents(%d) = %q, want %q", in, got, want)
		}
	}
}

// Splits must sum to the charge exactly — that is the whole contract with
// Firefly, which rejects a group whose parts don't reconcile.
func TestAllocateSumsToTotal(t *testing.T) {
	cases := []struct {
		name    string
		weights []int64
		total   int64
	}{
		{"exact", []int64{14237, 3891}, 18128},
		{"with tax", []int64{14237, 3891}, 19995},        // items < charge
		{"with discount", []int64{14237, 3891}, 27000},   // items > charge
		{"three ways", []int64{1000, 1000, 1000}, 10000}, // indivisible split
		{"lopsided", []int64{1, 99999}, 50000},
		{"repeating", []int64{1, 1, 1}, 100},
	}
	for _, tc := range cases {
		got := allocate(tc.weights, tc.total)
		if got == nil {
			t.Errorf("%s: allocate returned nil", tc.name)
			continue
		}
		if len(got) != len(tc.weights) {
			t.Errorf("%s: got %d shares, want %d", tc.name, len(got), len(tc.weights))
			continue
		}
		var sum int64
		for _, v := range got {
			if v <= 0 {
				t.Errorf("%s: non-positive share in %v", tc.name, got)
			}
			sum += v
		}
		if sum != tc.total {
			t.Errorf("%s: shares %v sum to %d, want %d", tc.name, got, sum, tc.total)
		}
	}
}

func TestAllocateProportional(t *testing.T) {
	// 142.37 + 38.91 of items billed as 199.95 (tax added): tax should be
	// spread in proportion, not dumped on one split.
	got := allocate([]int64{14237, 3891}, 19995)
	if got == nil {
		t.Fatal("allocate returned nil")
	}
	if got[0] <= got[1] {
		t.Fatalf("larger item should get the larger share, got %v", got)
	}
	// Each share should be within a cent of its exact proportion.
	for i, w := range []int64{14237, 3891} {
		exact := float64(w) / float64(14237+3891) * 19995
		if diff := float64(got[i]) - exact; diff > 1 || diff < -1 {
			t.Errorf("share %d = %d, off from exact %.2f by more than a cent", i, got[i], exact)
		}
	}
}

func TestAllocateRejectsUnusable(t *testing.T) {
	if got := allocate(nil, 100); got != nil {
		t.Errorf("no weights should not allocate, got %v", got)
	}
	if got := allocate([]int64{0, 0}, 100); got != nil {
		t.Errorf("zero weights should not allocate, got %v", got)
	}
	if got := allocate([]int64{50, 50}, 0); got != nil {
		t.Errorf("zero total should not allocate, got %v", got)
	}
	// A share that would round away to nothing must abort the split rather
	// than produce a zero-amount split Firefly will reject.
	if got := allocate([]int64{1, 1000000}, 100); got != nil {
		t.Errorf("vanishing share should abort splitting, got %v", got)
	}
}

func TestSplitDescription(t *testing.T) {
	if got := splitDescription([]string{"VET RX", "DOG FOOD"}, "FALLBACK"); got != "VET RX, DOG FOOD" {
		t.Errorf("got %q", got)
	}
	if got := splitDescription(nil, "FALLBACK"); got != "FALLBACK" {
		t.Errorf("empty names should fall back, got %q", got)
	}
	long := make([]string, 40)
	for i := range long {
		long[i] = "ITEM"
	}
	got := splitDescription(long, "FALLBACK")
	if len([]rune(got)) > maxSplitDescription {
		t.Errorf("description not truncated: %d runes", len([]rune(got)))
	}
}
