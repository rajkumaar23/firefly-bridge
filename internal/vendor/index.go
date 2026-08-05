package vendor

import (
	"fmt"
	"regexp"
	"time"

	"github.com/rajkumaar23/firefly-bridge/internal/ai"
)

type indexedVendor struct {
	name       string
	re         *regexp.Regexp
	windowDays int
	orders     []Order
}

// Index holds the scraped orders of every successfully-processed vendor and
// implements ai.OrderResolver. Vendors whose scrape failed are simply never
// added, so their transactions fall through to normal enrichment instead of
// being abstained on. The run is single-threaded, so no locking is needed.
type Index struct {
	vendors []indexedVendor
}

func NewIndex() *Index {
	return &Index{}
}

// Add registers a vendor and its scraped orders for resolution.
func (idx *Index) Add(v *Vendor, orders []Order) error {
	// Bank descriptions are wildly inconsistent in casing; match without it.
	re, err := regexp.Compile("(?i)" + v.Match)
	if err != nil {
		return fmt.Errorf("invalid match pattern for vendor %s: %w", v.Name, err)
	}
	idx.vendors = append(idx.vendors, indexedVendor{
		name:       v.Name,
		re:         re,
		windowDays: v.dateWindowDays(),
		orders:     orders,
	})
	return nil
}

// Resolve implements ai.OrderResolver: route the description to a vendor via
// its match regex, then look for exactly one order with the same absolute
// amount whose date is within the vendor's window of the charge date.
func (idx *Index) Resolve(description, amount string, date time.Time) ai.OrderMatch {
	for _, v := range idx.vendors {
		if !v.re.MatchString(description) {
			continue
		}

		cents, err := amountCents(amount)
		if err != nil {
			return unresolved(v.name, fmt.Sprintf("unparseable transaction amount %q", amount))
		}

		var hits []Order
		for _, o := range v.orders {
			if o.Cents == cents && withinDays(date, o.Date, v.windowDays) {
				hits = append(hits, o)
			}
		}

		switch {
		case len(hits) == 0:
			return unresolved(v.name,
				fmt.Sprintf("no order of %s within %d day(s)", amount, v.windowDays))
		case sameItems(hits):
			// One order, possibly scraped more than once (e.g. overlapping
			// history pages) — still an unambiguous match.
			lines := make([]ai.OrderLineItem, 0, len(hits[0].LineItems))
			for _, li := range hits[0].LineItems {
				lines = append(lines, ai.OrderLineItem{Name: li.Name, Cents: li.Cents})
			}
			return ai.OrderMatch{
				State:     ai.MatchResolved,
				Vendor:    v.name,
				Items:     hits[0].Items,
				LineItems: lines,
				Category:  hits[0].Category,
			}
		default:
			return unresolved(v.name,
				fmt.Sprintf("%d different orders of %s within %d day(s)", len(hits), amount, v.windowDays))
		}
	}
	return ai.OrderMatch{State: ai.MatchNotAVendor}
}

// unresolved marks a charge the categorizer should abstain on. The caller
// logs the reason per transaction.
func unresolved(vendor, reason string) ai.OrderMatch {
	return ai.OrderMatch{State: ai.MatchUnresolved, Vendor: vendor, Reason: reason}
}

// withinDays reports whether a and b fall within n calendar days of each
// other, ignoring the time-of-day component.
func withinDays(a, b time.Time, n int) bool {
	ad := time.Date(a.Year(), a.Month(), a.Day(), 0, 0, 0, 0, time.UTC)
	bd := time.Date(b.Year(), b.Month(), b.Day(), 0, 0, 0, 0, time.UTC)
	diff := ad.Sub(bd)
	if diff < 0 {
		diff = -diff
	}
	return diff <= time.Duration(n)*24*time.Hour
}

func sameItems(orders []Order) bool {
	for _, o := range orders[1:] {
		if o.Items != orders[0].Items {
			return false
		}
	}
	return true
}
