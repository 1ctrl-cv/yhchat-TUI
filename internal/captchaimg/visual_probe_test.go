package captchaimg

import (
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// TestVisualProbe is a development aid, not part of the regular suite. It
// renders the real fixture and writes previews so a human (or an image viewer)
// can judge whether the CAPTCHA characters survive the conversion to terminal
// cells. A terminal cell is drawn as cellW x cellH pixels: the upper half in the
// foreground color and the lower half in the background color, exactly what a
// '▀' half block looks like in a terminal.
//
//	CAPTCHA_RENDER_VISUAL=1 CAPTCHA_RENDER_VISUAL_DIR=_probe/captcha-visual \
//	  go test ./internal/captchaimg -run TestVisualProbe -v
func TestVisualProbe(t *testing.T) {
	if os.Getenv("CAPTCHA_RENDER_VISUAL") == "" {
		t.Skip("set CAPTCHA_RENDER_VISUAL=1 to dump rendered fixture previews")
	}
	outDir := os.Getenv("CAPTCHA_RENDER_VISUAL_DIR")
	if outDir == "" {
		outDir = t.TempDir()
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatalf("create visual output dir: %v", err)
	}

	img := mustDecodeFixture(t)
	logImageStats(t, img)

	// Ground truth: the fixture nearest-upscaled 4x with alpha flattened over
	// white, so the actual characters are unambiguous.
	cells := make([][][2]color.RGBA, img.Bounds().Dy())
	for y := range cells {
		for x := 0; x < img.Bounds().Dx(); x++ {
			c := flatten(img, img.Bounds().Min.X+x, img.Bounds().Min.Y+y)
			rgba := color.RGBA{R: c.r, G: c.g, B: c.b, A: 255}
			cells[y] = append(cells[y], [2]color.RGBA{rgba, rgba})
		}
	}
	writeCellPNG(t, outDir, "truth-upscaled-4x.png", cells, 4, 4)

	// Grayscale ASCII: print it so it can be read in the test log, and save it
	// for easier inspection. Compare the shipped rendering (bright background =
	// dense art) with its inverse (dark background = spaces), which is usually
	// easier on a dark terminal.
	for _, opt := range []Options{
		{MaxCols: 74, MaxRows: 14, Color: false},
		{MaxCols: 80, MaxRows: 16, Color: false},
	} {
		out := Render(img, opt)
		name := fmt.Sprintf("gray-%dx%d.txt", opt.MaxCols, opt.MaxRows)
		writeVisualFile(t, outDir, name, out)
		t.Logf("=== grayscale %+v: %d rows ===\n%s", opt, len(outputLines(out)), out)
		t.Logf("=== grayscale inverted %+v ===\n%s", opt, invertASCII(out))
	}

	// Colored output at the login screen's budget, previewed as a PNG. Both a
	// faithful cell size (8x16) and a larger one (16x32) are written.
	for _, opt := range []Options{
		{MaxCols: 74, MaxRows: 14, Color: true},
		{MaxCols: 80, MaxRows: 16, Color: true},
	} {
		out := Render(img, opt)
		writeVisualFile(t, outDir, fmt.Sprintf("color-%dx%d.ansi", opt.MaxCols, opt.MaxRows), out)
		cells, cellW, cellH := parseCellPNG(t, out)
		writeCellPNG(t, outDir, fmt.Sprintf("color-%dx%d.png", opt.MaxCols, opt.MaxRows), cells, cellW, cellH)
		writeCellPNG(t, outDir, fmt.Sprintf("color-%dx%d-big.png", opt.MaxCols, opt.MaxRows), cells, 16, 32)
		t.Logf("color %+v: %d rows", opt, len(outputLines(out)))
	}

	// Alternative downsampling/contrast strategies for the same 74x9 cell grid,
	// so the current implementation can be compared side by side.
	cols, rows := outputGrid(180, 40, 74, 14)
	box := resample(img, cols, rows*2)
	nearest := nearestPixels(img, cols, rows*2)
	variants := []struct {
		name   string
		pixels []pixel
	}{
		{"variant-box-average", box},
		{"variant-nearest", nearest},
		{"variant-contrast-1.25", contrastPixels(box, 1.25, 128)},
		{"variant-contrast-1.35", contrastPixels(box, 1.35, 128)},
		{"variant-saturation-1.8", saturationPixels(box, 1.8)},
		{"variant-contrast-1.15-sat-1.6", saturationPixels(contrastPixels(box, 1.15, 128), 1.6)},
		{"variant-unsharp", unsharpPixels(box, rows*2, 0.8)},
	}
	var toneRows []image.Image
	for _, v := range variants {
		rendered := Render(&pixelImage{w: cols, h: rows * 2, pix: v.pixels}, Options{MaxCols: 74, MaxRows: 14, Color: true})
		cells, cellW, cellH := parseCellPNG(t, rendered)
		writeCellPNG(t, outDir, v.name+"-big.png", cells, cellW*2, cellH*2)
		if v.name == "variant-box-average" || v.name == "variant-nearest" ||
			v.name == "variant-contrast-1.25" || v.name == "variant-unsharp" {
			toneRows = append(toneRows, cellGridToImage(cells, 16, 32))
		}
		t.Logf("wrote %s", filepath.Join(outDir, v.name+"-big.png"))
	}
	writeImagePNG(t, outDir, "tone-variants-sheet.png", stackVertical(toneRows, 10))

	// ASCII ramp mapping variants for the fixture (the shipped ASCII output is
	// printed above). Tests how far contrast stretching and ramp direction can
	// improve readability on a dark terminal.
	pixels := resample(img, cols, rows*2)
	for _, v := range []struct {
		name   string
		invert bool
		lo, hi float64
	}{
		{"ascii-minmax-forward", false, 0, 1},
		{"ascii-minmax-inverted", true, 0, 1},
		{"ascii-p05-p95-forward", false, 0.05, 0.95},
		{"ascii-p05-p95-inverted", true, 0.05, 0.95},
		{"ascii-p10-p90-inverted", true, 0.10, 0.90},
		{"ascii-p02-p80-inverted", true, 0.02, 0.80},
	} {
		t.Logf("=== %s ===\n%s", v.name, asciiVariant(pixels, cols, rows, v.invert, v.lo, v.hi))
	}

	// How the ASCII art would look "at a glance" as grayscale ink density: the
	// ramp index is drawn as a gray cell, so a viewer sees what a human reading
	// the monospace art perceives. Forward = bright field with dark glyph holes;
	// inverted = dark field with bright glyphs.
	looks := []image.Image{
		asciiLookPNG(pixels, cols, rows, false, 0, 1),
		asciiLookPNG(pixels, cols, rows, true, 0, 1),
		asciiLookPNG(pixels, cols, rows, false, 0.05, 0.95),
		asciiLookPNG(pixels, cols, rows, true, 0.05, 0.95),
	}
	writeImagePNG(t, outDir, "ascii-look-sheet.png", stackVertical(looks, 10))

	// If real API samples were fetched into a directory, build contact sheets:
	// one with the originals (ground truth) and one with the rendered result.
	if samplesDir := os.Getenv("CAPTCHA_RENDER_SAMPLES_DIR"); samplesDir != "" {
		writeSampleSheets(t, outDir, samplesDir)
	}
}

type sample struct {
	name string
	img  image.Image
}

func loadSamples(t *testing.T, dir string) []sample {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(dir, "*.png"))
	if err != nil {
		t.Fatalf("glob samples: %v", err)
	}
	sort.Strings(files)
	if len(files) == 0 {
		t.Fatalf("no sample PNGs in %s", dir)
	}
	var out []sample
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read sample %s: %v", f, err)
		}
		img, err := DecodeDataURL("data:image/png;base64," + base64.StdEncoding.EncodeToString(raw))
		if err != nil {
			t.Fatalf("decode sample %s: %v", f, err)
		}
		out = append(out, sample{name: strings.TrimSuffix(filepath.Base(f), ".png"), img: img})
	}
	return out
}

