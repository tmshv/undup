package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

// helper: run msg through the public Update (which clamps scroll) and recover
// the concrete Model.
func step(m Model, msg tea.Msg) Model {
	tm, _ := m.Update(msg)
	return tm.(Model)
}

func TestScroll_FirstAndLastStayVisible(t *testing.T) {
	m := newModelWithFindings(longDuplicateFinding(100))
	m.height, m.width = 20, 80

	m = step(m, keyPress("G")) // jump to last row
	out := m.View()
	if !strings.Contains(out, "file-099.bin") {
		t.Errorf("last member not visible after G:\n%s", out)
	}
	if strings.Count(out, "\n") > m.height {
		t.Errorf("view overflows height after G:\n%s", out)
	}

	m = step(m, keyPress("g")) // jump back to first row
	out = m.View()
	if !strings.Contains(out, "file-000.bin") {
		t.Errorf("first member not visible after g:\n%s", out)
	}
}

func TestScroll_DownScrollsWindow(t *testing.T) {
	m := newModelWithFindings(longDuplicateFinding(100))
	m.height, m.width = 20, 80
	// Walk the cursor past the bottom of the initial window; the visible
	// window must follow so the cursor row is always rendered.
	for range 40 {
		m = step(m, keyPress("down"))
	}
	out := m.View()
	if m.scrollOffset == 0 {
		t.Errorf("scrollOffset still 0 after scrolling down 40 rows")
	}
	wantPath := fmt.Sprintf("file-%03d.bin", m.cursor-1) // member rows are 1-based after the header
	if !strings.Contains(out, wantPath) {
		t.Errorf("cursor row %q not visible:\n%s", wantPath, out)
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

func TestUpdate_SelectGroupCycle(t *testing.T) {
	m := newModelWithFindings(sampleFindings()...)
	m, _ = m.update(keyPress("down")) // cursor → duplicate group header (finding 1)

	dup := func() []Member { return m.findings[1].Members }
	// finding 1 starts in default "all-except-one" (keep member 0).

	m, _ = m.update(keyPress("a")) // → all
	if !dup()[0].Selected || !dup()[1].Selected {
		t.Fatalf("press 1 (all): %+v", dup())
	}
	m, _ = m.update(keyPress("a")) // → none
	if dup()[0].Selected || dup()[1].Selected {
		t.Fatalf("press 2 (none): %+v", dup())
	}
	m, _ = m.update(keyPress("a")) // → all-except-one (default keep = member 0)
	if dup()[0].Selected || !dup()[1].Selected {
		t.Fatalf("press 3 (default): %+v", dup())
	}
	// 'a' must only touch the group under the cursor, not finding 0.
	if !m.findings[0].Members[0].Selected || m.findings[0].Members[1].Selected {
		t.Errorf("'a' altered the non-cursor group: %+v", m.findings[0].Members)
	}
}

func dupGroup(size int64, paths ...string) dupMsg {
	return dupMsg(scan.DuplicateGroup{Size: size, Paths: paths})
}

func TestUpdate_LiveSortByTotalSize(t *testing.T) {
	m := newModelWithFindings()
	m, _ = m.update(dupGroup(100, "/a", "/a2")) // total 200
	m, _ = m.update(dupGroup(500, "/b", "/b2")) // total 1000
	m, _ = m.update(dupGroup(50, "/c", "/c2"))  // total 100

	want := []string{"/b", "/a", "/c"} // descending by total bytes
	for i, w := range want {
		if got := m.findings[i].Members[0].Path; got != w {
			t.Errorf("findings[%d] first member = %q, want %q (order: %v)", i, got, w, firstPaths(m.findings))
		}
	}
}

func TestUpdate_SortKeepsCursorOnGroup(t *testing.T) {
	m := newModelWithFindings()
	m, _ = m.update(dupGroup(500, "/big", "/big2"))  // total 1000
	m, _ = m.update(dupGroup(100, "/mid", "/mid2"))  // total 200
	m, _ = m.update(keyPress("down"))                // cursor → /mid header (row 1)
	m, _ = m.update(dupGroup(1000, "/huge", "/hg2")) // total 2000, becomes first

	r := m.visibleRows()[m.cursor]
	if got := m.findings[r.findingIdx].Members[0].Path; got != "/mid" {
		t.Errorf("cursor left /mid after re-sort, now on %q (order: %v)", got, firstPaths(m.findings))
	}
}

func firstPaths(fs []Finding) []string {
	out := make([]string, len(fs))
	for i, f := range fs {
		if len(f.Members) > 0 {
			out[i] = f.Members[0].Path
		}
	}
	return out
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
	m, _ = m.update(dirSizeMsg{path: "/scan/root/foo", size: 5000, err: nil})
	if got := m.findings[0].Members[1].Size; got != 5000 {
		t.Errorf("Size = %d, want 5000", got)
	}
}

func TestUpdate_DirSizeMsgErrorMakesNonSelectable(t *testing.T) {
	m := newModelWithFindings(sampleFindings()...)
	m, _ = m.update(dirSizeMsg{path: "/scan/root/foo", err: os.ErrPermission})
	// Live sorting may reorder findings, so locate the affected member by path
	// rather than by a fixed index.
	mem := findMember(t, m, "/scan/root/foo")
	if mem.Selectable() {
		t.Error("member should be non-selectable after dir-size error")
	}
	if m.toast == "" {
		t.Error("expected toast set on dir-size error so user sees why selection was cleared")
	}
}

func findMember(t *testing.T, m Model, path string) Member {
	t.Helper()
	for _, f := range m.findings {
		for _, mem := range f.Members {
			if mem.Path == path {
				return mem
			}
		}
	}
	t.Fatalf("member %q not found in findings", path)
	return Member{}
}

// Regression: positional indices (findingIdx/memberIdx) went stale after an
// action pruned the original finding, letting a late dir-size walk overwrite
// or deselect a member of an unrelated finding. Keying by path makes stale
// messages a no-op.
func TestUpdate_DirSizeMsgStaleAfterPruneDoesNotCorruptOtherFinding(t *testing.T) {
	m := newModelWithFindings(sampleFindings()...)
	// Simulate the archive finding being pruned by an earlier action: drop
	// finding 0 entirely so the duplicate finding is now at index 0.
	m.findings = m.findings[1:]
	// dir-size walk for the now-gone archive's directory returns late.
	before := m.findings[0].Members[1].Size
	m, _ = m.update(dirSizeMsg{path: "/scan/root/foo", size: 9999, err: nil})
	if got := m.findings[0].Members[1].Size; got != before {
		t.Errorf("stale dir-size msg updated the wrong row: got %d, want %d", got, before)
	}
}

func TestUpdate_FirstAndLastKeys(t *testing.T) {
	m := newModelWithFindings(sampleFindings()...)
	m, _ = m.update(keyPress("enter")) // expand finding 0 → 4 visible rows
	m, _ = m.update(keyPress("G"))
	if want := len(m.visibleRows()) - 1; m.cursor != want {
		t.Errorf("G: cursor = %d, want %d", m.cursor, want)
	}
	m, _ = m.update(keyPress("g"))
	if m.cursor != 0 {
		t.Errorf("g: cursor = %d, want 0", m.cursor)
	}
}

func TestUpdate_CtrlCQuitsFromInteractiveModes(t *testing.T) {
	ctrlC := tea.KeyMsg{Type: tea.KeyCtrlC}
	for _, mode := range []uiMode{modeBrowse, modeMovePrompt, modeConfirm} {
		m := newModelWithFindings(sampleFindings()...)
		m.mode = mode
		_, cmd := m.update(ctrlC)
		if cmd == nil {
			t.Errorf("mode=%v: ctrl+c returned nil cmd, want tea.Quit", mode)
			continue
		}
		if msg := cmd(); msg == nil {
			t.Errorf("mode=%v: cmd produced nil msg, want QuitMsg", mode)
			continue
		} else if _, ok := msg.(tea.QuitMsg); !ok {
			t.Errorf("mode=%v: cmd produced %T, want tea.QuitMsg", mode, msg)
		}
	}
}

// In modeApplying a confirmed delete/move is already running as a tea.Cmd
// goroutine. Quitting now would terminate the process mid-action and could
// strand a half-removed directory or half-copied destination, so ctrl+c is
// swallowed with a warning toast until the action completes.
func TestUpdate_CtrlCInApplyingDoesNotQuit(t *testing.T) {
	m := newModelWithFindings(sampleFindings()...)
	m.mode = modeApplying
	m2, cmd := m.update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if m2.mode != modeApplying {
		t.Errorf("mode = %v, want modeApplying preserved", m2.mode)
	}
	if m2.toast == "" {
		t.Error("expected toast warning that action is in progress")
	}
	if cmd == nil {
		t.Fatal("expected toast-clear cmd, got nil")
	}
	if msg := cmd(); msg != nil {
		if _, ok := msg.(tea.QuitMsg); ok {
			t.Error("ctrl+c in modeApplying should NOT produce QuitMsg")
		}
	}
}

func TestWalkDirSize_SkipsUnreadableSubdir(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses permission bits")
	}
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "readable/x.bin"), make([]byte, 100))
	mustWrite(t, filepath.Join(root, "blocked/y.bin"), make([]byte, 9999))
	blocked := filepath.Join(root, "blocked")
	if err := os.Chmod(blocked, 0); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(blocked, 0o755) })

	got, err := walkDirSize(root)
	if err != nil {
		t.Fatalf("walkDirSize errored on unreadable subdir; want soft-skip: %v", err)
	}
	if got != 100 {
		t.Errorf("got = %d, want 100 (readable file only)", got)
	}
}

func TestApplyActionCmd_SnapshotIsolatedFromConcurrentMemberMutation(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "f.bin")
	mustWrite(t, p, []byte("x"))
	m := NewModel(nil, nil, root)
	m.findings = []Finding{{Members: []Member{{Path: p, Size: 1, Selected: true}}}}

	cmd := applyActionCmd(DeleteAction{}, root, m.findings, actionDelete, "")
	// Mutate the original Members backing array AFTER the snapshot is taken
	// but BEFORE the action goroutine reads it. A shallow copy would let this
	// mutation flip the snapshot's Selected to false and the file would survive.
	m.findings[0].Members[0].Selected = false
	msg := cmd()
	res, ok := msg.(actionResultMsg)
	if !ok {
		t.Fatalf("cmd produced %T, want actionResultMsg", msg)
	}
	if res.result.Ok != 1 {
		t.Errorf("Ok = %d, want 1 (snapshot must be insulated from foreground mutation)", res.result.Ok)
	}
	if _, err := os.Lstat(p); !os.IsNotExist(err) {
		t.Errorf("file still exists: %v", err)
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
