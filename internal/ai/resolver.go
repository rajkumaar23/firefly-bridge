package ai

import "time"

// MatchState describes the outcome of resolving a transaction against a
// vendor's scraped order history.
type MatchState int

const (
	// MatchNotAVendor means the transaction does not belong to any scraped
	// vendor; normal enrichment applies.
	MatchNotAVendor MatchState = iota
	// MatchResolved means exactly one order matched; its contents describe
	// what the charge actually was.
	MatchResolved
	// MatchUnresolved means the transaction belongs to a scraped vendor but no
	// single order matched. The categorizer abstains rather than guess.
	MatchUnresolved
)

// OrderMatch is the result of OrderResolver.Resolve.
type OrderMatch struct {
	State  MatchState
	Vendor string // configured vendor name (Resolved/Unresolved only)
	Items  string // item summary of the matched order (Resolved only)
	// Category is the merchant-provided category of the matched order, if the
	// vendor exposes one (e.g. a department name). May map directly onto a
	// Firefly category without a model call.
	Category string
	Reason   string // human-readable reason (Unresolved only)
}

// OrderResolver matches a bank transaction to a vendor order so charges from
// ambiguous all-in-one merchants can be categorized from what was actually
// bought instead of the opaque bank description. Implemented by
// internal/vendor.Index; this package only defines the contract so it stays
// decoupled from the scraping machinery.
type OrderResolver interface {
	Resolve(description, amount string, date time.Time) OrderMatch
}
