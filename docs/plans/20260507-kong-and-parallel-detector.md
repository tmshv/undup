# Kong CLI + parallel walk + stateless detector — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace ad-hoc `os.Args[1]` parsing with `kong`, expose a `-j / --workers` flag, parallelize `scan.Walk` across the root's immediate subdirectories, and rewrite `ArchiveDetector` so detection is stateless and order-independent (works correctly under parallel traversal).

**Architecture:** `scan.Walk(root, workers)` returns `<-chan Entry`. With `workers == 1` it runs a single `filepath.Walk` (current behavior, sans the now-irrelevant ordering guarantee). With `workers > 1` it lists the root's immediate children, emits root-level files inline, and distributes subdirectories across `workers` goroutines that each run `filepath.Walk` over their assigned subtree onto a shared channel. `ArchiveDetector.Detect` becomes a stateless transformer: it ignores directories, and for each file tests known archive extensions (longest match wins), strips the suffix, runs `os.Lstat` on the candidate path, and emits a finding when the candidate exists and is a directory. `cmd/undup/main.go` parses `Root` and `Workers` via `kong` and wires the two together.

**Tech Stack:** Go 1.22, `github.com/alecthomas/kong`, standard library (`os`, `path/filepath`, `sort`, `strings`, `sync`).

**Out of scope:** Go test files (project has none and the existing convention is fixture-based manual verification — see `docs/plans/completed/20260429-detector-channel-refactor.md`). The hash-based detector that will actually exercise `-j` is a future PR.

---

### Task 1: Rewrite `ArchiveDetector` to be stateless and order-independent

**Files:**
- Modify: `internal/scan/archives.go` (full rewrite of `Detect`; constructor adjusted to sort extensions longest-first)

- [x] **Step 1: Replace the contents of `internal/scan/archives.go` with:**

```go
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
```

- [x] **Step 2: Confirm `internal/scan/scan.go`, `internal/scan/extensions.go`, and `cmd/undup/main.go` are unchanged.** They still compile against the new `Detect` because its signature (`func (*ArchiveDetector) Detect(<-chan Entry) <-chan ArchiveFinding`) is unchanged.

- [x] **Step 3: Static checks.**

Run: `go vet ./...`
Expected: no output, exit 0.

Run: `go build ./...`
Expected: no output, exit 0.

- [x] **Step 4: Behavior parity smoke test.**

```bash
rm -rf /tmp/undup-fixture
mkdir -p /tmp/undup-fixture/foo/bar /tmp/undup-fixture/foo/baz
touch /tmp/undup-fixture/foo/bar/inside.txt
touch /tmp/undup-fixture/foo/bar.zip
touch /tmp/undup-fixture/foo/baz.tar.gz
touch /tmp/undup-fixture/foo/orphan.zip
mkdir /tmp/undup-fixture/foo/orphan-dir
go run ./cmd/undup /tmp/undup-fixture
```

Expected output (exactly two `Unpacked archive` lines, in some order — order is no longer guaranteed but with `filepath.Walk` it remains lexical):

```
Unpacked archive bar.zip [0 B] (/tmp/undup-fixture/foo/bar)
Unpacked archive baz.tar.gz [0 B] (/tmp/undup-fixture/foo/baz)
```

The lone `orphan.zip` (no sibling dir) and the lone `orphan-dir/` (no sibling archive) must NOT appear. This validates that the new path-derivation + `Lstat` algorithm matches the old map-based one.

- [x] **Step 5: Multi-part-extension regression check.**

Make sure `.tar.gz` wins over `.tar`. The `baz.tar.gz` line above already covers this — if the longest-first sort were broken, the detector would strip `.tar` instead, look up `/tmp/undup-fixture/foo/baz.gz`, find nothing, and the `baz.tar.gz` line would be missing.

- [x] **Step 6: Clean up the fixture.**

```bash
rm -rf /tmp/undup-fixture
```

