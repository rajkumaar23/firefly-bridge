package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/rajkumaar23/firefly-bridge/internal/firefly"
)

// maxSplitDescription caps how much of an item list is used as a split's
// description, so the Firefly UI stays readable.
const maxSplitDescription = 120

// maxPromptItem caps how much of one item name is sent to the model. Online
// order titles run to a couple of hundred characters of brand and packaging
// copy; the first words carry the classification signal and the rest is
// context a small local model can't spare.
const maxPromptItem = 80

// maxPromptItems caps a whole joined item summary in a single-call prompt.
const maxPromptItems = 600

// goodsSystemPrompt frames every "what was bought" call.
//
// Vendors describe items very differently — a till receipt abbreviates
// ("VET. RX"), an online store publishes a long catalogue title padded with
// brand and packaging copy — so rather than detecting the style in code and
// picking a prompt, the prompt covers both and lets the model recognise which
// it is looking at. That also degrades gracefully on formats neither of us
// anticipated.
//
// The final sentence exists because of an observed failure: a charge from a
// warehouse club was labelled "Groceries" wholesale, even for a veterinary
// prescription. The category has to follow the product, not the shop.
const goodsSystemPrompt = "You categorize purchased goods. " +
	"Item names come from the merchant's own records and vary in style. " +
	"Some are abbreviated till-receipt text, upper-case and truncated: expand them, e.g. " +
	`"VET. RX" is a veterinary prescription (pet medicine), "ORG SPINACH" is organic spinach, ` +
	`"PPR TWL" is paper towels, "RX" alone means a pharmacy prescription. ` +
	"Others are full catalogue titles padded with brand, model and packaging detail: " +
	"look past that to the kind of product being sold. " +
	"Either way, work out what the product actually is, then choose the category that fits it. " +
	"Never choose based on the shop it was bought from — a general store sells goods " +
	"from many different categories, so the shop tells you nothing about the item. "

// truncateItem shortens one item name for a prompt.
func truncateItem(name string) string {
	name = strings.TrimSpace(name)
	if len(name) <= maxPromptItem {
		return name
	}
	return strings.TrimSpace(name[:maxPromptItem]) + "…"
}

// truncateText caps a whole item summary. Item names contain commas of their
// own, so the joined string can't be reliably split back apart — cap the total
// instead, which is all the model's context budget actually cares about.
func truncateText(s string, max int) string {
	s = strings.TrimSpace(s)
	if len(s) <= max {
		return s
	}
	return strings.TrimSpace(s[:max]) + "…"
}

// itemGroup is a set of line items that share a category.
type itemGroup struct {
	category string
	names    []string
	budgets  []string // per-line budgets, reduced to one for the split
	cents    int64
}

// lineLabel is the model's verdict on one line.
type lineLabel struct {
	category string
	budget   string
}

