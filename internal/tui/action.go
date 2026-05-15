package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Action knows how to apply itself to one Member.
type Action interface {
	Apply(m Member, scanRoot string) error
}

// DeleteAction unlinks the member from disk. Directories are removed
// recursively. Selecting a directory member only happens for archive findings
// where the user has opted in via the recursive-warning confirmation.
type DeleteAction struct{}

func (DeleteAction) Apply(m Member, scanRoot string) error {
	if m.IsDir {
		return os.RemoveAll(m.Path)
	}
	return os.Remove(m.Path)
}

// ValidateMoveTarget rejects targets equal to or inside scanRoot. Both paths
// are compared after filepath.Abs to handle relative inputs.
func ValidateMoveTarget(target, scanRoot string) error {
	tAbs, err := filepath.Abs(target)
	if err != nil {
		return fmt.Errorf("resolve target: %w", err)
	}
	rAbs, err := filepath.Abs(scanRoot)
	if err != nil {
		return fmt.Errorf("resolve scan root: %w", err)
	}
	rel, err := filepath.Rel(rAbs, tAbs)
	if err != nil {
		return nil // unrelated trees
	}
	if rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))) {
		return fmt.Errorf("move target must be outside scan root")
	}
	return nil
}