- [x] **Step 7: Commit.**

```bash
git add internal/scan/archives.go
git commit -m "Rewrite ArchiveDetector as stateless per-file Lstat lookup"
```

---

### Task 2: Parallelize `scan.Walk` and thread a `workers` parameter

**Files:**
- Modify: `internal/scan/scan.go`
- Modify: `cmd/undup/main.go` (single-line callsite update)

- [ ] **Step 1: Replace the contents of `internal/scan/scan.go` with:**

```go
package scan

import (
	"os"
	"path/filepath"
	"sync"
)

type Entry struct {
	Path string
	Info os.FileInfo
	Err  error
}

// Walk traverses root and emits one Entry per filesystem entry on the
// returned channel, which is closed when traversal finishes.
//
// With workers <= 1, Walk runs a single filepath.Walk goroutine and
// preserves filepath.Walk's lexical ordering.
//
// With workers > 1, Walk lists the immediate children of root, emits
// root-level files inline, and distributes the immediate subdirectories
// across the worker pool — each worker runs filepath.Walk over its
// assigned subtree and emits onto the shared channel. Emission order is
// not guaranteed in this mode.
//
// Per-entry errors from filepath.Walk are surfaced via Entry.Err rather
// than aborting the walk.
func Walk(root string, workers int) <-chan Entry {
	if workers < 1 {
		workers = 1
	}
	out := make(chan Entry)

	if workers == 1 {
		go func() {
			defer close(out)
			filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
				out <- Entry{Path: path, Info: info, Err: err}
				return nil
			})
		}()
		return out
	}

	go func() {
		defer close(out)

		rootInfo, err := os.Lstat(root)
		if err != nil {
			out <- Entry{Path: root, Err: err}
			return
		}
		out <- Entry{Path: root, Info: rootInfo}
		if !rootInfo.IsDir() {
			return
		}

		children, err := os.ReadDir(root)
		if err != nil {
			out <- Entry{Path: root, Err: err}
			return
		}

		jobs := make(chan string, len(children))
		var wg sync.WaitGroup
		for i := 0; i < workers; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for sub := range jobs {
					filepath.Walk(sub, func(path string, info os.FileInfo, err error) error {
						out <- Entry{Path: path, Info: info, Err: err}
						return nil
					})
				}
			}()
		}

		for _, c := range children {
			full := filepath.Join(root, c.Name())
			if c.IsDir() {
				jobs <- full
				continue
			}
			info, err := c.Info()
			if err != nil {
				out <- Entry{Path: full, Err: err}
				continue
			}
			out <- Entry{Path: full, Info: info}
		}
		close(jobs)
		wg.Wait()
	}()
	return out
}
```

- [ ] **Step 2: Update the callsite in `cmd/undup/main.go`.**

Change the single line:

```go
	entries := scan.Walk(rootDir)
```

to:

```go
	entries := scan.Walk(rootDir, 1)
```

(The `1` is a placeholder. Task 3 replaces it with `cli.Workers` once `kong` is wired.)

- [ ] **Step 3: Static checks.**

Run: `go vet ./...`
Expected: no output, exit 0.

Run: `go build ./...`
Expected: no output, exit 0.

- [ ] **Step 4: Single-worker fixture check (parity with Task 1).**

```bash
rm -rf /tmp/undup-fixture
mkdir -p /tmp/undup-fixture/foo/bar /tmp/undup-fixture/foo/baz
touch /tmp/undup-fixture/foo/bar/inside.txt
touch /tmp/undup-fixture/foo/bar.zip
touch /tmp/undup-fixture/foo/baz.tar.gz
go run ./cmd/undup /tmp/undup-fixture
```

Expected: same two `Unpacked archive` lines as Task 1.

- [ ] **Step 5: Multi-worker fixture check (temporary edit).**

Temporarily change the callsite in `cmd/undup/main.go` from `scan.Walk(rootDir, 1)` to `scan.Walk(rootDir, 4)`, then rebuild and run:

