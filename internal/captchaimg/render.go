// Package captchaimg renders small CAPTCHA images as text that can be shown
// inline in a terminal UI.
//
// The Yunhu (云湖) CAPTCHA endpoint (POST /v1/user/captcha) returns a 180x40
// PNG containing a 4-character text CAPTCHA as a base64 data URL such as
// "data:image/png;base64,iVBOR...". This package decodes that data URL and
// renders the image either as 24-bit ANSI half blocks (opt.Color) or as a
// plain grayscale ASCII ramp, fitted into a caller supplied column/row budget
// so the result can be embedded in a bordered TUI box.
package captchaimg

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"math"
	"sort"
	"strings"
)

// Options configures Render.
type Options struct {
	MaxCols int  // maximum output width in terminal columns; 0 = unlimited
	MaxRows int  // maximum output height in terminal rows; 0 = unlimited
	Color   bool // 24-bit ANSI colors when true, grayscale ASCII when false
}

const (
	// upperHalfBlock is U+2580. With a foreground color for the top pixel and
	// a background color for the bottom pixel, one text cell shows two image
	// pixel rows, so terminal output keeps the image's aspect ratio (a text
	// cell is roughly twice as tall as it is wide).
	upperHalfBlock = '\u2580'

	// asciiRamp maps darkness (index 0, a space) to brightness (index 9, '@').
	asciiRamp = " .:-=+*#%@"

	ansiReset = "\x1b[0m"
)

// maxDecodedPixels rejects images whose header declares an implausibly large
// canvas (a 180x40 CAPTCHA is ~7.2k pixels). This keeps a malformed or hostile
// remote response from allocating gigabytes while decoding.
const maxDecodedPixels = 1 << 24

// DecodeDataURL decodes a base64 data URL such as
// "data:image/png;base64,...." into an image. Only PNG and JPEG are accepted,
// matching what the Yunhu API returns for image and slide CAPTCHAs. Errors are
// wrapped sentinels (ErrEmptyDataURL, ErrInvalidDataURL, ...) so callers can
// use errors.Is to tell user errors apart from corrupt-image errors.
func DecodeDataURL(dataURL string) (image.Image, error) {
	trimmed := strings.TrimSpace(dataURL)
	if trimmed == "" {
		return nil, ErrEmptyDataURL
	}
	if len(trimmed) < len("data:") || !strings.EqualFold(trimmed[:len("data:")], "data:") {
		return nil, fmt.Errorf("%w: missing \"data:\" prefix", ErrInvalidDataURL)
	}

	comma := strings.IndexByte(trimmed, ',')
	if comma < 0 {
		return nil, fmt.Errorf("%w: missing comma between metadata and payload", ErrInvalidDataURL)
	}
	metadata, payload := trimmed[len("data:"):comma], trimmed[comma+1:]

	mediaType, isBase64 := parseMetadata(metadata)
	if !isBase64 {
		return nil, fmt.Errorf("%w: metadata %q does not mark the payload as base64", ErrInvalidDataURL, metadata)
	}

	// Reject unsupported formats before touching the payload.
	switch mediaType {
	case "image/png", "image/jpeg", "image/jpg":
	default:
		return nil, fmt.Errorf("%w: %q (supported: image/png, image/jpeg)", ErrUnsupportedMediaType, mediaType)
	}

	payload = stripSpace(payload)
	if payload == "" {
		return nil, fmt.Errorf("%w: empty base64 payload", ErrInvalidDataURL)
	}
	raw, err := decodeBase64(payload)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidBase64, err)
	}
	if len(raw) == 0 {
		return nil, fmt.Errorf("%w: decoded payload is empty", ErrCorruptImage)
	}

	// Check the header size before decoding pixel data.
	cfg, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrCorruptImage, err)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 {
		return nil, fmt.Errorf("%w: image has empty bounds %dx%d", ErrCorruptImage, cfg.Width, cfg.Height)
	}
	if int64(cfg.Width)*int64(cfg.Height) > maxDecodedPixels {
		return nil, fmt.Errorf("%w: %dx%d exceeds %d pixels", ErrImageTooLarge, cfg.Width, cfg.Height, maxDecodedPixels)
	}

	var img image.Image
	switch mediaType {
	case "image/png":
		img, err = png.Decode(bytes.NewReader(raw))
	default: // image/jpeg or image/jpg
		img, err = jpeg.Decode(bytes.NewReader(raw))
	}
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrCorruptImage, err)
	}
	if img == nil {
		return nil, fmt.Errorf("%w: decoder returned a nil image", ErrCorruptImage)
	}
	return img, nil
}

