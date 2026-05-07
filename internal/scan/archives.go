package scan

import (
	"fmt"
	"os"
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
// from a concurrent walker. Per-entry walk errors are reported to stdout
// and skipped.
func (d *ArchiveDetector) Detect(entries <-chan Entry) <-chan ArchiveFinding {
	out := make(chan ArchiveFinding)
	go func() {
		defer close(out)
		for e := range entries {
			if e.Err != nil {
				fmt.Printf("error scanning %s: %v\n", e.Path, e.Err)
				continue
			}
			if e.Info.IsDir() {
				continue
			}
			for _, ext := range d.extensions {
				if !strings.HasSuffix(e.Path, ext) {
					continue
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