```bash
go run ./cmd/undup /tmp/undup-fixture
```

Expected: the same two `Unpacked archive` lines, possibly in a different order. Crucially, both must still appear — confirming the stateless detector handles out-of-order entries.

Now revert the callsite back to `scan.Walk(rootDir, 1)` (Task 3 will replace it with `cli.Workers`).

- [ ] **Step 6: Error-path check.**

```bash
mkdir /tmp/undup-fixture/locked
chmod 000 /tmp/undup-fixture/locked
go run ./cmd/undup /tmp/undup-fixture
echo "exit: $?"
```

Expected: an `error scanning /tmp/undup-fixture/locked: ...` line appears, the two `Unpacked archive ...` lines still print, and `exit: 0`.

- [ ] **Step 7: Clean up the fixture.**

```bash
chmod 755 /tmp/undup-fixture/locked
rm -rf /tmp/undup-fixture
```

- [ ] **Step 8: Commit.**

```bash
git add internal/scan/scan.go cmd/undup/main.go
git commit -m "Parallelize scan.Walk across root subdirectories"
```

---

### Task 3: Add `kong` CLI with `-j / --workers`

**Files:**
- Modify: `cmd/undup/main.go` (full rewrite)
- Modify: `go.mod`, `go.sum` (kong dependency)

- [ ] **Step 1: Add the kong dependency.**

```bash
go get github.com/alecthomas/kong@latest
go mod tidy
```

Expected: `go.mod` gains a `require github.com/alecthomas/kong vX.Y.Z` line; `go.sum` is populated.

- [ ] **Step 2: Replace the contents of `cmd/undup/main.go` with:**

```go
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
```

- [ ] **Step 3: Static checks.**

Run: `go vet ./...`
Expected: no output, exit 0.

Run: `go build ./...`
Expected: no output, exit 0.

- [ ] **Step 4: CLI behavior checks against a fixture.**

```bash
rm -rf /tmp/undup-fixture
mkdir -p /tmp/undup-fixture/foo/bar /tmp/undup-fixture/foo/baz
touch /tmp/undup-fixture/foo/bar/inside.txt
touch /tmp/undup-fixture/foo/bar.zip
touch /tmp/undup-fixture/foo/baz.tar.gz
make build
```

Run each of these and confirm the expected behavior:

```bash
./bin/undup /tmp/undup-fixture
```
Expected: two `Unpacked archive` lines (default `-j 1`).

```bash
./bin/undup -j 4 /tmp/undup-fixture
```
Expected: same two lines (order may differ).

```bash
./bin/undup --workers 4 /tmp/undup-fixture
```
Expected: same two lines (long form works too).

```bash
./bin/undup -j 0 /tmp/undup-fixture
echo "exit: $?"
```
Expected: stderr `undup: -j/--workers must be >= 1`, `exit: 1`.

```bash
./bin/undup
echo "exit: $?"
```
Expected: kong usage error referencing missing `<root>`, non-zero exit.

```bash
./bin/undup /tmp/undup-fixture-does-not-exist
echo "exit: $?"
```
Expected: kong usage error from `existingdir` validation, non-zero exit.

```bash
./bin/undup --help
```
Expected: kong-formatted help text listing the `<root>` positional and the `-j, --workers` flag with default `1`.

- [ ] **Step 5: Clean up the fixture.**

```bash
rm -rf /tmp/undup-fixture
```

- [ ] **Step 6: Commit.**

```bash
git add go.mod go.sum cmd/undup/main.go
git commit -m "Parse CLI args with kong and add -j workers flag"
```

---

### Task 4: Update `CLAUDE.md` to reflect the new architecture

**Files:**
- Modify: `CLAUDE.md`

- [ ] **Step 1: Update the Build & run section.**

Replace the existing block:

