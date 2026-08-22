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

// Orders without an effective date keep the symmetric window on BOTH sides:
// a charge that posts a couple of days before the order date still matches.
func TestResolveSymmetricBackwardWindow(t *testing.T) {
	idx := newTestIndex(t, []Order{{
		Date: date("2026-07-10"), Cents: 4567, Items: "Dog food",
	}}, nil) // default 3-day window, no effective date

	// 2 days before the order date: inside the symmetric window.
	if m := idx.Resolve("MEGASTORE.COM", "45.67", date("2026-07-08")); m.State != ai.MatchResolved {
		t.Errorf("expected Resolved 2 days before order (symmetric window), got %+v", m)
	}
	// 3 days before: window edge, still in.
	if m := idx.Resolve("MEGASTORE.COM", "45.67", date("2026-07-07")); m.State != ai.MatchResolved {
		t.Errorf("expected Resolved at backward window edge, got %+v", m)
	}
	// 4 days before: out.
	if m := idx.Resolve("MEGASTORE.COM", "45.67", date("2026-07-06")); m.State != ai.MatchUnresolved {
		t.Errorf("expected Unresolved 4 days before order, got %+v", m)
	}
}

// Effective-date matching: a charge that posts long after the order date but
// within window of the shipment/delivery date must resolve.
func TestResolveEffectiveDateExtendsRange(t *testing.T) {
	idx := newTestIndex(t, []Order{{
		Date:          date("2026-08-08"),
		EffectiveDate: date("2026-08-21"),
		Cents:         734,
		Items:         "Shower curtain",
	}}, nil) // default 3-day window

	// 13 days after the order date (out of the old symmetric window) but 0
	// days after delivery: resolves.
	if m := idx.Resolve("MEGASTORE.COM", "$7.34", date("2026-08-21")); m.State != ai.MatchResolved {
		t.Errorf("expected Resolved on delivery date, got %+v", m)
	}
	// 3 days after delivery: still inside the window.
	if m := idx.Resolve("MEGASTORE.COM", "$7.34", date("2026-08-24")); m.State != ai.MatchResolved {
		t.Errorf("expected Resolved at window edge after delivery, got %+v", m)
	}
	// 4 days after delivery: out.
	if m := idx.Resolve("MEGASTORE.COM", "$7.34", date("2026-08-25")); m.State != ai.MatchUnresolved {
		t.Errorf("expected Unresolved 4 days after delivery, got %+v", m)
	}
	// Day before the order: the range starts one day before the order date
	// (immediate billing at checkout).
	if m := idx.Resolve("MEGASTORE.COM", "$7.34", date("2026-08-07")); m.State != ai.MatchResolved {
		t.Errorf("expected Resolved day before order, got %+v", m)
	}
	// Two days before the order: out.
	if m := idx.Resolve("MEGASTORE.COM", "$7.34", date("2026-08-06")); m.State != ai.MatchUnresolved {
		t.Errorf("expected Unresolved 2 days before order, got %+v", m)
	}
}

// A charge inside the gap between order date and delivery date is inside the
// range (shipment happened somewhere in between).
func TestResolveChargeBetweenOrderAndDelivery(t *testing.T) {
	idx := newTestIndex(t, []Order{{
		Date:          date("2026-08-08"),
		EffectiveDate: date("2026-08-21"),
		Cents:         734,
		Items:         "Shower curtain",
	}}, nil)
	if m := idx.Resolve("MEGASTORE.COM", "$7.34", date("2026-08-15")); m.State != ai.MatchResolved {
		t.Errorf("expected Resolved mid-range, got %+v", m)
	}
}

// An order with an effective date earlier than its order date (should not
// happen, but a bad scrape could produce it) still yields a sensible range.
func TestResolveEffectiveDateBeforeOrder(t *testing.T) {
	idx := newTestIndex(t, []Order{{
		Date:          date("2026-08-21"),
		EffectiveDate: date("2026-08-08"),
		Cents:         734,
		Items:         "Shower curtain",
	}}, nil)
	// Range is [08-07, 08-21+3=08-24] regardless of which is lo/hi.
	if m := idx.Resolve("MEGASTORE.COM", "$7.34", date("2026-08-07")); m.State != ai.MatchResolved {
		t.Errorf("expected Resolved at range start, got %+v", m)
	}
	if m := idx.Resolve("MEGASTORE.COM", "$7.34", date("2026-08-24")); m.State != ai.MatchResolved {
		t.Errorf("expected Resolved at range end, got %+v", m)
	}
}

func TestParseEffectiveDateYearless(t *testing.T) {
	od := date("2026-08-20")
	got := parseEffectiveDate("August 21", od, nil)
	if got.Format(time.DateOnly) != "2026-08-21" {
		t.Errorf("yearless August 21 anchored to 2026-08-20 order = %s, want 2026-08-21", got.Format(time.DateOnly))
	}
	// December order, delivered in January: rolls forward one year.
	got = parseEffectiveDate("January 2", date("2026-12-28"), nil)
	if got.Format(time.DateOnly) != "2027-01-02" {
		t.Errorf("Jan 2 anchored to 2026-12-28 order = %s, want 2027-01-02", got.Format(time.DateOnly))
	}
	// January order, delivered in December of the previous year: rolls back.
	got = parseEffectiveDate("December 30", date("2027-01-03"), nil)
	if got.Format(time.DateOnly) != "2026-12-30" {
		t.Errorf("Dec 30 anchored to 2027-01-03 order = %s, want 2026-12-30", got.Format(time.DateOnly))
	}
	// A full date within 45 days of the order passes through.
	got = parseEffectiveDate("2027-01-02", date("2026-12-28"), nil)
	if got.Format(time.DateOnly) != "2027-01-02" {
		t.Errorf("full date mangled: %s", got.Format(time.DateOnly))
	}
	// A full date more than 45 days from the order is dropped — a garbage
	// full date must not open a wide match range.
	if !parseEffectiveDate("2027-06-15", date("2026-08-20"), nil).IsZero() {
		t.Error("far-off full date should be dropped")
	}
	// A date more than 45 days from the order in every candidate year is
	// dropped: a garbage value must not open a match range.
	got = parseEffectiveDate("July 4", date("2026-08-20"), nil)
	if !got.IsZero() {
		t.Errorf("far-off date should be dropped, got %s", got.Format(time.DateOnly))
	}
	// Unparseable input drops silently.
	if !parseEffectiveDate("nonsense", od, nil).IsZero() {
		t.Error("unparseable input should be dropped")
	}
}

// Parsed orders surface the effective date when the scrape supplied one, and
// leave it zero otherwise.
func TestParseOrderEffectiveDate(t *testing.T) {
	got, err := parseOrder(chromedp.Order{
		Date: "August 20, 2026", Amount: "$9.92", Items: "Liner",
		EffectiveDate: "August 21",
	}, []string{"January 2, 2006"})
	if err != nil {
		t.Fatalf("parseOrder: %v", err)
	}
	if got.EffectiveDate.Format(time.DateOnly) != "2026-08-21" {
		t.Errorf("effective date = %s, want 2026-08-21", got.EffectiveDate.Format(time.DateOnly))
	}

	got, err = parseOrder(chromedp.Order{Date: "August 20, 2026", Amount: "10.00"}, defaultDateLayouts)
	if err != nil {
		t.Fatalf("parseOrder: %v", err)
	}
	if !got.EffectiveDate.IsZero() {
		t.Errorf("expected zero effective date, got %s", got.EffectiveDate.Format(time.DateOnly))
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
