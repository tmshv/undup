# SHA256 Duplicate-File Detector Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:subagent-driven-development` (recommended) or `superpowers:executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a content-hash detector that finds duplicate files anywhere under the scanned root, coexisting with the existing name-based archive detector via a `--mode` CLI flag.

**Architecture:** Stateful detector that drains the walk channel into a `map[size][]path` (Phase 1), then runs a two-pass hash pipeline — 4 KiB prefix hash, then full SHA256 — through a worker pool sized by the existing `-j` flag (Phase 2). Each pass discards groups that shrink below 2 members. `--mode all` tees the walk channel to feed both detectors from a single traversal.

**Tech Stack:** Go stdlib only (`crypto/sha256`, `io`, `os`, `sync`). No new third-party deps.

**Spec:** `docs/specs/20260513-sha256-duplicate-detector.md`

---

## File structure

```
internal/scan/duplicates.go       # NEW — DuplicateDetector + DuplicateGroup
internal/scan/duplicates_test.go  # NEW — table-driven tests
cmd/undup/main.go                 # MODIFIED — add --mode flag, dispatch, tee helper
```

`duplicates.go` owns everything detector-side; `main.go` owns CLI wiring and output formatting. No changes to `scan.go`, `archives.go`, or `extensions.go`.

---

### Task 1: Detector types and empty-input skeleton

**Files:**
- Create: `internal/scan/duplicates.go`
- Create: `internal/scan/duplicates_test.go`

- [x] **Step 1 — Write the failing test** for an empty entries channel closing the output channel.

  Add `internal/scan/duplicates_test.go`:

  ```go
  package scan

  import (
  	"testing"
  	"time"
  )

  func TestDuplicateDetectorClosesChannelWhenEntriesClose(t *testing.T) {
  	in := make(chan Entry)
  	close(in)
  	d := NewDuplicateDetector(1, 4096, 1)
  	out := d.Detect(in)
  	select {
  	case _, ok := <-out:
  		if ok {
  			t.Fatal("Detect emitted a group from an empty input")
  		}
  	case <-time.After(time.Second):
  		t.Fatal("Detect did not close output channel within 1s of input close")
  	}
  }
  ```

- [x] **Step 2 — Run the test, confirm it fails to compile.**

  Run: `go test ./internal/scan -run TestDuplicateDetectorClosesChannelWhenEntriesClose`
  Expected: build error — `undefined: NewDuplicateDetector`.

- [x] **Step 3 — Write the minimal implementation.**

  Create `internal/scan/duplicates.go`:

  ```go
  package scan

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

  func (d *DuplicateDetector) Detect(entries <-chan Entry) <-chan DuplicateGroup {
  	out := make(chan DuplicateGroup)
  	go func() {
  		defer close(out)
  		for range entries {
  		}
  	}()
  	return out
  }
  ```

- [x] **Step 4 — Run the test, confirm it passes.**

  Run: `go test ./internal/scan -run TestDuplicateDetectorClosesChannelWhenEntriesClose -v`
  Expected: PASS.

- [x] **Step 5 — Commit.**

  ```sh
  git add internal/scan/duplicates.go internal/scan/duplicates_test.go
  git commit -m "feat: scaffold DuplicateDetector with closing-channel test"
  ```

---

### Task 2: Basic duplicate detection (size grouping + full SHA256)

**Files:**
- Modify: `internal/scan/duplicates.go`
- Modify: `internal/scan/duplicates_test.go`

