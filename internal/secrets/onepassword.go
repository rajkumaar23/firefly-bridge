package secrets

import (
	"context"
	"fmt"
	"regexp"

	"github.com/1password/onepassword-sdk-go"
)

// OnePasswordProvider implements the Provider interface for 1Password
// It uses the 1Password SDK to retrieve secrets
// URI format: op://vault/item/field
type OnePasswordProvider struct {
	client *onepassword.Client
}

// onePasswordURIPattern matches 1Password URIs like "op://vault/item/field/"
var onePasswordURIPattern = regexp.MustCompile(`^op://([^/]+)/([^/]+)/(.+)$`)

// NewOnePasswordProvider creates a new 1Password provider
func NewOnePasswordProvider(ctx context.Context, token string) (*OnePasswordProvider, error) {
	if token == "" {
		return nil, fmt.Errorf("1Password token cannot be empty")
	}

	client, err := onepassword.NewClient(ctx,
		onepassword.WithServiceAccountToken(token),
		onepassword.WithIntegrationInfo("Firefly Bridge", "v1.0.0"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create 1Password client: %w", err)
	}

	// NewClient only initializes the SDK locally; an invalid or expired token is
	// not detected until the first secret is resolved. Make one authenticated
	// call now so a bad token fails at startup rather than mid-sync.
	if _, err := client.Vaults().List(ctx); err != nil {
		return nil, fmt.Errorf("failed to authenticate with 1Password (check the service account token): %w", err)
	}

	return &OnePasswordProvider{
		client: client,
	}, nil
}

// Name returns the provider identifier
func (p *OnePasswordProvider) Name() string {
	return "op"
}

// GetSecret retrieves a secret from 1Password using the SDK
func (p *OnePasswordProvider) GetSecret(ctx context.Context, uri string) (string, error) {
	// Validate the URI format
	if !onePasswordURIPattern.MatchString(uri) {
		return "", fmt.Errorf("invalid 1Password URI format: %s (expected: op://vault/item/field)", uri)
	}

	// Use 1Password SDK's Resolve method which handles the full URI
	secret, err := p.client.Secrets().Resolve(ctx, uri)
	if err != nil {
		return "", fmt.Errorf("failed to resolve 1Password secret %s: %w", uri, err)
	}

	return secret, nil
}
