package main

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"

	"github.com/alecthomas/kong"

	"github.com/tmshv/undup/internal/scan"
)

var cli struct {
	Root    string `arg:"" type:"existingdir" help:"Root directory to scan."`
	Workers int    `short:"j" default:"1" help:"Number of parallel walker / hash workers (must be >= 1)."`
	Mode    string `short:"m" default:"archives" enum:"archives,duplicates,all" help:"Detector to run: archives, duplicates, or all."`
}

func main() {
	kong.Parse(&cli,
		kong.Name("undup"),
		kong.Description("Scan a directory tree for in-place unpacked archives and/or duplicate files."),
		kong.UsageOnError(),
	)
	if cli.Workers < 1 {
		fmt.Fprintln(os.Stderr, "undup: -j/--workers must be >= 1")
		os.Exit(1)
	}

	switch cli.Mode {
	case "archives":
		runArchives(cli.Root, cli.Workers)
	case "duplicates":
		runDuplicates(cli.Root, cli.Workers)
	case "all":
		runAll(cli.Root, cli.Workers)
	}
}

func runArchives(root string, workers int) {
	entries := scan.Walk(root, workers)
	detector := scan.NewArchiveDetector(scan.Extensions)
	for f := range detector.Detect(entries) {
		fmt.Printf("Unpacked archive %s [%s] (%s)\n", filepath.Base(f.ArchivePath), humanSize(f.Size), f.DirPath)
	}
}

func runDuplicates(root string, workers int) {
	entries := scan.Walk(root, workers)
	detector := scan.NewDuplicateDetector(workers, 4096, 1)

	var groups []scan.DuplicateGroup
	for g := range detector.Detect(entries) {
		groups = append(groups, g)
	}
	printDuplicates(groups)
}

func printDuplicates(groups []scan.DuplicateGroup) {
	if len(groups) == 0 {
		return
	}
	var maxSize int64
	for _, g := range groups {
		if g.Size > maxSize {
			maxSize = g.Size
		}
	}
	width := len(strconv.FormatInt(maxSize, 10))
	for _, g := range groups {
		short := hex.EncodeToString(g.SHA256[:4])
		for _, p := range g.Paths {
			fmt.Printf("%s  %*d  %s\n", short, width, g.Size, p)
		}
	}
}

func runAll(root string, workers int) {
	entries := scan.Walk(root, workers)
	archChan, dupChan := tee(entries)

	archDetector := scan.NewArchiveDetector(scan.Extensions)
	dupDetector := scan.NewDuplicateDetector(workers, 4096, 1)

	var stdoutMu sync.Mutex
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		for f := range archDetector.Detect(archChan) {
			stdoutMu.Lock()
			fmt.Printf("archive: %s [%s] (%s)\n", filepath.Base(f.ArchivePath), humanSize(f.Size), f.DirPath)
			stdoutMu.Unlock()
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		var groups []scan.DuplicateGroup
		for g := range dupDetector.Detect(dupChan) {
			groups = append(groups, g)
		}
		if len(groups) == 0 {
			return
		}
		var maxSize int64
		for _, g := range groups {
			if g.Size > maxSize {
				maxSize = g.Size
			}
		}
		width := len(strconv.FormatInt(maxSize, 10))
		stdoutMu.Lock()
		defer stdoutMu.Unlock()
		for _, g := range groups {
			short := hex.EncodeToString(g.SHA256[:4])
			for _, p := range g.Paths {
				fmt.Printf("dupe: %s  %*d  %s\n", short, width, g.Size, p)
			}
		}
	}()

	wg.Wait()
}

func tee(in <-chan scan.Entry) (<-chan scan.Entry, <-chan scan.Entry) {
	a := make(chan scan.Entry)
	b := make(chan scan.Entry)
	go func() {
		defer close(a)
		defer close(b)
		for e := range in {
			// Log walk errors once here so the two downstream detectors
			// don't each print the same "error scanning ..." line.
			if e.Err != nil {
				fmt.Fprintf(os.Stderr, "error scanning %s: %v\n", e.Path, e.Err)
				continue
			}
			a <- e
			b <- e
		}
	}()
	return a, b
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
