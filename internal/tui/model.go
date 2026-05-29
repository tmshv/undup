package tui

import (
	"sort"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/tmshv/undup/internal/scan"
)

type uiMode int

const (
	modeBrowse uiMode = iota
	modeMovePrompt
	modeConfirm
	modeApplying
)

type pendingAction int

const (
	actionNone pendingAction = iota
	actionDelete
	actionMove
)

type Model struct {
	findings []Finding
	cursor   int
	// scrollOffset is the index of the first visibleRows() entry shown in the
	// table viewport. Kept so the cursor stays on screen when a group is taller
	// than the terminal; clamped by clampScroll after every Update.
	scrollOffset int

	mode       uiMode
	pending    pendingAction
	moveInput  textinput.Model
	moveTarget string

	archCh   <-chan scan.ArchiveFinding
	dupCh    <-chan scan.DuplicateGroup
	archDone bool
	dupDone  bool

	scanRoot string

	toast      string
	toastUntil time.Time

	width, height int
}

func NewModel(archCh <-chan scan.ArchiveFinding, dupCh <-chan scan.DuplicateGroup, scanRoot string) Model {
	ti := textinput.New()
	ti.Prompt = ""
	return Model{
		archCh:    archCh,
		dupCh:     dupCh,
		archDone:  archCh == nil,
		dupDone:   dupCh == nil,
		scanRoot:  scanRoot,
		moveInput: ti,
	}
}

// row is one visible line in the table — either a group header (memberIdx == -1)
// or one of its members.
type row struct {
	findingIdx int
	memberIdx  int // -1 == group header
}

// hasSelection reports whether any selectable member is currently selected.
func (m Model) hasSelection() bool {
	for _, f := range m.findings {
		for _, mem := range f.Members {
			if mem.Selected {
				return true
			}
		}
	}
	return false
}

// chromeLines is the number of fixed non-table lines View always draws:
// title, status, top separator, bottom separator/footer, and help.
const chromeLines = 5

// reservedBelow counts the extra lines View appends under the help line for the
// toast and the active mode overlay. The table viewport shrinks to keep all of
// it — and the title — on screen.
func (m Model) reservedBelow() int {
	n := 0
	if m.toast != "" {
		n += 2 // blank line + toast text
	}
	switch m.mode {
	case modeMovePrompt:
		n += 4 // blank + header + input + hint
	case modeConfirm:
		n += 6 // blank + header + reclaim + optional warn + blank + hint
	case modeApplying:
		n += 2 // blank + "applying…"
	}
	return n
}

// bodyHeight is how many table rows fit in the current viewport. Before the
// first WindowSizeMsg (m.height == 0) and in tests that don't set a size, it
// returns the full row count so the whole table renders, preserving the
// pre-viewport behavior.
func (m Model) bodyHeight() int {
	if m.height <= 0 {
		return len(m.visibleRows())
	}
	h := m.height - chromeLines - m.reservedBelow()
	if h < 1 {
		h = 1
	}
	return h
}

// clampScroll keeps scrollOffset in a valid range and guarantees the cursor row
// stays within the viewport. Called once at the end of Update so every cursor
// move, resize, or prune settles the window in one place.
func (m *Model) clampScroll() {
	rows := len(m.visibleRows())
	h := m.bodyHeight()
	if rows == 0 || h <= 0 {
		m.scrollOffset = 0
		return
	}
	if m.cursor < m.scrollOffset {
		m.scrollOffset = m.cursor
	} else if m.cursor >= m.scrollOffset+h {
		m.scrollOffset = m.cursor - h + 1
	}
	maxOff := max0(rows - h)
	if m.scrollOffset > maxOff {
		m.scrollOffset = maxOff
	}
	if m.scrollOffset < 0 {
		m.scrollOffset = 0
	}
}

func (m Model) visibleRows() []row {
	var out []row
	for fi, f := range m.findings {
		out = append(out, row{findingIdx: fi, memberIdx: -1})
		if !f.Expanded {
			continue
		}
		for mi := range f.Members {
			out = append(out, row{findingIdx: fi, memberIdx: mi})
		}
	}
	return out
}

// sortFindings orders groups by total bytes descending. The sort is stable and
// tie-broken by key so equal-sized groups keep a deterministic order and don't
// jitter between renders.
func (m *Model) sortFindings() {
	sort.SliceStable(m.findings, func(i, j int) bool {
		si, sj := m.findings[i].totalSize(), m.findings[j].totalSize()
		if si != sj {
			return si > sj
		}
		return m.findings[i].key() < m.findings[j].key()
	})
}

// focusKey returns the identity of the group under the cursor and the member
// offset within it, so the cursor can be restored after the list reorders.
func (m Model) focusKey() (key string, memberIdx int) {
	rows := m.visibleRows()
	if len(rows) == 0 || m.cursor >= len(rows) {
		return "", -1
	}
	r := rows[m.cursor]
	return m.findings[r.findingIdx].key(), r.memberIdx
}

// restoreFocus moves the cursor back onto the group identified by key, at the
// same member offset when it still exists, else the group header. No-op if the
// group is gone (e.g. pruned by an action).
func (m *Model) restoreFocus(key string, memberIdx int) {
	if key == "" {
		return
	}
	rows := m.visibleRows()
	for idx, r := range rows {
		if m.findings[r.findingIdx].key() == key && r.memberIdx == memberIdx {
			m.cursor = idx
			return
		}
	}
	for idx, r := range rows {
		if m.findings[r.findingIdx].key() == key {
			m.cursor = idx
			return
		}
	}
}

// resort re-sorts the findings while keeping the cursor on the same group.
// Called whenever a group's total bytes change (arrival, dir-size resolution,
// or prune). clampScroll runs afterward in the Update choke point.
func (m *Model) resort() {
	key, mi := m.focusKey()
	m.sortFindings()
	m.restoreFocus(key, mi)
}

func (m Model) Init() tea.Cmd {
	var cmds []tea.Cmd
	if m.archCh != nil {
		cmds = append(cmds, recvArchiveCmd(m.archCh))
	}
	if m.dupCh != nil {
		cmds = append(cmds, recvDuplicateCmd(m.dupCh))
	}
	return tea.Batch(cmds...)
}
