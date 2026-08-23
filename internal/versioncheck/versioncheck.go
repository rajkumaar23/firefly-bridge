// Package versioncheck checks whether a newer GitHub release of this module
// is available, using a 24h on-disk cache so the GitHub API is called at most
// once a day. All failures (network, rate limit, unparseable response, cache
// errors) are non-fatal: Check returns a zero Result and a nil error.
package versioncheck

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"time"
)

// Constants.
const (
	// cacheMaxAge caps how often the GitHub API is hit per machine.
	cacheMaxAge = 24 * time.Hour

	// devVersion is what main.version holds when not injected by the
	// release workflow (i.e. `go run` / plain `go build`).
	devVersion = "dev"

	// defaultModule is the fallback module path when build info is
	// unavailable (should not happen for a normal Go build).
	defaultModule = "rajkumaar23/firefly-bridge"
)

// Result describes what Check found.
type Result struct {
	Updated     bool
	LatestTag   string
	DownloadURL string
}

// Options configures a Checker.
type Options struct {
	Version   string
	BuildTime string // RFC3339 commit timestamp; empty for local builds
	LatestURL string // empty → DefaultLatestURL()
	CachePath string // full path of the cache file
	Now       func() time.Time
}

// DefaultLatestURL derives the releases-latest endpoint from the module path
// recorded in the binary's build info (go.mod), so nothing is hardcoded.
func DefaultLatestURL() string {
	return "https://api.github.com/repos/" + RepoPath() + "/releases/latest"
}

// ModulePath returns the module path from build info (go.mod), with a
// fallback.
func ModulePath() string {
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Path != "" {
		return bi.Main.Path
	}
	return defaultModule
}

// RepoPath returns owner/repo (e.g. "rajkumaar23/firefly-bridge") for use in
// GitHub API and download URLs — the module path with its host stripped.
func RepoPath() string {
	return strings.TrimPrefix(ModulePath(), "github.com/")
}

// Checker caches its last successful API result on disk.
type Checker struct {
	version   string
	buildTime time.Time
	latestURL string
	cachePath string
	now       func() time.Time
	http      *http.Client
}

// New builds a Checker. An unparsable BuildTime is tolerated (zero time —
// the comparison then falls back to tag inequality, which is still correct
// for same-commit detection).
func New(o Options) (*Checker, error) {
	if o.LatestURL == "" {
		o.LatestURL = DefaultLatestURL()
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	bt, _ := time.Parse(time.RFC3339, o.BuildTime)
	return &Checker{
		version:   o.Version,
		buildTime: bt,
		latestURL: o.LatestURL,
		cachePath: o.CachePath,
		now:       o.Now,
		http:      &http.Client{Timeout: 10 * time.Second},
	}, nil
}

// IsDev reports whether this binary is a local/unreleased build.
func (c *Checker) IsDev() bool {
	return c.version == "" || c.version == devVersion
}

// Check returns the latest-release status. It never returns an error:
// transport, cache, and parse problems all yield a zero Result.
func (c *Checker) Check() (Result, error) {
	if c.IsDev() {
		return Result{}, nil
	}
	latest, err := c.latest()
	if err != nil {
		// Network failed: fall back to a stale cache entry (any age) when
		// one exists — better a possibly-out-of-date nudge than nothing.
		if stale, sErr := c.readCache(); sErr == nil {
			latest = stale.Release
		} else {
			return Result{}, nil
		}
	}
	if latest.Tag == "" || latest.Tag == c.version {
		return Result{}, nil // nothing, or same commit we were built from
	}
	if !c.buildTime.IsZero() && !latest.PublishedAt.After(c.buildTime) {
		return Result{}, nil // older or simultaneous — not newer
	}
	return Result{
		Updated:     true,
		LatestTag:   latest.Tag,
		DownloadURL: releaseDownloadURL(),
	}, nil
}

type releaseInfo struct {
	Tag         string    `json:"tag_name"`
	PublishedAt time.Time `json:"published_at"`
}

type cacheFile struct {
	Release   releaseInfo `json:"release"`
	CheckedAt time.Time   `json:"checked_at"`
}

// releaseDownloadURL builds the versionless /releases/latest/download/ asset
// URL for this binary's OS/arch — verified to 302-redirect to the current
// tag's asset.
func releaseDownloadURL() string {
	return "https://github.com/" + RepoPath() + "/releases/latest/download/" + BinaryAssetName()
}

// BinaryAssetName is the release asset name for the running machine
// (runtime.GOOS/GOARCH — so a copied binary still prints the right name).
func BinaryAssetName() string {
	var name string
	switch runtime.GOOS {
	case "darwin":
		name = "firefly-bridge-darwin"
	default:
		name = "firefly-bridge-linux"
	}
	if runtime.GOARCH == "arm64" {
		name += "-arm64"
	} else {
		name += "-amd64"
	}
	return name
}

// latest returns the newest release, preferring a fresh (< cacheMaxAge) cache.
func (c *Checker) latest() (releaseInfo, error) {
	if cf, err := c.readCache(); err == nil && c.now().Sub(cf.CheckedAt) < cacheMaxAge {
		return cf.Release, nil
	}
	req, err := http.NewRequest(http.MethodGet, c.latestURL, nil)
	if err != nil {
		return releaseInfo{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := c.http.Do(req)
	if err != nil {
		return releaseInfo{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return releaseInfo{}, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	var rel releaseInfo
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return releaseInfo{}, err
	}
	c.writeCache(rel)
	return rel, nil
}

func (c *Checker) readCache() (cacheFile, error) {
	data, err := os.ReadFile(c.cachePath)
	if err != nil {
		return cacheFile{}, err
	}
	var cf cacheFile
	if err := json.Unmarshal(data, &cf); err != nil {
		return cacheFile{}, err
	}
	return cf, nil
}

func (c *Checker) writeCache(rel releaseInfo) {
	cf := cacheFile{Release: rel, CheckedAt: c.now()}
	data, err := json.Marshal(cf)
	if err != nil {
		return
	}
	_ = os.MkdirAll(filepath.Dir(c.cachePath), 0o755) // best effort
	_ = os.WriteFile(c.cachePath, data, 0o644)        // best effort, silent by design
}
