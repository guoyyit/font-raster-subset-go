package engine_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/guoyyit/font-raster-subset-go/engine"
	"github.com/guoyyit/font-raster-subset-go/font"
	"github.com/guoyyit/font-raster-subset-go/internal/testfont"
)

func sampleFont(t *testing.T) *font.Font {
	t.Helper()
	f, err := font.Parse(testfont.Build(testfont.Spec{
		UnitsPerEm: 1000,
		Glyphs: []testfont.Glyph{
			{Advance: 500},
			{Advance: 600, Contours: [][]int32{{0, 0, 500, 0, 500, 700, 0, 700}}},
			{Advance: 550, Contours: [][]int32{{0, 0, 400, 0, 400, 700, 0, 700}}},
			{Advance: 1200, Components: []testfont.Component{
				{Glyph: 1, DX: 0, DY: 0},
				{Glyph: 2, DX: 600, DY: 0},
			}},
		},
		CMap: map[rune]uint16{'A': 1, 'B': 2, 'C': 3},
	}))
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestBatchRasterizeDeterministicAcrossConcurrency(t *testing.T) {
	f := sampleFont(t)
	dir := t.TempDir()
	mkJobs := func() []engine.RasterJob {
		var jobs []engine.RasterJob
		for i, r := range []rune{'A', 'B', 'C'} {
			for _, size := range []float64{16, 33.5, 64} {
				jobs = append(jobs, engine.RasterJob{
					Rune: r, Size: size,
					Path: filepath.Join(dir, string(r)+"_"+sizeStr(size)+".png"),
				})
				_ = i
			}
		}
		return jobs
	}
	// Run with 1 worker, capture bytes.
	for _, j := range mkJobs() {
		res := engine.BatchRasterize(context.Background(), f, []engine.RasterJob{j}, 1)
		if res[0].Err != nil {
			t.Fatal(res[0].Err)
		}
	}
	ref := map[string][]byte{}
	for _, j := range mkJobs() {
		b, err := os.ReadFile(j.Path)
		if err != nil {
			t.Fatal(err)
		}
		ref[j.Path] = b
	}
	// Run the whole batch with 8 workers; outputs must be byte-identical.
	jobs := mkJobs()
	results := engine.BatchRasterize(context.Background(), f, jobs, 8)
	for _, r := range results {
		if r.Err != nil {
			t.Fatal(r.Err)
		}
	}
	for _, j := range jobs {
		b, err := os.ReadFile(j.Path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(ref[j.Path], b) {
			t.Errorf("%s: output differs between 1-worker and 8-worker runs", j.Path)
		}
	}
}

func sizeStr(f float64) string {
	return strings.ReplaceAll(strconv.FormatFloat(f, 'f', -1, 64), ".", "_")
}

func TestBatchRasterizeCancellation(t *testing.T) {
	f := sampleFont(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before scheduling
	dir := t.TempDir()
	jobs := []engine.RasterJob{
		{Rune: 'A', Size: 32, Path: filepath.Join(dir, "a.png")},
		{Rune: 'B', Size: 32, Path: filepath.Join(dir, "b.png")},
	}
	results := engine.BatchRasterize(ctx, f, jobs, 4)
	for _, r := range results {
		if !errors.Is(r.Err, context.Canceled) {
			t.Errorf("expected context.Canceled, got %v", r.Err)
		}
	}
	// No output files may exist.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("cancelled run left files: %v", entries)
	}
}

func TestBatchSubsetConcurrent(t *testing.T) {
	f := sampleFont(t)
	jobs := []engine.SubsetJob{
		{Runes: []rune{'A'}},
		{Runes: []rune{'A', 'C'}},
		{Runes: []rune{'B', 'C'}},
	}
	// Concurrent batches must not race and must be deterministic.
	var wg sync.WaitGroup
	outs := make([][]engine.SubsetResult, 4)
	for k := 0; k < 4; k++ {
		wg.Add(1)
		go func(k int) {
			defer wg.Done()
			outs[k] = engine.BatchSubset(context.Background(), f, jobs, 3)
		}(k)
	}
	wg.Wait()
	for k := 1; k < 4; k++ {
		for j := range jobs {
			if !bytes.Equal(outs[0][j].Data, outs[k][j].Data) {
				t.Fatal("concurrent batches produced different bytes")
			}
			if outs[k][j].Err != nil {
				t.Fatal(outs[k][j].Err)
			}
		}
	}
	// Every subset must parse.
	for _, r := range outs[0] {
		if _, err := font.Parse(r.Data); err != nil {
			t.Fatalf("subset does not parse: %v", err)
		}
	}
}

func TestBatchRasterizeUnmappedRune(t *testing.T) {
	f := sampleFont(t)
	results := engine.BatchRasterize(context.Background(), f,
		[]engine.RasterJob{{Rune: 'Z', Size: 16}}, 1)
	if results[0].Err == nil {
		t.Fatal("expected error for unmapped rune")
	}
}
