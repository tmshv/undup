package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/alecthomas/kong"

	"github.com/tmshv/undup/internal/scan"
)

var cli struct {
	Root    string `arg:"" type:"existingdir" help:"Root directory to scan."`
	Workers int    `short:"j" default:"1" help:"Number of parallel walker goroutines (must be >= 1)."`
}

func main() {
	kong.Parse(&cli,
		kong.Name("undup"),
		kong.Description("Scan a directory tree for in-place unpacked archives."),
		kong.UsageOnError(),
	)
	if cli.Workers < 1 {
		fmt.Fprintln(os.Stderr, "undup: -j/--workers must be >= 1")
		os.Exit(1)
	}

	entries := scan.Walk(cli.Root, cli.Workers)
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
