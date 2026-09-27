// Package testutil copies the example knowledge base into a test's temp dir.
package testutil

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// Example returns the path of the repo's example/ directory.
func Example() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "example")
}

// CopyExample copies example/ into a fresh temp dir and returns that dir.
func CopyExample(t *testing.T) string {
	t.Helper()
	dst := t.TempDir()
	src := Example()
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		out := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(out, 0o755)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(out, b, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	return dst
}

// Write creates a file under dir, making parent directories.
func Write(t *testing.T, dir, rel, body string) string {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}
