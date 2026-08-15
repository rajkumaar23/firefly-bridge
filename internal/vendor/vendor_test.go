package vendor

import (
	"strings"
	"testing"
	"time"

	"github.com/rajkumaar23/firefly-bridge/internal/ai"
	"github.com/rajkumaar23/firefly-bridge/internal/chromedp"
	"gopkg.in/yaml.v3"
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

func TestParseOrderLineItems(t *testing.T) {
	got, err := parseOrder(chromedp.Order{
		Date:   "2026-07-29",
		Amount: "$199.95",
		LineItems: []chromedp.OrderLineItem{
			{Name: "Dog food", Amount: "$142.37"},
			{Name: "  ", Amount: "1.00"},    // nameless line: ignored entirely
			{Name: "Gift wrap", Amount: ""}, // unpriced: named but can't be split on
			{Name: "Vitamins", Amount: "38.91"},
		},
	}, defaultDateLayouts)
	if err != nil {
		t.Fatalf("parseOrder: %v", err)
	}

	if got.Cents != 19995 {
		t.Errorf("total = %d, want 19995", got.Cents)
	}
	// Only priced lines can form splits.
	if len(got.LineItems) != 2 {
		t.Fatalf("expected 2 priced lines, got %+v", got.LineItems)
	}
	if got.LineItems[0].Name != "Dog food" || got.LineItems[0].Cents != 14237 {
		t.Errorf("unexpected first line: %+v", got.LineItems[0])
	}
	// The summary still names every usable line, priced or not.
	if got.Items != "Dog food, Gift wrap, Vitamins" {
		t.Errorf("items summary = %q", got.Items)
	}
}

func TestParseOrderKeepsExplicitSummary(t *testing.T) {
	got, err := parseOrder(chromedp.Order{
		Date: "2026-07-29", Amount: "10.00", Items: "Weekly shop",
		LineItems: []chromedp.OrderLineItem{{Name: "Milk", Amount: "10.00"}},
	}, defaultDateLayouts)
	if err != nil {
		t.Fatalf("parseOrder: %v", err)
	}
	if got.Items != "Weekly shop" {
		t.Errorf("explicit items should win, got %q", got.Items)
	}
}

func TestParseOrderRejectsUnusableRows(t *testing.T) {
	if _, err := parseOrder(chromedp.Order{Date: "nope", Amount: "10.00"}, defaultDateLayouts); err == nil {
		t.Error("bad date should be rejected")
	}
	if _, err := parseOrder(chromedp.Order{Date: "2026-07-29", Amount: ""}, defaultDateLayouts); err == nil {
		t.Error("missing amount should be rejected")
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
	if m.Vendor != "" || m.Reason != "" {
		t.Errorf("NotAVendor should carry no vendor or reason, got %+v", m)
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

// Priced lines must reach the categorizer, which needs them to build splits.
func TestResolveCarriesLineItems(t *testing.T) {
	idx := newTestIndex(t, []Order{{
		Date: date("2026-07-14"), Cents: 4567, Items: "Dog food, vitamins",
		LineItems: []LineItem{
			{Name: "Dog food", Cents: 3300},
			{Name: "Vitamins", Cents: 1000},
		},
	}}, nil)

	m := idx.Resolve("MEGASTORE.COM", "45.67", date("2026-07-15"))
	if m.State != ai.MatchResolved {
		t.Fatalf("expected Resolved, got %+v", m)
	}
	if len(m.LineItems) != 2 {
		t.Fatalf("expected 2 line items on the match, got %+v", m.LineItems)
	}
	if m.LineItems[0].Name != "Dog food" || m.LineItems[0].Cents != 3300 {
		t.Errorf("unexpected line item: %+v", m.LineItems[0])
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
	// The reason is logged per transaction, so it must say what went wrong.
	if m.Vendor != "MegaStore" {
		t.Errorf("abstention should name the vendor, got %q", m.Vendor)
	}
	if !strings.Contains(m.Reason, "99.99") {
		t.Errorf("abstention reason should mention the unmatched amount, got %q", m.Reason)
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

func TestLogoutFlowUnmarshals(t *testing.T) {
	const yamlDoc = `
name: Test Store
match: "TEST STORE"
login:
  - type: navigate
    url: "https://store.example.com/login"
logout:
  - type: click
    selector: "#account-menu"
  - type: click
    selector: "#sign-out"
orders:
  - type: orders
    evaluate: "[]"
`
	var v Vendor
	if err := yaml.Unmarshal([]byte(yamlDoc), &v); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(v.LogoutFlow) != 2 {
		t.Fatalf("LogoutFlow len = %d, want 2", len(v.LogoutFlow))
	}
	if got := v.LogoutFlow[0].Step.Type(); got != chromedp.StepClick {
		t.Errorf("logout step 0 type = %s, want %s", got, chromedp.StepClick)
	}
	if got := v.LogoutFlow[1].Step.Type(); got != chromedp.StepClick {
		t.Errorf("logout step 1 type = %s, want %s", got, chromedp.StepClick)
	}
}
