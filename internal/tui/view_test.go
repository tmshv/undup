package tui

import (
	"strings"
	"testing"
)

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
	if !strings.Contains(strings.ToLower(out), "delete") {
		t.Errorf("expected 'delete' in confirm modal:\n%s", out)
	}
}

func TestView_MovePromptRendered(t *testing.T) {
	m := newModelWithFindings(sampleFindings()...)
	m.mode = modeMovePrompt
	out := m.View()
	if !strings.Contains(strings.ToLower(out), "move") {
		t.Errorf("expected 'move' in prompt:\n%s", out)
	}
}
