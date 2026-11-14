package captchaimg

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"
)

const fixturePath = "testdata/captcha.png"

// ansiEscape matches SGR escape sequences such as "\x1b[38;2;1;2;3m".
var ansiEscape = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func fixtureBytes(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("read fixture %s: %v", fixturePath, err)
	}
	if len(raw) == 0 {
		t.Fatalf("fixture %s is empty", fixturePath)
	}
	return raw
}

// fixtureDataURL builds the same "data:image/png;base64,..." string the Yunhu
// CAPTCHA endpoint returns, from the checked-in PNG fixture.
func fixtureDataURL(t *testing.T) string {
	t.Helper()
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(fixtureBytes(t))
}

func mustDecodeFixture(t *testing.T) image.Image {
	t.Helper()
	img, err := DecodeDataURL(fixtureDataURL(t))
	if err != nil {
		t.Fatalf("DecodeDataURL(fixture): %v", err)
	}
	if img == nil {
		t.Fatal("DecodeDataURL(fixture) returned a nil image")
	}
	return img
}

func stripANSI(s string) string { return ansiEscape.ReplaceAllString(s, "") }

// outputLines splits Render output into lines. Render never emits a trailing
// newline, so an empty output is zero lines.
func outputLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// assertBudget fails when out exceeds the Options budget or uses the wrong
// rendering mode.
func assertBudget(t *testing.T, out string, opt Options) {
	t.Helper()
	lines := outputLines(out)
	if len(lines) == 0 {
		t.Fatal("Render returned no output")
	}
	if opt.MaxRows > 0 && len(lines) > opt.MaxRows {
		t.Fatalf("output has %d rows, budget is %d", len(lines), opt.MaxRows)
	}
	wantRune := string(upperHalfBlock)
	for i, line := range lines {
		visible := line
		if opt.Color {
			visible = stripANSI(line)
			if strings.Contains(visible, "\x1b") {
				t.Fatalf("line %d still contains an escape sequence after stripping ANSI", i)
			}
			for _, r := range visible {
				if string(r) != wantRune {
					t.Fatalf("line %d contains rune %q, want only half blocks", i, r)
				}
			}
		}
		width := utf8.RuneCountInString(visible)
		if opt.MaxCols > 0 && width > opt.MaxCols {
			t.Fatalf("line %d has %d columns, budget is %d", i, width, opt.MaxCols)
		}
	}
}

func TestDecodeDataURLFixture(t *testing.T) {
	dataURL := fixtureDataURL(t)
	img, err := DecodeDataURL(dataURL)
	if err != nil {
		t.Fatalf("DecodeDataURL: %v", err)
	}
	if img == nil {
		t.Fatal("DecodeDataURL returned nil image for valid fixture")
	}
	if got := img.Bounds().Size(); got != (image.Point{X: 180, Y: 40}) {
		t.Fatalf("fixture bounds = %v, want 180x40 (was testdata/captcha.png updated?)", got)
	}

	// Surrounding whitespace (as could appear from a pretty-printed response)
	// must not matter.
	if _, err := DecodeDataURL(" \r\n\t" + dataURL + "\n "); err != nil {
		t.Fatalf("DecodeDataURL with surrounding whitespace: %v", err)
	}

	// Media type parameters and upper case are tolerated.
	payload := dataURL[strings.IndexByte(dataURL, ',')+1:]
	withParams := "data:IMAGE/PNG;charset=utf-8;base64," + payload
	if _, err := DecodeDataURL(withParams); err != nil {
		t.Fatalf("DecodeDataURL with media type parameters: %v", err)
	}

	// Line-wrapped base64 (as some encoders emit) is stripped of whitespace.
	var wrapped strings.Builder
	wrapped.WriteString("data:image/png;base64,")
	for i := 0; i < len(payload); i += 76 {
		end := i + 76
		if end > len(payload) {
			end = len(payload)
		}
		wrapped.WriteString(payload[i:end])
		wrapped.WriteString("\r\n")
	}
	if _, err := DecodeDataURL(wrapped.String()); err != nil {
		t.Fatalf("DecodeDataURL with wrapped base64: %v", err)
	}
}

