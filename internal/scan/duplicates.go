package scan

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"sync"
)

type DuplicateGroup struct {
	SHA256 [32]byte
	Size   int64
	Paths  []string
}

type DuplicateDetector struct {
	workers    int
	prefixSize int
	minSize    int64
}

func NewDuplicateDetector(workers, prefixSize int, minSize int64) *DuplicateDetector {
	if workers < 1 {
		workers = 1
	}
	if prefixSize < 1 {
		prefixSize = 4096
	}
	if minSize < 0 {
		minSize = 0
	}
	return &DuplicateDetector{workers: workers, prefixSize: prefixSize, minSize: minSize}
}

type sizedPath struct {
	path string
	size int64
}

type fullResult struct {
	hash [32]byte
	size int64
	path string
	ok   bool
}

func (d *DuplicateDetector) Detect(entries <-chan Entry) <-chan DuplicateGroup {
	out := make(chan DuplicateGroup)
	go func() {
		defer close(out)

		// Phase 1 — drain walk and bucket by size.
		sizeMap := make(map[int64][]string)
		for e := range entries {
			if e.Err != nil {
				fmt.Fprintf(os.Stderr, "error scanning %s: %v\n", e.Path, e.Err)
				continue
			}
			if e.Info == nil || !e.Info.Mode().IsRegular() {
				continue
			}
			size := e.Info.Size()
			if size < d.minSize {
				continue
			}
			sizeMap[size] = append(sizeMap[size], e.Path)
		}

		// Phase 2 — full SHA256 across size-collision groups.
		groups := d.hashCandidates(sizeMap)

		for hash, g := range groups {
			if len(g.paths) < 2 {
				continue
			}
			out <- DuplicateGroup{SHA256: hash, Size: g.size, Paths: g.paths}
		}
	}()
	return out
}

type fullGroup struct {
	size  int64
	paths []string
}

func (d *DuplicateDetector) hashCandidates(sizeMap map[int64][]string) map[[32]byte]*fullGroup {
	jobs := make(chan sizedPath)
	results := make(chan fullResult)

	var wg sync.WaitGroup
	for i := 0; i < d.workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobs {
				results <- hashFull(job)
			}
		}()
	}

	go func() {
		for size, paths := range sizeMap {
			if len(paths) < 2 {
				continue
			}
			for _, p := range paths {
				jobs <- sizedPath{path: p, size: size}
			}
		}
		close(jobs)
	}()

	go func() {
		wg.Wait()
		close(results)
	}()

	groups := make(map[[32]byte]*fullGroup)
	for r := range results {
		if !r.ok {
			continue
		}
		g, ok := groups[r.hash]
		if !ok {
			g = &fullGroup{size: r.size}
			groups[r.hash] = g
		}
		g.paths = append(g.paths, r.path)
	}
	return groups
}

func hashFull(job sizedPath) fullResult {
	f, err := os.Open(job.path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error opening %s: %v\n", job.path, err)
		return fullResult{path: job.path}
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		fmt.Fprintf(os.Stderr, "error hashing %s: %v\n", job.path, err)
		return fullResult{path: job.path}
	}
	var sum [32]byte
	copy(sum[:], h.Sum(nil))
	return fullResult{hash: sum, size: job.size, path: job.path, ok: true}
}
