package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/rajkumaar23/firefly-bridge/internal/firefly"
	"github.com/sirupsen/logrus"
)

// Categorizer assigns a category and/or budget to a transaction before upload.
//
// It is designed to complement, not compete with, Firefly III's own rule
// engine:
//   - By default it never overrides a value already present on the transaction
//     (e.g. a category parsed from the source CSV). With OverwriteExisting it
//     may re-map a noisy bank-provided label onto Firefly's taxonomy, but only
//     ever replaces it with another existing value — never blanks it out.
//   - It only chooses from categories/budgets that already exist in Firefly, so
//     it never creates new ones that a rule doesn't know about.
//   - It prefers to reuse the category/budget that similar past transactions
//     already carry (typically assigned by a rule or the user) over asking the
//     model, so established categorizations are mirrored rather than second-
//     guessed. The model is only consulted when there is no clear precedent.
type Categorizer struct {
	cfg        *Config
	client     *chatClient
	ff         *firefly.ClientWithResponses
	logger     *logrus.Entry
	categories []string      // existing category names (empty if categories disabled)
	budgets    []string      // existing budget names (empty if budgets disabled)
	resolver   OrderResolver // optional vendor-order resolver (nil unless -vendors ran)
}

// UseOrderResolver attaches a vendor-order resolver. It is a setter (rather
// than a New parameter) because vendors are scraped after the Categorizer is
// built: the browser session only exists later in startup.
func (c *Categorizer) UseOrderResolver(r OrderResolver) {
	c.resolver = r
}

// New builds a Categorizer, pre-fetching the set of existing categories and/or
// budgets that the model is allowed to pick from. apiKey is passed separately so
// the caller can resolve secret references before construction.
func New(ctx context.Context, cfg *Config, ff *firefly.ClientWithResponses, apiKey string, logger *logrus.Entry) (*Categorizer, error) {
	cfg.applyDefaults()
	cfg.APIKey = apiKey

	c := &Categorizer{
		cfg:    cfg,
		client: newChatClient(cfg),
		ff:     ff,
		logger: logger,
	}

	if cfg.Categories {
		names, err := ff.ListCategoryNames(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to load categories: %w", err)
		}
		sort.Strings(names)
		c.categories = names
		logger.Debugf("loaded %d categories for AI assignment", len(names))
	}
	if cfg.Budgets {
		names, err := ff.ListBudgetNames(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to load budgets: %w", err)
		}
		sort.Strings(names)
		c.budgets = names
		logger.Debugf("loaded %d budgets for AI assignment", len(names))
	}

	return c, nil
}

// Enrich populates the category and/or budget on txn in place and returns the
// splits to upload. That is normally txn itself as a single split; when a
// matched vendor order's items fall into different categories it is one split
// per category, together forming a Firefly split transaction.
//
// Any failure is returned so the caller can log it, but callers should treat
// enrichment as best-effort and still upload the transaction on error.
func (c *Categorizer) Enrich(ctx context.Context, txn *firefly.TransactionSplitStore) ([]firefly.TransactionSplitStore, error) {
	wantCategory := c.cfg.Categories && (c.cfg.OverwriteExisting || isEmpty(txn.CategoryName))
	wantBudget := c.cfg.Budgets && (c.cfg.OverwriteExisting || isEmpty(txn.BudgetName))
	if !wantCategory && !wantBudget {
		return single(txn), nil
	}

	// Vendor-order resolution comes first: for all-in-one merchants the
	// transaction history is ambiguous by design, so neither
	// reuse-first nor same-merchant examples can be trusted. A matched order
	// tells us what was actually bought; no single match means we abstain (the
	// charge lands in the review report) rather than guess.
	if c.resolver != nil {
		switch m := c.resolver.Resolve(txn.Description, txn.Amount, txn.Date); m.State {
		case MatchResolved:
			return c.enrichFromOrder(ctx, txn, m, wantCategory, wantBudget)
		case MatchUnresolved:
			c.logger.Infof("abstaining on %q (%s): %s", txn.Description, m.Vendor, m.Reason)
			return single(txn), nil
		}
	}

	// Fetch similar past transactions once; reuse for both reuse-first and as
	// few-shot context for the model.
	examples, err := c.ff.FindSimilarTransactions(ctx, keyword(txn.Description), c.cfg.MaxExamples)
	if err != nil {
		// Non-fatal: fall back to the model with no examples.
		c.logger.Debugf("could not fetch similar transactions for %q: %s", txn.Description, err.Error())
	}

	// Reuse-first: honor whatever similar transactions already carry, unless the
	// user has opted to always consult the model (examples are still passed to
	// it as few-shot context below).
	if !c.cfg.AlwaysAskModel {
		if wantCategory {
			if v, ok := consensus(exampleValues(examples, func(s firefly.TransactionSplit) *string { return s.CategoryName })); ok {
				if canonical, valid := match(v, c.categories); valid {
					txn.CategoryName = &canonical
					wantCategory = false
					c.logger.Debugf("reused category %q for %q from similar transactions", canonical, txn.Description)
				}
			}
		}
		if wantBudget {
			if v, ok := consensus(exampleValues(examples, func(s firefly.TransactionSplit) *string { return s.BudgetName })); ok {
				if canonical, valid := match(v, c.budgets); valid {
					txn.BudgetName = &canonical
					wantBudget = false
					c.logger.Debugf("reused budget %q for %q from similar transactions", canonical, txn.Description)
				}
			}
		}
	}

	// Anything still unresolved gets a single combined model call.
	if !wantCategory && !wantBudget {
		return single(txn), nil
	}
	if err := c.askModel(ctx, txn, txn.Description, examples, wantCategory, wantBudget, false); err != nil {
		return single(txn), err
	}
	return single(txn), nil
}

