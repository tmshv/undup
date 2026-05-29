package tui

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/tmshv/undup/internal/scan"
)

type archMsg scan.ArchiveFinding
type dupMsg scan.DuplicateGroup
type scanHalfDoneMsg struct{ Source Source }
type clearToastMsg struct{}

// dirSizeMsg is keyed by path rather than by (findingIdx, memberIdx). A
// dir-size walk runs concurrently with action dispatch; if an action prunes
// findings before the walk returns, positional indices would silently point
// at a different row and write the result there. Path-based lookup makes
// stale messages either match the original member or no-op cleanly.
type dirSizeMsg struct {
	path string
	size int64
	err  error
}

func walkDirSize(path string) (int64, error) {
	var total int64
	err := filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			// Root unreadable: surface the error so the user knows why.
			if p == path {
				return err
			}
			// Subentry unreadable: skip and keep walking — a single permission
			// glitch shouldn't make an otherwise-actionable directory unusable.
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		if info.Mode().IsRegular() {
			total += info.Size()
		}
		return nil
	})
	return total, err
}

func dirSizeCmd(path string) tea.Cmd {
	return func() tea.Msg {
		size, err := walkDirSize(path)
		return dirSizeMsg{path: path, size: size, err: err}
	}
}

type actionResultMsg struct {
	result ApplyResult
	action pendingAction
	target string
}

func applyActionCmd(action Action, scanRoot string, findings []Finding, kind pendingAction, target string) tea.Cmd {
	// Deep-copy Members so the action goroutine never reads a slice that the
	// foreground Update can mutate (e.g. dirSizeMsg writing to .Selected/.Size).
	snapshot := make([]Finding, len(findings))
	for i, f := range findings {
		members := make([]Member, len(f.Members))
		copy(members, f.Members)
		f.Members = members
		snapshot[i] = f
	}
	return func() tea.Msg {
		res := ApplyAction(action, scanRoot, snapshot)
		return actionResultMsg{result: res, action: kind, target: target}
	}
}

func max0(n int) int {
	if n < 0 {
		return 0
	}
	return n
}

func sprintToast(format string, a ...any) string { return fmt.Sprintf(format, a...) }

func clearToastCmd() tea.Cmd {
	return tea.Tick(3*time.Second, func(time.Time) tea.Msg { return clearToastMsg{} })
}

func recvArchiveCmd(ch <-chan scan.ArchiveFinding) tea.Cmd {
	return func() tea.Msg {
		f, ok := <-ch
		if !ok {
			return scanHalfDoneMsg{Source: SourceArchive}
		}
		return archMsg(f)
	}
}