func TestDecodeDataURLRejectsBadInput(t *testing.T) {
	junkB64 := base64.StdEncoding.EncodeToString([]byte("this is definitely not an image"))
	tests := []struct {
		name    string
		dataURL string
		wantErr error
	}{
		{"empty", "", ErrEmptyDataURL},
		{"whitespace-only", " \t\r\n ", ErrEmptyDataURL},
		{"not-a-data-url", "https://example.com/captcha.png", ErrInvalidDataURL},
		{"missing-data-prefix", "image/png;base64,AAAA", ErrInvalidDataURL},
		{"missing-comma", "data:image/png;base64", ErrInvalidDataURL},
		{"missing-base64-marker", "data:image/png," + junkB64, ErrInvalidDataURL},
		{"empty-payload", "data:image/png;base64,", ErrInvalidDataURL},
		{"whitespace-payload", "data:image/png;base64,   \r\n\t", ErrInvalidDataURL},
		{"bad-base64", "data:image/png;base64,@@not-base64@@", ErrInvalidBase64},
		{"truncated-base64", "data:image/png;base64,iVBORw0KGgo", ErrCorruptImage},
		{"unsupported-gif", "data:image/gif;base64,R0lGODlhAQABAIAAAAAAAP///ywAAAAAAQABAAACAUwAOw==", ErrUnsupportedMediaType},
		{"unsupported-text", "data:text/plain;base64,AAAA", ErrUnsupportedMediaType},
		{"png-junk", "data:image/png;base64," + junkB64, ErrCorruptImage},
		{"jpeg-junk", "data:image/jpeg;base64," + junkB64, ErrCorruptImage},
		{"jpeg-truncated-header", "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString([]byte{0xff, 0xd8, 0xff, 0xe0, 0x00, 0x10}), ErrCorruptImage},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			img, err := DecodeDataURL(tt.dataURL)
			if err == nil {
				t.Fatalf("DecodeDataURL(%q) = %v, want error", tt.dataURL, img)
			}
			if img != nil {
				t.Fatalf("DecodeDataURL(%q) returned a non-nil image alongside error", tt.dataURL)
			}
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("DecodeDataURL(%q) error = %v, want errors.Is(..., %v)", tt.dataURL, err, tt.wantErr)
			}
			out, renderErr := RenderDataURL(tt.dataURL, Options{MaxCols: 74, MaxRows: 14, Color: true})
			if renderErr == nil {
				t.Fatalf("RenderDataURL(%q) = %q, want error", tt.dataURL, out)
			}
			if out != "" {
				t.Fatalf("RenderDataURL(%q) returned non-empty output alongside error: %q", tt.dataURL, out)
			}
		})
	}
}

func TestDecodeDataURLRejectsHugeImage(t *testing.T) {
	// A minimal but valid PNG header declaring 100000x100000 pixels. Only the
	// header is parsed (image.DecodeConfig), so this never allocates the pixels.
	var buf bytes.Buffer
	buf.Write([]byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a})
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:], 100000) // width
	binary.BigEndian.PutUint32(ihdr[4:], 100000) // height
	ihdr[8] = 8                                  // bit depth
	ihdr[9] = 6                                  // color type: RGBA
	writePNGChunk(&buf, "IHDR", ihdr)
	writePNGChunk(&buf, "IEND", nil)

	dataURL := "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
	if _, err := DecodeDataURL(dataURL); !errors.Is(err, ErrImageTooLarge) {
		t.Fatalf("DecodeDataURL(huge png) error = %v, want ErrImageTooLarge", err)
	}
}

func writePNGChunk(buf *bytes.Buffer, typ string, data []byte) {
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(data)))
	buf.Write(length[:])
	buf.WriteString(typ)
	buf.Write(data)
	crc := crc32.NewIEEE()
	crc.Write([]byte(typ))
	crc.Write(data)
	var sum [4]byte
	binary.BigEndian.PutUint32(sum[:], crc.Sum32())
	buf.Write(sum[:])
}

