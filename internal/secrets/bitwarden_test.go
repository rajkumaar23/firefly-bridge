package secrets

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// fakeBW writes an executable stub that mimics the parts of the `bw` CLI the
// provider relies on, so the provider can be exercised without a real install.
func fakeBW(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "bw")
	script := `#!/usr/bin/env bash
# Strip the global flags the provider appends and keep positional args.
args=()
skip=0
for a in "$@"; do
  if [[ $skip -eq 1 ]]; then skip=0; continue; fi
  case "$a" in
    --session) skip=1 ;;
    --nointeraction) ;;
    *) args+=("$a") ;;
  esac
done
case "${args[0]}" in
  status) echo '{"status":"unlocked"}' ;;
  get)
    case "${args[1]}" in
      password) echo "pw-for-${args[2]}" ;;
      username) echo "user-for-${args[2]}" ;;
      item) echo '{"fields":[{"name":"API Key","value":"custom-123"}]}' ;;
      *) echo "unknown field ${args[1]}" >&2; exit 1 ;;
    esac ;;
  *) echo "unexpected command ${args[0]}" >&2; exit 1 ;;
esac
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake bw: %v", err)
	}
	return path
}

func newTestProvider(t *testing.T) *BitwardenProvider {
	t.Helper()
	p, err := NewBitwardenProvider(context.Background(), &BitwardenConfig{
		BWPath:  fakeBW(t),
		Session: "test-session",
	})
	if err != nil {
		t.Fatalf("NewBitwardenProvider: %v", err)
	}
	return p
}

func TestBitwardenProviderName(t *testing.T) {
	if got := newTestProvider(t).Name(); got != "bw" {
		t.Fatalf("Name() = %q, want %q", got, "bw")
	}
}

func TestBitwardenGetSecret(t *testing.T) {
	p := newTestProvider(t)
	ctx := context.Background()

	tests := []struct {
		name string
		uri  string
		want string
	}{
		{"builtin password", "bw://example-bank/password", "pw-for-example-bank"},
		{"builtin username", "bw://example-bank/username", "user-for-example-bank"},
		{"builtin field case-insensitive", "bw://example-bank/PASSWORD", "pw-for-example-bank"},
		{"item id with dashes", "bw://a1b2-c3d4/password", "pw-for-a1b2-c3d4"},
		{"custom field", "bw://example-bank/api-key", ""}, // no such field -> checked below
	}

	for _, tt := range tests {
		if tt.name == "custom field" {
			continue // handled separately since the fake matches "API Key"
		}
		t.Run(tt.name, func(t *testing.T) {
			got, err := p.GetSecret(ctx, tt.uri)
			if err != nil {
				t.Fatalf("GetSecret(%q): %v", tt.uri, err)
			}
			if got != tt.want {
				t.Fatalf("GetSecret(%q) = %q, want %q", tt.uri, got, tt.want)
			}
		})
	}

	t.Run("custom field case-insensitive", func(t *testing.T) {
		got, err := p.GetSecret(ctx, "bw://example-bank/api key")
		if err != nil {
			t.Fatalf("GetSecret custom field: %v", err)
		}
		if got != "custom-123" {
			t.Fatalf("custom field = %q, want %q", got, "custom-123")
		}
	})

	t.Run("missing custom field errors", func(t *testing.T) {
		_, err := p.GetSecret(ctx, "bw://example-bank/nonexistent")
		if err == nil {
			t.Fatal("expected error for missing custom field")
		}
	})
}

func TestBitwardenInvalidURI(t *testing.T) {
	p := newTestProvider(t)
	for _, uri := range []string{"bw://noslash", "bw://", "op://vault/item/field"} {
		if _, err := p.GetSecret(context.Background(), uri); err == nil {
			t.Fatalf("expected error for invalid URI %q", uri)
		}
	}
}

// TestManagerResolveRefsIsModular verifies the inline resolver only matches the
// schemes of registered providers and resolves each independently, leaving
// non-secret URLs (e.g. https://) untouched.
func TestManagerResolveRefsIsModular(t *testing.T) {
	m := NewManager()
	m.Register(newTestProvider(t)) // registers scheme "bw"

	js := `fetch('https://api.example.com', { headers: { pw: 'bw://example-bank/password' } })`
	got, err := m.ResolveRefs(context.Background(), js)
	if err != nil {
		t.Fatalf("ResolveRefs: %v", err)
	}
	want := `fetch('https://api.example.com', { headers: { pw: 'pw-for-example-bank' } })`
	if got != want {
		t.Fatalf("ResolveRefs = %q, want %q", got, want)
	}
}

// TestManagerResolveRefsNoProviders is a no-op when nothing is registered.
func TestManagerResolveRefsNoProviders(t *testing.T) {
	m := NewManager()
	s := `pw: 'bw://example-bank/password'`
	got, err := m.ResolveRefs(context.Background(), s)
	if err != nil {
		t.Fatalf("ResolveRefs: %v", err)
	}
	if got != s {
		t.Fatalf("ResolveRefs = %q, want unchanged %q", got, s)
	}
}
