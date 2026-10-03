# font-raster-subset-go

A pure-Go, offline TrueType font subsetting and grayscale rasterization engine.
No external dependencies; the font file is parsed directly with the standard
library.

## Features

- **Parse** TrueType (sfnt 0x00010000 / `true` / `typ1`) fonts and expose
  units-per-em, glyph advance widths, bounding boxes, and resolved outlines.
- **Subset** a font to a requested rune set. The output is a structurally
  complete sfnt file that any font loader can read. Composite glyph
  dependencies are retained automatically; no dangling references remain.
- **Rasterize** glyphs to 8-bit grayscale bitmaps with analytic anti-aliasing
  (scanline trapezoid rasterizer, nonzero winding).
- **Kerning** is applied when a format-0 `kern` table is present. Missing or
  malformed kerning data falls back to a deterministic no-kerning default.
- **Deterministic**: identical input and configuration produce byte-identical
  subset files and bitmaps, regardless of concurrency.
- **Concurrent batch** rasterization with bounded worker pools and context
  cancellation. Atomic file writes (temp file + fsync + rename) leave no
  half-written output on cancellation.

## Limitations

- Only TrueType-outline fonts (`glyf` table) are supported. CFF/OpenType
  (`OTTO`) and TrueType collections (`ttcf`) return `ErrUnsupportedFormat`.
- Kerning is read from the format-0 `kern` table only. GPOS kerning is not
  parsed.
- Rasterization uses no hinting (deterministic scaling). Glyph outlines are
  scaled linearly from font units to pixels.
- `ppem` is capped at 1024 to bound memory.

## Installation

```sh
go get github.com/guoyyit/font-raster-subset-go
```

## Quick start

```go
import "github.com/guoyyit/font-raster-subset-go/font"

data, _ := os.ReadFile("font.ttf")
f, err := font.Parse(data)
if err != nil {
    log.Fatal(err)
}

// Metrics
upem := f.UnitsPerEm()
g := f.GlyphIndex('A')
m, _ := f.GlyphMetrics(g)
fmt.Println(m.Advance, m.Bounds)

// Kerning
k := f.KerningRunes('A', 'V') // 0 if no kerning

// Rasterize
bmp, _ := f.Rasterize(g, 48) // 48 ppem
// bmp.Pixels is bmp.Width*bmp.Height grayscale values (0..255)

// Subset
sub, _ := f.Subset([]rune("Hello, World"))
os.WriteFile("subset.ttf", sub, 0644)
```

## API

### Parsing

```go
func Parse(data []byte) (*Font, error)
```

`Parse` validates the sfnt directory, verifies table checksums, and parses the
required tables. The returned `Font` is immutable and safe for concurrent use.

Errors are distinguishable via `errors.Is`:

| Error | Meaning |
|-------|---------|
| `ErrTruncated` | Input ends before a required field |
| `ErrBadMagic` | Unrecognized sfnt magic number |
| `ErrUnsupportedFormat` | CFF/OpenType or TrueType collection |
| `ErrMissingTable` | A required table (`head`, `hhea`, `maxp`, `hmtx`, `cmap`, `loca`, `glyf`) is absent |
| `ErrBadTable` | A table's structure is invalid |
| `ErrBadChecksum` | A table checksum does not match |
| `ErrBadGlyph` | Glyph index out of range |
| `ErrBadOutline` | Glyph outline is malformed |
| `ErrBadArgument` | Invalid call argument (e.g. bad ppem) |

### Metrics

```go
func (f *Font) UnitsPerEm() int
func (f *Font) NumGlyphs() int
func (f *Font) GlyphIndex(r rune) GlyphIndex
func (f *Font) GlyphMetrics(g GlyphIndex) (Metrics, error)
func (f *Font) GlyphOutline(g GlyphIndex) (*Outline, error)
```

`GlyphIndex` maps a rune to its glyph index. Runes absent from the font's
character map map to glyph 0 (`.notdef`).

`GlyphOutline` returns the resolved outline in font units (y-up). Composite
glyphs are fully resolved into a single contour list.

### Kerning