func TestRenderFixtureColor(t *testing.T) {
	img := mustDecodeFixture(t)
	opt := Options{MaxCols: 74, MaxRows: 14, Color: true}
	out := Render(img, opt)
	assertBudget(t, out, opt)

	if !strings.Contains(out, "\x1b[38;2;") {
		t.Fatal("color output does not set a 24-bit foreground color")
	}
	if !strings.Contains(out, "\x1b[48;2;") {
		t.Fatal("color output does not set a 24-bit background color")
	}
	if !strings.Contains(out, "\x1b[0m") {
		t.Fatal("color output does not reset SGR state")
	}
	if !strings.Contains(out, string(upperHalfBlock)) {
		t.Fatal("color output does not contain the upper half block rune")
	}

	// Exact grid for the real fixture at the login form's default budget:
	// 180x40 scaled to 74 columns (scale 0.4111) -> 74 cols, 9 rows.
	lines := outputLines(out)
	if len(lines) != 9 {
		t.Fatalf("fixture at 74x14 rendered %d rows, want 9", len(lines))
	}
	for i, line := range lines {
		if got := utf8.RuneCountInString(stripANSI(line)); got != 74 {
			t.Fatalf("fixture row %d has %d columns, want 74", i, got)
		}
		if !strings.HasSuffix(line, "\x1b[0m") {
			t.Fatalf("fixture row %d does not end with an SGR reset", i)
		}
	}
}

func TestRenderFixtureNoColor(t *testing.T) {
	img := mustDecodeFixture(t)
	opt := Options{MaxCols: 74, MaxRows: 14, Color: false}
	out := Render(img, opt)
	assertBudget(t, out, opt)

	if strings.ContainsRune(out, '\x1b') {
		t.Fatal("Color:false output contains escape sequences")
	}
	lines := outputLines(out)
	if len(lines) != 9 {
		t.Fatalf("grayscale fixture rendered %d rows, want 9", len(lines))
	}
	nonSpace := 0
	distinct := map[rune]bool{}
	for i, line := range lines {
		if utf8.RuneCountInString(line) > 74 {
			t.Fatalf("row %d wider than 74 columns", i)
		}
		for _, r := range line {
			if !strings.ContainsRune(asciiRamp, r) {
				t.Fatalf("row %d contains %q which is not in the ASCII ramp", i, r)
			}
			distinct[r] = true
			if r != ' ' {
				nonSpace++
			}
		}
	}
	if nonSpace == 0 {
		t.Fatal("grayscale output is blank")
	}
	if len(distinct) < 4 {
		t.Fatalf("grayscale output uses only %d distinct ramp characters; expected a legible range", len(distinct))
	}
}

func TestRenderLimits(t *testing.T) {
	img := mustDecodeFixture(t)
	tests := []struct {
		name               string
		opt                Options
		wantCols, wantRows int
	}{
		{"74x14-color", Options{MaxCols: 74, MaxRows: 14, Color: true}, 74, 9},
		{"80x16-color", Options{MaxCols: 80, MaxRows: 16, Color: true}, 80, 9},
		{"40x6-color", Options{MaxCols: 40, MaxRows: 6, Color: true}, 40, 5},
		{"60-width-only", Options{MaxCols: 60, Color: true}, 60, 7},
		{"height-only-10", Options{MaxRows: 10, Color: true}, 90, 10},
		{"height-only-3", Options{MaxRows: 3, Color: true}, 27, 3},
		{"10x10-color", Options{MaxCols: 10, MaxRows: 10, Color: true}, 10, 2},
		{"74x1-color", Options{MaxCols: 74, MaxRows: 1, Color: true}, 9, 1},
		{"1x1-color", Options{MaxCols: 1, MaxRows: 1, Color: true}, 1, 1},
		{"3x2-color", Options{MaxCols: 3, MaxRows: 2, Color: true}, 3, 1},
		{"unlimited-color", Options{Color: true}, 180, 20},
		{"unlimited-gray", Options{Color: false}, 180, 20},
		{"negative-limits-unlimited", Options{MaxCols: -5, MaxRows: -1, Color: true}, 180, 20},
		{"74x14-gray", Options{MaxCols: 74, MaxRows: 14, Color: false}, 74, 9},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := Render(img, tt.opt)
			assertBudget(t, out, tt.opt)
			lines := outputLines(out)
			if len(lines) != tt.wantRows {
				t.Fatalf("got %d rows, want %d", len(lines), tt.wantRows)
			}
			// Colored output always fills every column; grayscale may have
			// blank right-edge cells trimmed.
			if tt.opt.Color {
				for i, line := range lines {
					if got := utf8.RuneCountInString(stripANSI(line)); got != tt.wantCols {
						t.Fatalf("row %d has %d columns, want %d", i, got, tt.wantCols)
					}
				}
			} else {
				widest := 0
				for _, line := range lines {
					if w := utf8.RuneCountInString(line); w > widest {
						widest = w
					}
				}
				if widest > tt.wantCols {
					t.Fatalf("widest row is %d columns, want <= %d", widest, tt.wantCols)
				}
			}
		})
	}
}