- [ ] **Step 1 — Add a test helper and the first behavioral test.**

  Append to `internal/scan/duplicates_test.go`:

  ```go
  import (
  	"path/filepath"
  	"sort"
  )

  func collectGroups(ch <-chan DuplicateGroup) [][]string {
  	var out [][]string
  	for g := range ch {
  		paths := append([]string(nil), g.Paths...)
  		sort.Strings(paths)
  		out = append(out, paths)
  	}
  	sort.Slice(out, func(i, j int) bool { return out[i][0] < out[j][0] })
  	return out
  }

  func TestDuplicateDetectorBasic(t *testing.T) {
  	tests := []struct {
  		name  string
  		setup func(t *testing.T, root string)
  		want  [][]string // sorted paths per group, sorted by first path; relative to root
  	}{
  		{
  			name: "two identical files",
  			setup: func(t *testing.T, root string) {
  				mustWrite(t, filepath.Join(root, "a.bin"), []byte("hello world"))
  				mustWrite(t, filepath.Join(root, "b.bin"), []byte("hello world"))
  			},
  			want: [][]string{{"a.bin", "b.bin"}},
  		},
  		{
  			name: "same size, different content",
  			setup: func(t *testing.T, root string) {
  				mustWrite(t, filepath.Join(root, "a.bin"), []byte("hello world"))
  				mustWrite(t, filepath.Join(root, "b.bin"), []byte("HELLO WORLD"))
  			},
  			want: nil,
  		},
  		{
  			name: "different sizes",
  			setup: func(t *testing.T, root string) {
  				mustWrite(t, filepath.Join(root, "a.bin"), []byte("short"))
  				mustWrite(t, filepath.Join(root, "b.bin"), []byte("longer content"))
  			},
  			want: nil,
  		},
  		{
  			name: "three files, two identical plus one same-size near-miss",
  			setup: func(t *testing.T, root string) {
  				mustWrite(t, filepath.Join(root, "a.bin"), []byte("hello world"))
  				mustWrite(t, filepath.Join(root, "b.bin"), []byte("hello world"))
  				mustWrite(t, filepath.Join(root, "c.bin"), []byte("HELLO WORLD"))
  			},
  			want: [][]string{{"a.bin", "b.bin"}},
  		},
  	}

  	for _, tc := range tests {
  		t.Run(tc.name, func(t *testing.T) {
  			root := t.TempDir()
  			tc.setup(t, root)

  			d := NewDuplicateDetector(2, 4096, 1)
  			got := collectGroups(d.Detect(Walk(root, 1)))

  			wantAbs := absolutize(root, tc.want)
  			if !equalGroupings(got, wantAbs) {
  				t.Fatalf("got %v, want %v", got, wantAbs)
  			}
  		})
  	}
  }

  func absolutize(root string, groups [][]string) [][]string {
  	if groups == nil {
  		return nil
  	}
  	out := make([][]string, len(groups))
  	for i, g := range groups {
  		out[i] = make([]string, len(g))
  		for j, p := range g {
  			out[i][j] = filepath.Join(root, p)
  		}
  	}
  	return out
  }

  func equalGroupings(a, b [][]string) bool {
  	if len(a) != len(b) {
  		return false
  	}
  	for i := range a {
  		if len(a[i]) != len(b[i]) {
  			return false
  		}
  		for j := range a[i] {
  			if a[i][j] != b[i][j] {
  				return false
  			}
  		}
  	}
  	return true
  }
  ```

- [ ] **Step 2 — Run the new test, confirm it fails.**

  Run: `go test ./internal/scan -run TestDuplicateDetectorBasic`
  Expected: failures — every subtest reports `got [] want ...` or `got ... want []` because `Detect` still does nothing.

- [ ] **Step 3 — Implement Phase 1 (drain) and Phase 2 (full-hash worker pool).**

  Replace the `Detect` method in `internal/scan/duplicates.go`:

  ```go
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
  ```

- [ ] **Step 4 — Run all `scan` tests, confirm they pass.**

  Run: `go test ./internal/scan -v`
  Expected: all subtests of `TestDuplicateDetectorBasic` PASS plus the existing archive tests still PASS.

- [ ] **Step 5 — Commit.**

  ```sh
  git add internal/scan/duplicates.go internal/scan/duplicates_test.go
  git commit -m "feat: implement size-bucket + full SHA256 duplicate detection"
  ```

---

### Task 3: Skip empty files and symlinks

**Files:**
- Modify: `internal/scan/duplicates_test.go`

