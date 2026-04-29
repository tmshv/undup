package main

import (
	"os"

	"github.com/tmshv/fsdup/internal/scan"
)

func main() {
	rootDir := os.Args[1]
	entries := scan.Walk(rootDir)
	detector := scan.NewArchiveDetector(scan.Extensions)
	detector.Detect(entries)
}