// TestRenderBudgetSweep exhaustively checks the hard budget invariant: no
// output may ever be wider or taller than requested, for all combinations of
// small/large/latent limits and both rendering modes.
func TestRenderBudgetSweep(t *testing.T) {
	img := mustDecodeFixture(t)
	colBudgets := []int{0, 1, 2, 3, 7, 10, 37, 64, 70, 74, 80, 180, 300}
	rowBudgets := []int{0, 1, 2, 3, 4, 5, 9, 10, 14, 20, 40}
	for _, maxCols := range colBudgets {
		for _, maxRows := range rowBudgets {
			for _, useColor := range []bool{true, false} {
				opt := Options{MaxCols: maxCols, MaxRows: maxRows, Color: useColor}
				out := Render(img, opt)
				if out == "" {
					t.Fatalf("Render with %+v returned empty output", opt)
				}
				assertBudget(t, out, opt)
				lines := outputLines(out)
				if useColor {
					// A colored row is exactly the requested width and
					// colored output is deterministic.
					if out != Render(img, opt) {
						t.Fatalf("Render with %+v is not deterministic", opt)
					}
					for i, line := range lines {
						if got := utf8.RuneCountInString(stripANSI(line)); maxCols > 0 && got > maxCols {
							t.Fatalf("row %d width %d exceeds MaxCols %d", i, got, maxCols)
						}
					}
				}
			}
		}
	}
}

func TestRenderSynthetic4x4Color(t *testing.T) {
	grid := [4][4]color.RGBA{
		{{255, 0, 0, 255}, {0, 255, 0, 255}, {0, 0, 255, 255}, {255, 255, 0, 255}},
		{{0, 255, 255, 255}, {255, 0, 255, 255}, {10, 20, 30, 255}, {40, 50, 60, 255}},
		{{1, 2, 3, 255}, {4, 5, 6, 255}, {7, 8, 9, 255}, {11, 12, 13, 255}},
		{{0, 0, 0, 255}, {128, 128, 128, 255}, {200, 100, 50, 255}, {255, 255, 255, 255}},
	}
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			img.Set(x, y, grid[y][x])
		}
	}

	out := Render(img, Options{Color: true})
	wantLine0 := ""
	wantLine1 := ""
	for x := 0; x < 4; x++ {
		wantLine0 += colorCell(grid[0][x], grid[1][x])
		wantLine1 += colorCell(grid[2][x], grid[3][x])
	}
	want := wantLine0 + "\x1b[0m\n" + wantLine1 + "\x1b[0m"
	if out != want {
		t.Fatalf("4x4 render mismatch\n got: %q\nwant: %q", out, want)
	}

	gray := Render(img, Options{Color: false})
	lines := outputLines(gray)
	if len(lines) != 2 {
		t.Fatalf("4x4 grayscale rendered %d rows, want 2", len(lines))
	}
	for i, line := range lines {
		if got := utf8.RuneCountInString(line); got != 4 {
			t.Fatalf("grayscale row %d has %d columns, want 4", i, got)
		}
	}
}

