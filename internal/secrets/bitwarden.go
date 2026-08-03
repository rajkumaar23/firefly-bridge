package secrets

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
)

// BitwardenProvider implements the Provider interface for the Bitwarden
// Password Manager vault. Unlike 1Password, Bitwarden's Go SDK only covers
// Secrets Manager, so structured login items (username/password/custom fields)
// are read by shelling out to the official `bw` CLI.
//
// URI format: bw://<item>/<field>
//   - <item>  an item name (matched by the CLI's search) or an item ID (UUID)
//   - <field> one of the built-in fields (username, password, totp, notes, uri)
//     or the name of a custom field defined on the item
type BitwardenProvider struct {
	bwPath  string   // path to the `bw` binary
	session string   // BW_SESSION unlock key (optional; falls back to ambient env)
	env     []string // precomputed environment for `bw` invocations
}

// bitwardenURIPattern matches bw://<item>/<field>. The item segment is greedy so
// item names may themselves contain slashes; the field is always the final segment.
var bitwardenURIPattern = regexp.MustCompile(`^bw://(.+)/([^/]+)$`)

// builtinBitwardenFields are the field names that `bw get <field> <item>`
// understands directly and returns as a raw scalar. Any other field name is
// looked up as a custom field on the item's JSON.
var builtinBitwardenFields = map[string]bool{
	"username": true,
	"password": true,
	"totp":     true,
	"notes":    true,
	"uri":      true,
}

// NewBitwardenProvider creates a new Bitwarden provider backed by the `bw` CLI.
func NewBitwardenProvider(ctx context.Context, cfg *BitwardenConfig) (*BitwardenProvider, error) {
	if cfg == nil {
		return nil, fmt.Errorf("bitwarden config cannot be nil")
	}

	bwPath := cfg.BWPath
	if bwPath == "" {
		bwPath = "bw"
	}

	p := &BitwardenProvider{
		bwPath:  bwPath,
		session: cfg.Session,
	}

	// Build the environment once: inherit the parent env (so an ambient
	// BW_SESSION keeps working) and layer our overrides on top.
	p.env = os.Environ()
	if cfg.AppDataDir != "" {
		p.env = append(p.env, "BITWARDENCLI_APPDATA_DIR="+cfg.AppDataDir)
	}

	// Point the CLI at a non-default server (EU cloud, self-hosted, Vaultwarden)
	// when requested. The `bw` CLI stores the server as part of its login state,
	// so `bw config server` fails once you are logged in — which itself means the
	// server is already configured. Tolerate that case and only surface genuine
	// failures (e.g. an unreachable or malformed URL).
	if cfg.ServerURL != "" {
		if _, err := p.run(ctx, "config", "server", cfg.ServerURL); err != nil {
			if !strings.Contains(strings.ToLower(err.Error()), "logged in") {
				return nil, fmt.Errorf("failed to set bitwarden server %q: %w", cfg.ServerURL, err)
			}
		}
	}

	return p, nil
}

// Name returns the provider identifier.
func (p *BitwardenProvider) Name() string {
	return "bw"
}

// GetSecret retrieves a secret from the Bitwarden vault via the `bw` CLI.
func (p *BitwardenProvider) GetSecret(ctx context.Context, uri string) (string, error) {
	m := bitwardenURIPattern.FindStringSubmatch(uri)
	if m == nil {
		return "", fmt.Errorf("invalid bitwarden URI format: %s (expected: bw://item/field)", uri)
	}
	item, field := m[1], m[2]

	// Built-in fields are returned directly by the CLI as a raw scalar.
	if builtinBitwardenFields[strings.ToLower(field)] {
		value, err := p.run(ctx, "get", strings.ToLower(field), item)
		if err != nil {
			return "", fmt.Errorf("failed to resolve bitwarden secret %s: %w", uri, err)
		}
		return value, nil
	}

	// Otherwise treat <field> as a custom field defined on the item.
	value, err := p.getCustomField(ctx, item, field)
	if err != nil {
		return "", fmt.Errorf("failed to resolve bitwarden secret %s: %w", uri, err)
	}
	return value, nil
}

// getCustomField fetches the full item JSON and returns the value of the custom
// field whose name matches (case-insensitively).
func (p *BitwardenProvider) getCustomField(ctx context.Context, item, field string) (string, error) {
	out, err := p.run(ctx, "get", "item", item)
	if err != nil {
		return "", err
	}

	var parsed struct {
		Fields []struct {
			Name  string `json:"name"`
			Value string `json:"value"`
		} `json:"fields"`
	}
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		return "", fmt.Errorf("failed to parse bitwarden item JSON: %w", err)
	}

	for _, f := range parsed.Fields {
		if strings.EqualFold(f.Name, field) {
			return f.Value, nil
		}
	}
	return "", fmt.Errorf("custom field %q not found on item %q", field, item)
}

// run invokes the `bw` CLI with the shared environment and session, returning
// trimmed stdout. On failure it surfaces the CLI's stderr for a useful message.
func (p *BitwardenProvider) run(ctx context.Context, args ...string) (string, error) {
	// --nointeraction prevents the CLI from ever blocking on a stdin prompt.
	fullArgs := append([]string{}, args...)
	fullArgs = append(fullArgs, "--nointeraction")
	if p.session != "" {
		fullArgs = append(fullArgs, "--session", p.session)
	}

	cmd := exec.CommandContext(ctx, p.bwPath, fullArgs...)
	cmd.Env = p.env

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = strings.TrimSpace(stdout.String())
		}
		if msg != "" {
			return "", fmt.Errorf("%s: %w", msg, err)
		}
		return "", err
	}

	return strings.TrimSpace(stdout.String()), nil
}
