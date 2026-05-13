# SHA256 duplicate-file detector

## Purpose

Add a second detector to `undup` that finds files with identical byte content anywhere under the scanned root, regardless of name or location. Coexists with the existing name-based archive detector; the user picks one (or both) per invocation.

## CLI

```
undup --mode archives   <root>   # current behavior; default
undup --mode duplicates <root>   # new
undup --mode all        <root>   # both detectors against a single walk
undup -j N --mode ...   <root>   # workers flag applies to both walk and hash workers
```

- `--mode` defaults to `archives` so existing usage is unchanged.
- `--mode all` runs `scan.Walk` once and fans the `Entry` channel out to both detectors via a small tee goroutine.
- Output lines from each detector are prefixed (`archive:` / `dupe:`) when both run, so they're distinguishable when interleaved.

## Output format

One line per duplicate path, ASCII columns, no headers:

```
<short-sha256>  <size-bytes>  <path>
```

- `short-sha256` is the first 8 hex chars of the full SHA256.
- `size-bytes` is the file size as a decimal integer, right-aligned to the width of the largest size emitted in this run (computed once before any line is printed).
- Each duplicate file emits its own line. Members of the same group share the same `short-sha256` prefix, making it easy to group with `sort` or `awk`.

Example:

```
7f3a1c2d  1310720  /a/file.bin
7f3a1c2d  1310720  /b/file.bin
7f3a1c2d  1310720  /c/copy.bin
9b21ef00      512  /x/notes.txt
9b21ef00      512  /y/notes.txt
```

Groups with fewer than 2 surviving members are never printed.

## Architecture

### Module layout

```
internal/scan/duplicates.go       # new detector
internal/scan/duplicates_test.go  # table-driven tests
cmd/undup/main.go                 # --mode flag, dispatch
```

### Detector surface

`DuplicateDetector` mirrors `ArchiveDetector`: a constructor + a `Detect` method that consumes `<-chan Entry` and returns a closed-on-completion output channel.

```go
type DuplicateGroup struct {
    SHA256 [32]byte
    Size   int64
    Paths  []string
}

type DuplicateDetector struct {
    workers    int
    prefixSize int   // 4096 by default
    minSize    int64 // 1 by default; zero-byte files are skipped
}

func NewDuplicateDetector(workers, prefixSize int, minSize int64) *DuplicateDetector
func (d *DuplicateDetector) Detect(entries <-chan Entry) <-chan DuplicateGroup
```

Unlike `ArchiveDetector`, this detector is stateful: it must see the whole walk before it can know which files share a size. The streaming output channel is therefore quiet during Phase 1 and bursts during Phase 2.

### Phase 1 — walk drain

A single goroutine inside `Detect` drains the `entries` channel. For each entry it:

- Skips directories.
- Skips symlinks (consistent with `ArchiveDetector` which already uses `Lstat`).
- Logs walk errors to stderr (`fmt.Fprintf(os.Stderr, ...)`) and skips.
- Skips files where `info.Size() < minSize` (default `< 1`, i.e., empty files).
- Appends the path to `sizeMap[info.Size()]`.

When the input channel closes, Phase 1 is done.

### Phase 2 — three-pass hash pipeline

1. **Prefix pass.** Build a job channel of every path whose size group has `len >= 2`. Spawn `workers` goroutines. Each worker:
   - Opens the file.
   - Reads up to `prefixSize` bytes into a fixed buffer (4 KiB by default).
   - Computes `sha256.Sum256(buf[:n])`.
   - Sends `{size, prefix, path}` on a result channel.
   - On any `os.Open` or `io.Read` error, logs to stderr and drops the path.

   An aggregator goroutine collects results into `map[prefixKey][]string` where `prefixKey = struct{ size int64; prefix [32]byte }`. After all workers finish, groups with `len < 2` are discarded.

2. **Full-hash pass.** Surviving paths feed a second job channel. A fresh set of `workers` goroutines (simpler than reusing the pool across pass boundaries) streams each file through `sha256.New()` using `io.Copy`. Each worker emits `{fullHash, size, path}`. If the file errors mid-stream, log to stderr and drop. The aggregator groups by `[32]byte` full hash.

3. **Emit.** Every group with `len(paths) >= 2` is sent on the output channel as a `DuplicateGroup`, then the channel is closed.

### Worker count

- The existing `-j / --workers` flag controls both the walker and the hash worker pool.
- One knob keeps the UX simple. A power user wanting separate tuning can add `--hash-workers` later; YAGNI for now.
- Recommended user setting for I/O-bound trees on local SSD: `-j 10` (matches the user's stated target).

### Concurrency safety

- Workers share no mutable state; communication is via channels.
- Aggregators run as single goroutines, so their maps never see concurrent writes.
- The walk → Phase 1 stage runs concurrently with `scan.Walk`'s emission (it reads the channel), but Phase 2 begins only after Phase 1 finishes draining, so there is no overlap between hash workers and walk workers competing for the channel.

## Edge-case policy

| Case                     | Policy                                          | Reason                                                                                  |
| ------------------------ | ----------------------------------------------- | --------------------------------------------------------------------------------------- |
| Zero-byte files          | Skipped                                         | All empty files share a SHA256; reporting them is pure noise.                           |
| Symlinks                 | Skipped (Lstat-based, like `ArchiveDetector`)   | Avoid double-counting and avoid following potentially circular links.                   |
| Hardlinks                | Reported as duplicates                          | Technically correct; the user can spot them via inode if desired.                       |
| Files with `size < 1`    | Skipped (default `minSize = 1`)                 | Default chosen to skip only empties; `--min-size` flag is a future addition.            |
| Prefix size              | 4 KiB                                           | One page; diverges for most non-identical files; cheap to read in a single `read(2)`.   |
| Read errors mid-hash     | Log to stderr, drop the affected path           | Matches existing walk-error handling in `ArchiveDetector`.                              |
| File shrinks during scan | Treated as a normal read; whatever hashes hashes | A racing writer is a user problem; we don't lock or retry.                              |

## Testing strategy

`internal/scan/duplicates_test.go` is table-driven, building fixture trees in `t.TempDir()`. Required cases:

- Two identical files at different paths → one group of size 2.
- Two same-size, different-content files → no group emitted.
- Two files with identical 4 KiB prefix but differing tails → no group emitted (full-hash pass filters them).
- Three files, two identical + one near-miss with same prefix → one group of size 2.
- Empty files present → ignored.
- A symlink whose target is one of the duplicate files → the symlink entry is skipped; the two actual duplicate files at their real paths are still reported as a group of 2 (the symlink doesn't become a phantom third copy).
- Unreadable file (chmod 000) inside a candidate group → error logged, other duplicates still detected.
- Same fixture run with `workers = 1` and `workers = 8` produces identical groupings (order-independent equality check).

Static checks: `go build ./...`, `go vet ./...`, `go test ./...`.

## Non-goals

- No content-based verification that an unpacked directory really came from a sibling archive — that remains the archive detector's name-based heuristic.
- No directory-level Merkle hashing.
- No persistence of hash results between runs.
- No `--min-size`, `--prefix-size`, or `--hash-workers` flags in this iteration. Defaults are baked in; flags can be added later if a real need emerges.
