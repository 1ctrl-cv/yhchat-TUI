package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Palette uses adaptive colors so the UI is readable on both dark and light
// terminals.
var (
	colorAccent   = lipgloss.AdaptiveColor{Light: "#5B4BD6", Dark: "#9D8CFF"}
	colorCyan     = lipgloss.AdaptiveColor{Light: "#0E7490", Dark: "#22D3EE"}
	colorText     = lipgloss.AdaptiveColor{Light: "#1F2328", Dark: "#E6EDF3"}
	colorMuted    = lipgloss.AdaptiveColor{Light: "#6E7781", Dark: "#8B949E"}
	colorBorder   = lipgloss.AdaptiveColor{Light: "#D0D7DE", Dark: "#30363D"}
	colorInputBg  = lipgloss.AdaptiveColor{Light: "#F0F2F5", Dark: "#1B2230"}
	colorSuccess  = lipgloss.AdaptiveColor{Light: "#1A7F37", Dark: "#3FB950"}
	colorError    = lipgloss.AdaptiveColor{Light: "#CF222E", Dark: "#F85149"}
	colorWarn     = lipgloss.AdaptiveColor{Light: "#9A6700", Dark: "#D29922"}
	colorOnAccent = lipgloss.AdaptiveColor{Light: "#FFFFFF", Dark: "#0D1117"}
)

var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorCyan)

	titleAccentStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(colorAccent)

	subtitleStyle = lipgloss.NewStyle().Foreground(colorMuted)

	labelStyle        = lipgloss.NewStyle().Foreground(colorMuted)
	labelFocusedStyle = lipgloss.NewStyle().Bold(true).Foreground(colorAccent)

	inputStyle = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder(), false, false, true, false).
			BorderForeground(colorBorder).
			Padding(0, 2)

	inputFocusedStyle = inputStyle.Copy().BorderForeground(colorAccent)

	tabActiveStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorOnAccent).
			Background(colorAccent).
			Padding(0, 2)

	tabInactiveStyle = lipgloss.NewStyle().
				Foreground(colorMuted).
				Padding(0, 2)

	// Compact button styles used inside the centered add-friend dialog.
	dialogCancelStyle  = lipgloss.NewStyle().Foreground(colorMuted).Padding(0, 1)
	dialogPrimaryStyle = lipgloss.NewStyle().Bold(true).Foreground(colorOnAccent).Background(colorAccent).Padding(0, 1)

	// Compact tabs for narrow columns (community list / add-friend dialog).
	tabActiveCompactStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(colorOnAccent).
				Background(colorAccent)
	tabInactiveCompactStyle = lipgloss.NewStyle().
				Foreground(colorMuted)

	buttonStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorBorder).
			Foreground(colorText).
			Padding(0, 2)

	buttonFocusedStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(colorAccent).
				Background(colorAccent).
				Foreground(colorOnAccent).
				Bold(true).
				Padding(0, 2)

	buttonDisabledStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(colorBorder).
				Foreground(colorMuted).
				Padding(0, 2)

	separatorStyle = lipgloss.NewStyle().Foreground(colorBorder)

	// Chat window styles.
	dividerStyle     = lipgloss.NewStyle().Foreground(colorBorder)
	sidebarItemStyle = lipgloss.NewStyle().
				Foreground(colorMuted).
				Padding(0, 1)
	sidebarActiveStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(colorOnAccent).
				Background(colorAccent).
				Padding(0, 1)
	convSelectedStyle        = lipgloss.NewStyle().Bold(true).Foreground(colorAccent)
	convNameStyle            = lipgloss.NewStyle().Foreground(colorText)
	convTimeStyle            = lipgloss.NewStyle().Foreground(colorMuted)
	convPreviewStyle         = lipgloss.NewStyle().Foreground(colorMuted)
	unreadBadgeStyle         = lipgloss.NewStyle().Bold(true).Foreground(colorAccent)
	chatTitleStyle           = lipgloss.NewStyle().Bold(true).Foreground(colorText)
	chatSubtitleStyle        = lipgloss.NewStyle().Foreground(colorMuted)
	messageNameStyle         = lipgloss.NewStyle().Foreground(colorMuted)
	messageOwnNameSt         = lipgloss.NewStyle().Foreground(colorAccent)
	messageTimeStyle         = lipgloss.NewStyle().Foreground(colorMuted)
	messageTextStyle         = lipgloss.NewStyle().Foreground(colorText)
	messageOwnTextSt         = lipgloss.NewStyle().Foreground(colorText)
	messageTipStyle          = lipgloss.NewStyle().Foreground(colorMuted).Italic(true)
	profileMenuStyle         = lipgloss.NewStyle().Foreground(colorText)
	profileMenuSelectedStyle = lipgloss.NewStyle().Bold(true).Foreground(colorAccent)
	chatStatusErrStyle       = lipgloss.NewStyle().Foreground(colorError)

	hintStyle = lipgloss.NewStyle().Foreground(colorMuted)

	okStyle    = lipgloss.NewStyle().Foreground(colorSuccess)
	errStyle   = lipgloss.NewStyle().Foreground(colorError)
	warnStyle  = lipgloss.NewStyle().Foreground(colorWarn)
	valueStyle = lipgloss.NewStyle().Foreground(colorText).Bold(true)

	captchaBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorBorder).
			Padding(0, 1)

	homeHeaderStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorCyan)
)

