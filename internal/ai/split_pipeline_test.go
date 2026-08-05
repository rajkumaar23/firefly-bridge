package ai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rajkumaar23/firefly-bridge/internal/firefly"
	"github.com/sirupsen/logrus"
)

// stubModel serves an OpenAI-compatible endpoint that replies with the given
// content, and records the prompts it was sent. Messages are recorded decoded
// rather than as the raw request body, so assertions see the text the model
// sees — the body has every quote escaped, which silently defeats substring
// checks for JSON field names.
func stubModel(t *testing.T, content string) (*Categorizer, *[]string) {
	t.Helper()
	var prompts []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Messages []struct{ Content string } `json:"messages"`
		}
		if err := json.Unmarshal(body, &req); err != nil {
			t.Errorf("stub model got undecodable request: %v", err)
		}
		var sb strings.Builder
		for _, m := range req.Messages {
			sb.WriteString(m.Content)
			sb.WriteString("\n")
		}
		prompts = append(prompts, sb.String())
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{map[string]any{"message": map[string]any{"content": content}}},
		})
	}))
	t.Cleanup(srv.Close)

	logger := logrus.New()
	logger.SetOutput(io.Discard)
	cfg := &Config{BaseURL: srv.URL, Model: "test", Categories: true}
	cfg.applyDefaults()
	return &Categorizer{
		cfg:        cfg,
		client:     newChatClient(cfg),
		logger:     logger.WithField("component", "ai"),
		categories: []string{"Pets", "Groceries", "Medicine"},
	}, &prompts
}

// testRef mirrors the dedupe hash institution.GetTransactions stamps onto a
// scraped transaction before enrichment.
const testRef = "0f1e2d3c4b5a69788796a5b4c3d2e1f0"

func testTxn(amount string) *firefly.TransactionSplitStore {
	ref, src, tag := testRef, "12", []string{"bridge-test"}
	return &firefly.TransactionSplitStore{
		Amount:            amount,
		Description:       "MEGASTORE #0001",
		Date:              time.Date(2026, 7, 29, 0, 0, 0, 0, time.UTC),
		Type:              firefly.Withdrawal,
		InternalReference: &ref,
		SourceId:          &src,
		Tags:              &tag,
	}
}

func TestSplitByLineItemsAcrossCategories(t *testing.T) {
	c, prompts := stubModel(t, `{"items":[{"n":1,"category":"Medicine"},{"n":2,"category":"Groceries"},{"n":3,"category":"Groceries"}]}`)

	// Items total 154.87 but the charge is 199.95 — tax makes up the rest.
	txn := testTxn("199.95")
	m := OrderMatch{State: MatchResolved, Vendor: "MegaStore", LineItems: []OrderLineItem{
		{Name: "VET RX", Cents: 14237},
		{Name: "BANANAS", Cents: 750},
		{Name: "MILK", Cents: 500},
	}}

	wantRef := *txn.InternalReference

	splits := c.splitByLineItems(context.Background(), txn, m, false)
	if len(splits) != 2 {
		t.Fatalf("expected 2 splits (Medicine, Groceries), got %d", len(splits))
	}

	// The splits must reconcile to the charge exactly.
	var sum int64
	for _, s := range splits {
		cents, err := parseCents(s.Amount)
		if err != nil {
			t.Fatalf("split amount %q unparseable: %v", s.Amount, err)
		}
		sum += cents
	}
	if want, _ := parseCents(txn.Amount); sum != want {
		t.Errorf("splits sum to %d, want %d", sum, want)
	}

	if got := *splits[0].CategoryName; got != "Medicine" {
		t.Errorf("first split category = %q, want Medicine", got)
	}
	if got := splits[0].Description; got != "VET RX" {
		t.Errorf("first split description = %q, want the item name", got)
	}
	if got := *splits[1].CategoryName; got != "Groceries" {
		t.Errorf("second split category = %q, want Groceries", got)
	}
	if got := splits[1].Description; got != "BANANAS, MILK" {
		t.Errorf("second split should list its items, got %q", got)
	}

	// Every split must carry the original charge's internal reference, or a
	// re-run would not recognise the group and would upload duplicates.
	for i, s := range splits {
		if s.InternalReference == nil {
			t.Fatalf("split %d lost its internal reference", i)
		}
		if *s.InternalReference != wantRef {
			t.Errorf("split %d internal reference = %q, want %q", i, *s.InternalReference, wantRef)
		}
		if s.Date != txn.Date || s.Type != txn.Type {
			t.Errorf("split %d lost date/type", i)
		}
		if s.Order == nil || *s.Order != int32(i) {
			t.Errorf("split %d has wrong order", i)
		}
		if s.SourceId == nil || *s.SourceId != *txn.SourceId {
			t.Errorf("split %d lost its source account", i)
		}
		if s.Tags == nil || len(*s.Tags) != 1 || (*s.Tags)[0] != "bridge-test" {
			t.Errorf("split %d lost its tags", i)
		}
	}

	if len(*prompts) != 1 {
		t.Errorf("expected exactly one model call, got %d", len(*prompts))
	}
	if !strings.Contains((*prompts)[0], "VET RX") {
		t.Error("prompt should list the line items")
	}
}

