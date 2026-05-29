package tui

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
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
	// Cross-device fallback: copy then delete. Directories require a
	// recursive walk; the file-only path predates archive-directory members
	// being movable.
	//
	// Failure handling is asymmetric on purpose: if the copy fails the
	// source is still fully intact, so we wipe the partial destination so a
	// retry isn't blocked by the destination-exists guard. But once we
	// start removing the source we must NOT touch the destination on
	// failure — os.RemoveAll is not atomic (it walks the tree removing
	// entries one by one), so a mid-walk error leaves the source half-gone,
	// and os.Remove on a single file can also race with concurrent
	// deletion. In both cases the destination may hold the only surviving
	// copy of bytes already removed from the source, so deleting it would
	// turn a recoverable cleanup error into permanent data loss. Leave
	// dest in place and let the user resolve the half-moved state.
	if m.IsDir {
		if err := copyDir(m.Path, dest); err != nil {
			_ = os.RemoveAll(dest)
			return err
		}
		if err := os.RemoveAll(m.Path); err != nil {
			return fmt.Errorf("copy to %s succeeded but source cleanup failed: %w", dest, err)
		}
		return nil
	}
	if err := copyFile(m.Path, dest); err != nil {
		return err
	}
	if err := os.Remove(m.Path); err != nil {
		return fmt.Errorf("copy to %s succeeded but source cleanup failed: %w", dest, err)
	}
	return nil
}

func isCrossDevice(err error) bool {
	var linkErr *os.LinkError
	if errors.As(err, &linkErr) {
		return errors.Is(linkErr.Err, syscall.EXDEV)
	}
	return errors.Is(err, syscall.EXDEV)
}

// copyDir recursively copies a directory tree from src to dst. Regular files
// are copied; symlinks are recreated (tar archives commonly contain them, so
// silently dropping them on a cross-device move would lose data). Other
// non-regular entries (devices, sockets, named pipes) fail loudly rather
// than getting deleted with the source.
func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			info, err := d.Info()
			if err != nil {
				return err
			}
			return os.MkdirAll(target, info.Mode().Perm())
		}
		if d.Type()&fs.ModeSymlink != 0 {
			link, err := os.Readlink(p)
			if err != nil {
				return fmt.Errorf("readlink %s: %w", p, err)
			}
			return os.Symlink(link, target)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("unsupported non-regular file %s (mode %v)", p, info.Mode())
		}
		return copyFile(p, target)
	})
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
		// Remove the partial destination so the next attempt isn't blocked by
		// the destination-exists guard in MoveAction.Apply.
		os.Remove(dst)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(dst)
		return err
	}
	return nil
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
