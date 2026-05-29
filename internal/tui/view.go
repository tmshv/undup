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
	helpLine = "↑/↓ move  PgUp/PgDn page  g/G first/last  space toggle  ⏎ expand  a sel-group  A defaults  c clear  d delete  m move  q quit"
)

func (m Model) View() string {
	var b strings.Builder

	b.WriteString(titleStyle.Render("undup") + "\n")
	b.WriteString(m.statusLine() + "\n")
	b.WriteString(strings.Repeat("─", maxWidth(m.width, 60)) + "\n")
	b.WriteString(m.tableBody())
	b.WriteString(m.tableFooter() + "\n")
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
	rows := m.visibleRows()
	if len(rows) == 0 {
		return dimStyle.Render("  (no findings yet)\n")
	}
	start, end := m.viewport()
	var b strings.Builder
	for i := start; i < end; i++ {
		r := rows[i]
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
	return b.String()
}

// viewport returns the [start, end) slice of visibleRows() currently on screen.
// scrollOffset is clamped by clampScroll, so start is always valid here.
func (m Model) viewport() (start, end int) {
	rows := len(m.visibleRows())
	start = m.scrollOffset
	if start > max0(rows-1) {
		start = max0(rows - 1)
	}
	end = start + m.bodyHeight()
	if end > rows {
		end = rows
	}
	return start, end
}

// tableFooter draws the bottom separator. When the table is taller than the
// viewport it embeds a position readout (↑/↓ arrows + "first–last/total") so
// the user can tell there is more content above or below.
func (m Model) tableFooter() string {
	w := maxWidth(m.width, 60)
	rows := len(m.visibleRows())
	start, end := m.viewport()
	if m.height <= 0 || rows <= end-start {
		return strings.Repeat("─", w)
	}
	up, down := " ", " "
	if start > 0 {
		up = "↑"
	}
	if end < rows {
		down = "↓"
	}
	label := fmt.Sprintf("─ %s%s %d–%d/%d ", up, down, start+1, end, rows)
	if pad := w - lipgloss.Width(label); pad > 0 {
		return label + strings.Repeat("─", pad)
	}
	return label
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
