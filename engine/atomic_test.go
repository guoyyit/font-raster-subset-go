package engine_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/guoyyit/font-raster-subset-go/engine"
)

func TestWriteFileAtomic(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "out.bin")
	if err := engine.WriteFileAtomic(p, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "hello" {
		t.Fatalf("got %q", b)
	}
	// Overwrite keeps content complete.
	if err := engine.WriteFileAtomic(p, []byte("world!"), 0o644); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(p)
	if string(b) != "world!" {
		t.Fatalf("got %q", b)
	}
	// Failure (bad directory) must not create stray files.
	bad := filepath.Join(dir, "nonexistent-dir", "x.bin")
	if err := engine.WriteFileAtomic(bad, []byte("x"), 0o644); err == nil {
		t.Fatal("expected error")
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if e.Name() != "out.bin" {
			t.Errorf("stray file left behind: %s", e.Name())
		}
	}
}
