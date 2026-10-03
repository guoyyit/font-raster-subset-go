package font

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestRasterizeBatch(t *testing.T) {
	tf := newTestFont(t)
	f, _ := Parse(tf.fontBytes())
	glyphs := []GlyphIndex{0, 1, 2, 3, 1, 2, 3, 0}
	results, err := RasterizeBatch(context.Background(), f, glyphs, 48)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != len(glyphs) {
		t.Fatalf("got %d results, want %d", len(results), len(glyphs))
	}
	// Results should match serial rasterization
	for i, g := range glyphs {
		want, err := f.Rasterize(g, 48)
		if err != nil {
			t.Fatal(err)
		}
		if results[i] == nil {
			t.Errorf("result %d is nil", i)
			continue
		}
		if results[i].Width != want.Width || results[i].Height != want.Height {
			t.Errorf("result %d size mismatch", i)
			continue
		}
		for j := range want.Pixels {
			if results[i].Pixels[j] != want.Pixels[j] {
				t.Errorf("result %d pixels mismatch", i)
				break
			}
		}
	}
}

func TestRasterizeBatchConcurrent(t *testing.T) {
	tf := newTestFont(t)
	f, _ := Parse(tf.fontBytes())
	glyphs := make([]GlyphIndex, 100)
	for i := range glyphs {
		glyphs[i] = GlyphIndex(i % 4)
	}
	// Run multiple batches concurrently
	var wg sync.WaitGroup
	for w := 0; w < 8; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results, err := RasterizeBatch(context.Background(), f, glyphs, 48)
			if err != nil {
				t.Error(err)
				return
			}
			for _, r := range results {
				if r == nil || len(r.Pixels) == 0 {
					t.Error("empty result")
				}
			}
		}()
	}
	wg.Wait()
}

func TestRasterizeBatchCancel(t *testing.T) {
	tf := newTestFont(t)
	f, _ := Parse(tf.fontBytes())
	glyphs := make([]GlyphIndex, 1000)
	for i := range glyphs {
		glyphs[i] = GlyphIndex(i % 4)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately
	_, err := RasterizeBatch(ctx, f, glyphs, 48)
	if err != context.Canceled {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestRasterizeStream(t *testing.T) {
	tf := newTestFont(t)
	f, _ := Parse(tf.fontBytes())
	glyphs := []GlyphIndex{0, 1, 2, 3}
	var mu sync.Mutex
	count := 0
	err := RasterizeStream(context.Background(), f, glyphs, 48, func(g GlyphIndex, bmp *Bitmap) error {
		mu.Lock()
		count++
		mu.Unlock()
		if bmp == nil || len(bmp.Pixels) == 0 {
			t.Errorf("empty bitmap for glyph %d", g)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if count != len(glyphs) {
		t.Errorf("streamed %d, want %d", count, len(glyphs))
	}
}

func TestRasterizeStreamCancel(t *testing.T) {
	tf := newTestFont(t)
	f, _ := Parse(tf.fontBytes())
	glyphs := make([]GlyphIndex, 1000)
	for i := range glyphs {
		glyphs[i] = GlyphIndex(i % 4)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := RasterizeStream(ctx, f, glyphs, 48, func(g GlyphIndex, bmp *Bitmap) error {
		return nil
	})
	if err != context.Canceled {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestSubsetToFile(t *testing.T) {
	tf := newTestFont(t)
	f, _ := Parse(tf.fontBytes())
	dir := t.TempDir()
	path := filepath.Join(dir, "subset.ttf")
	err := SubsetToFile(context.Background(), f, []rune{'A', 'B'}, path)
	if err != nil {
		t.Fatal(err)
	}
	// File should exist
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() == 0 {
		t.Error("subset file is empty")
	}
	// Should be parseable
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	f2, err := Parse(data)
	if err != nil {
		t.Fatalf("subset file parse: %v", err)
	}
	if f2.NumGlyphs() != 3 {
		t.Errorf("subset glyphs = %d, want 3", f2.NumGlyphs())
	}
}

func TestSubsetToFileCancel(t *testing.T) {
	tf := newTestFont(t)
	f, _ := Parse(tf.fontBytes())
	dir := t.TempDir()
	path := filepath.Join(dir, "subset.ttf")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := SubsetToFile(ctx, f, []rune{'A', 'B'}, path)
	if err != context.Canceled {
		t.Errorf("expected context.Canceled, got %v", err)
	}
	// Final file should not exist
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("final file exists after cancel")
	}
	// No temp files should remain
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if e.Name() != filepath.Base(path) {
			t.Errorf("temp file remains: %s", e.Name())
		}
	}
}

func TestSubsetToFileTimeout(t *testing.T) {
	tf := newTestFont(t)
	f, _ := Parse(tf.fontBytes())
	dir := t.TempDir()
	path := filepath.Join(dir, "subset.ttf")
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()
	time.Sleep(1 * time.Millisecond) // ensure timeout
	err := SubsetToFile(ctx, f, []rune{'A', 'B'}, path)
	if err != context.DeadlineExceeded {
		t.Errorf("expected context.DeadlineExceeded, got %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("final file exists after timeout")
	}
}
