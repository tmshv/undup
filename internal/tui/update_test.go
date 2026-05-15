package tui

import (
	"os"
	"path/filepath"
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

func TestUpdate_SelectAllInGroup(t *testing.T) {
	m := newModelWithFindings(sampleFindings()...)
	m, _ = m.update(keyPress("enter")) // expand finding 0 (cursor on group)
	m, _ = m.update(keyPress("a"))     // select all members of finding 0
	for i, mem := range m.findings[0].Members {
		if mem.Selectable() && !mem.Selected {
			t.Errorf("member[%d].Selected = false after 'a'", i)
		}
	}
}

func TestUpdate_ClearAll(t *testing.T) {
	m := newModelWithFindings(sampleFindings()...)
	m, _ = m.update(keyPress("c"))
	for fi, f := range m.findings {
		for mi, mem := range f.Members {
			if mem.Selected {
				t.Errorf("findings[%d].Members[%d].Selected = true after 'c'", fi, mi)
			}
		}
	}
}

func TestUpdate_ApplyDefaults(t *testing.T) {
	m := newModelWithFindings(sampleFindings()...)
	m, _ = m.update(keyPress("c")) // clear everything first
	m, _ = m.update(keyPress("A")) // re-apply defaults
	// Archive finding default: archive selected, dir not.
	if !m.findings[0].Members[0].Selected || m.findings[0].Members[1].Selected {
		t.Errorf("archive defaults wrong: %+v", m.findings[0].Members)
	}
	// Duplicate finding default: keep first, select rest.
	if m.findings[1].Members[0].Selected || !m.findings[1].Members[1].Selected {
		t.Errorf("duplicate defaults wrong: %+v", m.findings[1].Members)
	}
}

func TestUpdate_DeleteOpensConfirmModal(t *testing.T) {
	m := newModelWithFindings(sampleFindings()...)
	m, _ = m.update(keyPress("d"))
	if m.mode != modeConfirm {
		t.Errorf("mode = %v, want modeConfirm", m.mode)
	}
	if m.pending != actionDelete {
		t.Errorf("pending = %v, want actionDelete", m.pending)
	}
}

func TestUpdate_DeleteWithNoSelectionIgnored(t *testing.T) {
	m := newModelWithFindings(sampleFindings()...)
	// Clear all selections.
	m, _ = m.update(keyPress("c"))
	m, _ = m.update(keyPress("d"))
	if m.mode != modeBrowse {
		t.Errorf("mode = %v, want modeBrowse (no selection -> no modal)", m.mode)
	}
}

func TestUpdate_ConfirmCancelClosesModal(t *testing.T) {
	m := newModelWithFindings(sampleFindings()...)
	m, _ = m.update(keyPress("d"))
	m, _ = m.update(keyPress("n"))
	if m.mode != modeBrowse {
		t.Errorf("mode = %v, want modeBrowse after cancel", m.mode)
	}
	if m.pending != actionNone {
		t.Errorf("pending = %v, want actionNone", m.pending)
	}
}

func TestUpdate_MoveOpensPrompt(t *testing.T) {
	m := newModelWithFindings(sampleFindings()...)
	m, _ = m.update(keyPress("m"))
	if m.mode != modeMovePrompt {
		t.Errorf("mode = %v, want modeMovePrompt", m.mode)
	}
}

func TestUpdate_MovePromptEnterValidatesAndConfirms(t *testing.T) {
	m := newModelWithFindings(sampleFindings()...)
	m, _ = m.update(keyPress("m"))
	m.moveInput.SetValue("/tmp/quarantine") // outside scanRoot=/scan/root
	m, _ = m.update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.mode != modeConfirm {
		t.Errorf("mode = %v, want modeConfirm after valid target", m.mode)
	}
	if m.pending != actionMove {
		t.Errorf("pending = %v, want actionMove", m.pending)
	}
	if m.moveTarget != "/tmp/quarantine" {
		t.Errorf("moveTarget = %q, want /tmp/quarantine", m.moveTarget)
	}
}

func TestUpdate_MovePromptInsideScanRootStays(t *testing.T) {
	m := newModelWithFindings(sampleFindings()...)
	m, _ = m.update(keyPress("m"))
	m.moveInput.SetValue("/scan/root/sub")
	m, _ = m.update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.mode != modeMovePrompt {
		t.Errorf("mode = %v, want modeMovePrompt (validation rejected)", m.mode)
	}
	if m.toast == "" {
		t.Errorf("toast should be set on validation failure")
	}
}

func TestUpdate_MovePromptEscCancels(t *testing.T) {
	m := newModelWithFindings(sampleFindings()...)
	m, _ = m.update(keyPress("m"))
	m, _ = m.update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.mode != modeBrowse {
		t.Errorf("mode = %v, want modeBrowse after Esc", m.mode)
	}
}

func TestUpdate_ActionResultRemovesSucceededMembers(t *testing.T) {
	m := newModelWithFindings(sampleFindings()...)
	m.findings[1].Members[1].Selected = true
	res := ApplyResult{
		Ok:        1,
		Succeeded: map[string]bool{"/scan/root/b/x.bin": true},
	}
	m, _ = m.update(actionResultMsg{result: res, action: actionDelete})
	// Finding 1 had 2 members; one removed -> remaining count 1 -> finding pruned.
	if len(m.findings) != 1 {
		t.Errorf("len(findings) = %d, want 1 (singleton group pruned)", len(m.findings))
	}
	if m.toast == "" {
		t.Errorf("toast should be set after action")
	}
}

func TestUpdate_ConfirmYDispatches(t *testing.T) {
	m := newModelWithFindings(sampleFindings()...)
	m, _ = m.update(keyPress("d"))
	m, cmd := m.update(keyPress("y"))
	if m.mode != modeApplying {
		t.Errorf("mode = %v, want modeApplying after y", m.mode)
	}
	if cmd == nil {
		t.Error("expected a tea.Cmd to perform the action")
	}
}

func TestDirSizeWalk_SumsRegularFiles(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "a/x.bin"), make([]byte, 100))
	mustWrite(t, filepath.Join(root, "a/y.bin"), make([]byte, 250))
	mustWrite(t, filepath.Join(root, "z.bin"), make([]byte, 50))

	got, err := walkDirSize(root)
	if err != nil {
		t.Fatalf("walkDirSize: %v", err)
	}
	if got != 400 {
		t.Errorf("got = %d, want 400", got)
	}
}

