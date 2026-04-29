# Detector / channel-walk refactor — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Split `internal/scan` into a channel-emitting filesystem walker and a separate `ArchiveDetector` consumer, so future detectors can plug into the same pipeline.

**Architecture:** `scan.Walk(root)` returns `<-chan scan.Entry` produced by a goroutine running `filepath.Walk`. `scan.ArchiveDetector` is a struct holding the extension list with a single `Detect(<-chan Entry)` method that consumes entries and prints findings. `main` wires the two together.

**Tech Stack:** Go 1.22, standard library only (`path/filepath`, `os`, `fmt`).

**Spec:** [docs/specs/20260429-detector-channel-refactor.md](../specs/20260429-detector-channel-refactor.md)

---

### Task 1: Rewrite scan package with channel-based walk + detector

**Files:**
- Modify: `internal/scan/scan.go` (full rewrite)
- Create: `internal/scan/archives.go`
- Verify unchanged: `internal/scan/extensions.go`

- [x] Replace the contents of `internal/scan/scan.go` with:

```go
package scan

import (
	"os"
	"path/filepath"
)

type Entry struct {
	Path string
	Info os.FileInfo
	Err  error
}

// Walk traverses root and emits one Entry per filesystem entry in
// filepath.Walk order. The returned channel is closed when traversal
// finishes. Per-entry errors from filepath.Walk are surfaced via Entry.Err
// rather than aborting the walk.
func Walk(root string) <-chan Entry {
	out := make(chan Entry)
	go func() {
		defer close(out)
		filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			out <- Entry{Path: path, Info: info, Err: err}
			return nil
		})
	}()
	return out
}
```

- [x] Create `internal/scan/archives.go` with:

```go
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
```

- [x] Confirm `internal/scan/extensions.go` is unchanged (still exports `var Extensions []string = []string{".zip", ".7z", ".rar", ".tar", ".tar.gz"}`).

- [x] Replace the contents of `cmd/fsdup/main.go` with:

```go
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
```

- [x] Run `go vet ./...`. Expected: no output, exit 0.

- [x] Run `go build ./cmd/fsdup`. Expected: produces `./fsdup` binary, no errors.

- [x] Commit:

```bash
git add internal/scan/scan.go internal/scan/archives.go cmd/fsdup/main.go
git commit -m "Refactor scan into channel-based Walk + ArchiveDetector"
```

---

### Task 2: Verify behavior parity against a fixture

**Files:** none — manual verification.

- [ ] Create a fixture directory simulating an unpacked archive next to its source:

```bash
rm -rf /tmp/fsdup-fixture
mkdir -p /tmp/fsdup-fixture/foo/bar
touch /tmp/fsdup-fixture/foo/bar.zip
touch /tmp/fsdup-fixture/foo/bar/inside.txt
```

- [ ] Run the new binary against the fixture:

```bash
./fsdup /tmp/fsdup-fixture
```

Expected output (exactly one line):

```
Unpacked archive bar.zip (/tmp/fsdup-fixture/foo/bar)
```

- [ ] Add a multi-part-extension fixture to confirm `.tar.gz` still works:

```bash
mkdir -p /tmp/fsdup-fixture/foo/baz
touch /tmp/fsdup-fixture/foo/baz.tar.gz
./fsdup /tmp/fsdup-fixture
```

Expected output (two lines, order may vary by lexical traversal but `bar` comes before `baz`):

```
Unpacked archive bar.zip (/tmp/fsdup-fixture/foo/bar)
Unpacked archive baz.tar.gz (/tmp/fsdup-fixture/foo/baz)
```

- [ ] Verify the error path: introduce an unreadable directory and confirm the binary prints an error and continues without aborting:

```bash
mkdir /tmp/fsdup-fixture/locked
chmod 000 /tmp/fsdup-fixture/locked
./fsdup /tmp/fsdup-fixture
echo "exit: $?"
```

Expected: an `error scanning /tmp/fsdup-fixture/locked: ...` line appears in the output, the two `Unpacked archive ...` lines still print, and `exit: 0`.

- [ ] Restore permissions and remove the fixture:

```bash
chmod 755 /tmp/fsdup-fixture/locked
rm -rf /tmp/fsdup-fixture
```

- [ ] No commit for this task — verification only.

---

### Task 3: Update CLAUDE.md to reflect the new architecture

**Files:**
- Modify: `CLAUDE.md`

- [ ] Update the Architecture section of `CLAUDE.md` so the file list and detection-algorithm description match the post-refactor layout. Specifically:

  - Replace "Three files" with "Four files" and add `internal/scan/archives.go` to the list, described as: "the archive detector — consumes the entry channel produced by `scan.Walk` and prints unpacked-archive findings."
  - Update the description of `internal/scan/scan.go` to: "the filesystem walker — `Walk(root)` returns `<-chan Entry` populated by a background goroutine running `filepath.Walk`."
  - Update the `cmd/fsdup/main.go` description to: "wires `scan.Walk` to a `scan.ArchiveDetector` constructed from `scan.Extensions`."
  - In the "Detection algorithm" subsection, change the opening sentence from "`Scanner.Walk` does not look inside archives" to "`ArchiveDetector.Detect` does not look inside archives", since the detection logic now lives there.
  - In the same subsection, update the bullet about ordering so the warning applies to the producer: "`Walk` preserves `filepath.Walk`'s lexical ordering when emitting on the channel, which is what makes the directory-before-archive detection work. A switch to `filepath.WalkDir` or any concurrent traversal must preserve this ordering or move detection to a two-pass design."
  - Leave the bullets about the unbounded candidate map, multi-part extensions, and false positives as they are.

- [ ] Commit:

```bash
git add CLAUDE.md
git commit -m "Update CLAUDE.md for channel-based scan refactor"
```