// single wraps a transaction as the one-element split list of an unsplit
// upload.
func single(txn *firefly.TransactionSplitStore) []firefly.TransactionSplitStore {
	return []firefly.TransactionSplitStore{*txn}
}

// enrichFromOrder categorizes a transaction from the contents of the vendor
// order it was matched to, instead of the opaque bank description.
func (c *Categorizer) enrichFromOrder(ctx context.Context, txn *firefly.TransactionSplitStore, m OrderMatch, wantCategory, wantBudget bool) ([]firefly.TransactionSplitStore, error) {
	c.logger.Debugf("matched %q to %s order: %s", txn.Description, m.Vendor, m.Items)

	// Surface the matched items in the notes so the assignment is explainable
	// when reviewing the transaction in Firefly later.
	if isEmpty(txn.Notes) && m.Items != "" {
		notes := fmt.Sprintf("%s order: %s", m.Vendor, m.Items)
		txn.Notes = &notes
	}

	// A merchant-provided order category may map straight onto Firefly's
	// taxonomy — no model call needed for that field.
	if wantCategory && m.Category != "" {
		if canonical, valid := match(m.Category, c.categories); valid {
			txn.CategoryName = &canonical
			wantCategory = false
			c.logger.Debugf("assigned category %q to %q from %s order category", canonical, txn.Description, m.Vendor)
		}
	}
	if !wantCategory && !wantBudget {
		return single(txn), nil
	}

	// The order contents stand in for the opaque bank description everywhere
	// below — and deliberately without the vendor name: naming the shop pulls
	// small models toward whatever that shop mostly sells (a warehouse club
	// makes everything look like groceries) instead of the goods in hand. No
	// few-shot examples either: this vendor's history is ambiguous by
	// definition, and Firefly's descriptions are bank strings that won't match
	// item text.
	desc := m.Items

	// Per-item splitting: one order can span several categories (pet supplies
	// and medicine in the same basket), which is exactly what a Firefly split
	// transaction represents. Category and budget are decided together per
	// line in one call, so each split lands in its own budget rather than
	// every split inheriting a single verdict about the whole basket.
	if wantCategory && c.cfg.splitOrders() && len(m.LineItems) >= 2 {
		if splits := c.splitByLineItems(ctx, txn, m, wantBudget); splits != nil {
			if len(splits) > 1 {
				c.logger.Infof("splitting %q into %d categories from %s order", txn.Description, len(splits), m.Vendor)
			}
			return splits, nil
		}
		// Splitting didn't apply; fall through and categorize as a whole.
	}

	if err := c.askModel(ctx, txn, desc, nil, wantCategory, wantBudget, true); err != nil {
		return single(txn), err
	}
	return single(txn), nil
}

