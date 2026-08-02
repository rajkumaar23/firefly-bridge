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
	categories []string // existing category names (empty if categories disabled)
	budgets    []string // existing budget names (empty if budgets disabled)
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

// Enrich populates the category and/or budget on txn in place. Any failure is
// returned so the caller can log it, but callers should treat enrichment as
// best-effort and still upload the transaction on error.
func (c *Categorizer) Enrich(ctx context.Context, txn *firefly.TransactionSplitStore) error {
	wantCategory := c.cfg.Categories && (c.cfg.OverwriteExisting || isEmpty(txn.CategoryName))
	wantBudget := c.cfg.Budgets && (c.cfg.OverwriteExisting || isEmpty(txn.BudgetName))
	if !wantCategory && !wantBudget {
		return nil
	}

	// Fetch similar past transactions once; reuse for both reuse-first and as
	// few-shot context for the model.
	examples, err := c.ff.FindSimilarTransactions(ctx, keyword(txn.Description), c.cfg.MaxExamples)
	if err != nil {
		// Non-fatal: fall back to the model with no examples.
		c.logger.Debugf("could not fetch similar transactions for %q: %s", txn.Description, err.Error())
	}

	// Reuse-first: honor whatever similar transactions already carry.
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

	// Anything still unresolved gets a single combined model call.
	if !wantCategory && !wantBudget {
		return nil
	}
	return c.askModel(ctx, txn, examples, wantCategory, wantBudget)
}

func (c *Categorizer) askModel(ctx context.Context, txn *firefly.TransactionSplitStore, examples []firefly.TransactionSplit, wantCategory, wantBudget bool) error {
	system := "You label bank transactions. Reply with ONLY a JSON object of the form " +
		`{"category":"","budget":""}. Pick each value from the corresponding allowed list exactly as written. ` +
		"If nothing fits, use an empty string. Never invent names that are not in the lists."

	var b strings.Builder
	fmt.Fprintf(&b, "Transaction: %q, type %s, amount %s\n", txn.Description, txn.Type, txn.Amount)
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

	raw, err := c.client.complete(ctx, system, b.String())
	if err != nil {
		return fmt.Errorf("model call failed: %w", err)
	}

	var out struct {
		Category string `json:"category"`
		Budget   string `json:"budget"`
	}
	if err := json.Unmarshal([]byte(extractJSON(raw)), &out); err != nil {
		return fmt.Errorf("could not parse model output %q: %w", raw, err)
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