// Render converts img into terminal text made of half-block runes (or an ASCII
// ramp when opt.Color is false). It returns "" when img is nil or has empty
// bounds. The output never exceeds opt.MaxCols columns or opt.MaxRows rows
// (0 means unlimited); aspect ratio is preserved, fitting the width first and
// then capping the height.
func Render(img image.Image, opt Options) string {
	if img == nil {
		return ""
	}
	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	if w <= 0 || h <= 0 {
		return ""
	}

	cols, rows := outputGrid(w, h, opt.MaxCols, opt.MaxRows)
	pixels := resample(img, cols, rows*2)
	if opt.Color {
		return renderColor(pixels, cols, rows)
	}
	return renderASCII(pixels, cols, rows)
}

// RenderDataURL is a convenience wrapper around DecodeDataURL + Render.
func RenderDataURL(dataURL string, opt Options) (string, error) {
	img, err := DecodeDataURL(dataURL)
	if err != nil {
		return "", err
	}
	return Render(img, opt), nil
}

// parseMetadata splits the part of a data URL between "data:" and the comma
// (for example "image/png;base64" or "image/jpeg;charset=utf-8;base64") into a
// lowercase media type and a flag telling whether the payload is base64.
func parseMetadata(metadata string) (mediaType string, isBase64 bool) {
	parts := strings.Split(metadata, ";")
	mediaType = strings.ToLower(strings.TrimSpace(parts[0]))
	for _, param := range parts[1:] {
		if strings.EqualFold(strings.TrimSpace(param), "base64") {
			isBase64 = true
		}
	}
	return mediaType, isBase64
}

// stripSpace removes ASCII whitespace from a base64 payload so data URLs that
// were pretty-printed (line wrapped) still decode.
func stripSpace(s string) string {
	if !strings.ContainsAny(s, " \t\r\n\v\f") {
		return s
	}
	return strings.Map(func(r rune) rune {
		switch r {
		case ' ', '\t', '\r', '\n', '\v', '\f':
			return -1
		}
		return r
	}, s)
}

// decodeBase64 accepts both padded and unpadded standard base64, which is what
// appears in the wild from JSON APIs.
func decodeBase64(s string) ([]byte, error) {
	unpadded := strings.TrimRight(s, "=")
	if raw, err := base64.RawStdEncoding.DecodeString(unpadded); err == nil {
		return raw, nil
	}
	return base64.StdEncoding.DecodeString(s) // returns the informative error
}

// pixel is one resampled RGB pixel; alpha is composited over white at sampling
// time (CAPTCHA images are opaque, but JPEG/PNG may carry alpha).
type pixel struct {
	r, g, b uint8
}

// outputGrid finds the number of terminal columns and rows needed for a w x h
// image. Without a budget (or when the image already fits) the natural grid is
// used: one column per pixel and one row per two pixel rows. Otherwise the
// image is scaled down uniformly until it fits both limits; the width limit is
// applied first and a height cap can shrink it further. The result is always
// within maxCols/maxRows when those are positive.
func outputGrid(w, h, maxCols, maxRows int) (cols, rows int) {
	naturalCols, naturalRows := w, (h+1)/2
	if maxCols <= 0 && maxRows <= 0 {
		return naturalCols, naturalRows
	}

	scale := 1.0
	if maxCols > 0 && naturalCols > maxCols {
		scale = math.Min(scale, float64(maxCols)/float64(naturalCols))
	}
	if maxRows > 0 && naturalRows > maxRows {
		scale = math.Min(scale, float64(maxRows)/float64(naturalRows))
	}
	if scale >= 1 {
		return naturalCols, naturalRows
	}

	cols = int(math.Round(float64(w) * scale))
	rows = int(math.Ceil(float64(h) * scale / 2))
	if cols < 1 {
		cols = 1
	}
	if rows < 1 {
		rows = 1
	}
	// Safety clamp: rounding must never push the output over the budget.
	if maxCols > 0 && cols > maxCols {
		cols = maxCols
	}
	if maxRows > 0 && rows > maxRows {
		rows = maxRows
	}
	return cols, rows
}