- [ ] **Step 1 — Write the failing tests.**

  Append to `duplicates_test.go`:

  ```go
  func TestDuplicateDetectorIgnoresEmptyFiles(t *testing.T) {
  	root := t.TempDir()
  	mustWrite(t, filepath.Join(root, "a.empty"), nil)
  	mustWrite(t, filepath.Join(root, "b.empty"), nil)

  	d := NewDuplicateDetector(2, 4096, 1)
  	got := collectGroups(d.Detect(Walk(root, 1)))
  	if len(got) != 0 {
  		t.Fatalf("expected no groups for empty files, got %v", got)
  	}
  }

  func TestDuplicateDetectorIgnoresSymlinks(t *testing.T) {
  	root := t.TempDir()
  	mustWrite(t, filepath.Join(root, "a.bin"), []byte("payload"))
  	mustWrite(t, filepath.Join(root, "b.bin"), []byte("payload"))
  	if err := os.Symlink(filepath.Join(root, "a.bin"), filepath.Join(root, "link.bin")); err != nil {
  		t.Skipf("symlinks unsupported on this platform: %v", err)
  	}

  	d := NewDuplicateDetector(2, 4096, 1)
  	got := collectGroups(d.Detect(Walk(root, 1)))

  	want := [][]string{{filepath.Join(root, "a.bin"), filepath.Join(root, "b.bin")}}
  	if !equalGroupings(got, want) {
  		t.Fatalf("got %v, want %v", got, want)
  	}
  }
  ```

  These imports may need to be added — `os` is already used in `archives_test.go` via `mustMkdir`, so it should be present via that file's `import "os"`. If not, add `os` to the test file's imports.

- [ ] **Step 2 — Run the tests, confirm behavior.**

  Run: `go test ./internal/scan -run "TestDuplicateDetectorIgnoresEmptyFiles|TestDuplicateDetectorIgnoresSymlinks" -v`
  Expected: both PASS — the existing Phase 1 filter (`IsRegular()` excludes symlinks; `size < minSize` excludes empties since default `minSize=1`) already covers these cases. This task is a regression lock.

  If either fails, debug the Phase 1 filter — do **not** modify the test fixtures. The detector's behavior must match the spec's edge-case policy.

- [ ] **Step 3 — Commit.**

  ```sh
  git add internal/scan/duplicates_test.go
  git commit -m "test: lock empty-file and symlink skip behavior for DuplicateDetector"
  ```

---

### Task 4: Tolerate read errors on individual files

**Files:**
- Modify: `internal/scan/duplicates_test.go`

- [ ] **Step 1 — Write the failing test.**

  Append to `duplicates_test.go`:

  ```go
  func TestDuplicateDetectorTolerantOfUnreadableFile(t *testing.T) {
  	if os.Geteuid() == 0 {
  		t.Skip("running as root bypasses permission checks")
  	}
  	root := t.TempDir()
  	mustWrite(t, filepath.Join(root, "a.bin"), []byte("payload"))
  	mustWrite(t, filepath.Join(root, "b.bin"), []byte("payload"))
  	unreadable := filepath.Join(root, "c.bin")
  	mustWrite(t, unreadable, []byte("payload"))
  	if err := os.Chmod(unreadable, 0); err != nil {
  		t.Fatalf("chmod: %v", err)
  	}
  	t.Cleanup(func() { _ = os.Chmod(unreadable, 0o644) })

  	d := NewDuplicateDetector(2, 4096, 1)
  	got := collectGroups(d.Detect(Walk(root, 1)))

  	// The unreadable file is dropped; the remaining two are still detected.
  	want := [][]string{{filepath.Join(root, "a.bin"), filepath.Join(root, "b.bin")}}
  	if !equalGroupings(got, want) {
  		t.Fatalf("got %v, want %v", got, want)
  	}
  }
  ```

- [ ] **Step 2 — Run the test, confirm it passes.**

  Run: `go test ./internal/scan -run TestDuplicateDetectorTolerantOfUnreadableFile -v`
  Expected: PASS. The existing `hashFull` error path drops failing paths with `ok: false` and a stderr message; the aggregator skips `!r.ok` entries. Two readable copies survive and form a group.

  If it fails because `Walk` itself errors on the chmod-0 file before hashing, double-check that the test runs on macOS/Linux (the platform under development) — `filepath.Walk` reports `os.Lstat` results, which work on a file the running user owns regardless of mode.

- [ ] **Step 3 — Commit.**

  ```sh
  git add internal/scan/duplicates_test.go
  git commit -m "test: lock read-error tolerance for DuplicateDetector"
  ```

