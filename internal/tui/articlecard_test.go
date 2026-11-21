package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"yhchat-tui/internal/api"
)

// TestArticleMessageCardRendering verifies the bordered title/preview card used
// for content_type=6 article messages in the chat history.
func TestArticleMessageCardRendering(t *testing.T) {
	var model mainWindowModel
	msg := api.Message{
		ContentType: 6,
		Content: api.MessageContent{
			PostTitle:   "一篇测试文章",
			PostContent: "# 第一节\n这是第一行正文\n\n第二行正文\n第三行正文\n第四行不应显示",
		},
	}
	lines := model.messageBodyLines(msg, 60)
	if len(lines) < 3 {
		t.Fatalf("card lines = %d, want at least 3", len(lines))
	}
	joined := strings.Join(lines, "\n")
	for _, want := range []string{"╭", "╮", "╰", "╯", "◆ 一篇测试文章", "这是第一行正文", "第二行正文"} {
		if !strings.Contains(joined, want) {
			t.Errorf("card missing %q:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "第四行不应显示") {
		t.Errorf("card should only show the first three preview lines:\n%s", joined)
	}
	// The box must be rectangular: all rendered lines have equal visual width.
	wantWidth := lipgloss.Width(lines[0])
	for i, line := range lines {
		if got := lipgloss.Width(line); got != wantWidth {
			t.Errorf("line %d width = %d, want %d:\n%s", i, got, wantWidth, joined)
		}
	}
}
