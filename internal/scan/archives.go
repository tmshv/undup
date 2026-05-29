package scan

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ArchiveFinding reports a directory that appears to be an in-place unpacked
// copy of a sibling archive file (matched by name only, not by content).
type ArchiveFinding struct {
	ArchivePath string
	DirPath     string
	Size        int64
}

type ArchiveDetector struct {
	// extensions is sorted longest-first so multi-part suffixes
	// like ".tar.gz" win over ".tar".
	extensions []string
}

func NewArchiveDetector(extensions []string) *ArchiveDetector {
	sorted := append([]string(nil), extensions...)
	sort.Slice(sorted, func(i, j int) bool {
		return len(sorted[i]) > len(sorted[j])
	})
	return &ArchiveDetector{extensions: sorted}
}

// Detect consumes entries and emits an ArchiveFinding for each file whose
// path equals <directory>+<archive-extension> for some sibling directory
// that exists on disk. Detection is per-entry and stateless: the candidate
// directory path is derived by stripping the matching extension from the
// file path and verified with os.Lstat. Order-independent — safe to drive
// from a concurrent walker. Per-entry walk errors are reported to stderr
// and skipped.
func (d *ArchiveDetector) Detect(entries <-chan Entry) <-chan ArchiveFinding {
	out := make(chan ArchiveFinding)
	go func() {
		defer close(out)
		for e := range entries {
			if e.Err != nil {
				fmt.Fprintf(os.Stderr, "error scanning %s: %v\n", e.Path, e.Err)
				continue
			}
			// Only regular files are eligible. Symlinks would be silently
			// converted to regular copies by the cross-device move
			// fallback (os.Open follows them); FIFOs/devices/sockets can
			// fail or block on open. Skipping them at the detector keeps
			// the action layer free of mode checks and matches the
			// duplicate detector's filtering.
			if e.Info == nil || !e.Info.Mode().IsRegular() {
				continue
			}
			for _, ext := range d.extensions {
				if !strings.HasSuffix(e.Path, ext) {
					continue
				}
				// Skip files where stripping the extension would leave
				// a basename of "", ".", or ".." — e.g. ".zip", "..zip",
				// "...zip". Those candidates resolve via os.Lstat to the
				// parent directory (or even outside the scanned root)
				// and produce false positives.
				trimmedBase := strings.TrimSuffix(filepath.Base(e.Path), ext)
				if trimmedBase == "" || trimmedBase == "." || trimmedBase == ".." {
					break
				}
				candidate := strings.TrimSuffix(e.Path, ext)
				info, err := os.Lstat(candidate)
				if err == nil && info.IsDir() {
					out <- ArchiveFinding{
						ArchivePath: e.Path,
						DirPath:     candidate,
						Size:        e.Info.Size(),
					}
				}
				break // longest match wins; don't test shorter extensions
			}
		}
	}()
	return out
}