---

### Task 5: Insert the 4 KiB prefix-hash pre-pass

**Files:**
- Modify: `internal/scan/duplicates.go`
- Modify: `internal/scan/duplicates_test.go`

- [ ] **Step 1 — Write a test that exercises the prefix-pass shortcut.**

  Append to `duplicates_test.go`:

  ```go
  import "bytes" // add if not already imported

  func TestDuplicateDetectorPrefixDifferentialAvoidsFullHash(t *testing.T) {
  	root := t.TempDir()
  	// Two same-size files whose first byte differs — the prefix pass alone
  	// should suffice to rule out a match.
  	a := bytes.Repeat([]byte{'A'}, 8192)
  	b := bytes.Repeat([]byte{'B'}, 8192)
  	mustWrite(t, filepath.Join(root, "a.bin"), a)
  	mustWrite(t, filepath.Join(root, "b.bin"), b)

  	d := NewDuplicateDetector(2, 4096, 1)
  	got := collectGroups(d.Detect(Walk(root, 1)))
  	if len(got) != 0 {
  		t.Fatalf("expected no groups, got %v", got)
  	}
  }

  func TestDuplicateDetectorPrefixMatchTailDivergesGivesNoGroup(t *testing.T) {
  	root := t.TempDir()
  	// Identical first 4 KiB, divergent tail — the prefix pass passes them
  	// through; the full-hash pass discards them.
  	prefix := bytes.Repeat([]byte{'P'}, 4096)
  	a := append(append([]byte(nil), prefix...), bytes.Repeat([]byte{'A'}, 1024)...)
  	b := append(append([]byte(nil), prefix...), bytes.Repeat([]byte{'B'}, 1024)...)
  	mustWrite(t, filepath.Join(root, "a.bin"), a)
  	mustWrite(t, filepath.Join(root, "b.bin"), b)

  	d := NewDuplicateDetector(2, 4096, 1)
  	got := collectGroups(d.Detect(Walk(root, 1)))
  	if len(got) != 0 {
  		t.Fatalf("expected no groups, got %v", got)
  	}
  }
  ```

- [ ] **Step 2 — Run the tests; they pass against the current (non-optimized) code but lock behavior for the upcoming refactor.**

  Run: `go test ./internal/scan -run TestDuplicateDetectorPrefix -v`
  Expected: both PASS. The full-hash pass alone correctly excludes both cases.

- [ ] **Step 3 — Refactor `Detect` to add the prefix pre-pass.**

  Replace the body of `Detect` and add the prefix helpers in `internal/scan/duplicates.go`. The full file becomes:

  ```go
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

  type prefixKey struct {
  	size   int64
  	prefix [32]byte
  }

  type prefixResult struct {
  	key  prefixKey
  	path string
  	ok   bool
  }

  type fullResult struct {
  	hash [32]byte
  	size int64
  	path string
  	ok   bool
  }

  type fullGroup struct {
  	size  int64
  	paths []string
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

  		// Phase 2a — prefix hash to narrow candidates.
  		prefixGroups := d.prefixPass(sizeMap)

  		// Phase 2b — full SHA256 over surviving candidates.
  		groups := d.fullPass(prefixGroups)

  		for hash, g := range groups {
  			if len(g.paths) < 2 {
  				continue
  			}
  			out <- DuplicateGroup{SHA256: hash, Size: g.size, Paths: g.paths}
  		}
  	}()
  	return out
  }

  func (d *DuplicateDetector) prefixPass(sizeMap map[int64][]string) map[prefixKey][]string {
  	jobs := make(chan sizedPath)
  	results := make(chan prefixResult)

  	var wg sync.WaitGroup
  	for i := 0; i < d.workers; i++ {
  		wg.Add(1)
  		go func() {
  			defer wg.Done()
  			buf := make([]byte, d.prefixSize)
  			for job := range jobs {
  				results <- hashPrefix(job, buf)
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

  	groups := make(map[prefixKey][]string)
  	for r := range results {
  		if !r.ok {
  			continue
  		}
  		groups[r.key] = append(groups[r.key], r.path)
  	}
  	return groups
  }

  func (d *DuplicateDetector) fullPass(prefixGroups map[prefixKey][]string) map[[32]byte]*fullGroup {
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
  		for key, paths := range prefixGroups {
  			if len(paths) < 2 {
  				continue
  			}
  			for _, p := range paths {
  				jobs <- sizedPath{path: p, size: key.size}
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

  func hashPrefix(job sizedPath, buf []byte) prefixResult {
  	f, err := os.Open(job.path)
  	if err != nil {
  		fmt.Fprintf(os.Stderr, "error opening %s: %v\n", job.path, err)
  		return prefixResult{path: job.path}
  	}
  	defer f.Close()
  	n, err := io.ReadFull(f, buf)
  	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
  		fmt.Fprintf(os.Stderr, "error reading prefix %s: %v\n", job.path, err)
  		return prefixResult{path: job.path}
  	}
  	sum := sha256.Sum256(buf[:n])
  	return prefixResult{key: prefixKey{size: job.size, prefix: sum}, path: job.path, ok: true}
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
  ```