// A basket spans several budgets, so each split needs its own — copying one
// whole-transaction budget onto every split charges the wrong envelope.
func TestSplitAssignsBudgetPerSplit(t *testing.T) {
	c, prompts := stubModel(t, `{"items":[
		{"n":1,"category":"Medicine","budget":"Healthcare"},
		{"n":2,"category":"Groceries","budget":"Groceries"}]}`)
	c.budgets = []string{"Healthcare", "Groceries", "Pets"}

	txn := testTxn("100.00")
	m := OrderMatch{State: MatchResolved, Vendor: "MegaStore", LineItems: []OrderLineItem{
		{Name: "VET RX", Cents: 5000},
		{Name: "BANANAS", Cents: 5000},
	}}

	splits := c.splitByLineItems(context.Background(), txn, m, true)
	if len(splits) != 2 {
		t.Fatalf("expected 2 splits, got %d", len(splits))
	}
	if splits[0].BudgetName == nil || *splits[0].BudgetName != "Healthcare" {
		t.Errorf("first split budget = %v, want Healthcare", splits[0].BudgetName)
	}
	if splits[1].BudgetName == nil || *splits[1].BudgetName != "Groceries" {
		t.Errorf("second split budget = %v, want Groceries", splits[1].BudgetName)
	}

	// Both fields come from the one call — a small model shouldn't be asked twice.
	if len(*prompts) != 1 {
		t.Fatalf("expected one model call, got %d", len(*prompts))
	}
	if !strings.Contains((*prompts)[0], "Allowed budgets") {
		t.Error("prompt should offer the budget list when budgets are wanted")
	}
}

// When the model gives no usable budget, a budget named like the category is
// the next best guess — mirroring the two is a common setup.
func TestSplitBudgetFallsBackToCategoryName(t *testing.T) {
	c, _ := stubModel(t, `{"items":[
		{"n":1,"category":"Pets","budget":"Nonsense"},
		{"n":2,"category":"Groceries","budget":""}]}`)
	c.budgets = []string{"Pets", "Groceries"}

	splits := c.splitByLineItems(context.Background(), testTxn("100.00"), OrderMatch{
		State: MatchResolved, LineItems: []OrderLineItem{
			{Name: "DOG FOOD", Cents: 5000},
			{Name: "BANANAS", Cents: 5000},
		}}, true)
	if len(splits) != 2 {
		t.Fatalf("expected 2 splits, got %d", len(splits))
	}
	for i, want := range []string{"Pets", "Groceries"} {
		if splits[i].BudgetName == nil || *splits[i].BudgetName != want {
			t.Errorf("split %d budget = %v, want %s", i, splits[i].BudgetName, want)
		}
	}
}

