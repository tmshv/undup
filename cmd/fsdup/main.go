package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/tmshv/fsdup/internal/scan"
)

func main() {
	rootDir := os.Args[1]
	entries := scan.Walk(rootDir)
	detector := scan.NewArchiveDetector(scan.Extensions)
	for f := range detector.Detect(entries) {
		fmt.Printf("Unpacked archive %s (%s)\n", filepath.Base(f.ArchivePath), f.DirPath)
	}
}