// splitByLineItems categorizes each priced line of a matched order and returns
// the splits to upload:
//
//   - nil when splitting doesn't apply (no priced lines, the model failed, or
//     the amounts can't be reconciled) — the caller falls back to categorizing
//     the charge as a whole.
//   - one split when every line landed in the same category: not a split at
//     all, just the transaction with that category set.
//   - one split per category otherwise, together forming a Firefly split
//     transaction whose amounts sum exactly to the charge.
func (c *Categorizer) splitByLineItems(ctx context.Context, txn *firefly.TransactionSplitStore, m OrderMatch, wantBudget bool) []firefly.TransactionSplitStore {
	// Unpriced lines can't carry an amount, so they can't form a split.
	var lines []OrderLineItem
	for _, li := range m.LineItems {
		if li.Cents > 0 && strings.TrimSpace(li.Name) != "" {
			lines = append(lines, li)
		}
	}
	if len(lines) < 2 {
		return nil
	}

	total, err := parseCents(txn.Amount)
	if err != nil || total <= 0 {
		c.logger.Debugf("cannot split %q: unusable transaction amount %q", txn.Description, txn.Amount)
		return nil
	}

	labels, err := c.categorizeItems(ctx, txn, lines, wantBudget)
	if err != nil {
		c.logger.Warnf("could not categorize line items for %q: %s", txn.Description, err.Error())
		return nil
	}

	// Group by category, preserving first-seen order so output is stable.
	var groups []*itemGroup
	byCategory := map[string]*itemGroup{}
	var uncategorized int64
	for i, li := range lines {
		cat := labels[i].category
		if cat == "" {
			// Fold unknown lines into the whole-transaction remainder rather
			// than inventing a category for them.
			uncategorized += li.Cents
			continue
		}
		g, ok := byCategory[cat]
		if !ok {
			g = &itemGroup{category: cat}
			byCategory[cat] = g
			groups = append(groups, g)
		}
		g.names = append(g.names, li.Name)
		if b := labels[i].budget; b != "" {
			g.budgets = append(g.budgets, b)
		}
		g.cents += li.Cents
	}
	if len(groups) == 0 {
		c.logger.Debugf("no line item of %q matched a known category", txn.Description)
		return nil
	}

	// A single category isn't a split — just label the whole transaction.
	if len(groups) == 1 {
		cat := groups[0].category
		txn.CategoryName = &cat
		if wantBudget {
			if b := c.budgetFor(groups[0], txn); b != "" {
				txn.BudgetName = &b
			}
		}
		c.logger.Debugf("assigned category %q to %q (all items agree)", cat, txn.Description)
		return []firefly.TransactionSplitStore{*txn}
	}

	// Line items rarely sum to the charge exactly — tax, fees, discounts and
	// any uncategorized lines are missing. Scale each group proportionally so
	// the splits add up to the amount actually billed.
	weights := make([]int64, len(groups))
	for i, g := range groups {
		weights[i] = g.cents
	}
	amounts := allocate(weights, total)
	if amounts == nil {
		c.logger.Debugf("cannot split %q: amounts do not reconcile", txn.Description)
		return nil
	}
	if uncategorized > 0 {
		c.logger.Debugf("split of %q distributed %s of uncategorized lines proportionally",
			txn.Description, formatCents(uncategorized))
	}

	splits := make([]firefly.TransactionSplitStore, 0, len(groups))
	for i, g := range groups {
		// Copying the transaction carries over date, accounts, type, tags,
		// budget and — critically — InternalReference. Every split must keep
		// the original charge's hash: TransactionExists looks a charge up by
		// internal_reference, so a re-run finds the existing group and skips
		// it instead of uploading duplicates. Only the fields set below may
		// differ between splits.
		s := *txn
		s.Amount = formatCents(amounts[i])
		category := g.category
		s.CategoryName = &category
		// Each split gets its own budget: one basket can span several, and
		// copying a single whole-transaction budget onto every split would
		// charge the wrong envelope.
		if wantBudget {
			if b := c.budgetFor(g, txn); b != "" {
				s.BudgetName = &b
			}
		}
		s.Description = splitDescription(g.names, txn.Description)
		order := int32(i)
		s.Order = &order
		splits = append(splits, s)
		c.logger.Debugf("split %q: %s → %s / %s (%s)",
			txn.Description, s.Amount, category, budgetLabel(s.BudgetName), s.Description)
	}
	return splits
}

// budgetFor picks a group's budget, in decreasing order of confidence: what
// the model said about the group's own lines, then the budget of the same name
// as the group's category (users commonly mirror the two), then whatever
// budget the transaction already carried.
func (c *Categorizer) budgetFor(g *itemGroup, txn *firefly.TransactionSplitStore) string {
	if b := mostCommon(g.budgets); b != "" {
		return b
	}
	if canonical, ok := match(g.category, c.budgets); ok {
		return canonical
	}
	if !isEmpty(txn.BudgetName) {
		return strings.TrimSpace(*txn.BudgetName)
	}
	return ""
}

// mostCommon returns the most frequent value, preferring the earliest on a
// tie. Unlike consensus it does not require a majority: any signal beats none.
func mostCommon(values []string) string {
	counts := make(map[string]int, len(values))
	best, bestN := "", 0
	for _, v := range values {
		counts[v]++
		if counts[v] > bestN {
			best, bestN = v, counts[v]
		}
	}
	return best
}

func budgetLabel(b *string) string {
	if isEmpty(b) {
		return "no budget"
	}
	return *b
}