// Budgets are only touched when they were asked for.
func TestSplitLeavesBudgetAloneWhenNotWanted(t *testing.T) {
	c, prompts := stubModel(t, `{"items":[
		{"n":1,"category":"Pets"},{"n":2,"category":"Groceries"}]}`)
	c.budgets = []string{"Pets", "Groceries"}

	splits := c.splitByLineItems(context.Background(), testTxn("100.00"), OrderMatch{
		State: MatchResolved, LineItems: []OrderLineItem{
			{Name: "DOG FOOD", Cents: 5000},
			{Name: "BANANAS", Cents: 5000},
		}}, false)
	for i, s := range splits {
		if s.BudgetName != nil {
			t.Errorf("split %d got budget %q without being asked", i, *s.BudgetName)
		}
	}
	if strings.Contains((*prompts)[0], "Allowed budgets") {
		t.Error("prompt should not mention budgets when they aren't wanted")
	}
}

func TestSplitByLineItemsSingleCategory(t *testing.T) {
	c, _ := stubModel(t, `{"items":[{"n":1,"category":"Groceries"},{"n":2,"category":"Groceries"}]}`)

	txn := testTxn("20.00")
	m := OrderMatch{State: MatchResolved, Vendor: "MegaStore", LineItems: []OrderLineItem{
		{Name: "BANANAS", Cents: 750},
		{Name: "MILK", Cents: 500},
	}}

	splits := c.splitByLineItems(context.Background(), txn, m, false)
	if len(splits) != 1 {
		t.Fatalf("one category should not split, got %d", len(splits))
	}
	if txn.CategoryName == nil || *txn.CategoryName != "Groceries" {
		t.Error("the whole transaction should be labelled Groceries")
	}
	if splits[0].Description != txn.Description {
		t.Errorf("unsplit charge should keep its description, got %q", splits[0].Description)
	}
	if splits[0].Amount != "20.00" {
		t.Errorf("unsplit charge should keep the full amount, got %q", splits[0].Amount)
	}
}

func TestSplitByLineItemsIgnoresUnknownCategories(t *testing.T) {
	// The model invents one category and omits another line entirely.
	c, _ := stubModel(t, `{"items":[{"n":1,"category":"Pets"},{"n":2,"category":"Nonsense"}]}`)

	txn := testTxn("100.00")
	m := OrderMatch{State: MatchResolved, Vendor: "MegaStore", LineItems: []OrderLineItem{
		{Name: "DOG FOOD", Cents: 5000},
		{Name: "MYSTERY", Cents: 5000},
	}}

	splits := c.splitByLineItems(context.Background(), txn, m, false)
	// Only one usable category, so this is a label, not a split — and the
	// unusable line's value is absorbed rather than dropped.
	if len(splits) != 1 {
		t.Fatalf("expected 1 split, got %d", len(splits))
	}
	if txn.CategoryName == nil || *txn.CategoryName != "Pets" {
		t.Error("expected the transaction to be labelled Pets")
	}
}

func TestSplitByLineItemsSkipsWhenNotApplicable(t *testing.T) {
	c, prompts := stubModel(t, `{"items":[]}`)
	ctx := context.Background()

	// Fewer than two priced lines.
	if got := c.splitByLineItems(ctx, testTxn("10.00"), OrderMatch{
		LineItems: []OrderLineItem{{Name: "ONLY", Cents: 1000}},
	}, false); got != nil {
		t.Errorf("single line should not split, got %v", got)
	}
	// Unpriced lines can't form splits.
	if got := c.splitByLineItems(ctx, testTxn("10.00"), OrderMatch{
		LineItems: []OrderLineItem{{Name: "A"}, {Name: "B"}},
	}, false); got != nil {
		t.Errorf("unpriced lines should not split, got %v", got)
	}
	// Unusable transaction amount.
	if got := c.splitByLineItems(ctx, testTxn("not-a-number"), OrderMatch{
		LineItems: []OrderLineItem{{Name: "A", Cents: 100}, {Name: "B", Cents: 100}},
	}, false); got != nil {
		t.Errorf("bad amount should not split, got %v", got)
	}
	if len(*prompts) != 0 {
		t.Errorf("no model call should be made when splitting can't apply, got %d", len(*prompts))
	}
}

func TestSplitOrdersConfigDefault(t *testing.T) {
	if !(&Config{}).splitOrders() {
		t.Error("splitting should be enabled by default")
	}
	off := false
	if (&Config{SplitOrders: &off}).splitOrders() {
		t.Error("split_orders: false should disable splitting")
	}
}
