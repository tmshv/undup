package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var (
	titleStyle  = lipgloss.NewStyle().Bold(true)
	cursorStyle = lipgloss.NewStyle().Reverse(true)
	dimStyle    = lipgloss.NewStyle().Faint(true)
	warnStyle   = lipgloss.NewStyle().Bold(true)
)

const (
	helpLine = "↑/↓ move  g/G first/last  space toggle  ⏎ expand  a sel-group  A defaults  c clear  d delete  m move  q quit"
)

func (m Model) View() string {
	var b strings.Builder

	b.WriteString(titleStyle.Render("undup") + "\n")
	b.WriteString(m.statusLine() + "\n")
	b.WriteString(strings.Repeat("─", maxWidth(m.width, 60)) + "\n")
	b.WriteString(m.tableBody())
	b.WriteString(strings.Repeat("─", maxWidth(m.width, 60)) + "\n")
	b.WriteString(dimStyle.Render(helpLine) + "\n")

	if m.toast != "" {
		b.WriteString("\n" + warnStyle.Render(m.toast) + "\n")
	}

	switch m.mode {
	case modeMovePrompt:
		b.WriteString("\n" + m.movePromptView())
	case modeConfirm:
		b.WriteString("\n" + m.confirmView())
	case modeApplying:
		b.WriteString("\n" + dimStyle.Render("applying…"))
	}
	return b.String()
}

func (m Model) statusLine() string {
	scan := "Scanning…"
	if m.archDone && m.dupDone {
		scan = "Done"
	}
	selN, selX, reclaim := m.selectedTotals()
	if len(m.findings) == 0 {
		return fmt.Sprintf("%s · no findings", scan)
	}
	return fmt.Sprintf("%s   Selected: %d / %d   Reclaim: %s", scan, selN, selX, humanSize(reclaim))
}

func (m Model) selectedTotals() (selected, selectable int, reclaim int64) {
	for _, f := range m.findings {
		for _, mem := range f.Members {
			if !mem.Selectable() {
				continue
			}
			selectable++
			if mem.Selected && mem.Size > 0 {
				reclaim += mem.Size
			}
			if mem.Selected {
				selected++
			}
		}
	}
	return
}

func (m Model) tableBody() string {
	var b strings.Builder
	rows := m.visibleRows()
	for i, r := range rows {
		f := m.findings[r.findingIdx]
		var line string
		if r.memberIdx == -1 {
			indicator := "▶"
			if f.Expanded {
				indicator = "▼"
			}
			line = fmt.Sprintf("%s %s  %-20s   %10s   %d",
				indicator, f.Source.Tag(), truncate(f.Label, 20), groupSizeLabel(f), len(f.Members))
		} else {
			mem := f.Members[r.memberIdx]
			check := "[ ]"
			if mem.Selected {
				check = "[x]"
			}
			if !mem.Selectable() {
				check = "[!]"
			}
			line = fmt.Sprintf("    %s %s   %s",
				check, truncate(mem.Path, 50), memberSizeLabel(mem))
		}
		if i == m.cursor {
			line = cursorStyle.Render(line)
		}
		b.WriteString(line + "\n")
	}
	if len(rows) == 0 {
		b.WriteString(dimStyle.Render("  (no findings yet)\n"))
	}
	return b.String()
}

func groupSizeLabel(f Finding) string {
	var total int64
	known := false
	for _, m := range f.Members {
		if m.Selected && m.Size > 0 {
			total += m.Size
			known = true
		}
	}
	if !known {
		return "—"
	}
	return humanSize(total)
}

func memberSizeLabel(m Member) string {
	if m.SizeErr != nil {
		return "?"
	}
	if m.Size < 0 {
		return "…"
	}
	return humanSize(m.Size)
}

func (m Model) movePromptView() string {
	selN, _, reclaim := m.selectedTotals()
	header := fmt.Sprintf("Move %d items (%s) to:", selN, humanSize(reclaim))
	return strings.Join([]string{
		header,
		m.moveInput.View(),
		dimStyle.Render("Enter to confirm · Esc to cancel"),
	}, "\n")
}

func (m Model) confirmView() string {
	selN, _, reclaim := m.selectedTotals()
	dirCount := 0
	for _, f := range m.findings {
		for _, mem := range f.Members {
			if mem.Selected && mem.IsDir {
				dirCount++
			}
		}
	}
	var lines []string
	switch m.pending {
	case actionDelete:
		lines = append(lines, fmt.Sprintf("Delete %d files / directories?", selN))
	case actionMove:
		lines = append(lines, fmt.Sprintf("Move %d items to %s?", selN, m.moveTarget))
	}
	lines = append(lines, fmt.Sprintf("Total reclaim: %s", humanSize(reclaim)))
	if dirCount > 0 && m.pending == actionDelete {
		lines = append(lines, "", warnStyle.Render(fmt.Sprintf("Includes %d directory (recursive).", dirCount)))
	}
	lines = append(lines, "", dimStyle.Render("[y] yes   [n / Esc] cancel"))
	return strings.Join(lines, "\n")
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return "…" + s[len(s)-max+1:]
}

func maxWidth(have, fallback int) int {
	if have > 0 {
		return have
	}
	return fallback
}

func humanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for x := n / unit; x >= unit; x /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}