// askModel labels a transaction in one call. When fromOrder is set, desc holds
// what was actually purchased rather than the bank's description, so the model
// is told to categorize the goods instead of the merchant.
func (c *Categorizer) askModel(ctx context.Context, txn *firefly.TransactionSplitStore, desc string, examples []firefly.TransactionSplit, wantCategory, wantBudget, fromOrder bool) error {
	var b strings.Builder
	var system string
	if fromOrder {
		// desc is what was bought, with no merchant string attached — so there
		// is nothing for the model to infer a vendor from, and we already know
		// which vendor matched. Asking for it would only waste tokens.
		system = goodsSystemPrompt +
			"Reply with ONLY a JSON object of the form " +
			`{"category":"","budget":""}. ` +
			"Pick each value from the corresponding allowed list exactly as written. " +
			"If nothing fits, use an empty string. Never invent names that are not in the lists."
		fmt.Fprintf(&b, "Purchased: %s, total %s\n", truncateText(desc, maxPromptItems), txn.Amount)
	} else {
		system = "You label bank transactions. Reply with ONLY a JSON object of the form " +
			`{"vendor":"","category":"","budget":""}. vendor is the canonical merchant name you infer from the transaction. ` +
			"Pick category and budget from the corresponding allowed list exactly as written. " +
			"If nothing fits, use an empty string. Never invent names that are not in the lists."
		fmt.Fprintf(&b, "Transaction: %q, type %s, amount %s\n", desc, txn.Type, txn.Amount)
	}
	if wantCategory {
		fmt.Fprintf(&b, "Allowed categories: %s\n", strings.Join(c.categories, ", "))
		// A non-empty value here is the source/bank label (reuse-first didn't
		// resolve it). Offer it as a hint to map onto the allowed list.
		if !isEmpty(txn.CategoryName) {
			fmt.Fprintf(&b, "The source labeled the category as %q; map it to the closest allowed category.\n", strings.TrimSpace(*txn.CategoryName))
		}
	}
	if wantBudget {
		fmt.Fprintf(&b, "Allowed budgets: %s\n", strings.Join(c.budgets, ", "))
		if !isEmpty(txn.BudgetName) {
			fmt.Fprintf(&b, "The source labeled the budget as %q; map it to the closest allowed budget.\n", strings.TrimSpace(*txn.BudgetName))
		}
	}
	if ex := formatExamples(examples, wantCategory, wantBudget); ex != "" {
		fmt.Fprintf(&b, "Similar past transactions:\n%s", ex)
	}

	userPrompt := b.String()
	c.logger.Debugf("model request for %q:\n--- system ---\n%s\n--- user ---\n%s", txn.Description, system, userPrompt)

	raw, err := c.client.complete(ctx, system, userPrompt)
	if err != nil {
		return fmt.Errorf("model call failed: %w", err)
	}
	c.logger.Debugf("model response for %q:\n%s", txn.Description, raw)

	var out struct {
		Vendor   string `json:"vendor"`
		Category string `json:"category"`
		Budget   string `json:"budget"`
	}
	if err := json.Unmarshal([]byte(extractJSON(raw)), &out); err != nil {
		return fmt.Errorf("could not parse model output %q: %w", raw, err)
	}

	// The inferred vendor is informational and only asked for on the non-order
	// path: it helps when writing match patterns for the vendors config block.
	if v := strings.TrimSpace(out.Vendor); v != "" {
		c.logger.Debugf("model inferred vendor %q for %q", v, txn.Description)
	}

	if wantCategory {
		if canonical, valid := match(out.Category, c.categories); valid {
			txn.CategoryName = &canonical
			c.logger.Debugf("assigned category %q to %q", canonical, txn.Description)
		} else if strings.TrimSpace(out.Category) != "" {
			c.logger.Debugf("model returned unknown category %q for %q, ignoring", out.Category, txn.Description)
		}
	}
	if wantBudget {
		if canonical, valid := match(out.Budget, c.budgets); valid {
			txn.BudgetName = &canonical
			c.logger.Debugf("assigned budget %q to %q", canonical, txn.Description)
		} else if strings.TrimSpace(out.Budget) != "" {
			c.logger.Debugf("model returned unknown budget %q for %q, ignoring", out.Budget, txn.Description)
		}
	}
	return nil
}