// clamp constrains v to [low, high]. If high < low, high is returned.
func clamp(v, low, high int) int {
	if high < low {
		return high
	}
	if v < low {
		return low
	}
	if v > high {
		return high
	}
	return v
}

// maxInt returns the larger of a and b.
func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// wsDotStyle returns the style of the realtime connection indicator dot.
func wsDotStyle(connected bool) lipgloss.Style {
	if connected {
		return lipgloss.NewStyle().Foreground(colorSuccess)
	}
	return lipgloss.NewStyle().Foreground(colorWarn)
}

// separator renders a horizontal rule of the given width.
func separator(width int) string {
	if width <= 0 {
		return ""
	}
	return separatorStyle.Render(strings.Repeat("─", width))
}

// padLine pads s with spaces on the right to width terminal cells. Lines wider
// than width are truncated (ANSI-aware) so multi-column layouts never overflow.
func padLine(s string, width int) string {
	if width <= 0 {
		return ""
	}
	w := lipgloss.Width(s)
	if w > width {
		s = ansi.Truncate(s, width, "…")
		w = lipgloss.Width(s)
		if w > width {
			s = ansi.Truncate(s, width, "")
			w = lipgloss.Width(s)
		}
	}
	if w >= width {
		return s
	}
	return s + strings.Repeat(" ", width-w)
}

// truncate cuts s to at most width terminal cells, appending "…" when cut.
func truncate(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= width {
		return s
	}
	var b strings.Builder
	used := 0
	for _, r := range s {
		w := lipgloss.Width(string(r))
		if used+w > width-1 {
			break
		}
		b.WriteRune(r)
		used += w
	}
	return b.String() + "…"
}

// maskPhone hides the middle digits of a phone number: 138****0000.
func maskPhone(phone string) string {
	r := []rune(phone)
	if len(r) < 7 {
		return phone
	}
	var b strings.Builder
	b.WriteString(string(r[:3]))
	b.WriteString("****")
	b.WriteString(string(r[len(r)-4:]))
	return b.String()
}

// maskEmail hides most of the local part: a***@example.com.
func maskEmail(email string) string {
	at := strings.LastIndex(email, "@")
	if at <= 0 {
		return email
	}
	local, domain := email[:at], email[at:]
	first := string([]rune(local)[:1])
	return first + "***" + domain
}

// vipLabel converts the VipStatus enum into display text.
func vipLabel(status int32, isVIP bool) string {
	switch status {
	case 1:
		return "已开通"
	case 2:
		return "已过期"
	default:
		if isVIP {
			return "已开通"
		}
		return "未开通"
	}
}