func writeSampleSheets(t *testing.T, outDir, samplesDir string) {
	t.Helper()
	samples := loadSamples(t, samplesDir)
	truthRows := make([]image.Image, len(samples))
	renderRows := make([]image.Image, len(samples))
	sheets := map[string][]image.Image{}
	var asciiLooks []image.Image
	for i, s := range samples {
		truthRows[i] = upscaleFlattened(s.img, 3)
		colorOut := Render(s.img, Options{MaxCols: 74, MaxRows: 14, Color: true})
		cells, cellW, cellH := parseCellPNG(t, colorOut)
		renderRows[i] = cellGridToImage(cells, cellW, cellH)
		gray := Render(s.img, Options{MaxCols: 74, MaxRows: 14, Color: false})
		writeVisualFile(t, outDir, s.name+"-gray.txt", gray)
		t.Logf("=== %s %v grayscale 74x14 ===\n%s", s.name, s.img.Bounds().Size(), gray)

		// Per-strategy contact sheets so the downsampling choice can be judged
		// across all real samples, not just the fixture.
		b := s.img.Bounds()
		cols, rows := outputGrid(b.Dx(), b.Dy(), 74, 14)
		box := resample(s.img, cols, rows*2)
		asciiLooks = append(asciiLooks, asciiLookPNG(box, cols, rows, false, 0.05, 0.95))
		strategies := map[string][]pixel{
			"box-average":  box,
			"unsharp":      unsharpPixels(box, rows*2, 0.8),
			"contrast-125": contrastPixels(box, 1.25, 128),
			"nearest":      nearestPixels(s.img, cols, rows*2),
		}
		for name, pix := range strategies {
			rendered := Render(&pixelImage{w: cols, h: rows * 2, pix: pix}, Options{MaxCols: 74, MaxRows: 14, Color: true})
			vc, vcw, vch := parseCellPNG(t, rendered)
			sheets[name] = append(sheets[name], cellGridToImage(vc, vcw, vch))
		}
	}
	writeImagePNG(t, outDir, "samples-truth-sheet.png", stackVertical(truthRows, 8))
	writeImagePNG(t, outDir, "samples-render-sheet.png", stackVertical(renderRows, 8))
	writeImagePNG(t, outDir, "samples-ascii-look-sheet.png", stackVertical(asciiLooks, 8))
	for name, rowsImgs := range sheets {
		writeImagePNG(t, outDir, "samples-"+name+"-sheet.png", stackVertical(rowsImgs, 8))
	}
	t.Logf("wrote sample contact sheets to %s", outDir)
}