- [ ] **Step 4 — Run all scan tests.**

  Run: `go test ./internal/scan -v`
  Expected: every test in this file plus `archives_test.go` PASSES.

- [ ] **Step 5 — Commit.**

  ```sh
  git add internal/scan/duplicates.go internal/scan/duplicates_test.go
  git commit -m "perf: add 4 KiB prefix-hash pre-pass to DuplicateDetector"
  ```

---

### Task 6: Worker-count idempotency regression test

**Files:**
- Modify: `internal/scan/duplicates_test.go`

- [ ] **Step 1 — Write the regression test.**

  Append to `duplicates_test.go`:

  ```go
  import "fmt" // add if not already imported

  func TestDuplicateDetectorIdempotentAcrossWorkerCounts(t *testing.T) {
  	root := t.TempDir()
  	// 6 duplicate pairs + 4 unique files.
  	for i := 0; i < 6; i++ {
  		payload := []byte(fmt.Sprintf("payload-%d", i))
  		mustWrite(t, filepath.Join(root, fmt.Sprintf("a-%d.bin", i)), payload)
  		mustWrite(t, filepath.Join(root, fmt.Sprintf("b-%d.bin", i)), payload)
  	}
  	for i := 0; i < 4; i++ {
  		mustWrite(t, filepath.Join(root, fmt.Sprintf("unique-%d.bin", i)), []byte(fmt.Sprintf("unique-%d", i)))
  	}

  	d1 := NewDuplicateDetector(1, 4096, 1)
  	d8 := NewDuplicateDetector(8, 4096, 1)
  	got1 := collectGroups(d1.Detect(Walk(root, 1)))
  	got8 := collectGroups(d8.Detect(Walk(root, 8)))

  	if !equalGroupings(got1, got8) {
  		t.Fatalf("worker count changed groupings:\n  workers=1: %v\n  workers=8: %v", got1, got8)
  	}
  	if len(got1) != 6 {
  		t.Fatalf("expected 6 groups, got %d: %v", len(got1), got1)
  	}
  }
  ```

- [ ] **Step 2 — Run the test, confirm it passes.**

  Run: `go test ./internal/scan -run TestDuplicateDetectorIdempotentAcrossWorkerCounts -race -v`
  Expected: PASS, no race warnings.

- [ ] **Step 3 — Run the entire test suite under `-race` to catch latent goroutine bugs.**

  Run: `go test ./... -race`
  Expected: PASS for every package.

- [ ] **Step 4 — Commit.**

  ```sh
  git add internal/scan/duplicates_test.go
  git commit -m "test: lock DuplicateDetector idempotency across worker counts"
  ```

---

### Task 7: Wire `--mode` CLI flag for archives and duplicates modes

**Files:**
- Modify: `cmd/undup/main.go`

- [ ] **Step 1 — Replace `cmd/undup/main.go` with the new wiring.**

  ```go
  package main

  import (
  	"encoding/hex"
  	"fmt"
  	"os"
  	"path/filepath"
  	"strconv"

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
  		// Implemented in the next task.
  		fmt.Fprintln(os.Stderr, "undup: --mode all is not yet implemented")
  		os.Exit(2)
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
  ```

