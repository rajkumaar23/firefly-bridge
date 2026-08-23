// Package datadir locates firefly-bridge's persistent data in the
// platform-standard cache directory:
//
//	macOS:  ~/Library/Caches/firefly-bridge
//	Linux:  $XDG_CACHE_HOME or ~/.cache/firefly-bridge
package datadir

import (
	"io"
	"os"
	"path/filepath"
)

// Dir returns the app data directory, creating it if needed.
func Dir() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	d := filepath.Join(base, "firefly-bridge")
	if err := os.MkdirAll(d, 0o755); err != nil {
		return "", err
	}
	return d, nil
}

func join(name string) string {
	// Best-effort: falls back to CWD-relative (legacy) behavior if the
	// cache dir cannot be determined, so the CLI still works.
	d, err := Dir()
	if err != nil {
		return name
	}
	return filepath.Join(d, name)
}

// StateFile is the default -state path.
func StateFile() string { return join(".state.json") }

// ProfileDir is the chromedp user-data directory (persistent browser session).
func ProfileDir() string { return join("chromedp-data") }

// DownloadsDir is the ephemeral browser download landing zone.
func DownloadsDir() string { return join("downloads") }

// UpdateCacheFile is the versioncheck cache.
func UpdateCacheFile() string { return join("update-check.json") }

// CopyDir recursively copies src into dst (creating dst). Used by the
// one-time migration of CWD-relative data into the data dir.
func CopyDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode())
		if err != nil {
			return err
		}
		defer out.Close()
		_, err = io.Copy(out, in)
		return err
	})
}
