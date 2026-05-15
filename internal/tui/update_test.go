package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/tmshv/undup/internal/scan"
)

func keyPress(s string) tea.KeyMsg {
	switch s {
	case " ":
		return tea.KeyMsg{Type: tea.KeySpace}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func newModelWithFindings(findings ...Finding) Model {
	m := NewModel(nil, nil, "/scan/root")
	m.findings = findings
	return m
}

func sampleFindings() []Finding {
	return []Finding{
		{Source: SourceArchive, Label: "foo.zip", Members: []Member{
			{Path: "/scan/root/foo.zip", Size: 100, Selected: true},
			{Path: "/scan/root/foo", IsDir: true, Size: -1},
		}},
		{Source: SourceDuplicate, Label: "abcd1234", Members: []Member{
			{Path: "/scan/root/a/x.bin", Size: 50},
			{Path: "/scan/root/b/x.bin", Size: 50, Selected: true},
		}},
	}
}

func TestUpdate_CursorMovement(t *testing.T) {
	m := newModelWithFindings(sampleFindings()...)
	if m.cursor != 0 {
		t.Fatalf("initial cursor = %d, want 0", m.cursor)
	}
	m, _ = m.update(keyPress("down"))
	if m.cursor != 1 {
		t.Errorf("after down: cursor = %d, want 1", m.cursor)
	}
	m, _ = m.update(keyPress("down"))
	if m.cursor != 1 {
		t.Errorf("cursor should clamp at last visible row (groups only collapsed); got %d", m.cursor)
	}
	m, _ = m.update(keyPress("up"))
	if m.cursor != 0 {
		t.Errorf("after up: cursor = %d, want 0", m.cursor)
	}
}

func TestUpdate_ExpandCollapse(t *testing.T) {
	m := newModelWithFindings(sampleFindings()...)
	m, _ = m.update(keyPress("enter")) // expand finding 0
	if !m.findings[0].Expanded {
		t.Error("finding[0].Expanded = false after enter")
	}
	rows := m.visibleRows()
	if len(rows) != 4 { // 2 groups + 2 members of expanded group
		t.Errorf("len(visibleRows) = %d, want 4", len(rows))
	}
	m, _ = m.update(keyPress("enter")) // collapse
	if m.findings[0].Expanded {
		t.Error("finding[0].Expanded = true after second enter")
	}
}

func TestUpdate_ToggleMemberSelection(t *testing.T) {
	m := newModelWithFindings(sampleFindings()...)
	m, _ = m.update(keyPress("enter")) // expand finding 0
	m, _ = m.update(keyPress("down"))  // cursor on first member
	m, _ = m.update(keyPress(" "))     // toggle off
	if m.findings[0].Members[0].Selected {
		t.Error("member[0].Selected = true after toggle (was true initially)")
	}
}

func TestUpdate_ArchMsgAppendsFinding(t *testing.T) {
	m := NewModel(make(<-chan scan.ArchiveFinding), nil, "/scan/root")
	m, _ = m.update(archMsg(scan.ArchiveFinding{
		ArchivePath: "/scan/root/foo.zip",
		DirPath:     "/scan/root/foo",
		Size:        42,
	}))
	if len(m.findings) != 1 {
		t.Fatalf("len(findings) = %d, want 1", len(m.findings))
	}
	if m.findings[0].Source != SourceArchive || m.findings[0].Label != "foo.zip" {
		t.Errorf("got %+v", m.findings[0])
	}
}

func TestUpdate_ScanHalfDoneMarksDone(t *testing.T) {
	m := NewModel(make(<-chan scan.ArchiveFinding), make(<-chan scan.DuplicateGroup), "/scan/root")
	if m.archDone || m.dupDone {
		t.Fatalf("initial done flags wrong: arch=%v dup=%v", m.archDone, m.dupDone)
	}
	m, _ = m.update(scanHalfDoneMsg{Source: SourceArchive})
	if !m.archDone {
		t.Error("archDone = false after scanHalfDoneMsg")
	}
	if m.dupDone {
		t.Error("dupDone flipped unexpectedly")
	}
}
