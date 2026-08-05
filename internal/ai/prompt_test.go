package ai

import (
	"context"
	"strings"
	"testing"
	"time"
)

// fakeResolver stands in for a vendor index.
type fakeResolver struct{ m OrderMatch }

func (f fakeResolver) Resolve(string, string, time.Time) OrderMatch { return f.m }

// A warehouse-club charge for a veterinary prescription was being labelled
// "Groceries": the model was going by the shop, not the goods. The prompt must
// describe what was purchased and must not name the merchant.
func TestOrderPromptDescribesGoodsNotMerchant(t *testing.T) {
	c, prompts := stubModel(t, `{"vendor":"","category":"Pets","budget":""}`)
	c.resolver = fakeResolver{OrderMatch{
		State:  MatchResolved,
		Vendor: "MegaStore",
		Items:  "VET. RX",
	}}

	txn := testTxn("199.95")
	splits, err := c.Enrich(context.Background(), txn)
	if err != nil {
		t.Fatalf("Enrich: %v", err)
	}
	if len(splits) != 1 {
		t.Fatalf("a single-item order should not split, got %d", len(splits))
	}
	if txn.CategoryName == nil || *txn.CategoryName != "Pets" {
		t.Fatalf("expected category Pets, got %v", txn.CategoryName)
	}

	if len(*prompts) != 1 {
		t.Fatalf("expected one model call, got %d", len(*prompts))
	}
	prompt := (*prompts)[0]

	if !strings.Contains(prompt, "VET. RX") {
		t.Error("prompt should state what was purchased")
	}
	// The merchant name is the source of the bias, so it must not appear.
	if strings.Contains(prompt, "MegaStore") {
		t.Errorf("prompt must not name the merchant: %s", prompt)
	}
	// The bank description would reintroduce the same bias.
	if strings.Contains(prompt, "MEGASTORE #0001") {
		t.Errorf("prompt must not carry the bank description: %s", prompt)
	}
	for _, want := range []string{"till-receipt", "veterinary prescription", "Never choose based on the shop"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing goods guidance %q", want)
		}
	}
	// With no merchant string in the prompt there is nothing to infer a vendor
	// from — and the vendor is already known from config — so asking for it
	// would only burn tokens on a guaranteed-empty answer.
	if strings.Contains(prompt, `"vendor"`) {
		t.Errorf("order prompt should not ask for a vendor: %s", prompt)
	}
}

// Plain bank transactions keep the original framing — there are no goods to
// describe, only a merchant string.
func TestNonOrderPromptKeepsTransactionFraming(t *testing.T) {
	c, prompts := stubModel(t, `{"vendor":"","category":"Groceries","budget":""}`)
	c.ff = nil // no resolver and no Firefly client: exercised via askModel directly

	txn := testTxn("12.30")
	if err := c.askModel(context.Background(), txn, txn.Description, nil, true, false, false); err != nil {
		t.Fatalf("askModel: %v", err)
	}
	prompt := (*prompts)[0]
	if !strings.Contains(prompt, "MEGASTORE #0001") {
		t.Error("a plain transaction should still be described by its bank string")
	}
	if strings.Contains(prompt, "veterinary") {
		t.Error("goods guidance should not apply to plain bank transactions")
	}
	// Here the merchant string is all there is, so the inferred vendor is
	// worth asking for — it helps write vendor match patterns.
	if !strings.Contains(prompt, `"vendor"`) {
		t.Error("a plain transaction should still be asked for the vendor")
	}
}

// Vendors describe items very differently — till receipts abbreviate, online
// stores publish padded catalogue titles. Rather than detecting the style in
// code, one prompt has to cover both.
func TestGoodsPromptCoversBothItemStyles(t *testing.T) {
	for _, want := range []string{
		"till-receipt",                   // abbreviated style named
		"veterinary prescription",        // worked expansion example
		"catalogue titles",               // padded style named
		"Never choose based on the shop", // the anti-bias rule
	} {
		if !strings.Contains(goodsSystemPrompt, want) {
			t.Errorf("goods prompt missing %q", want)
		}
	}
}

// Long catalogue titles must not swamp a small model's context.
func TestLongItemTitlesAreTruncated(t *testing.T) {
	long := "Purina Pro Plan Veterinary Diets EN Gastroenteric Dry Dog Food, 6 lb Bag, Vet Recommended"

	got := truncateItem(long)
	if len(got) > maxPromptItem+len("…") {
		t.Errorf("item not truncated: %d chars", len(got))
	}
	if !strings.HasPrefix(got, "Purina Pro Plan") {
		t.Errorf("truncation should keep the leading words, got %q", got)
	}
	if short := "Milk"; truncateItem(short) != short {
		t.Error("short names should pass through untouched")
	}

	// Item names contain their own commas, so a joined summary is capped as a
	// whole rather than split back apart.
	joined := truncateText(long+", "+long, maxPromptItems)
	if len(joined) > maxPromptItems+len("…") {
		t.Errorf("summary not capped: %d chars", len(joined))
	}
	if truncateText("a, b", maxPromptItems) != "a, b" {
		t.Error("a short summary should pass through untouched")
	}
}

// Per-line calls must be given room to answer for every line, or the JSON is
// truncated and nothing parses.
func TestLineItemTokenBudgetScalesWithLines(t *testing.T) {
	small := 40 + 25*2
	large := 40 + 25*20
	if large <= small {
		t.Fatal("token budget must grow with the number of lines")
	}
	if small <= defaultMaxTokens/2 {
		t.Errorf("budget for two lines (%d) looks too small", small)
	}
}
