package tui

import (
	"github.com/charmbracelet/lipgloss"
)

// section identifies a top-level page selected from the left sidebar.
type section int

const (
	sectionChat section = iota
	sectionContacts
	sectionCommunity
	sectionProfile
)

// sectionCount is the number of sidebar entries.
const sectionCount = 4

// sectionLabels are the single-character sidebar labels.
var sectionLabels = [sectionCount]string{"聊", "友", "社", "我"}

// sectionTitle returns the human readable page title of a section.
func sectionTitle(s section) string {
	switch s {
	case sectionChat:
		return "聊天"
	case sectionContacts:
		return "通讯录"
	case sectionCommunity:
		return "社区"
	case sectionProfile:
		return "我的"
	default:
		return ""
	}
}

// switchSectionMsg asks the App to switch to another top-level section.
type switchSectionMsg struct{ to section }

// nextSectionCursor moves the sidebar cursor by delta (clamped).
func nextSectionCursor(current section, delta int) section {
	next := int(current) + delta
	if next < 0 {
		next = 0
	}
	if next >= sectionCount {
		next = sectionCount - 1
	}
	return section(next)
}

// renderSidebar renders the narrow left sidebar column for bodyH rows.
//
// active is the section currently displayed; cursor is the sidebar selection
// used while the sidebar has focus.
func renderSidebar(bodyH int, active, cursor section, focused bool) []string {
	lines := make([]string, bodyH)
	for i := 0; i < sectionCount && i < bodyH; i++ {
		sec := section(i)
		label := sectionLabels[i]
		var cell string
		switch {
		case focused && sec == cursor:
			cell = sidebarActiveStyle.Render(label)
		case sec == active:
			cell = lipgloss.NewStyle().
				Bold(true).
				Foreground(colorAccent).
				Padding(0, 1).
				Render(label)
		default:
			cell = sidebarItemStyle.Render(label)
		}
		lines[i] = padLine(cell, sidebarWidth)
	}
	for i := sectionCount; i < bodyH; i++ {
		lines[i] = padLine("", sidebarWidth)
	}
	return lines
}

// sidebarHint is the shared footer hint shown while the sidebar has focus.
func sidebarHint() string {
	return "↑↓ 选择栏目 · Enter 进入 · Tab 切换焦点 · Ctrl+C 退出"
}

// joinColumns joins fixed-width columns with the shared divider.
func joinColumns(columns ...[]string) []string {
	if len(columns) == 0 {
		return nil
	}
	height := len(columns[0])
	divider := dividerStyle.Render("│")
	lines := make([]string, height)
	for row := 0; row < height; row++ {
		line := ""
		for i, column := range columns {
			if i > 0 {
				line += " " + divider + " "
			}
			var value string
			if row < len(column) {
				value = column[row]
			}
			line += value
		}
		lines[row] = line
	}
	return lines
}
