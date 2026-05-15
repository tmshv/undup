package tui

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
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

// MoveAction relocates the member into Target preserving its path relative to
// scanRoot. Cross-device renames fall back to copy-then-remove.
type MoveAction struct{ Target string }

func (a MoveAction) Apply(m Member, scanRoot string) error {
	rel, err := filepath.Rel(scanRoot, m.Path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		rel = filepath.Base(m.Path)
	}
	dest := filepath.Join(a.Target, rel)
	// Refuse to overwrite: both os.Rename (on a non-dir) and the copy fallback
	// (O_TRUNC) would silently clobber unrelated content at the destination.
	if _, err := os.Lstat(dest); err == nil {
		return fmt.Errorf("destination already exists: %s", dest)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat destination: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return fmt.Errorf("mkdir parent: %w", err)
	}
	if err := os.Rename(m.Path, dest); err == nil {
		return nil
	} else if !isCrossDevice(err) {
		return err
	}
	if err := copyFile(m.Path, dest); err != nil {
		return err
	}
	return os.Remove(m.Path)
}

func isCrossDevice(err error) bool {
	var linkErr *os.LinkError
	if errors.As(err, &linkErr) {
		return errors.Is(linkErr.Err, syscall.EXDEV)
	}
	return errors.Is(err, syscall.EXDEV)
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

type ApplyResult struct {
	Ok        int
	Failed    int
	Succeeded map[string]bool  // by member.Path (as displayed)
	Errors    map[string]error // first error per failed path
}

// ApplyAction iterates every selected member across findings, deduplicating
// by absolute path so the same file is never acted on twice. Errors per
// member are collected; the action layer never aborts mid-way.
func ApplyAction(action Action, scanRoot string, findings []Finding) ApplyResult {
	res := ApplyResult{
		Succeeded: make(map[string]bool),
		Errors:    make(map[string]error),
	}
	seen := make(map[string]bool)
	for _, f := range findings {
		for _, m := range f.Members {
			if !m.Selected || !m.Selectable() {
				continue
			}
			abs, err := filepath.Abs(m.Path)
			if err != nil {
				res.Failed++
				res.Errors[m.Path] = err
				continue
			}
			if seen[abs] {
				continue
			}
			seen[abs] = true
			if err := action.Apply(m, scanRoot); err != nil {
				res.Failed++
				res.Errors[m.Path] = err
				continue
			}
			res.Ok++
			res.Succeeded[m.Path] = true
		}
	}
	return res
}