// resample downscales (or, for tiny images with an odd pixel height, samples
// one-to-one) img into a cols x rows*2 pixel grid. Each target pixel is the
// average of the source rectangle it covers (box filter). Bounds.Min is
// honoured so sub-images work too.
func resample(img image.Image, cols, rows2 int) []pixel {
	bounds := img.Bounds()
	sw, sh := bounds.Dx(), bounds.Dy()
	out := make([]pixel, cols*rows2)
	for y := 0; y < rows2; y++ {
		sy0 := y * sh / rows2
		sy1 := (y + 1) * sh / rows2
		if sy1 <= sy0 {
			sy1 = sy0 + 1
		}
		for x := 0; x < cols; x++ {
			sx0 := x * sw / cols
			sx1 := (x + 1) * sw / cols
			if sx1 <= sx0 {
				sx1 = sx0 + 1
			}
			var sr, sg, sb, n uint64
			for py := sy0; py < sy1; py++ {
				for px := sx0; px < sx1; px++ {
					r, g, b, a := img.At(bounds.Min.X+px, bounds.Min.Y+py).RGBA()
					if a < 0xffff {
						// RGBA() values are alpha-premultiplied; composite
						// the remaining opacity over opaque white.
						r += 0xffff - a
						g += 0xffff - a
						b += 0xffff - a
					}
					sr += uint64(r)
					sg += uint64(g)
					sb += uint64(b)
					n++
				}
			}
			out[y*cols+x] = pixel{
				r: uint8((sr / n) >> 8),
				g: uint8((sg / n) >> 8),
				b: uint8((sb / n) >> 8),
			}
		}
	}
	return out
}

// renderColor emits one ANSI sequence pair plus '▀' per cell: foreground is the
// upper sample and background the lower sample. The style is reset at the end
// of each line (every cell sets both colors, so no state leaks between cells).
func renderColor(pixels []pixel, cols, rows int) string {
	var sb strings.Builder
	sb.Grow(rows * (cols*31 + len(ansiReset) + 1))
	for y := 0; y < rows; y++ {
		top := pixels[(2*y)*cols : (2*y+1)*cols]
		bottom := pixels[(2*y+1)*cols : (2*y+2)*cols]
		for x := 0; x < cols; x++ {
			t, b := top[x], bottom[x]
			fmt.Fprintf(&sb, "\x1b[38;2;%d;%d;%dm\x1b[48;2;%d;%d;%dm%c",
				t.r, t.g, t.b, b.r, b.g, b.b, upperHalfBlock)
		}
		sb.WriteString(ansiReset)
		if y != rows-1 {
			sb.WriteByte('\n')
		}
	}
	return sb.String()
}

// renderASCII emits escape-free grayscale ASCII art. Each character covers one
// column and two pixel rows (a terminal cell), so the result keeps the same
// framing as the colored output. Luminance is contrast-stretched before being
// mapped onto the ramp: CAPTCHAs are dark text on a bright, noisy background,
// and without stretching the glyphs collapse into the same few ramp characters
// as the background. The stretch window is clipped to the 5th/95th percentiles
// so a handful of white noise pixels cannot flatten the contrast. Trailing
// spaces are trimmed.
func renderASCII(pixels []pixel, cols, rows int) string {
	// Average the two vertical samples of each cell and convert to luminance.
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

	lo, hi := contrastWindow(cell)

	last := float64(len(asciiRamp) - 1)
	lines := make([]string, rows)
	for y := 0; y < rows; y++ {
		var sb strings.Builder
		sb.Grow(cols)
		for x := 0; x < cols; x++ {
			lum := cell[y*cols+x]
			v := (lum - lo) / (hi - lo)
			if v < 0 {
				v = 0
			} else if v > 1 {
				v = 1
			}
			idx := int(math.Round(v * last))
			if idx < 0 {
				idx = 0
			} else if idx > int(last) {
				idx = int(last)
			}
			sb.WriteByte(asciiRamp[idx])
		}
		lines[y] = strings.TrimRight(sb.String(), " ")
	}
	return strings.Join(lines, "\n")
}

// contrastWindow returns the luminance range that maps to the ASCII ramp. It
// uses the 5th/95th percentiles when the image has enough variation so that
// outliers (a bright noise line, a dark border) do not compress the readable
// midtones; otherwise it falls back to the true minimum and maximum.
func contrastWindow(cell []float64) (lo, hi float64) {
	if len(cell) == 0 {
		return 0, 1
	}
	sorted := append([]float64(nil), cell...)
	sort.Float64s(sorted)
	at := func(p float64) float64 {
		idx := int(math.Round(p * float64(len(sorted)-1)))
		if idx < 0 {
			idx = 0
		} else if idx >= len(sorted) {
			idx = len(sorted) - 1
		}
		return sorted[idx]
	}
	lo, hi = at(0.05), at(0.95)
	if hi-lo < 0.02 {
		lo, hi = sorted[0], sorted[len(sorted)-1]
	}
	if hi-lo < 0.02 {
		// Nearly uniform image: avoid dividing by ~0 and keep the absolute
		// luminance so the output still reflects how bright the image is.
		return 0, 1
	}
	return lo, hi
}
