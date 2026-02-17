package secrets

import (
	"context"
	"fmt"
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
}

// OnePasswordConfig represents 1Password provider configuration
type OnePasswordConfig struct {
	Token string `yaml:"token" validate:"required"`
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

	return manager, nil
}
