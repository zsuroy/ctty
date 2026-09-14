package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestRenderCanvasNoOverflow(t *testing.T) {
	// A multi-line string with wide characters, ANSI codes, and lines longer than width
	content := "Line 1: Hello World, this is a very long line that will definitely exceed terminal width\n" +
		"Line 2: 中文测试 超长字符 包含表情 🚀 和各种符号\n" +
		"Line 3: Short\n"

	width := 30
	height := 10

	rendered := RenderCanvas(width, height, content)
	lines := strings.Split(rendered, "\n")

	// Must be exactly height rows
	if len(lines) != height {
		t.Fatalf("expected exactly %d lines, got %d", height, len(lines))
	}

	// No line must exceed width in terminal columns
	for i, line := range lines {
		w := ansi.StringWidth(line)
		if w > width {
			t.Fatalf("line %d width %d exceeds canvas width %d: %q", i, w, width, line)
		}
	}
}

func TestRenderCanvasZeroSize(t *testing.T) {
	content := "test content"
	// Should not panic with 0 or negative size
	if got := RenderCanvas(0, 0, content); got != content {
		t.Fatalf("expected raw content for 0 size, got %q", got)
	}
	if got := RenderCanvas(-10, -5, content); got != content {
		t.Fatalf("expected raw content for negative size, got %q", got)
	}
}

func TestRenderCanvasHeightOverflow(t *testing.T) {
	// Content with 50 lines rendered into height of 10
	var sb strings.Builder
	for i := 0; i < 50; i++ {
		sb.WriteString("line\n")
	}

	rendered := RenderCanvas(20, 10, sb.String())
	lines := strings.Split(rendered, "\n")
	if len(lines) != 10 {
		t.Fatalf("expected exactly 10 lines, got %d", len(lines))
	}
}

func TestFitHelpLineKeepsWholeItems(t *testing.T) {
	text := "↑/↓: navigate • Enter: connect • i: info • a: add • e: edit"
	for _, w := range []int{12, 20, 38, 80} {
		got := fitHelpLine(text, w)
		if ansi.StringWidth(got) > w {
			t.Fatalf("width %d: help exceeds frame: %q", w, got)
		}
		// At w=12 even the first item cannot fit; there the fallback
		// hard-truncates instead of emitting an empty footer, so the
		// whole-item contract only applies from w=20 up.
		if w < 20 {
			continue
		}
		for _, item := range []string{"↑/↓: navigate", "Enter: connect", "i: info", "a: add", "e: edit"} {
			if strings.Contains(got, item[:len(item)/2]) && !strings.Contains(got, item) {
				t.Fatalf("width %d: item half-present in %q: %q", w, item, got)
			}
		}
	}
	// Dropped items are marked when the tail fits (bubbles/help semantics:
	// at exactly-full widths the ellipsis is omitted instead of overflowing).
	if !strings.Contains(fitHelpLine(text, 45), "…") {
		t.Fatal("expected ellipsis when items are dropped")
	}
	// Everything fits: no ellipsis.
	if strings.Contains(fitHelpLine(text, 200), "…") {
		t.Fatal("unexpected ellipsis when all items fit")
	}
}

func TestFitHelpLineCJKWide(t *testing.T) {
	text := "↑↓: 移动 • →/回车: 进入目录 • ←/h: 上级 • 回车: 下载 • v: 布局 • i: 详情"
	for _, w := range []int{15, 30, 44, 72} {
		if got := ansi.StringWidth(fitHelpLine(text, w)); got > w {
			t.Fatalf("cjk width %d: help exceeds frame: %d cols", w, got)
		}
	}
}

func TestRenderMainHelpCategorized(t *testing.T) {
	styles := NewStyles(80)

	zhHelp := " [选中主机] ⏎: 连接 • e: 编辑 • i: 详情 • f: 端口转发 • o: SFTP • x: 执行 • d: 删除\n [全局功能] a: 添加 • t: 串口 • T: Telnet • F: FTP • b: 浏览 • p: 探测 • S: 设置 • /: 搜索 • h: 帮助 • q: 退出"
	renderedZH := renderMainHelp(styles, zhHelp, 120)

	if !strings.Contains(renderedZH, "选中主机") {
		t.Fatalf("expected rendered help to contain '选中主机', got: %s", renderedZH)
	}
	if !strings.Contains(renderedZH, "全局功能") {
		t.Fatalf("expected rendered help to contain '全局功能', got: %s", renderedZH)
	}

	enHelp := " [Host Actions] Enter: connect • e: edit • i: info • f: forward • o: sftp • x: exec • d: delete\n [Global Tools] a: add • t: serial • T: telnet • F: ftp • b: browse • p: ping • S: settings • /: search • h: help • q: quit"
	renderedEN := renderMainHelp(styles, enHelp, 120)

	if !strings.Contains(renderedEN, "Host Actions") {
		t.Fatalf("expected rendered help to contain 'Host Actions', got: %s", renderedEN)
	}
	if !strings.Contains(renderedEN, "Global Tools") {
		t.Fatalf("expected rendered help to contain 'Global Tools', got: %s", renderedEN)
	}
}

func TestRenderProtocolTabs(t *testing.T) {
	styles := NewStyles(100)

	// Below width threshold, returns empty
	if got := renderProtocolTabs(styles, "ssh", 12, 50); got != "" {
		t.Fatalf("expected empty tabs below width threshold 64, got %q", got)
	}

	// At sufficient width, renders all protocols
	rendered := renderProtocolTabs(styles, "ssh", 12, 100)
	if !strings.Contains(rendered, "SSH") {
		t.Fatalf("expected tabs to contain 'SSH', got %q", rendered)
	}
	if !strings.Contains(rendered, "12") {
		t.Fatalf("expected tabs to contain count '12', got %q", rendered)
	}
	if !strings.Contains(rendered, "t") || !strings.Contains(rendered, "T") {
		t.Fatalf("expected tabs to contain shortcut keys, got %q", rendered)
	}

	// Never exceeds terminal width
	for _, w := range []int{64, 70, 80, 100, 120} {
		out := renderProtocolTabs(styles, "serial", 3, w)
		if ansi.StringWidth(out) > w-4 {
			t.Fatalf("width %d: tabs line width %d exceeds budget %d", w, ansi.StringWidth(out), w-4)
		}
	}
}

