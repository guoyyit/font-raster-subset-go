package font

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
)

// RasterizeBatch rasterizes a set of glyphs concurrently. Results are
// returned in the same order as glyphs. If ctx is cancelled, the error
// context.Canceled (or context.DeadlineExceeded) is returned and no partial
// results are returned.
func RasterizeBatch(ctx context.Context, f *Font, glyphs []GlyphIndex, ppem int) ([]*Bitmap, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	results := make([]*Bitmap, len(glyphs))
	var once sync.Once
	var firstErr error
	workers := runtime.NumCPU()
	if workers < 1 {
		workers = 1
	}
	if workers > len(glyphs) {
		workers = len(glyphs)
	}
	if workers < 1 {
		workers = 1
	}
	jobs := make(chan int)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				if ctx.Err() != nil {
					return
				}
				bmp, err := f.Rasterize(glyphs[i], ppem)
				if err != nil {
					once.Do(func() { firstErr = err })
					return
				}
				results[i] = bmp
			}
		}()
	}
	for i := range glyphs {
		select {
		case jobs <- i:
		case <-ctx.Done():
			close(jobs)
			wg.Wait()
			return nil, ctx.Err()
		}
	}
	close(jobs)
	wg.Wait()
	if firstErr != nil {
		return nil, firstErr
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return results, nil
}

// RasterizeStream rasterizes glyphs concurrently and delivers each result to
// fn as it completes. Results are NOT ordered; fn must be safe for concurrent
// calls. If fn returns an error, processing stops and that error is returned.
// Memory use is bounded by the worker pool size (bitmaps are released after
// fn returns).
func RasterizeStream(ctx context.Context, f *Font, glyphs []GlyphIndex, ppem int, fn func(GlyphIndex, *Bitmap) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	workers := runtime.NumCPU()
	if workers < 1 {
		workers = 1
	}
	if workers > len(glyphs) {
		workers = len(glyphs)
	}
	if workers < 1 {
		workers = 1
	}
	jobs := make(chan int)
	var wg sync.WaitGroup
	var once sync.Once
	var firstErr error
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				if ctx.Err() != nil {
					return
				}
				bmp, err := f.Rasterize(glyphs[i], ppem)
				if err != nil {
					once.Do(func() { firstErr = err })
					return
				}
				if err := fn(glyphs[i], bmp); err != nil {
					once.Do(func() { firstErr = err })
					return
				}
			}
		}()
	}
	for i := range glyphs {
		select {
		case jobs <- i:
		case <-ctx.Done():
			close(jobs)
			wg.Wait()
			return ctx.Err()
		}
	}
	close(jobs)
	wg.Wait()
	if firstErr != nil {
		return firstErr
	}
	return ctx.Err()
}

// SubsetToFile writes a subset font to path atomically: the output is written
// to a temporary file in the same directory and renamed into place only after
// a successful write and fsync. Cancellation or error leaves no final output
// file and no temporary files.
func SubsetToFile(ctx context.Context, f *Font, runes []rune, path string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	data, err := f.Subset(runes)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	dir := filepath.Dir(path)
	base := filepath.Base(path)
	tmp, err := os.CreateTemp(dir, ".font-subset-"+base+"-*.tmp")
	if err != nil {
		return fmt.Errorf("font: cannot create temp file: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := func() {
		tmp.Close()
		os.Remove(tmpName)
	}
	if _, err := tmp.Write(data); err != nil {
		cleanup()
		return fmt.Errorf("font: write failed: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		cleanup()
		return fmt.Errorf("font: fsync failed: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("font: close failed: %w", err)
	}
	if err := ctx.Err(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("font: rename failed: %w", err)
	}
	return nil
}