func colorCell(fg, bg color.RGBA) string {
	return fmt.Sprintf("\x1b[38;2;%d;%d;%dm\x1b[48;2;%d;%d;%dm%c",
		fg.R, fg.G, fg.B, bg.R, bg.G, bg.B, upperHalfBlock)
}

func TestRenderNonZeroBounds(t *testing.T) {
	// Sub-images (Bounds().Min != 0) must sample by absolute coordinates.
	img := image.NewRGBA(image.Rect(10, 20, 14, 24))
	for y := 20; y < 24; y++ {
		for x := 10; x < 14; x++ {
			img.Set(x, y, color.RGBA{uint8(x * 3), uint8(y * 4), 7, 255})
		}
	}
	out := Render(img, Options{Color: true})
	want := ""
	for x := 10; x < 14; x++ {
		fg := img.At(x, 20).(color.RGBA)
		bg := img.At(x, 21).(color.RGBA)
		want += colorCell(fg, bg)
	}
	want += "\x1b[0m\n"
	for dx := 10; dx < 14; dx++ {
		fg := img.At(dx, 22).(color.RGBA)
		bg := img.At(dx, 23).(color.RGBA)
		want += colorCell(fg, bg)
	}
	want += "\x1b[0m"
	if out != want {
		t.Fatalf("non-zero-bounds render mismatch\n got: %q\nwant: %q", out, want)
	}
}

func TestRenderOddHeight(t *testing.T) {
	// 3x3: two rows of half blocks; the last output row repeats the last
	// source row rather than panicking or dropping it.
	img := image.NewRGBA(image.Rect(0, 0, 3, 3))
	for y := 0; y < 3; y++ {
		for x := 0; x < 3; x++ {
			img.Set(x, y, color.RGBA{uint8(10 * (y + 1)), uint8(10 * (x + 1)), 0, 255})
		}
	}
	colorOut := Render(img, Options{Color: true})
	assertBudget(t, colorOut, Options{Color: true})
	if lines := outputLines(colorOut); len(lines) != 2 {
		t.Fatalf("3x3 color rendered %d rows, want 2", len(lines))
	}
	grayOut := Render(img, Options{Color: false})
	if lines := outputLines(grayOut); len(lines) != 2 {
		t.Fatalf("3x3 grayscale rendered %d rows, want 2", len(lines))
	}
	if strings.ContainsRune(grayOut, '\x1b') {
		t.Fatal("grayscale output contains escape sequences")
	}
}

func TestRenderGrayscaleContrastStretch(t *testing.T) {
	// Dark glyph columns on a bright background must use both ends of the ramp:
	// the 5th/95th percentile stretch should map the background to '@' and the
	// letter strokes to ' ', not compress everything into '#' and '%'.
	img := image.NewRGBA(image.Rect(0, 0, 8, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 8; x++ {
			v := uint8(220)
			if x >= 2 && x < 6 {
				v = 30
			}
			img.Set(x, y, color.RGBA{v, v, v, 255})
		}
	}
	out := Render(img, Options{Color: false})
	if strings.ContainsRune(out, '\x1b') {
		t.Fatal("grayscale output contains escape sequences")
	}
	if want := "@@    @@\n@@    @@"; out != want {
		t.Fatalf("contrast-stretched grayscale render = %q, want %q", out, want)
	}

	// A uniform image must not divide by a zero contrast window; the shade maps
	// to a single ramp character based on its absolute luminance.
	uniform := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			uniform.Set(x, y, color.RGBA{128, 128, 128, 255})
		}
	}
	if got, want := Render(uniform, Options{Color: false}), "++++\n++++"; got != want {
		t.Fatalf("uniform grayscale render = %q, want %q", got, want)
	}

	// A fully dark image is blank (spaces) but still has the right row count.
	black := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			black.Set(x, y, color.RGBA{0, 0, 0, 255})
		}
	}
	if got, want := Render(black, Options{Color: false}), "\n"; got != want {
		t.Fatalf("black grayscale render = %q, want %q", got, want)
	}
}