// asciiVariant mirrors renderASCII but exposes the contrast window and ramp
// direction for experimentation.
func asciiVariant(pixels []pixel, cols, rows int, invert bool, loPct, hiPct float64) string {
	idx := asciiIndexGrid(pixels, cols, rows, invert, loPct, hiPct)
	lines := make([]string, rows)
	for y := 0; y < rows; y++ {
		var sb strings.Builder
		for x := 0; x < cols; x++ {
			sb.WriteByte(asciiRamp[idx[y*cols+x]])
		}
		lines[y] = strings.TrimRight(sb.String(), " ")
	}
	return strings.Join(lines, "\n")
}

// asciiLookPNG draws the ramp index as a gray cell (an ink-density surrogate),
// so the perceived shape of a monospace ASCII rendering can be inspected.
func asciiLookPNG(pixels []pixel, cols, rows int, invert bool, loPct, hiPct float64) image.Image {
	idx := asciiIndexGrid(pixels, cols, rows, invert, loPct, hiPct)
	last := float64(len(asciiRamp) - 1)
	cells := make([][][2]color.RGBA, rows)
	for y := 0; y < rows; y++ {
		for x := 0; x < cols; x++ {
			g := uint8(math.Round(float64(idx[y*cols+x]) / last * 255))
			c := color.RGBA{R: g, G: g, B: g, A: 255}
			cells[y] = append(cells[y], [2]color.RGBA{c, c})
		}
	}
	return cellGridToImage(cells, 16, 32)
}

