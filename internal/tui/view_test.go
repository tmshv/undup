package tui

import (
	"fmt"
	"strings"
	"testing"
)

// longDuplicateFinding builds an expanded duplicate group with n members, used
// to exercise viewport windowing when a group does not fit on screen.
func longDuplicateFinding(n int) Finding {
	members := make([]Member, n)
	for i := range members {
		members[i] = Member{
			Path:     fmt.Sprintf("/scan/root/dup/file-%03d.bin", i),
			Size:     50,
			Selected: i != 0,
		}
	}
	return Finding{Source: SourceDuplicate, Label: "longdup", Members: members, Expanded: true}
}

func TestView_LongGroupFitsHeight(t *testing.T) {
	m := newModelWithFindings(longDuplicateFinding(100))
	m.height, m.width = 20, 80
	out := m.View()
	if lines := strings.Count(out, "\n"); lines > m.height {
		t.Errorf("view rendered %d lines, exceeds terminal height %d:\n%s", lines, m.height, out)
	}
	if !strings.Contains(out, "undup") {
		t.Errorf("title row not present — chrome scrolled off:\n%s", out)
	}
}

func TestView_BrowseMode_Empty(t *testing.T) {
	m := NewModel(nil, nil, "/scan/root")
	out := m.View()
	if out == "" {
		t.Error("View() returned empty string")
	}
	if !strings.Contains(out, "no findings") && !strings.Contains(out, "Done") {
		t.Errorf("View output lacks expected status hint:\n%s", out)
	}
}

func TestView_BrowseMode_WithFindings(t *testing.T) {
	m := newModelWithFindings(sampleFindings()...)
	out := m.View()
	if !strings.Contains(out, "foo.zip") {
		t.Errorf("expected archive label in view:\n%s", out)
	}
	if !strings.Contains(out, "abcd1234") {
		t.Errorf("expected duplicate label in view:\n%s", out)
	}
}

func TestView_FullWidthPathNotTruncated(t *testing.T) {
	longPath := "/scan/root/some/deeply/nested/directory/structure/file-name.bin"
	f := Finding{Source: SourceDuplicate, Label: "abcd1234", Expanded: true, Members: []Member{
		{Path: longPath, Size: 50},
		{Path: longPath + ".2", Size: 50, Selected: true},
	}}
	m := newModelWithFindings(f)
	m.width, m.height = 120, 24
	out := m.View()
	if !strings.Contains(out, longPath) {
		t.Errorf("path truncated on a 120-col terminal; want full %q in:\n%s", longPath, out)
	}
}

func TestView_FullWidthLabelNotTruncated(t *testing.T) {
	longLabel := "a-very-long-archive-filename-that-exceeds-twenty-characters.tar.gz"
	f := Finding{Source: SourceArchive, Label: longLabel, Members: []Member{
		{Path: "/x/" + longLabel, Size: 100, Selected: true},
		{Path: "/x/dir", IsDir: true, Size: -1},
	}}
	m := newModelWithFindings(f)
	m.width, m.height = 120, 24
	out := m.View()
	if !strings.Contains(out, longLabel) {
		t.Errorf("group label truncated on a 120-col terminal; want full %q in:\n%s", longLabel, out)
	}
}

func TestView_PathTruncatedWhenTooNarrow(t *testing.T) {
	longPath := "/scan/root/some/deeply/nested/directory/structure/file-name.bin"
	f := Finding{Source: SourceDuplicate, Label: "abcd1234", Expanded: true, Members: []Member{
		{Path: longPath, Size: 50},
		{Path: "/scan/root/short.bin", Size: 50, Selected: true},
	}}
	m := newModelWithFindings(f)
	m.width, m.height = 50, 24
	out := m.View()
	if strings.Contains(out, longPath) {
		t.Errorf("path should be truncated on a 50-col terminal:\n%s", out)
	}
	if !strings.Contains(out, "…") {
		t.Errorf("expected leading … truncation marker:\n%s", out)
	}
	if !strings.Contains(out, "name.bin") {
		t.Errorf("filename tail should survive truncation:\n%s", out)
	}
}

func TestView_HeaderShowsTotalAlways(t *testing.T) {
	const mib = 1024 * 1024
	f := Finding{Source: SourceDuplicate, Label: "abcd1234", Members: []Member{
		{Path: "/a", Size: 5 * mib},
		{Path: "/b", Size: 5 * mib},
		{Path: "/c", Size: 5 * mib},
	}} // nothing selected
	m := newModelWithFindings(f)
	m.width, m.height = 100, 24
	out := m.View()
	header := lineContaining(t, out, "abcd1234")
	if !strings.Contains(header, "15.0 MiB") {
		t.Errorf("header should show total size (15.0 MiB) even with nothing selected:\n%s", header)
	}
	if strings.Contains(header, "↓") {
		t.Errorf("no reclaim arrow expected in header when nothing is selected:\n%s", header)
	}
}

// lineContaining returns the single output line that contains sub, failing if
// none does. Used to assert on a specific row rather than the whole frame
// (which also includes the "↑/↓ move" help line).
func lineContaining(t *testing.T, out, sub string) string {
	t.Helper()
	for _, ln := range strings.Split(out, "\n") {
		if strings.Contains(ln, sub) {
			return ln
		}
	}
	t.Fatalf("no line containing %q in:\n%s", sub, out)
	return ""
}

func TestView_HeaderShowsReclaimWhenSelected(t *testing.T) {
	const mib = 1024 * 1024
	f := Finding{Source: SourceDuplicate, Label: "abcd1234", Members: []Member{
		{Path: "/a", Size: 5 * mib},
		{Path: "/b", Size: 5 * mib, Selected: true},
		{Path: "/c", Size: 5 * mib, Selected: true},
	}} // reclaim = 10 MiB
	m := newModelWithFindings(f)
	m.width, m.height = 100, 24
	out := m.View()
	if !strings.Contains(out, "15.0 MiB") {
		t.Errorf("header should still show total (15.0 MiB):\n%s", out)
	}
	if !strings.Contains(out, "↓10.0 MiB") {
		t.Errorf("header should show ↓reclaim (↓10.0 MiB) when members selected:\n%s", out)
	}
}

func TestView_ConfirmModalRendered(t *testing.T) {
	m := newModelWithFindings(sampleFindings()...)
	m.mode = modeConfirm
	m.pending = actionDelete
	out := m.View()
	// "[y] yes" is unique to the confirm modal and not in the help line, so
	// this fails if the modal is not rendered.
	if !strings.Contains(out, "[y] yes") {
		t.Errorf("expected confirm-modal-specific text in output:\n%s", out)
	}
}

func TestView_MovePromptRendered(t *testing.T) {
	m := newModelWithFindings(sampleFindings()...)
	m.mode = modeMovePrompt
	out := m.View()
	// "Esc to cancel" is unique to the move prompt; the help line says "q quit".
	if !strings.Contains(out, "Esc to cancel") {
		t.Errorf("expected move-prompt-specific text in output:\n%s", out)
	}
}