func TestRenderNilAndEmpty(t *testing.T) {
	for _, opt := range []Options{
		{},
		{Color: true},
		{MaxCols: 74, MaxRows: 14, Color: true},
		{MaxCols: 74, MaxRows: 14, Color: false},
	} {
		if got := Render(nil, opt); got != "" {
			t.Fatalf("Render(nil, %+v) = %q, want \"\"", opt, got)
		}
	}

	zero := image.NewRGBA(image.Rect(0, 0, 0, 0))
	if got := Render(zero, Options{Color: true}); got != "" {
		t.Fatalf("Render(0x0 image) = %q, want \"\"", got)
	}
	empty := image.NewRGBA(image.Rect(5, 5, 5, 5))
	if got := Render(empty, Options{Color: false}); got != "" {
		t.Fatalf("Render(empty bounds) = %q, want \"\"", got)
	}
}

func TestRenderTransparentOverWhite(t *testing.T) {
	// Fully transparent pixels are composited over white so a (hypothetical)
	// transparent PNG does not become a black hole in the terminal.
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.RGBA{0, 0, 0, 0})
	want := colorCell(color.RGBA{255, 255, 255, 255}, color.RGBA{255, 255, 255, 255}) + "\x1b[0m"
	if got := Render(img, Options{Color: true}); got != want {
		t.Fatalf("transparent pixel render = %q, want %q", got, want)
	}
}

func TestRenderDataURLMatchesRender(t *testing.T) {
	dataURL := fixtureDataURL(t)
	img := mustDecodeFixture(t)
	for _, opt := range []Options{
		{MaxCols: 74, MaxRows: 14, Color: true},
		{MaxCols: 74, MaxRows: 14, Color: false},
		{},
	} {
		out, err := RenderDataURL(dataURL, opt)
		if err != nil {
			t.Fatalf("RenderDataURL(%+v): %v", opt, err)
		}
		want := Render(img, opt)
		if out != want {
			t.Fatalf("RenderDataURL and Render disagree for %+v", opt)
		}
	}
}

func TestRenderJPEG(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			src.Set(x, y, color.RGBA{uint8(32 * x), uint8(32 * y), 128, 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, src, &jpeg.Options{Quality: 95}); err != nil {
		t.Fatalf("jpeg.Encode: %v", err)
	}
	payload := base64.StdEncoding.EncodeToString(buf.Bytes())

	for _, mediaType := range []string{"image/jpeg", "image/jpg"} {
		dataURL := "data:" + mediaType + ";base64," + payload
		img, err := DecodeDataURL(dataURL)
		if err != nil {
			t.Fatalf("DecodeDataURL(%s): %v", mediaType, err)
		}
		if got := img.Bounds().Size(); got != (image.Point{X: 8, Y: 8}) {
			t.Fatalf("decoded JPEG bounds = %v, want 8x8", got)
		}
		out, err := RenderDataURL(dataURL, Options{Color: true})
		if err != nil {
			t.Fatalf("RenderDataURL(%s): %v", mediaType, err)
		}
		if lines := outputLines(out); len(lines) != 4 {
			t.Fatalf("%s rendered %d rows, want 4", mediaType, len(lines))
		}
	}
}

func TestRenderPNGReEncode(t *testing.T) {
	// A PNG produced in memory (rather than the fixture) also works end to end,
	// with alpha preserved through the pipeline.
	src := image.NewRGBA(image.Rect(0, 0, 6, 5))
	for y := 0; y < 5; y++ {
		for x := 0; x < 6; x++ {
			src.Set(x, y, color.RGBA{uint8(20 * x), uint8(30 * y), 90, 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, src); err != nil {
		t.Fatalf("png.Encode: %v", err)
	}
	dataURL := "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
	out, err := RenderDataURL(dataURL, Options{MaxCols: 6, MaxRows: 3, Color: true})
	if err != nil {
		t.Fatalf("RenderDataURL: %v", err)
	}
	if lines := outputLines(out); len(lines) != 3 {
		t.Fatalf("6x5 image rendered %d rows, want 3", len(lines))
	}
}