// categorizeItems labels every line in a single call — category, and budget
// too when budgets are enabled, since a basket can span several budgets and
// each split needs its own. Lines are numbered so the model echoes indices
// rather than long names, keeping the response small for local models.
func (c *Categorizer) categorizeItems(ctx context.Context, txn *firefly.TransactionSplitStore, lines []OrderLineItem, wantBudget bool) (map[int]lineLabel, error) {
	shape := `{"items":[{"n":1,"category":""}]}`
	fields := "Pick each category from the allowed list exactly as written."
	if wantBudget {
		shape = `{"items":[{"n":1,"category":"","budget":""}]}`
		fields = "Pick each category and budget from the corresponding allowed list exactly as written."
	}
	system := goodsSystemPrompt +
		"Reply with ONLY a JSON object of the form " + shape + ", " +
		"one entry per line, using the line numbers given. " + fields + " " +
		"If nothing fits, use an empty string. Never invent names that are not in the lists."

	var b strings.Builder
	fmt.Fprintf(&b, "Allowed categories: %s\n", strings.Join(c.categories, ", "))
	if wantBudget {
		fmt.Fprintf(&b, "Allowed budgets: %s\n", strings.Join(c.budgets, ", "))
	}
	b.WriteString("Lines:\n")
	for i, li := range lines {
		fmt.Fprintf(&b, "%d. %s (%s)\n", i+1, truncateItem(li.Name), formatCents(li.Cents))
	}

	userPrompt := b.String()
	c.logger.Debugf("line item request for %q:\n--- system ---\n%s\n--- user ---\n%s", txn.Description, system, userPrompt)

	// A category-only entry costs roughly 20 tokens, one carrying a budget
	// nearer 35; without headroom the reply is truncated mid-object and fails
	// to parse.
	perLine := 25
	if wantBudget {
		perLine = 40
	}
	raw, err := c.client.completeWithTokens(ctx, system, userPrompt, 40+perLine*len(lines))
	if err != nil {
		return nil, fmt.Errorf("model call failed: %w", err)
	}
	c.logger.Debugf("line item response for %q:\n%s", txn.Description, raw)

	var out struct {
		Items []struct {
			N        int    `json:"n"`
			Category string `json:"category"`
			Budget   string `json:"budget"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(extractJSON(raw)), &out); err != nil {
		return nil, fmt.Errorf("could not parse model output %q: %w", raw, err)
	}

	labels := make(map[int]lineLabel, len(out.Items))
	for _, item := range out.Items {
		idx := item.N - 1 // prompt is 1-based
		if idx < 0 || idx >= len(lines) {
			continue
		}
		var l lineLabel
		if canonical, valid := match(item.Category, c.categories); valid {
			l.category = canonical
		} else if strings.TrimSpace(item.Category) != "" {
			c.logger.Debugf("model returned unknown category %q for line %q, ignoring", item.Category, lines[idx].Name)
		}
		if wantBudget {
			if canonical, valid := match(item.Budget, c.budgets); valid {
				l.budget = canonical
			} else if strings.TrimSpace(item.Budget) != "" {
				c.logger.Debugf("model returned unknown budget %q for line %q, ignoring", item.Budget, lines[idx].Name)
			}
		}
		labels[idx] = l
	}
	return labels, nil
}

// allocate distributes total across weights proportionally, using the
// largest-remainder method so the result sums to total exactly. It returns nil
// if the inputs are unusable or any share would round away to nothing (Firefly
// rejects zero-amount splits).
func allocate(weights []int64, total int64) []int64 {
	var sum int64
	for _, w := range weights {
		sum += w
	}
	if sum <= 0 || total <= 0 {
		return nil
	}

	out := make([]int64, len(weights))
	rems := make([]struct{ idx, frac int64 }, len(weights))
	var assigned int64
	for i, w := range weights {
		num := w * total
		out[i] = num / sum
		assigned += out[i]
		rems[i] = struct{ idx, frac int64 }{int64(i), num % sum}
	}

	// Hand the leftover cents to the largest fractional remainders.
	sort.SliceStable(rems, func(a, b int) bool { return rems[a].frac > rems[b].frac })
	for i := 0; assigned < total; i++ {
		out[rems[i%len(rems)].idx]++
		assigned++
	}

	for _, v := range out {
		if v <= 0 {
			return nil
		}
	}
	return out
}

// splitDescription renders a split's description from its item names, falling
// back to the original transaction description.
func splitDescription(names []string, fallback string) string {
	joined := strings.Join(names, ", ")
	if joined == "" {
		return fallback
	}
	if len(joined) > maxSplitDescription {
		joined = strings.TrimSpace(joined[:maxSplitDescription-1]) + "…"
	}
	return joined
}

// parseCents converts a Firefly amount string ("142.37") to cents.
func parseCents(s string) (int64, error) {
	var whole, frac int64
	var neg bool
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "-") {
		neg, s = true, s[1:]
	}
	parts := strings.SplitN(s, ".", 2)
	if _, err := fmt.Sscanf(parts[0], "%d", &whole); err != nil {
		return 0, fmt.Errorf("unparseable amount %q", s)
	}
	if len(parts) == 2 {
		d := parts[1] + "00"
		if _, err := fmt.Sscanf(d[:2], "%d", &frac); err != nil {
			return 0, fmt.Errorf("unparseable amount %q", s)
		}
	}
	cents := whole*100 + frac
	if neg {
		cents = -cents
	}
	return cents, nil
}

// formatCents renders cents as a Firefly amount string.
func formatCents(cents int64) string {
	if cents < 0 {
		cents = -cents
	}
	return fmt.Sprintf("%d.%02d", cents/100, cents%100)
}