// asciiIndexGrid computes the ramp index of every cell for a given contrast
// window and ramp direction.
func asciiIndexGrid(pixels []pixel, cols, rows int, invert bool, loPct, hiPct float64) []int {
	cell := make([]float64, cols*rows)
	for y := 0; y < rows; y++ {
		for x := 0; x < cols; x++ {
			top, bottom := pixels[(2*y)*cols+x], pixels[(2*y+1)*cols+x]
			r := (float64(top.r) + float64(bottom.r)) / 2
			g := (float64(top.g) + float64(bottom.g)) / 2
			b := (float64(top.b) + float64(bottom.b)) / 2
			cell[y*cols+x] = (0.299*r + 0.587*g + 0.114*b) / 255
		}
	}
	sorted := append([]float64(nil), cell...)
	sort.Float64s(sorted)
	at := func(p float64) float64 {
		i := int(math.Round(p * float64(len(sorted)-1)))
		return sorted[i]
	}
	lo, hi := at(loPct), at(hiPct)
	if hi-lo < 0.02 {
		lo, hi = sorted[0], sorted[len(sorted)-1]
	}
	span := hi - lo
	last := float64(len(asciiRamp) - 1)
	out := make([]int, len(cell))
	for i, lum := range cell {
		v := (lum - lo) / span
		if v < 0 {
			v = 0
		} else if v > 1 {
			v = 1
		}
		idx := int(math.Round(v * last))
		if invert {
			idx = int(last) - idx
		}
		out[i] = idx
	}
	return out
}

func upscaleFlattened(img image.Image, scale int) image.Image {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	dst := image.NewRGBA(image.Rect(0, 0, w*scale, h*scale))
	for y := 0; y < h*scale; y++ {
		for x := 0; x < w*scale; x++ {
			p := flatten(img, b.Min.X+x/scale, b.Min.Y+y/scale)
			dst.SetRGBA(x, y, color.RGBA{R: p.r, G: p.g, B: p.b, A: 255})
		}
	}
	return dst
}

func cellGridToImage(cells [][][2]color.RGBA, cellW, cellH int) *image.RGBA {
	cols := 0
	for _, row := range cells {
		if len(row) > cols {
			cols = len(row)
		}
	}
	dst := image.NewRGBA(image.Rect(0, 0, cols*cellW, len(cells)*cellH))
	set := func(x0, y0, x1, y1 int, c color.RGBA) {
		for y := y0; y < y1; y++ {
			for x := x0; x < x1; x++ {
				dst.SetRGBA(x, y, c)
			}
		}
	}
	for cy, row := range cells {
		for cx, pair := range row {
			x0, y0 := cx*cellW, cy*cellH
			mid := y0 + cellH/2
			set(x0, y0, x0+cellW, mid, pair[0])
			set(x0, mid, x0+cellW, y0+cellH, pair[1])
		}
	}
	return dst
}

// stackVertical puts images in one column, centered on a white canvas, padded.
func stackVertical(imgs []image.Image, gap int) image.Image {
	width := 0
	height := gap
	for _, img := range imgs {
		if b := img.Bounds(); b.Dx() > width {
			width = b.Dx()
		}
		if dy := img.Bounds().Dy(); dy > 0 {
			height += dy + gap
		}
	}
	dst := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			dst.SetRGBA(x, y, color.RGBA{255, 255, 255, 255})
		}
	}
	y := gap
	for _, img := range imgs {
		b := img.Bounds()
		x0 := (width - b.Dx()) / 2
		for sy := 0; sy < b.Dy(); sy++ {
			for sx := 0; sx < b.Dx(); sx++ {
				dst.Set(x0+sx, y+sy, img.At(sx, sy))
			}
		}
		y += b.Dy() + gap
	}
	return dst
}

func writeImagePNG(t *testing.T, dir, name string, img image.Image) {
	t.Helper()
	path := filepath.Join(dir, name)
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create %s: %v", path, err)
	}
	if err := png.Encode(f, img); err != nil {
		f.Close()
		t.Fatalf("encode %s: %v", path, err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close %s: %v", path, err)
	}
	t.Logf("wrote %s", path)
}

