package tui

import (
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

// View is a stub until Task 14 implements rendering.
func (m Model) View() string { return "" }
