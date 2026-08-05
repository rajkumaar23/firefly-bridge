// Package ai provides optional, LLM-assisted enrichment of transactions before
// they are uploaded to Firefly III. It talks to any OpenAI-compatible
// chat-completions endpoint (llama.cpp, Ollama's OpenAI shim, LocalAI, OpenAI
// itself, …) so it can run against a small self-hosted model.
//
// The package is intentionally self-contained: the only firefly-bridge package
// it depends on is internal/firefly, and it is wired in from main via a single
// Categorizer.Enrich call. When no ai config block is present, nothing here runs.
package ai

// Config is the `ai:` section of the firefly-bridge YAML config. It is optional;
// when omitted (or Enabled is false) transactions are uploaded unchanged.
type Config struct {
	// Enabled turns the whole feature on. Defaults to false so existing configs
	// behave exactly as before.
	Enabled bool `yaml:"enabled"`

	// BaseURL is the OpenAI-compatible API root, including the version segment,
	// e.g. "http://jetson.local:8080/v1". Chat completions are POSTed to
	// "{BaseURL}/chat/completions".
	BaseURL string `yaml:"base_url" validate:"required_if=Enabled true,omitempty,http_url"`

	// APIKey is sent as a Bearer token. Optional for local servers that don't
	// require auth. Supports ${ENV:...} expansion and op:// secret references,
	// resolved by the caller before the Categorizer is built.
	APIKey string `yaml:"api_key"`

	// Model is the model name passed to the endpoint, e.g. "qwen2.5:9b".
	Model string `yaml:"model" validate:"required_if=Enabled true"`

	// Categories, when true, lets the model assign a category. Budgets does the
	// same for budgets. Both default to false so the user opts into each
	// independently.
	Categories bool `yaml:"categories"`
	Budgets    bool `yaml:"budgets"`

	// AlwaysAskModel, when true, disables the reuse-first shortcut: the model is
	// consulted for every wanted field even when similar past transactions agree
	// on a value. Those past values are still passed to the model as few-shot
	// context. Enable this when you don't trust historical labels and want the
	// model to have the final say. Note this bypasses the "mirror existing rule
	// assignments" behavior. Defaults to false.
	AlwaysAskModel bool `yaml:"always_ask_model"`

	// OverwriteExisting, when true, lets enrichment run even for transactions
	// that already carry a category/budget (e.g. one parsed from the source
	// CSV). Banks often emit noisy or unhelpful labels; enabling this lets them
	// be re-mapped onto the tidy taxonomy in Firefly. A pre-existing value is
	// only ever replaced with a better one that exists in Firefly — it is never
	// blanked out. Defaults to false (existing values are left untouched).
	OverwriteExisting bool `yaml:"overwrite_existing"`

	// SplitOrders controls what happens when a matched vendor order's priced
	// line items fall into different categories. When true (the default), the
	// charge is uploaded as a Firefly split transaction with one split per
	// category, whose amounts are scaled to sum exactly to the charge. Set it
	// to false to categorize such charges as a single transaction instead.
	// Only ever applies to vendors whose orders step supplies line_items.
	SplitOrders *bool `yaml:"split_orders"`

	// MaxExamples caps how many similar past transactions are fetched and shown
	// to the model as few-shot context. Kept small on purpose to fit tiny local
	// models' context windows. Defaults to 5 when unset.
	MaxExamples int `yaml:"max_examples"`

	// TimeoutSeconds bounds each chat-completion request. Defaults to 30.
	TimeoutSeconds int `yaml:"timeout_seconds"`
}

const (
	defaultMaxExamples    = 5
	defaultTimeoutSeconds = 30
)

// splitOrders reports whether multi-category vendor orders become Firefly
// split transactions. Unset means enabled.
func (c *Config) splitOrders() bool {
	return c.SplitOrders == nil || *c.SplitOrders
}

// applyDefaults fills zero-valued optional fields with their defaults.
func (c *Config) applyDefaults() {
	if c.MaxExamples <= 0 {
		c.MaxExamples = defaultMaxExamples
	}
	if c.TimeoutSeconds <= 0 {
		c.TimeoutSeconds = defaultTimeoutSeconds
	}
}