func TestUpdate_DirSizeMsgUpdatesMember(t *testing.T) {
	m := newModelWithFindings(sampleFindings()...)
	m, _ = m.update(dirSizeMsg{findingIdx: 0, memberIdx: 1, size: 5000, err: nil})
	if got := m.findings[0].Members[1].Size; got != 5000 {
		t.Errorf("Size = %d, want 5000", got)
	}
}

func TestUpdate_DirSizeMsgErrorMakesNonSelectable(t *testing.T) {
	m := newModelWithFindings(sampleFindings()...)
	m, _ = m.update(dirSizeMsg{findingIdx: 0, memberIdx: 1, err: os.ErrPermission})
	if m.findings[0].Members[1].Selectable() {
		t.Error("member should be non-selectable after dir-size error")
	}
}

func TestUpdate_ExpandTriggersDirSizeForArchive(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "foo/x.bin"), []byte("hi"))
	zipPath := filepath.Join(root, "foo.zip")
	mustWrite(t, zipPath, []byte{})
	m := NewModel(nil, nil, root)
	m.findings = []Finding{FromArchive(scan.ArchiveFinding{
		ArchivePath: zipPath,
		DirPath:     filepath.Join(root, "foo"),
		Size:        0,
	})}
	_, cmd := m.update(keyPress("enter"))
	if cmd == nil {
		t.Fatal("expected dir-size cmd on first expansion of archive group")
	}
	msg := cmd()
	if _, ok := msg.(dirSizeMsg); !ok {
		t.Errorf("cmd returned %T, want dirSizeMsg", msg)
	}
}
