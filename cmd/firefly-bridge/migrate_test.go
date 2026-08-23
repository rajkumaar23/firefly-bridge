package main

import (
	"bufio"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rajkumaar23/firefly-bridge/internal/datadir"
	"github.com/sirupsen/logrus"
)

func testLogger() *logrus.Logger {
	l := logrus.New()
	l.SetOutput(io.Discard)
	return l
}

// makeLegacyProfile simulates an old project directory: <root>/chromedp-data/
// holding a real Chrome user-data dir (with a Default/ profile subdir and a
// marker file inside it). Returns the project root, like the one the user
// would type at the prompt.
func makeLegacyProfile(t *testing.T, root string) string {
	t.Helper()
	profile := filepath.Join(root, "chromedp-data")
	defaultDir := filepath.Join(profile, "Default")
	if err := os.MkdirAll(defaultDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(defaultDir, "marker"), []byte("session-data"), 0o600); err != nil {
		t.Fatal(err)
	}
	return root
}

func assertFreshProfileDir(t *testing.T, dataDir string) {
	t.Helper()
	profileDir := filepath.Join(dataDir, "chromedp-data")
	fi, err := os.Stat(profileDir)
	if err != nil || !fi.IsDir() {
		t.Fatalf("expected a profile dir to exist at %s: %v", profileDir, err)
	}
	if datadir.IsProfile(profileDir) {
		t.Errorf("fresh profile dir must not contain a Default/ profile yet")
	}
}

// "n" → start fresh, do not prompt again for a path.
func TestEnsureBrowserProfile_NoExisting_SaysNo(t *testing.T) {
	dataDir := t.TempDir()
	r := bufio.NewReader(strings.NewReader("n\n"))
	legacySrc, err := ensureBrowserProfile(dataDir, testLogger(), r)
	if err != nil {
		t.Fatalf("ensureBrowserProfile: %v", err)
	}
	assertFreshProfileDir(t, dataDir)
	if legacySrc != "" {
		t.Errorf("expected no legacy source, got %q", legacySrc)
	}
}

// Non-interactive stdin (cron, pipes): EOF must default to fresh, never block.
func TestEnsureBrowserProfile_NoExisting_EOFDefaultsFresh(t *testing.T) {
	dataDir := t.TempDir()
	r := bufio.NewReader(strings.NewReader(""))
	legacySrc, err := ensureBrowserProfile(dataDir, testLogger(), r)
	if err != nil {
		t.Fatalf("ensureBrowserProfile: %v", err)
	}
	assertFreshProfileDir(t, dataDir)
	if legacySrc != "" {
		t.Errorf("expected no legacy source, got %q", legacySrc)
	}
}

// "y" + explicit project path → profile copied, original left in place.
func TestEnsureBrowserProfile_YesAndExplicitPath(t *testing.T) {
	dataDir := t.TempDir()
	legacyRoot := makeLegacyProfile(t, t.TempDir())
	// CWD is not a project dir with a profile, so the typed path is used.
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(origDir) })
	r := bufio.NewReader(strings.NewReader("y\n" + legacyRoot + "\n"))
	legacySrc, err := ensureBrowserProfile(dataDir, testLogger(), r)
	if err != nil {
		t.Fatalf("ensureBrowserProfile: %v", err)
	}
	marker := filepath.Join(dataDir, "chromedp-data", "Default", "marker")
	got, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("marker not copied to data dir: %v", err)
	}
	if string(got) != "session-data" {
		t.Errorf("marker content = %q, want %q", got, "session-data")
	}
	if _, err := os.Stat(filepath.Join(legacyRoot, "chromedp-data", "Default", "marker")); err != nil {
		t.Error("original profile must be left in place")
	}
	wantSrc := filepath.Join(legacyRoot, "chromedp-data")
	if legacySrc != wantSrc {
		t.Errorf("returned legacy source = %q, want %q", legacySrc, wantSrc)
	}
}

// "y" + a path that is not a chromedp profile, then empty → fall back to
// fresh instead of crashing.
func TestEnsureBrowserProfile_BadPathFallsBackFresh(t *testing.T) {
	dataDir := t.TempDir()
	bad := filepath.Join(t.TempDir(), "nope")
	r := bufio.NewReader(strings.NewReader("y\n" + bad + "\n\n"))
	legacySrc, err := ensureBrowserProfile(dataDir, testLogger(), r)
	if err != nil {
		t.Fatalf("ensureBrowserProfile: %v", err)
	}
	assertFreshProfileDir(t, dataDir)
	if legacySrc != "" {
		t.Errorf("expected no legacy source after fallback, got %q", legacySrc)
	}
}

// "y" then immediate EOF (user answered yes but gave no path, e.g. piped
// input that ends early) → cancel the copy and start fresh, without
// looping on the prompt forever.
func TestEnsureBrowserProfile_YesThenEOFCancels(t *testing.T) {
	dataDir := t.TempDir()
	r := bufio.NewReader(strings.NewReader("y\n"))
	legacySrc, err := ensureBrowserProfile(dataDir, testLogger(), r)
	if err != nil {
		t.Fatalf("ensureBrowserProfile: %v", err)
	}
	assertFreshProfileDir(t, dataDir)
	if legacySrc != "" {
		t.Errorf("expected no legacy source after EOF, got %q", legacySrc)
	}
}

// A profile dir already present (real or just-created) → no prompt, no work.
func TestEnsureBrowserProfile_ExistingProfileSkipsPrompt(t *testing.T) {
	dataDir := t.TempDir()
	profileDir := filepath.Join(dataDir, "chromedp-data")
	if err := os.MkdirAll(profileDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := ensureBrowserProfile(dataDir, testLogger(), bufio.NewReader(strings.NewReader(""))); err != nil {
		t.Fatalf("ensureBrowserProfile: %v", err)
	}
	if fi, err := os.Stat(profileDir); err != nil || !fi.IsDir() {
		t.Fatalf("existing profile dir was disturbed: %v", err)
	}
}
