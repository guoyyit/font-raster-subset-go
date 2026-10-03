// Package engine provides concurrency-safe batch subsetting and
// rasterization with cancellation, atomic file output and memory bounds.
//
// All functions are safe to call concurrently from multiple goroutines.
// Results are deterministic: output bytes and pixels depend only on the
// font, jobs and parameters — never on worker count or scheduling.
package engine

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"sync"

	"github.com/guoyyit/font-raster-subset-go/font"
	"github.com/guoyyit/font-raster-subset-go/raster"
	"github.com/guoyyit/font-raster-subset-go/subset"
)

// RasterJob describes one glyph render in a batch.
type RasterJob struct {
	Rune rune
	Size float64
	// Path, if non-empty, is written atomically with PNG-encoded output:
	// the file is first written to a temporary sibling and renamed, so
	// cancellation or crashes never leave a half-written file.
	Path string
}

// RasterResult reports the outcome of one job. Results are returned in
// job order regardless of execution order.
type RasterResult struct {
	Job RasterJob
	Err error
}

// BatchRasterize renders jobs with bounded parallelism. Unmapped runes
// yield an error on that job's result only. Cancellation stops scheduling
// new jobs promptly; jobs already running finish, and file writes are
// atomic. Results for unstarted jobs have Err set to ctx.Err().
func BatchRasterize(ctx context.Context, f *font.Font, jobs []RasterJob, workers int) []RasterResult {
	if workers < 1 {
		workers = 1
	}
	results := make([]RasterResult, len(jobs))
	var wg sync.WaitGroup
	sem := make(chan struct{}, workers)
	for i, job := range jobs {
		results[i].Job = job
		if err := ctx.Err(); err != nil {
			results[i].Err = err
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, job RasterJob) {
			defer wg.Done()
			defer func() { <-sem }()
			results[i].Err = runRasterJob(f, job)
		}(i, job)
	}
	wg.Wait()
	return results
}

func runRasterJob(f *font.Font, job RasterJob) error {
	g, ok := f.GlyphIndex(job.Rune)
	if !ok {
		return fmt.Errorf("engine: rune U+%04X has no glyph in font", job.Rune)
	}
	img, err := raster.Glyph(f, g, raster.Params{Size: job.Size})
	if err != nil {
		return err
	}
	if job.Path == "" {
		return nil
	}
	return WritePNGAtomic(job.Path, img)
}

// SubsetJob describes one subsetting task.
type SubsetJob struct {
	Runes []rune
	// Path, if non-empty, is written atomically with the subset font.
	Path string
}

// SubsetResult reports the outcome of one subsetting task.
type SubsetResult struct {
	Job  SubsetJob
	Data []byte // always populated on success, even when Path is set
	Err  error
}

// BatchSubset subsets f with bounded parallelism. Cancellation and
// determinism semantics match BatchRasterize.
func BatchSubset(ctx context.Context, f *font.Font, jobs []SubsetJob, workers int) []SubsetResult {
	if workers < 1 {
		workers = 1
	}
	results := make([]SubsetResult, len(jobs))
	var wg sync.WaitGroup
	sem := make(chan struct{}, workers)
	for i, job := range jobs {
		results[i].Job = job
		if err := ctx.Err(); err != nil {
			results[i].Err = err
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, job SubsetJob) {
			defer wg.Done()
			defer func() { <-sem }()
			data, err := subset.Subset(f, subset.Options{Runes: job.Runes})
			if err != nil {
				results[i].Err = err
				return
			}
			if job.Path != "" {
				if err := WriteFileAtomic(job.Path, data, 0o644); err != nil {
					results[i].Err = err
					return
				}
			}
			results[i].Data = data
		}(i, job)
	}
	wg.Wait()
	return results
}

// WriteFileAtomic writes data to path atomically: it writes to a temporary
// file in the same directory, fsyncs, then renames. On failure no partial
// file remains at path.
func WriteFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op after successful rename
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

// WritePNGAtomic encodes img as PNG and writes it atomically (see
// WriteFileAtomic).
func WritePNGAtomic(path string, img *image.Gray) error {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return err
	}
	return WriteFileAtomic(path, buf.Bytes(), 0o644)
}
