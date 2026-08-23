package datadir

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCopyDir(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir() + "/copied" // dst starts out non-existent

	// Nested layout: top-level file + two nested dirs with files.
	if err := os.MkdirAll(filepath.Join(src, "a/b"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(src, "c"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{
		"top.txt",
		filepath.Join("a", "nested.txt"),
		filepath.Join("a", "b", "deep.txt"),
		filepath.Join("c", "leaf.txt"),
	} {
		want := "content of " + p
		if err := os.WriteFile(filepath.Join(src, p), []byte(want), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	if err := CopyDir(src, dst); err != nil {
		t.Fatalf("CopyDir: %v", err)
	}

	for _, p := range []string{
		"top.txt",
		filepath.Join("a", "nested.txt"),
		filepath.Join("a", "b", "deep.txt"),
		filepath.Join("c", "leaf.txt"),
	} {
		got, err := os.ReadFile(filepath.Join(dst, p))
		if err != nil {
			t.Errorf("missing file %q after copy: %v", p, err)
			continue
		}
		if string(got) != "content of "+p {
			t.Errorf("%q: got %q, want %q", p, string(got), "content of "+p)
		}
	}

	// Source must be left in place (copy, never move).
	if _, err := os.Stat(filepath.Join(src, "top.txt")); err != nil {
		t.Errorf("source %q no longer exists after copy: %v", "top.txt", err)
	}

	// Idempotent: copying again over an existing destination succeeds.
	if err := CopyDir(src, dst); err != nil {
		t.Fatalf("second CopyDir: %v", err)
	}
}

func TestCopyDirMissingSource(t *testing.T) {
	if err := CopyDir(t.TempDir()+"/does-not-exist", t.TempDir()); err == nil {
		t.Error("expected error copying a non-existent source")
	}
}