func recvDuplicateCmd(ch <-chan scan.DuplicateGroup) tea.Cmd {
	return func() tea.Msg {
		g, ok := <-ch
		if !ok {
			return scanHalfDoneMsg{Source: SourceDuplicate}
		}
		return dupMsg(g)
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	nm, cmd := m.update(msg)
	// Single choke point: after any state change, settle the viewport so the
	// cursor row is always on screen (handles cursor moves, resizes, prunes,
	// and overlay open/close all at once).
	nm.clampScroll()
	return nm, cmd
}

// update is the internal handler. Returning the concrete Model type makes
// it easier to write synthetic tests without constant type assertions.
func (m Model) update(msg tea.Msg) (Model, tea.Cmd) {
	keys := DefaultKeyMap()

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case archMsg:
		m.findings = append(m.findings, FromArchive(scan.ArchiveFinding(msg)))
		return m, recvArchiveCmd(m.archCh)

	case dupMsg:
		m.findings = append(m.findings, FromDuplicate(scan.DuplicateGroup(msg)))
		return m, recvDuplicateCmd(m.dupCh)

	case scanHalfDoneMsg:
		switch msg.Source {
		case SourceArchive:
			m.archDone = true
		case SourceDuplicate:
			m.dupDone = true
		}
		return m, nil

	case clearToastMsg:
		if time.Now().After(m.toastUntil.Add(-50 * time.Millisecond)) {
			m.toast = ""
		}
		return m, nil

	case dirSizeMsg:
		// Path-based lookup: the original (findingIdx, memberIdx) may have
		// been invalidated by a prune in actionResultMsg. If no member with
		// this path still exists the walk result is silently dropped.
		var toastCmd tea.Cmd
		for fi := range m.findings {
			for mi := range m.findings[fi].Members {
				mem := &m.findings[fi].Members[mi]
				if mem.Path != msg.path {
					continue
				}
				if msg.err != nil {
					mem.SizeErr = msg.err
					mem.Selected = false
					m.toast = sprintToast("dir-size walk failed for %s: %v", msg.path, msg.err)
					m.toastUntil = time.Now().Add(3 * time.Second)
					toastCmd = clearToastCmd()
					continue
				}
				mem.Size = msg.size
			}
		}
		return m, toastCmd

	case actionResultMsg:
		out := make([]Finding, 0, len(m.findings))
		for _, f := range m.findings {
			kept := make([]Member, 0, len(f.Members))
			for _, mem := range f.Members {
				if !msg.result.Succeeded[mem.Path] {
					kept = append(kept, mem)
				}
			}
			f.Members = kept
			if len(f.Members) >= 2 {
				out = append(out, f)
			}
		}
		m.findings = out
		if m.cursor >= len(m.visibleRows()) {
			m.cursor = max0(len(m.visibleRows()) - 1)
		}
		switch msg.action {
		case actionDelete:
			m.toast = sprintToast("Deleted %d · failed %d", msg.result.Ok, msg.result.Failed)
		case actionMove:
			m.toast = sprintToast("Moved %d to %s · failed %d", msg.result.Ok, msg.target, msg.result.Failed)
		}
		m.toastUntil = time.Now().Add(3 * time.Second)
		m.mode = modeBrowse
		m.pending = actionNone
		return m, clearToastCmd()

	case tea.KeyMsg:
		// ctrl+c is the universal escape hatch except while an action is in
		// flight: bubbletea would terminate the running goroutine on quit,
		// which can strand a half-deleted directory or a half-copied move
		// destination. Surface a toast so the user knows the keystroke was
		// seen; the action completes quickly and quit becomes available
		// again once mode returns to modeBrowse.
		if msg.Type == tea.KeyCtrlC {
			if m.mode == modeApplying {
				m.toast = "Action in progress — wait for completion"
				m.toastUntil = time.Now().Add(3 * time.Second)
				return m, clearToastCmd()
			}
			return m, tea.Quit
		}
		if m.mode == modeMovePrompt {
			switch msg.Type {
			case tea.KeyEsc:
				m.mode = modeBrowse
				m.pending = actionNone
				m.moveInput.Blur()
				return m, nil
			case tea.KeyEnter:
				target := m.moveInput.Value()
				if err := ValidateMoveTarget(target, m.scanRoot); err != nil {
					m.toast = err.Error()
					m.toastUntil = time.Now().Add(3 * time.Second)
					return m, clearToastCmd()
				}
				m.moveTarget = target
				m.moveInput.Blur()
				m.mode = modeConfirm
				return m, nil
			}
			var cmd tea.Cmd
			m.moveInput, cmd = m.moveInput.Update(msg)
			return m, cmd
		}
		if m.mode == modeConfirm {
			switch {
			case key.Matches(msg, keys.Confirm):
				var action Action
				switch m.pending {
				case actionDelete:
					action = DeleteAction{}
				case actionMove:
					action = MoveAction{Target: m.moveTarget}
				default:
					m.mode = modeBrowse
					m.pending = actionNone
					return m, nil
				}
				cmd := applyActionCmd(action, m.scanRoot, m.findings, m.pending, m.moveTarget)
				m.mode = modeApplying
				return m, cmd
			case key.Matches(msg, keys.Cancel), key.Matches(msg, keys.Quit):
				m.mode = modeBrowse
				m.pending = actionNone
				return m, nil
			}
			return m, nil
		}
		if m.mode != modeBrowse {
			return m, nil
		}
		switch {
		case key.Matches(msg, keys.Up):
			if m.cursor > 0 {
				m.cursor--
			}
		case key.Matches(msg, keys.Down):
			if m.cursor < len(m.visibleRows())-1 {
				m.cursor++
			}
		case key.Matches(msg, keys.PageUp):
			m.cursor = max0(m.cursor - m.bodyHeight())
		case key.Matches(msg, keys.PageDown):
			m.cursor += m.bodyHeight()
			if last := len(m.visibleRows()) - 1; m.cursor > last {
				m.cursor = max0(last)
			}
		case key.Matches(msg, keys.First):
			m.cursor = 0
		case key.Matches(msg, keys.Last):
			if n := len(m.visibleRows()); n > 0 {
				m.cursor = n - 1
			}
		case key.Matches(msg, keys.Expand):
			rows := m.visibleRows()
			if len(rows) == 0 {
				return m, nil
			}
			r := rows[m.cursor]
			if r.memberIdx != -1 {
				return m, nil
			}
			f := &m.findings[r.findingIdx]
			f.Expanded = !f.Expanded
			if m.cursor >= len(m.visibleRows()) {
				m.cursor = len(m.visibleRows()) - 1
			}
			if f.Expanded && f.Source == SourceArchive {
				for _, mem := range f.Members {
					if mem.IsDir && mem.Size == -1 && mem.SizeErr == nil {
						return m, dirSizeCmd(mem.Path)
					}
				}
			}
			return m, nil
		case key.Matches(msg, keys.Toggle):
			rows := m.visibleRows()
			if len(rows) == 0 {
				return m, nil
			}
			r := rows[m.cursor]
			if r.memberIdx >= 0 {
				mem := &m.findings[r.findingIdx].Members[r.memberIdx]
				if mem.Selectable() {
					mem.Selected = !mem.Selected
				}
			}
		case key.Matches(msg, keys.SelectGroup):
			rows := m.visibleRows()
			if len(rows) == 0 {
				return m, nil
			}
			fi := rows[m.cursor].findingIdx
			for i := range m.findings[fi].Members {
				if m.findings[fi].Members[i].Selectable() {
					m.findings[fi].Members[i].Selected = true
				}
			}
		case key.Matches(msg, keys.ApplyDefault):
			for fi := range m.findings {
				applyDefaultSelection(&m.findings[fi])
			}
		case key.Matches(msg, keys.ClearAll):
			for fi := range m.findings {
				for mi := range m.findings[fi].Members {
					m.findings[fi].Members[mi].Selected = false
				}
			}
		case key.Matches(msg, keys.Delete):
			if !m.hasSelection() {
				return m, nil
			}
			m.pending = actionDelete
			m.mode = modeConfirm
		case key.Matches(msg, keys.Move):
			if !m.hasSelection() {
				return m, nil
			}
			m.pending = actionMove
			m.mode = modeMovePrompt
			if m.moveTarget == "" {
				m.moveInput.SetValue("")
			} else {
				m.moveInput.SetValue(m.moveTarget)
			}
			m.moveInput.Focus()
		case key.Matches(msg, keys.Quit):
			return m, tea.Quit
		}
	}
	return m, nil
}
