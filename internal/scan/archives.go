package scan

import "fmt"

// ArchiveFinding reports a directory that appears to be an in-place unpacked
// copy of a sibling archive file (matched by name only, not by content).
type ArchiveFinding struct {
	ArchivePath string
	DirPath     string
	Size        int64
}

type ArchiveDetector struct {
	extensions []string
}

func NewArchiveDetector(extensions []string) *ArchiveDetector {
	return &ArchiveDetector{extensions: extensions}
}

// Detect consumes entries and emits an ArchiveFinding for each file whose
// path matches a previously-seen sibling directory plus a known archive
// extension. The returned channel is closed when entries is closed.
// Per-entry walk errors are reported to stdout and skipped.
func (d *ArchiveDetector) Detect(entries <-chan Entry) <-chan ArchiveFinding {
	out := make(chan ArchiveFinding)
	go func() {
		defer close(out)
		candidates := map[string]string{}
		for e := range entries {
			if e.Err != nil {
				fmt.Printf("error scanning %s: %v\n", e.Path, e.Err)
				continue
			}
			if e.Info.IsDir() {
				for _, ext := range d.extensions {
					candidates[e.Path+ext] = e.Path
				}
				continue
			}
			if dir, ok := candidates[e.Path]; ok {
				out <- ArchiveFinding{ArchivePath: e.Path, DirPath: dir, Size: e.Info.Size()}
			}
		}
	}()
	return out
}