```go
func (f *Font) Kerning(left, right GlyphIndex) int
func (f *Font) KerningRunes(left, right rune) int
```

`Kerning` returns the horizontal kerning adjustment in font units for the glyph
pair. It returns 0 when the font has no `kern` table, the pair is absent, or
the kerning table is malformed. This fallback is deterministic: malformed
kerning never causes an error; it is treated as no kerning.

### Rasterization

```go
func (f *Font) Rasterize(g GlyphIndex, ppem int) (*Bitmap, error)
```

`ppem` is pixels per em (must be 1..1024). The returned `Bitmap` covers the
glyph's pixel bounding box with 8-bit coverage values (0 = empty, 255 = fully
covered). `Left` and `Top` are pen-relative bearings in pixels; `Advance` is
the horizontal pen advance in pixels.

### Subsetting

```go
func (f *Font) Subset(runes []rune) ([]byte, error)
```

`Subset` produces a new sfnt font containing only the glyphs needed to render
the given runes. The output is deterministic: the same rune set always produces
byte-identical output.

#### Behavior for characters outside the set

- Glyph 0 (`.notdef`) is always retained.
- Composite glyph dependencies are retained automatically (transitively).
- Runes absent from the source character map map to glyph 0 in the subset.
- Runes not in the requested set are not included; rendering them with the
  subset font yields `.notdef`.
- The subset's `cmap` maps only the requested runes.

#### Retained tables

`head`, `hhea`, `maxp`, `hmtx`, `cmap`, `loca` (long format), `glyf`, `name`,
`post` (format 3.0), `kern` (subset, if present), `OS/2` (if present). Hinting
tables (`fpgm`, `prep`, `cvt`) and layout tables (`GPOS`, `GSUB`, `GDEF`,
etc.) are dropped. Glyph hinting instructions are copied verbatim with the
glyph data.

### Batch operations

```go
func RasterizeBatch(ctx context.Context, f *Font, glyphs []GlyphIndex, ppem int) ([]*Bitmap, error)
func RasterizeStream(ctx context.Context, f *Font, glyphs []GlyphIndex, ppem int, fn func(GlyphIndex, *Bitmap) error) error
func SubsetToFile(ctx context.Context, f *Font, runes []rune, path string) error
```

`RasterizeBatch` returns results in the same order as the input glyphs.
`RasterizeStream` delivers results as they complete (unordered); `fn` must be
safe for concurrent calls. Memory use is bounded by the worker pool size.

`SubsetToFile` writes atomically: the subset is written to a temp file in the
same directory, fsynced, then renamed into place. Cancellation or error leaves
no final file and no temp files.

All batch functions respect `ctx` cancellation. If the context is cancelled,
`context.Canceled` (or `context.DeadlineExceeded`) is returned.

## Determinism

- Subset bytes are deterministic: runes are deduplicated and sorted; glyph IDs
  are assigned in sorted order; table records are sorted by tag.
- Bitmap output is deterministic: float64 arithmetic with a fixed transform;
  no map iteration in hot paths; no randomness.
- A `Font` is immutable after parsing and safe for concurrent reads. Batch
  operations produce identical results regardless of concurrency level.

## Memory

- The font data is retained in memory (not copied) for the lifetime of the
  `Font`.
- Rasterization allocates a coverage buffer of `width * height` float64 values
  per glyph. `ppem` is capped at 1024 (≈1M pixels, 8 MB per glyph).
- `RasterizeStream` bounds in-flight bitmaps by the worker pool size; bitmaps
  are released after `fn` returns.

## Architecture

- `font/font.go` — public API, parsing, metrics.
- `font/tables.go` — table directory and table parsers (`head`, `hhea`,
  `maxp`, `hmtx`, `cmap`, `kern`).
- `font/glyf.go` — `glyf` table parsing, composite resolution, outline
  extraction.
- `font/subset.go` — subset construction and sfnt assembly.
- `font/raster.go` — scanline trapezoid rasterizer with analytic AA.
- `font/batch.go` — concurrent batch operations and atomic file writes.

## License

MIT