// invertASCII swaps ramp brightness; runes outside the ramp are left alone.
func invertASCII(s string) string {
	lines := outputLines(s)
	for i, line := range lines {
		runes := []rune(line)
		for j, r := range runes {
			if idx := strings.IndexRune(asciiRamp, r); idx >= 0 {
				runes[j] = rune(asciiRamp[len(asciiRamp)-1-idx])
			}
		}
		lines[i] = strings.TrimRight(string(runes), " ")
	}
	return strings.Join(lines, "\n")
}

func writeVisualFile(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func writeCellPNG(t *testing.T, dir, name string, cells [][][2]color.RGBA, cellW, cellH int) {
	t.Helper()
	if len(cells) == 0 {
		t.Fatalf("cannot write %s: no cells", name)
	}
	writeImagePNG(t, dir, name, cellGridToImage(cells, cellW, cellH))
}

// cellRE parses one cell emitted by renderColor into fg and bg components.
var cellRE = regexp.MustCompile("\\x1b\\[38;2;(\\d+);(\\d+);(\\d+)m\\x1b\\[48;2;(\\d+);(\\d+);(\\d+)m" + string(upperHalfBlock))

// parseCellPNG turns ANSI output into a grid of fg/bg cell colors. Default
// terminal cell geometry is 8x16 pixels.
func parseCellPNG(t *testing.T, ansi string) ([][][2]color.RGBA, int, int) {
	t.Helper()
	lines := outputLines(ansi)
	cells := make([][][2]color.RGBA, len(lines))
	for i, line := range lines {
		for _, m := range cellRE.FindAllStringSubmatch(line, -1) {
			var vals [6]uint8
			for k := 0; k < 6; k++ {
				n, err := strconv.Atoi(m[k+1])
				if err != nil {
					t.Fatalf("parse cell color: %v", err)
				}
				vals[k] = uint8(n)
			}
			cells[i] = append(cells[i], [2]color.RGBA{
				{R: vals[0], G: vals[1], B: vals[2], A: 255},
				{R: vals[3], G: vals[4], B: vals[5], A: 255},
			})
		}
		if len(cells[i]) == 0 {
			t.Fatalf("line %d of ANSI output has no parsed cells", i)
		}
	}
	return cells, 8, 16
}

// flatten composites one source pixel over opaque white.
func flatten(img image.Image, x, y int) pixel {
	nr := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
	if nr.A == 255 {
		return pixel{nr.R, nr.G, nr.B}
	}
	alpha := float64(nr.A) / 255
	blend := func(c uint8) uint8 {
		return uint8(math.Round(float64(c)*alpha + 255*(1-alpha)))
	}
	return pixel{blend(nr.R), blend(nr.G), blend(nr.B)}
}

// pixelImage is an in-memory image.Image over already-resampled pixels; it lets
// the probe feed alternate sampling strategies through Render unchanged.
type pixelImage struct {
	w, h int
	pix  []pixel
}

func (p *pixelImage) ColorModel() color.Model { return color.RGBAModel }
func (p *pixelImage) Bounds() image.Rectangle { return image.Rect(0, 0, p.w, p.h) }
func (p *pixelImage) At(x, y int) color.Color {
	s := p.pix[y*p.w+x]
	return color.RGBA{R: s.r, G: s.g, B: s.b, A: 255}
}

func nearestPixels(img image.Image, cols, rows2 int) []pixel {
	b := img.Bounds()
	sw, sh := b.Dx(), b.Dy()
	out := make([]pixel, cols*rows2)
	for y := 0; y < rows2; y++ {
		sy := b.Min.Y + (y*sh+sh/2)/rows2
		for x := 0; x < cols; x++ {
			sx := b.Min.X + (x*sw+sw/2)/cols
			out[y*cols+x] = flatten(img, sx, sy)
		}
	}
	return out
}

func contrastPixels(src []pixel, k, mid float64) []pixel {
	apply := func(v uint8) uint8 {
		f := (float64(v)-mid)*k + mid
		if f < 0 {
			f = 0
		} else if f > 255 {
			f = 255
		}
		return uint8(f + 0.5)
	}
	out := make([]pixel, len(src))
	for i, p := range src {
		out[i] = pixel{apply(p.r), apply(p.g), apply(p.b)}
	}
	return out
}

func saturationPixels(src []pixel, k float64) []pixel {
	out := make([]pixel, len(src))
	for i, p := range src {
		lum := 0.299*float64(p.r) + 0.587*float64(p.g) + 0.114*float64(p.b)
		apply := func(v uint8) uint8 {
			f := lum + (float64(v)-lum)*k
			if f < 0 {
				f = 0
			} else if f > 255 {
				f = 255
			}
			return uint8(f + 0.5)
		}
		out[i] = pixel{apply(p.r), apply(p.g), apply(p.b)}
	}
	return out
}

// unsharpPixels sharpens a low-resolution pixel grid: out = src + amount*(src-blur).
func unsharpPixels(src []pixel, h int, amount float64) []pixel {
	w := len(src) / h
	clamp := func(f float64) uint8 {
		if f < 0 {
			return 0
		}
		if f > 255 {
			return 255
		}
		return uint8(f + 0.5)
	}
	out := make([]pixel, len(src))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var sr, sg, sb, n float64
			for dy := -1; dy <= 1; dy++ {
				for dx := -1; dx <= 1; dx++ {
					xx, yy := x+dx, y+dy
					if xx < 0 || xx >= w || yy < 0 || yy >= h {
						xx, yy = x, y
					}
					p := src[yy*w+xx]
					sr, sg, sb, n = sr+float64(p.r), sg+float64(p.g), sb+float64(p.b), n+1
				}
			}
			avg := pixel{clamp(sr / n), clamp(sg / n), clamp(sb / n)}
			p := src[y*w+x]
			out[y*w+x] = pixel{
				clamp(float64(p.r) + amount*(float64(p.r)-float64(avg.r))),
				clamp(float64(p.g) + amount*(float64(p.g)-float64(avg.g))),
				clamp(float64(p.b) + amount*(float64(p.b)-float64(avg.b))),
			}
		}
	}
	return out
}

