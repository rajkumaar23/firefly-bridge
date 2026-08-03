package vendor

import (
	"testing"
	"time"

	"github.com/rajkumaar23/firefly-bridge/internal/ai"
)

func date(s string) time.Time {
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		panic(err)
	}
	return t
}

func TestAmountCents(t *testing.T) {
	tests := []struct {
		in      string
		want    int64
		wantErr bool
	}{
		{"45.67", 4567, false},
		{"$45.67", 4567, false},
		{"-45.67", 4567, false},
		{"$1,234.56", 123456, false},
		{"1234", 123400, false},
		{"", 0, true},
		{"—", 0, true},
	}
	for _, tt := range tests {
		got, err := amountCents(tt.in)
		if (err != nil) != tt.wantErr {
			t.Errorf("amountCents(%q) error = %v, wantErr %v", tt.in, err, tt.wantErr)
			continue
		}
		if !tt.wantErr && got != tt.want {
			t.Errorf("amountCents(%q) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

func TestParseDate(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"2026-07-15", "2026-07-15"},
		{"July 15, 2026", "2026-07-15"},
		{"Jul 15, 2026", "2026-07-15"},
		{"07/15/2026", "2026-07-15"},
	}
	for _, tt := range tests {
		got, err := parseDate(tt.in, defaultDateLayouts)
		if err != nil {
			t.Errorf("parseDate(%q) unexpected error: %v", tt.in, err)
			continue
		}
		if got.Format(time.DateOnly) != tt.want {
			t.Errorf("parseDate(%q) = %s, want %s", tt.in, got.Format(time.DateOnly), tt.want)
		}
	}
	if _, err := parseDate("not a date", defaultDateLayouts); err == nil {
		t.Error("parseDate(\"not a date\") expected error, got none")
	}
}

func newTestIndex(t *testing.T, orders []Order, windowDays *int) *Index {
	t.Helper()
	idx := NewIndex()
	v := &Vendor{Name: "MegaStore", Match: "MEGASTORE|MEGA STR", DateWindowDays: windowDays}
	if err := idx.Add(v, orders); err != nil {
		t.Fatalf("Add: %v", err)
	}
	return idx
}

func TestResolveNotAVendor(t *testing.T) {
	idx := newTestIndex(t, nil, nil)
	m := idx.Resolve("CORNER CAFE 1234", "5.60", date("2026-07-15"))
	if m.State != ai.MatchNotAVendor {
		t.Errorf("expected NotAVendor, got %+v", m)
	}
	if len(idx.Report()) != 0 {
		t.Errorf("NotAVendor must not produce a report entry, got %d", len(idx.Report()))
	}
}

func TestResolveSingleMatch(t *testing.T) {
	idx := newTestIndex(t, []Order{
		{Date: date("2026-07-14"), Cents: 4567, Items: "Dog food, cat litter", Category: "Pets"},
		{Date: date("2026-07-01"), Cents: 1299, Items: "USB cable"},
	}, nil)

	// Charge posts two days after the order date; matches case-insensitively.
	m := idx.Resolve("MEGA STR #4471*2X4Y5", "$45.67", date("2026-07-16"))
	if m.State != ai.MatchResolved {
		t.Fatalf("expected Resolved, got %+v", m)
	}
	if m.Items != "Dog food, cat litter" || m.Category != "Pets" || m.Vendor != "MegaStore" {
		t.Errorf("unexpected match: %+v", m)
	}
}

func TestResolveNoMatchAbstains(t *testing.T) {
	idx := newTestIndex(t, []Order{
		{Date: date("2026-07-14"), Cents: 4567, Items: "Dog food"},
	}, nil)

	m := idx.Resolve("MEGASTORE.COM", "99.99", date("2026-07-15"))
	if m.State != ai.MatchUnresolved {
		t.Fatalf("expected Unresolved, got %+v", m)
	}
	report := idx.Report()
	if len(report) != 1 {
		t.Fatalf("expected 1 report entry, got %d", len(report))
	}
	if report[0].Vendor != "MegaStore" || report[0].Description != "MEGASTORE.COM" {
		t.Errorf("unexpected report entry: %+v", report[0])
	}
}

func TestResolveAmbiguousAbstains(t *testing.T) {
	idx := newTestIndex(t, []Order{
		{Date: date("2026-07-14"), Cents: 4567, Items: "Dog food"},
		{Date: date("2026-07-15"), Cents: 4567, Items: "Vitamins"},
	}, nil)

	m := idx.Resolve("MEGASTORE.COM", "45.67", date("2026-07-15"))
	if m.State != ai.MatchUnresolved {
		t.Fatalf("expected Unresolved for two different orders, got %+v", m)
	}
}

func TestResolveDuplicateScrapeStillResolves(t *testing.T) {
	// The same order scraped twice (e.g. overlapping history pages) is not an
	// ambiguity.
	idx := newTestIndex(t, []Order{
		{Date: date("2026-07-14"), Cents: 4567, Items: "Dog food"},
		{Date: date("2026-07-14"), Cents: 4567, Items: "Dog food"},
	}, nil)

	m := idx.Resolve("MEGASTORE.COM", "45.67", date("2026-07-15"))
	if m.State != ai.MatchResolved {
		t.Fatalf("expected Resolved for duplicate orders, got %+v", m)
	}
}

func TestResolveDateWindow(t *testing.T) {
	orders := []Order{{Date: date("2026-07-10"), Cents: 4567, Items: "Dog food"}}

	// Default window (3 days): 3 days out matches, 4 does not.
	idx := newTestIndex(t, orders, nil)
	if m := idx.Resolve("MEGASTORE.COM", "45.67", date("2026-07-13")); m.State != ai.MatchResolved {
		t.Errorf("expected Resolved at window edge, got %+v", m)
	}
	if m := idx.Resolve("MEGASTORE.COM", "45.67", date("2026-07-14")); m.State != ai.MatchUnresolved {
		t.Errorf("expected Unresolved outside window, got %+v", m)
	}

	// Explicit 0: same day only.
	zero := 0
	idx = newTestIndex(t, orders, &zero)
	if m := idx.Resolve("MEGASTORE.COM", "45.67", date("2026-07-10")); m.State != ai.MatchResolved {
		t.Errorf("expected Resolved same-day with window 0, got %+v", m)
	}
	if m := idx.Resolve("MEGASTORE.COM", "45.67", date("2026-07-11")); m.State != ai.MatchUnresolved {
		t.Errorf("expected Unresolved next-day with window 0, got %+v", m)
	}
}

// A refund carries a negative total and must resolve against the matching
// bank credit — amounts are compared on absolute value.
func TestResolveRefundMatchesCredit(t *testing.T) {
	idx := newTestIndex(t, []Order{
		{Date: date("2026-07-14"), Cents: 1104, Items: "Refund: leafy greens"},
	}, nil)
	if m := idx.Resolve("MEGASTORE.COM", "11.04", date("2026-07-15")); m.State != ai.MatchResolved {
		t.Errorf("refund should resolve against the bank credit, got %+v", m)
	}
}

func TestAddInvalidRegex(t *testing.T) {
	idx := NewIndex()
	if err := idx.Add(&Vendor{Name: "Bad", Match: "("}, nil); err == nil {
		t.Error("expected error for invalid match regex, got none")
	}
}
