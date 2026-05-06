package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/tmshv/undup/internal/scan"
)

func main() {
	rootDir := os.Args[1]
	entries := scan.Walk(rootDir)
	detector := scan.NewArchiveDetector(scan.Extensions)
	for f := range detector.Detect(entries) {
		fmt.Printf("Unpacked archive %s [%s] (%s)\n", filepath.Base(f.ArchivePath), humanSize(f.Size), f.DirPath)
	}
}

func humanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for x := n / unit; x >= unit; x /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}
