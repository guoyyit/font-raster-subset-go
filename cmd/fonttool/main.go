// Command fonttool subsets fonts and rasterizes glyphs from the command line.
//
//	fonttool subset -font in.ttf -out subset.ttf -chars "Hello世界"
//	fonttool raster -font in.ttf -chars "ABC" -size 32 -outdir glyphs/
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/guoyyit/font-raster-subset-go/engine"
	"github.com/guoyyit/font-raster-subset-go/font"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "subset":
		err = cmdSubset(os.Args[2:])
	case "raster":
		err = cmdRaster(os.Args[2:])
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "fonttool:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `usage:
  fonttool subset -font in.ttf -out out.ttf -chars "TEXT"
  fonttool raster -font in.ttf -chars "TEXT" -size 32 -outdir DIR`)
}

func loadFont(path string) (*font.Font, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return font.Parse(b)
}

func cmdSubset(args []string) error {
	fs := flag.NewFlagSet("subset", flag.ContinueOnError)
	fontPath := fs.String("font", "", "input font file (required)")
	out := fs.String("out", "", "output subset file (required)")
	chars := fs.String("chars", "", "characters to keep")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *fontPath == "" || *out == "" {
		return fmt.Errorf("-font and -out are required")
	}
	f, err := loadFont(*fontPath)
	if err != nil {
		return err
	}
	results := engine.BatchSubset(nil2ctx(), f,
		[]engine.SubsetJob{{Runes: []rune(*chars), Path: *out}}, 1)
	if results[0].Err != nil {
		return results[0].Err
	}
	fmt.Printf("wrote %s (%d bytes)\n", *out, len(results[0].Data))
	return nil
}

func cmdRaster(args []string) error {
	fs := flag.NewFlagSet("raster", flag.ContinueOnError)
	fontPath := fs.String("font", "", "input font file (required)")
	outdir := fs.String("outdir", ".", "output directory")
	chars := fs.String("chars", "", "characters to render")
	size := fs.Float64("size", 32, "pixels per em")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *fontPath == "" {
		return fmt.Errorf("-font is required")
	}
	f, err := loadFont(*fontPath)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(*outdir, 0o755); err != nil {
		return err
	}
	var jobs []engine.RasterJob
	for _, r := range *chars {
		jobs = append(jobs, engine.RasterJob{
			Rune: r, Size: *size,
			Path: filepath.Join(*outdir, fmt.Sprintf("U+%04X.png", r)),
		})
	}
	results := engine.BatchRasterize(nil2ctx(), f, jobs, 4)
	for _, r := range results {
		if r.Err != nil {
			return r.Err
		}
	}
	fmt.Printf("rendered %d glyphs to %s\n", len(jobs), *outdir)
	return nil
}