- [ ] **Step 2 — Build the binary.**

  Run: `go build ./cmd/undup`
  Expected: no errors; `./undup` produced in the current directory.

- [ ] **Step 3 — Smoke-test `--mode archives` against a fixture (preserves current behavior).**

  ```sh
  tmp=$(mktemp -d)
  mkdir -p "$tmp/foo"
  printf 'zipdata' > "$tmp/foo.zip"
  ./undup --mode archives "$tmp"
  rm -rf "$tmp"
  ```
  Expected: one line like `Unpacked archive foo.zip [7 B] (<tmp>/foo)`.

- [ ] **Step 4 — Smoke-test `--mode duplicates` against a fixture.**

  ```sh
  tmp=$(mktemp -d)
  printf 'hello world' > "$tmp/a.bin"
  printf 'hello world' > "$tmp/b.bin"
  printf 'unique' > "$tmp/c.bin"
  ./undup --mode duplicates -j 2 "$tmp"
  rm -rf "$tmp"
  ```
  Expected: two lines starting with the same 8-char short SHA256, one for `a.bin` and one for `b.bin`. No line for `c.bin`. Lines look like:

  ```
  b94d27b9  11  /var/folders/.../a.bin
  b94d27b9  11  /var/folders/.../b.bin
  ```

- [ ] **Step 5 — Confirm static checks pass.**

  Run: `go vet ./...`
  Expected: no output (no warnings).

- [ ] **Step 6 — Commit.**

  ```sh
  git add cmd/undup/main.go
  git commit -m "feat: add --mode flag and duplicate-file output to undup CLI"
  ```

---

### Task 8: Implement `--mode all` with a walk-channel tee

**Files:**
- Modify: `cmd/undup/main.go`

- [ ] **Step 1 — Replace the `case "all":` branch and add `runAll` + `tee` helpers.**

  In `cmd/undup/main.go`, replace the placeholder `case "all":` body:

  ```go
  case "all":
  	runAll(cli.Root, cli.Workers)
  ```

  And append the helpers below `printDuplicates`:

  ```go
  import "sync" // add to the existing import block

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
  			a <- e
  			b <- e
  		}
  	}()
  	return a, b
  }
  ```

  Note: the `import "sync"` statement above is for guidance — fold it into the existing `import (...)` block at the top of `main.go`.

- [ ] **Step 2 — Build.**

  Run: `go build ./cmd/undup`
  Expected: no errors.

- [ ] **Step 3 — Smoke-test `--mode all` against a mixed fixture.**

  ```sh
  tmp=$(mktemp -d)
  mkdir -p "$tmp/foo"
  printf 'zipdata' > "$tmp/foo.zip"
  printf 'hello world' > "$tmp/a.bin"
  printf 'hello world' > "$tmp/b.bin"
  ./undup --mode all -j 4 "$tmp"
  rm -rf "$tmp"
  ```
  Expected output (order may interleave, but both kinds appear):

  ```
  archive: foo.zip [7 B] (<tmp>/foo)
  dupe: b94d27b9  11  <tmp>/a.bin
  dupe: b94d27b9  11  <tmp>/b.bin
  ```

- [ ] **Step 4 — Run the full test suite under `-race`.**

  Run: `go test ./... -race`
  Expected: PASS.

- [ ] **Step 5 — Run static checks.**

  Run: `go vet ./...`
  Expected: no warnings.

- [ ] **Step 6 — Commit.**

  ```sh
  git add cmd/undup/main.go
  git commit -m "feat: implement --mode all with walk-channel tee fan-out"
  ```

---

### Task 9: Update CLAUDE.md for the new detector

**Files:**
- Modify: `CLAUDE.md`