```sh
go build ./cmd/undup        # produces ./undup binary
./undup <root_dir>          # walk root_dir, print directories that look like in-place unpacked archives
go run ./cmd/undup <root>   # run without producing a binary
```

with:

```sh
go build ./cmd/undup            # produces ./undup binary
./undup <root>                  # default: 1 walker goroutine
./undup -j 4 <root>             # 4 parallel walker goroutines
./undup --help                  # kong-generated usage
go run ./cmd/undup <root>       # run without producing a binary
```

- [ ] **Step 2: Update the Architecture section.**

Replace the file list and detection-algorithm subsection so they describe the post-change layout:

- `cmd/undup/main.go` — kong-based CLI parsing (`Root` positional and `-j / --workers` flag, default 1) that wires `scan.Walk` to a `scan.ArchiveDetector` constructed from `scan.Extensions`.
- `internal/scan/scan.go` — the filesystem walker — `Walk(root, workers)` returns `<-chan Entry`. With `workers == 1` it runs a single `filepath.Walk` (lexical order). With `workers > 1` it fans out across the immediate subdirectories of `root`; emission order is not guaranteed.
- `internal/scan/archives.go` — the archive detector — stateless per-entry transformer. For each file it tests known extensions (longest-first), strips the suffix to derive a candidate directory path, runs `os.Lstat`, and emits a finding when the candidate exists and is a directory.
- `internal/scan/extensions.go` — hard-coded list of archive extensions (`.zip`, `.7z`, `.rar`, `.tar`, `.tar.gz`).

Replace the "Detection algorithm" subsection's body with:

> `ArchiveDetector.Detect` does **not** look inside archives. It detects "directory `X` was created by unpacking `X.zip` next to it" by deriving the candidate directory name from the archive's filename and checking the filesystem:
>
> 1. For each file entry off the walk channel, the detector tests the path against the configured extensions, longest-first (so `.tar.gz` wins over `.tar`).
> 2. On a match, the detector strips that suffix and runs `os.Lstat` on the resulting path. If it exists and is a directory, an `ArchiveFinding` is emitted.
> 3. Directory entries from the walker are ignored.
>
> Consequences worth knowing before changing this code:
>
> - Detection is **stateless and order-independent** — entries can arrive in any order, which is what makes parallel traversal under `-j > 1` safe.
> - Multi-part extensions like `.tar.gz` work because `NewArchiveDetector` sorts its extensions longest-first, so the detector tries the most specific match first and `break`s on hit. Don't "fix" this by switching to `filepath.Ext` — that would return `.gz` and miss `.tar.gz`.
> - Each archive-extension match costs one extra `os.Lstat`. Non-matching files cost only `strings.HasSuffix` calls.
> - The check is purely name-based — there is no verification that the directory's contents actually came from the archive. False positives are possible if a user happens to have `foo/` and `foo.zip` as unrelated siblings.

Remove the bullet about the candidate map growing unbounded — the map no longer exists.

Remove the bullet warning that switching to `filepath.WalkDir` or any concurrent traversal must preserve ordering — concurrent traversal is now the documented `-j > 1` mode and the detector is order-independent.

- [ ] **Step 3: Commit.**

```bash
git add CLAUDE.md
git commit -m "Update CLAUDE.md for kong CLI, parallel walk, and stateless detector"
```

---

## Self-review notes

- Spec coverage: kong CLI ✔ (Task 3), `-j` flag with default 1 ✔ (Task 3), parallel `Walk` ✔ (Task 2), order-independent stateless detector ✔ (Task 1), CLAUDE.md update ✔ (Task 4).
- Each task ends with a buildable, behavior-verified binary.
- Type/name consistency: `Walk(root string, workers int) <-chan Entry`, `ArchiveDetector.Detect(<-chan Entry) <-chan ArchiveFinding`, `cli.Root`, `cli.Workers`, `scan.Extensions` — used consistently across tasks.
- No placeholders. Every code-touching step shows the full code or the exact change.
