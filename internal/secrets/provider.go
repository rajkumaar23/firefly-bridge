package secrets

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Provider is the interface that all secret providers must implement
// Each provider is responsible for parsing its own URI format
type Provider interface {
	// Name returns the provider identifier (e.g., "op" for 1Password, "bw" for Bitwarden)
	Name() string

	// GetSecret retrieves a secret value from the provider given a URI
	// The URI format is provider-specific (e.g., "op://vault/item/field" for 1Password)
	GetSecret(ctx context.Context, uri string) (string, error)
}

// Manager manages multiple secret providers and resolves secret references
type Manager struct {
	providers map[string]Provider
	// inlineRefRegex matches embedded references for every registered scheme.
	// It is rebuilt whenever a provider is registered so no single provider has
	// to know about any other provider's URI scheme.
	inlineRefRegex *regexp.Regexp
}

// NewManager creates a new secret manager
func NewManager() *Manager {
	return &Manager{
		providers: make(map[string]Provider),
	}
}

// Register adds a provider to the manager
func (m *Manager) Register(provider Provider) {
	m.providers[provider.Name()] = provider
	m.rebuildInlineRefRegex()
}

// rebuildInlineRefRegex compiles a regex that matches `<scheme>://...` for every
// registered provider's scheme, stopping at quote/backtick characters that would
// delimit the reference inside JS or YAML.
func (m *Manager) rebuildInlineRefRegex() {
	if len(m.providers) == 0 {
		m.inlineRefRegex = nil
		return
	}
	schemes := make([]string, 0, len(m.providers))
	for name := range m.providers {
		schemes = append(schemes, regexp.QuoteMeta(name))
	}
	sort.Strings(schemes) // deterministic ordering
	m.inlineRefRegex = regexp.MustCompile(`(?:` + strings.Join(schemes, "|") + `)://[^"'` + "`" + `]+`)
}

// ResolveRefs replaces every embedded secret reference within a larger string s,
// leaving all surrounding text untouched. Each registered provider contributes
// its own scheme (via Name), so this stays modular as providers are added.
// Unlike Resolve, it does not treat the whole string as a single reference,
// making it suitable for JS snippets or YAML values with inline refs.
func (m *Manager) ResolveRefs(ctx context.Context, s string) (string, error) {
	if m == nil || m.inlineRefRegex == nil {
		return s, nil
	}
	var resolveErr error
	result := m.inlineRefRegex.ReplaceAllStringFunc(s, func(ref string) string {
		if resolveErr != nil {
			return ref
		}
		resolved, err := m.Resolve(ctx, ref)
		if err != nil {
			resolveErr = err
			return ref
		}
		return resolved
	})
	return result, resolveErr
}

// Resolve resolves a secret reference string to its actual value
// If the input is not a secret reference (doesn't contain "://"), it returns the input unchanged
func (m *Manager) Resolve(ctx context.Context, value string) (string, error) {
	value = strings.TrimSpace(value)

	// Check if this looks like a secret reference
	if !strings.Contains(value, "://") {
		return value, nil
	}

	// Extract provider name from URI scheme
	parts := strings.SplitN(value, "://", 2)
	if len(parts) != 2 {
		return "", fmt.Errorf("invalid secret reference format: %s", value)
	}

	providerName := parts[0]
	provider, ok := m.providers[providerName]
	if !ok {
		return "", fmt.Errorf("unknown secret provider: %s", providerName)
	}

	return provider.GetSecret(ctx, value)
}

// SecretsConfig represents the secrets configuration
type SecretsConfig struct {
	OnePassword *OnePasswordConfig `yaml:"onepassword,omitempty"`
	Bitwarden   *BitwardenConfig   `yaml:"bitwarden,omitempty"`
}

// OnePasswordConfig represents 1Password provider configuration
type OnePasswordConfig struct {
	Token string `yaml:"token" validate:"required"`
}

// BitwardenConfig configures the Bitwarden Password Manager provider, which
// resolves bw://item/field references by shelling out to the `bw` CLI.
type BitwardenConfig struct {
	// Session is the vault unlock key produced by `bw unlock`. Optional — when
	// empty, the ambient BW_SESSION environment variable is used instead.
	Session string `yaml:"session,omitempty"`
	// ServerURL points the CLI at a non-default server (EU cloud, self-hosted,
	// or Vaultwarden). Optional; defaults to Bitwarden US cloud.
	ServerURL string `yaml:"server_url,omitempty"`
	// AppDataDir overrides the CLI's data directory (BITWARDENCLI_APPDATA_DIR),
	// useful for isolating firefly-bridge's Bitwarden state. Optional.
	AppDataDir string `yaml:"appdata_dir,omitempty"`
	// BWPath is the path to the `bw` binary. Optional; defaults to "bw" on PATH.
	BWPath string `yaml:"bw_path,omitempty"`
}

// NewManagerFromConfig creates a secret manager and registers providers based on the config
func NewManagerFromConfig(ctx context.Context, config *SecretsConfig) (*Manager, error) {
	manager := NewManager()

	if config == nil {
		return manager, nil
	}

	// Register 1Password provider if configured
	if config.OnePassword != nil && config.OnePassword.Token != "" {
		provider, err := NewOnePasswordProvider(ctx, config.OnePassword.Token)
		if err != nil {
			return nil, fmt.Errorf("failed to create 1Password provider: %w", err)
		}
		manager.Register(provider)
	}

	// Register Bitwarden provider if configured
	if config.Bitwarden != nil {
		provider, err := NewBitwardenProvider(ctx, config.Bitwarden)
		if err != nil {
			return nil, fmt.Errorf("failed to create Bitwarden provider: %w", err)
		}
		manager.Register(provider)
	}

	return manager, nil
}