func logImageStats(t *testing.T, img image.Image) {
	t.Helper()
	b := img.Bounds()
	colors := map[color.RGBA]int{}
	minLum, maxLum, sum := 1.0, 0.0, 0.0
	var n int
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bb, a := img.At(x, y).RGBA()
			colors[color.RGBA{uint8(r >> 8), uint8(g >> 8), uint8(bb >> 8), uint8(a >> 8)}]++
			lum := (0.299*float64(r) + 0.587*float64(g) + 0.114*float64(bb)) / 65535
			if lum < minLum {
				minLum = lum
			}
			if lum > maxLum {
				maxLum = lum
			}
			sum += lum
			n++
		}
	}
	t.Logf("fixture: %T %v, %d unique RGBA colors, luminance min=%.2f max=%.2f mean=%.2f",
		img, b, len(colors), minLum, maxLum, sum/float64(n))
	type kv struct {
		c color.RGBA
		n int
	}
	var top []kv
	for c, count := range colors {
		top = append(top, kv{c, count})
	}
	for i := 0; i < len(top); i++ {
		for j := i + 1; j < len(top); j++ {
			if top[j].n > top[i].n {
				top[i], top[j] = top[j], top[i]
			}
		}
	}
	if len(top) > 6 {
		top = top[:6]
	}
	parts := make([]string, len(top))
	for i, e := range top {
		parts[i] = fmt.Sprintf("%v:%d", e.c, e.n)
	}
	t.Logf("most common colors: %s", strings.Join(parts, " "))
	for _, p := range []image.Point{{b.Min.X, b.Min.Y}, {b.Max.X - 1, b.Min.Y}, {b.Min.X, b.Max.Y - 1}, {b.Max.X - 1, b.Max.Y - 1}} {
		t.Logf("corner %v = %v", p, flatten(img, p.X, p.Y))
	}
}
