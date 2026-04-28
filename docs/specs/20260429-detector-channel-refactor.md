# Detector / channel-walk refactor

Status: design approved, ready for plan.

## Goal

Refactor `fsdup` so that future duplicate-detection methods (content hashing, folder-content equality, fuzzy similarity, etc.) can be added without rewriting the walker each time. Today's user-visible behavior must not change: `./fsdup <root>` still walks the tree once, prints `Unpacked archive <name> (<dir>)` lines for archives whose contents have been unpacked next to them, in the same order as before.

This is a refactor only. No new detectors, no new flags, no new output.

## Non-goals

- No new detection methods.
- No CLI flags for selecting detectors. (Future direction is opt-in flags, but that's a separate change.)
- No structured findings / JSON / TUI. Detectors keep printing directly to stdout.
- No `context.Context`. Add later when something actually needs cancellation.
- No fan-out helper. Today's pipeline is one producer feeding one consumer.
- No tests added as part of this refactor (none exist today; testability is a side-benefit, not a deliverable).

## Architecture

Two layers in package `internal/scan`:

1. **Walk** — a producer that traverses the filesystem and emits `Entry` values on a channel.
2. **Detector** — a struct that holds detector-specific config and consumes the entry channel via a single `Detect` method.

`main` wires them together.

### Types and signatures

```go
// internal/scan/scan.go
package scan

type Entry struct {
    Path string
    Info os.FileInfo
    Err  error  // set if filepath.Walk reported an error for this entry
}

// Walk traverses root and emits one Entry per filesystem entry, in
// filepath.Walk order. The returned channel is closed when traversal
// finishes. Errors from the underlying walk are surfaced via Entry.Err
// rather than aborting the walk.
func Walk(root string) <-chan Entry
```

```go
// internal/scan/archives.go
package scan

type ArchiveDetector struct {
    extensions []string
}

func NewArchiveDetector(extensions []string) *ArchiveDetector

// Detect consumes entries until the channel is closed and prints
// "Unpacked archive <name> (<dir>)" for each detected pair.
func (d *ArchiveDetector) Detect(entries <-chan Entry)
```

```go
// internal/scan/extensions.go (unchanged)
package scan

var Extensions []string = []string{
    ".zip", ".7z", ".rar", ".tar", ".tar.gz",
}
```

```go
// cmd/fsdup/main.go
func main() {
    root := os.Args[1]
    entries := scan.Walk(root)
    detector := scan.NewArchiveDetector(scan.Extensions)
    detector.Detect(entries)
}
```

### Behavior preservation

The detection algorithm itself does not change. `ArchiveDetector.Detect` keeps the existing walk-order trick: when it sees a directory, it registers candidate archive paths (`dir + ext` for each extension); when it later sees a file matching a candidate, it prints the finding. This depends on `filepath.Walk` visiting a directory before its sibling archive file, which it does in lexical order. `Walk` preserves that ordering by sending entries on the channel in the same order the underlying `filepath.Walk` callback fires.

### Error handling

- `filepath.Walk` reports per-entry errors via the `err` argument to its callback. The producer wraps these into `Entry.Err` and continues walking (returns `nil` from the callback).
- For each entry it receives, the detector first checks `Err`. If non-nil, it prints `error scanning <path>: <err>` to stdout and moves on without running detection logic for that entry.
- This is a behavior change versus today: the current code aborts on the first walk error and prints `error scanning directory: <err>`. The new code reports per-entry errors and keeps going. Acceptable because today's error path is essentially never hit on a healthy filesystem, and continuing past one unreadable entry is more useful for the eventual TUI-driven workflow.

### Concurrency

- `Walk` runs `filepath.Walk` in a single goroutine and sends to an unbuffered channel.
- `Detect` runs in the caller's goroutine (today, that's `main`).
- Backpressure: the producer blocks on send when the consumer is slow. That's fine — there's only ever one consumer.

## Future direction (not implemented here)

When a second detector is added:

- A small fan-out helper in `internal/scan` will read from one source channel and forward each entry to N per-detector channels. Each detector runs in its own goroutine.
- Detectors that need file contents will read the file themselves inside `Detect`. Read-once-shared-content (so two content detectors don't open the same file twice) is the next architectural step after fan-out, and will be designed when the second content-reading detector is on the roadmap.
- CLI flags will gate which detectors are constructed and wired in `main`.

These are explicitly out of scope for this refactor. The point of this change is to put `Walk` and the archive detector on opposite sides of a channel boundary so those follow-ups have a clean place to plug in.

## Acceptance criteria

- `go build ./cmd/fsdup` produces a working binary.
- `./fsdup <some_dir_with_unpacked_archives>` prints the same `Unpacked archive ...` lines as the current binary, in the same order.
- The package layout matches the architecture section: `Walk` and `Entry` in `scan.go`, `ArchiveDetector` in `archives.go`, `Extensions` unchanged in `extensions.go`.
- `cmd/fsdup/main.go` constructs `Walk` and `ArchiveDetector` separately and connects them via the channel.
- No `context.Context`, no fan-out helper, no flags introduced.
