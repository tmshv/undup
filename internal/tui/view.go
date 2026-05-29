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
	helpLine = "↑/↓ move  PgUp/PgDn page  g/G first/last  space toggle  ⏎ expand  a group cycle  A defaults  c clear  d delete  m move  q quit"
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
	w := maxWidth(m.width, 60)
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
			// Group header: "▼ dup  <label fills>   <total>  ↓<reclaim>  <count>"
			prefix := fmt.Sprintf("%s %s  ", indicator, f.Source.Tag())
			right := groupRight(f)
			labelW := flexWidth(w, lipgloss.Width(prefix), lipgloss.Width(right))
			line = prefix + padRight(truncate(f.Label, labelW), labelW) + "  " + right
		} else {
			mem := f.Members[r.memberIdx]
			check := "[ ]"
			if mem.Selected {
				check = "[x]"
			}
			if !mem.Selectable() {
				check = "[!]"
			}
			// Member: "    [x] <path fills>   <size>            " — the size sits
			// in the same column as the group header's total; trailing pad keeps
			// the right segment as wide as the header's (rightColWidth).
			prefix := "    " + check + " "
			right := padRight(padLeft(memberSizeLabel(mem), sizeColWidth), rightColWidth)
			pathW := flexWidth(w, lipgloss.Width(prefix), lipgloss.Width(right))
			line = prefix + padRight(truncate(mem.Path, pathW), pathW) + "  " + right
		}
		if i == m.cursor {
			line = cursorStyle.Render(padRight(line, w))
		}
		b.WriteString(line + "\n")
	}
	return b.String()
}

// flexWidth is the width left for the flexible middle column (path or label)
// after the row prefix, a 2-space gap, and the fixed right column. Clamped so a
// very narrow terminal still leaves room for a few characters.
func flexWidth(total, prefix, right int) int {
	w := total - prefix - 2 - right
	if w < 8 {
		return 8
	}
	return w
}

// padRight pads s with spaces to display width w (no-op if already wider).
func padRight(s string, w int) string {
	if gap := w - lipgloss.Width(s); gap > 0 {
		return s + strings.Repeat(" ", gap)
	}
	return s
}

// padLeft right-aligns s within display width w. It measures display width
// (not byte length, as %*s would) so multi-byte runes like ↓ pad correctly.
func padLeft(s string, w int) string {
	if gap := w - lipgloss.Width(s); gap > 0 {
		return strings.Repeat(" ", gap) + s
	}
	return s
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

// Right-column geometry, shared by group headers and member rows so their size
// columns line up. The header packs total + reclaim + count into rightColWidth;
// a member shows just its size in the leading sizeColWidth slot.
const (
	sizeColWidth  = 10                            // a single right-aligned size
	rightColWidth = sizeColWidth + 2 + 10 + 2 + 3 // total, ↓reclaim, count
)

// groupRight renders a header's right column: the total size (always), the
// reclaimable size prefixed with ↓ (only when something is selected), and the
// member count.
func groupRight(f Finding) string {
	reclaim := ""
	if r := groupReclaim(f); r > 0 {
		reclaim = "↓" + humanSize(r)
	}
	return padLeft(humanSize(f.totalSize()), sizeColWidth) + "  " +
		padLeft(reclaim, 10) + "  " +
		fmt.Sprintf("%3d", len(f.Members))
}

// groupReclaim is the bytes freed by deleting the currently-selected members.
func groupReclaim(f Finding) int64 {
	var total int64
	for _, m := range f.Members {
		if m.Selected && m.Size > 0 {
			total += m.Size
		}
	}
	return total
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
