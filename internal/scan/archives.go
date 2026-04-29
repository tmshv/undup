package scan

import "fmt"

type ArchiveDetector struct {
	extensions []string
}

func NewArchiveDetector(extensions []string) *ArchiveDetector {
	return &ArchiveDetector{extensions: extensions}
}

// Detect consumes entries until the channel is closed. For each directory,
// it registers candidate archive paths (dir + ext for each known extension);
// when a file matching a candidate is later visited, it prints
// "Unpacked archive <name> (<dir>)". Per-entry walk errors are reported and
// skipped.
func (d *ArchiveDetector) Detect(entries <-chan Entry) {
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
			fmt.Printf("Unpacked archive %s (%s)\n", e.Info.Name(), dir)
		}
	}
}
