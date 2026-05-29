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
