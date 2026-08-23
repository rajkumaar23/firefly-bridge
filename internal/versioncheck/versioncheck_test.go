package versioncheck

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

func testServer(t *testing.T, tag, publishedAt string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"tag_name":"` + tag + `","published_at":"` + publishedAt + `"}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func newChecker(t *testing.T, version, buildTime, latestURL string) *Checker {
	t.Helper()
	dir := t.TempDir()
	c, err := New(Options{
		Version:   version,
		BuildTime: buildTime,
		LatestURL: latestURL,
		CachePath: filepath.Join(dir, "update-check.json"),
		Now:       func() time.Time { return time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

func TestNewerRelease(t *testing.T) {
	srv := testServer(t, "sha-abc1234", "2026-08-22T18:31:45Z")
	c := newChecker(t, "sha-722083f", "2026-08-22T10:00:00Z", srv.URL)
	res, err := c.Check()
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !res.Updated || res.LatestTag != "sha-abc1234" {
		t.Errorf("expected updated to sha-abc1234, got %+v", res)
	}
}

func TestSameCommitIsUpToDate(t *testing.T) {
	srv := testServer(t, "sha-abc1234", "2026-08-22T18:31:45Z")
	c := newChecker(t, "sha-abc1234", "2026-08-22T10:00:00Z", srv.URL)
	res, err := c.Check()
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if res.Updated {
		t.Errorf("same tag must not report an update: %+v", res)
	}
}

func TestOlderReleaseIsNotNewer(t *testing.T) {
	srv := testServer(t, "sha-older99", "2026-07-01T00:00:00Z")
	c := newChecker(t, "sha-722083f", "2026-08-22T10:00:00Z", srv.URL)
	res, err := c.Check()
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if res.Updated {
		t.Errorf("older publish time must not report an update: %+v", res)
	}
}

func TestDevBuildSkipsCheck(t *testing.T) {
	c := newChecker(t, "dev", "", "http://127.0.0.1:1") // unreachable
	res, err := c.Check()
	if err != nil {
		t.Fatalf("Check must never error for dev builds: %v", err)
	}
	if res.Updated {
		t.Errorf("dev build must not report an update: %+v", res)
	}
}

func TestRepoAndDownloadURLs(t *testing.T) {
	// Regression: ModulePath() is a Go module path (github.com/owner/repo)
	// but GitHub URLs need owner/repo. A doubled host (…/github.com/…) 404s
	// and silently disables the update check in production.
	if got := RepoPath(); got != "rajkumaar23/firefly-bridge" {
		t.Fatalf("RepoPath() = %q, want rajkumaar23/firefly-bridge", got)
	}
	if got := DefaultLatestURL(); got != "https://api.github.com/repos/rajkumaar23/firefly-bridge/releases/latest" {
		t.Fatalf("DefaultLatestURL() = %q", got)
	}
	if got := releaseDownloadURL(); got != "https://github.com/rajkumaar23/firefly-bridge/releases/latest/download/"+BinaryAssetName() {
		t.Fatalf("releaseDownloadURL() = %q", got)
	}
}

func TestCacheHitsAtMostOnceIn24h(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Write([]byte(`{"tag_name":"sha-abc1234","published_at":"2026-08-22T18:31:45Z"}`))
	}))
	defer srv.Close()

	c := newChecker(t, "sha-722083f", "2026-08-22T10:00:00Z", srv.URL)
	for i := 0; i < 3; i++ {
		if _, err := c.Check(); err != nil {
			t.Fatalf("Check %d: %v", i, err)
		}
	}
	if hits != 1 {
		t.Errorf("expected exactly 1 HTTP call in 3 checks (24h cache), got %d", hits)
	}
}

func TestFailureIsSilentAndStaleCacheStillUsed(t *testing.T) {
	srv := testServer(t, "sha-abc1234", "2026-08-22T18:31:45Z")
	c := newChecker(t, "sha-722083f", "2026-08-22T10:00:00Z", srv.URL)
	if _, err := c.Check(); err != nil {
		t.Fatalf("prime cache: %v", err)
	}
	srv.Close() // subsequent checks must fail at the network layer
	if _, err := c.Check(); err != nil {
		t.Fatalf("Check with stale cache must not error: %v", err)
	}
}
