// Package vendor scrapes order history from all-in-one merchants so that a
// bank charge can be categorized from what was actually bought instead of an
// opaque, undifferentiated description. It is a sibling of
// internal/institution: vendors are configured with the same browser-step DSL
// and run in the same shared chromedp session, but only on demand (the
// -vendors flag) — never as part of a regular sync.
package vendor

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/rajkumaar23/firefly-bridge/internal/chromedp"
	"github.com/rajkumaar23/firefly-bridge/internal/utils"
)

// defaultDateWindowDays is how many days a charge date may differ from the
// order date and still match. Card charges typically post at shipment, a few
// days after the order was placed.
const defaultDateWindowDays = 3

// defaultDateLayouts are tried in order when a vendor doesn't configure an
// explicit date_format for its scraped order dates.
var defaultDateLayouts = []string{
	"2006-01-02",
	time.RFC3339,
	"January 2, 2006",
	"Jan 2, 2006",
	"01/02/2006",
}

// Vendor is one entry of the `vendors:` config block.
type Vendor struct {
	Name string `yaml:"name" validate:"required"`
	// Match is a regular expression (applied case-insensitively) that routes a
	// bank transaction's description to this vendor.
	Match string `yaml:"match" validate:"required"`
	// DateFormat is the Go reference layout of the order dates produced by the
	// orders step. When empty, a list of common layouts is tried.
	DateFormat string `yaml:"date_format"`
	// DateWindowDays is how many days a charge date may differ from the order
	// date and still match. Unset defaults to 3; an explicit 0 means the charge
	// must post on the order date itself.
	DateWindowDays *int                   `yaml:"date_window_days"`
	LoginFlow      []chromedp.BrowserStep `yaml:"login" validate:"min=1,dive"`
	OrdersFlow     []chromedp.BrowserStep `yaml:"orders" validate:"min=1,dive"`
}

func (v *Vendor) dateWindowDays() int {
	if v.DateWindowDays != nil && *v.DateWindowDays >= 0 {
		return *v.DateWindowDays
	}
	return defaultDateWindowDays
}

// LineItem is a priced line of an order, used to split a charge across
// categories. Cents is the absolute line amount.
type LineItem struct {
	Name  string
	Cents int64
}

// Order is a parsed vendor order.
type Order struct {
	Date     time.Time
	Cents    int64 // absolute order total in cents
	Items    string
	Category string
	// LineItems is empty unless the vendor's orders JS supplied priced
	// entries; only lines with a parseable amount are kept.
	LineItems []LineItem
}

func (v *Vendor) Login(cdp *chromedp.ChromeDP) error {
	if _, err := cdp.RunSteps(v.LoginFlow); err != nil {
		return err
	}
	return nil
}

// GetOrders runs the vendor's orders flow and parses the scraped rows. Rows
// with an unparseable date or amount (e.g. a pending order with no total yet)
// are skipped with a warning rather than failing the whole scrape.
func (v *Vendor) GetOrders(cdp *chromedp.ChromeDP) ([]Order, error) {
	results, err := cdp.RunSteps(v.OrdersFlow)
	if err != nil {
		return nil, err
	}

	raw, ok := results[chromedp.StepGetOrders].([]chromedp.Order)
	if !ok {
		return nil, fmt.Errorf("failed to retrieve orders")
	}

	logger := utils.GetLogger(cdp.Ctx)
	layouts := defaultDateLayouts
	if v.DateFormat != "" {
		layouts = []string{v.DateFormat}
	}

	orders := make([]Order, 0, len(raw))
	for _, r := range raw {
		order, err := parseOrder(r, layouts)
		if err != nil {
			logger.Warnf("vendor %s: skipping order (%s): %s", v.Name, r.Date, err.Error())
			continue
		}
		orders = append(orders, order)
	}
	return orders, nil
}

// parseOrder converts one scraped row into an Order. Rows whose date or total
// can't be read are rejected; individual line items that can't be priced are
// dropped rather than failing the whole order.
func parseOrder(r chromedp.Order, layouts []string) (Order, error) {
	date, err := parseDate(r.Date, layouts)
	if err != nil {
		return Order{}, fmt.Errorf("unparseable date %q", r.Date)
	}
	cents, err := amountCents(r.Amount)
	if err != nil {
		return Order{}, fmt.Errorf("unparseable amount %q", r.Amount)
	}

	var lines []LineItem
	var names []string
	for _, li := range r.LineItems {
		name := strings.TrimSpace(li.Name)
		if name == "" {
			continue
		}
		names = append(names, name)
		c, err := amountCents(li.Amount)
		if err != nil {
			continue // unpriced line: still worth naming, can't form a split
		}
		lines = append(lines, LineItem{Name: name, Cents: c})
	}

	// Fall back to the joined line names when no summary string was supplied.
	items := strings.TrimSpace(r.Items)
	if items == "" {
		items = strings.Join(names, ", ")
	}

	return Order{
		Date: date, Cents: cents, Items: items,
		Category: r.Category, LineItems: lines,
	}, nil
}

func parseDate(s string, layouts []string) (time.Time, error) {
	for _, layout := range layouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("no layout matched %q", s)
}

// amountCents normalizes an amount string ("$1,234.56", "45.67", "-45.67") to
// absolute cents so scraped and bank-provided amounts compare exactly.
func amountCents(s string) (int64, error) {
	if strings.TrimSpace(s) == "" {
		// ParseAmountFromString treats "" as 0; a missing amount (e.g. a
		// pending order) must not be indexed as a zero-cent order.
		return 0, fmt.Errorf("empty amount")
	}
	f, err := utils.ParseAmountFromString(s)
	if err != nil {
		return 0, err
	}
	return int64(math.Round(math.Abs(f) * 100)), nil
}
