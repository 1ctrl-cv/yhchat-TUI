package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// chromePadX is the horizontal padding of the full-screen frame.
const chromePadX = 2

// screenSize normalizes a possibly unknown terminal size to usable defaults.
func screenSize(width, height int) (int, int) {
	if width <= 0 {
		width = 80
	}
	if height <= 0 {
		height = 24
	}
	return width, height
}

// layoutHeight counts rendered rows of a body slice; entries may be
// multi-line strings (inputs and buttons render their own border rows).
func layoutHeight(entries []string) int {
	total := 0
	for _, entry := range entries {
		total += strings.Count(entry, "\n") + 1
	}
	return total
}

// flattenLines expands multi-line entries into single lines.
func flattenLines(entries []string) []string {
	lines := make([]string, 0, len(entries)+8)
	for _, entry := range entries {
		lines = append(lines, strings.Split(entry, "\n")...)
	}
	return lines
}

// wrapText wraps s to lines of at most width terminal cells. It works for CJK
// text (which has no spaces) and keeps explicit newlines.
func wrapText(s string, width int) []string {
	if width <= 0 {
		return []string{s}
	}
	var lines []string
	for _, paragraph := range strings.Split(s, "\n") {
		if paragraph == "" {
			lines = append(lines, "")
			continue
		}
		var line strings.Builder
		lineWidth := 0
		for _, r := range paragraph {
			w := lipgloss.Width(string(r))
			if lineWidth+w > width && line.Len() > 0 {
				lines = append(lines, line.String())
				line.Reset()
				lineWidth = 0
			}
			line.WriteRune(r)
			lineWidth += w
		}
		if line.Len() > 0 {
			lines = append(lines, line.String())
		}
	}
	return lines
}

// screenChrome composes a screen that fills the whole terminal:
//
//	row 0        header: left title, right status
//	row 1        full-width rule
//	rows 2..h-3  body, vertically centred inside the available space
//	row h-2      full-width rule
//	row h-1      keyboard hint bar
//
// Every returned line is exactly width cells wide and the result has exactly
// height lines, so the UI uses the entire terminal. Body lines that do not fit
// are dropped (only possible on extremely short terminals).
func screenChrome(width, height int, headerLeft, headerRight string, body []string, hint string) string {
	return screenChromeAligned(width, height, headerLeft, headerRight, body, hint, false)
}

// screenChromeTop is screenChrome with the body aligned to the top of the body
// area instead of being vertically centred.
func screenChromeTop(width, height int, headerLeft, headerRight string, body []string, hint string) string {
	return screenChromeAligned(width, height, headerLeft, headerRight, body, hint, true)
}

func screenChromeAligned(width, height int, headerLeft, headerRight string, body []string, hint string, topAligned bool) string {
	if width < 8 {
		width = 8
	}
	if height < 5 {
		height = 5
	}
	padX := chromePadX
	inner := width - 2*padX
	if inner < 8 {
		padX = 1
		inner = width - 2*padX
	}
	if inner < 1 {
		inner = 1
	}

	lines := make([]string, 0, height)
	rule := separator(width)

	// Header row.
	left := truncate(headerLeft, inner)
	right := truncate(headerRight, inner)
	gap := inner - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		right = ""
		gap = maxInt(0, inner-lipgloss.Width(left))
	}
	lines = append(lines, strings.Repeat(" ", padX)+left+
		strings.Repeat(" ", gap)+right+strings.Repeat(" ", padX))
	lines = append(lines, rule)

	// Body area.
	bodySpace := height - 4
	if bodySpace < 0 {
		bodySpace = 0
	}
	flat := flattenLines(body)
	if len(flat) > bodySpace {
		flat = flat[:bodySpace]
	}
	top := (bodySpace - len(flat)) / 2
	if topAligned {
		top = 0
	}
	bottom := bodySpace - len(flat) - top
	blank := strings.Repeat(" ", width)
	for i := 0; i < top; i++ {
		lines = append(lines, blank)
	}
	for _, line := range flat {
		lines = append(lines, strings.Repeat(" ", padX)+padLine(line, inner)+strings.Repeat(" ", padX))
	}
	for i := 0; i < bottom; i++ {
		lines = append(lines, blank)
	}

	// Footer: rule + hint bar.
	lines = append(lines, rule)
	lines = append(lines, strings.Repeat(" ", padX)+
		padLine(hintStyle.Render(truncate(hint, inner)), inner)+strings.Repeat(" ", padX))

	return strings.Join(lines, "\n")
}