- [ ] **Step 1 — Edit `CLAUDE.md` to describe the new file and CLI surface.**

  Apply these changes:

  1. Under the `## Build & run` section, replace the example block with:

     ```sh
     go build ./cmd/undup                       # produces ./undup binary
     ./undup <root>                             # --mode archives (default), 1 walker
     ./undup -j 4 <root>                        # 4 parallel walkers, archives mode
     ./undup --mode duplicates -j 10 <root>     # SHA256 duplicate-file detector
     ./undup --mode all -j 10 <root>            # both detectors, single walk via tee
     ./undup --help                             # kong-generated usage
     go run ./cmd/undup <root>                  # run without producing a binary
     ```

     And update the next sentence to: `Tests live in internal/scan/archives_test.go and internal/scan/duplicates_test.go.`

  2. Under `## Architecture`, change the file-count line from "Four production files plus one test file:" to "Five production files plus two test files:" and add a bullet for `duplicates.go`:

     ```
     - `internal/scan/duplicates.go` — content-hash duplicate detector — Phase 1 drains the walk into a `map[size][]path`, Phase 2 runs a 4 KiB prefix-hash pass then a full SHA256 pass through a worker pool sized by `-j`. Stateful per run, but output is order-independent.
     ```

  3. Update the `cmd/undup/main.go` description to mention `--mode` and the three modes plus the tee for `all`.

- [ ] **Step 2 — Confirm no other docs claim the old single-detector architecture.**

  Run: `rg -n "Four production files" .`
  Expected: no matches (the bullet you just rewrote was the only one).

- [ ] **Step 3 — Commit.**

  ```sh
  git add CLAUDE.md
  git commit -m "docs: update CLAUDE.md for duplicate detector and --mode flag"
  ```

---

### Task 10: Move the spec into completed/ and final verification

**Files:**
- Move: `docs/specs/20260513-sha256-duplicate-detector.md` — keep in place; only plans move per user CLAUDE.md.
- Move: `docs/plans/20260513-sha256-duplicate-detector.md` → `docs/plans/completed/20260513-sha256-duplicate-detector.md`

- [ ] **Step 1 — Final full-suite verification.**

  Run: `go build ./... && go vet ./... && go test ./... -race`
  Expected: every step exits 0, no warnings, all tests PASS.

- [ ] **Step 2 — End-to-end smoke against a richer fixture.**

  ```sh
  tmp=$(mktemp -d)
  mkdir -p "$tmp/foo" "$tmp/bar"
  printf 'zipdata'    > "$tmp/foo.zip"
  printf 'targzdata!' > "$tmp/bar.tar.gz"
  printf 'hello'      > "$tmp/a.bin"
  printf 'hello'      > "$tmp/b.bin"
  printf 'world'      > "$tmp/foo/c.bin"
  printf 'world'      > "$tmp/bar/d.bin"
  ./undup --mode all -j 4 "$tmp"
  rm -rf "$tmp"
  ```
  Expected: two `archive:` lines (foo.zip, bar.tar.gz) and four `dupe:` lines forming two groups of 2.

- [ ] **Step 3 — Move the completed plan.**

  ```sh
  mkdir -p docs/plans/completed
  git mv docs/plans/20260513-sha256-duplicate-detector.md docs/plans/completed/20260513-sha256-duplicate-detector.md
  ```

- [ ] **Step 4 — Commit.**

  ```sh
  git commit -m "docs: archive completed SHA256 duplicate-detector plan"
  ```

---

## Self-review notes

- **Spec coverage.** Module layout (Task 1), Phase 1 + Phase 2 design (Tasks 2 + 5), CLI `--mode` (Task 7), `--mode all` tee (Task 8), edge-case policy (Tasks 3 + 4), test cases including idempotency under workers (Task 6), CLAUDE.md update (Task 9), spec/plan archival (Task 10).
- **Placeholders.** None — every step that produces code embeds the code; every step that runs a check embeds the command and expected outcome.
- **Type consistency.** `DuplicateGroup`, `DuplicateDetector`, `NewDuplicateDetector`, `sizedPath`, `prefixKey`, `prefixResult`, `fullResult`, `fullGroup`, `hashPrefix`, `hashFull`, `prefixPass`, `fullPass` are all consistent across tasks. `cli.Mode` with enum `archives,duplicates,all` matches the CLI smoke tests and `runArchives` / `runDuplicates` / `runAll` dispatch.
- **Edge case omitted from tests, intentionally.** Hardlinks are reported as duplicates (spec policy); no separate test — the existing duplicate-detection tests already cover the underlying mechanism.
