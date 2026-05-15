# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build & run

```sh
go build ./cmd/undup                       # produces ./undup binary
./undup <root>                             # --mode archives (default), 1 walker
./undup -j 4 <root>                        # 4 parallel walkers, archives mode
./undup --mode hashsum -j 10 <root>        # SHA256 duplicate-file detector
./undup --mode all -j 10 <root>            # both detectors, single walk via tee
./undup --tui --mode all -j 10 <root>      # interactive TUI over both detectors
./undup --help                             # kong-generated usage
go run ./cmd/undup <root>                  # run without producing a binary
```

Tests live in `internal/scan/archives_test.go` and `internal/scan/duplicates_test.go`. Run with `go test ./...`. `go vet ./...` and `go build ./...` are also useful static checks.

## Architecture

Single-purpose CLI. Five production files plus two test files:

- `cmd/undup/main.go` — kong-based CLI parsing (`Root` positional, `-j / --workers` flag default 1, and `-m / --mode` flag with enum `archives,hashsum,all`). `-j` sizes both the walker fan-out and the duplicate detector's hash worker pool. Dispatches to `runArchives`, `runDuplicates`, or `runAll`. `runAll` uses a `tee` helper to fan a single `scan.Walk` channel out to both detectors so the filesystem is traversed only once; `tee` logs walk errors centrally and filters them out so each error prints once even with both detectors active.
- `internal/scan/scan.go` — the filesystem walker — `Walk(root, workers)` returns `<-chan Entry`. With `workers == 1` it runs a single `filepath.Walk` (lexical order). With `workers > 1` it fans out across the immediate subdirectories of `root`; emission order is not guaranteed.
- `internal/scan/archives.go` — the archive detector — stateless per-entry transformer. For each file it tests known extensions (longest-first), strips the suffix to derive a candidate directory path, runs `os.Lstat`, and emits a finding when the candidate exists and is a directory. Walk errors are written to stderr.
- `internal/scan/duplicates.go` — content-hash duplicate detector — Phase 1 drains the walk into a `map[size][]path`, Phase 2 runs a 4 KiB prefix-hash pass then a full SHA256 pass through a worker pool sized by `-j`. Stateful per run, but output is order-independent (paths inside each emitted group are lexicographically sorted so output is stable across runs and worker counts).
- `internal/tui/` — bubbletea-based interactive view. `Run(archCh, dupCh, scanRoot)` launches the program. `Finding` unifies `ArchiveFinding` (2 members: archive + dir) and `DuplicateGroup` (N members: paths) so one table can show both. `--tui` / `-i` flag in `cmd/undup/main.go` dispatches to `runTUI`, which reuses the existing scan pipeline. Actions: `delete` (file or recursive dir) and `move` (preserves path relative to scan root, refuses targets equal to or inside the scan root).
- `internal/scan/extensions.go` — hard-coded list of archive extensions (`.zip`, `.7z`, `.rar`, `.tar`, `.tar.gz`).
- `internal/scan/archives_test.go` — table-driven tests for `ArchiveDetector.Detect` against fixture trees built in `t.TempDir()`.
- `internal/scan/duplicates_test.go` — table-driven tests for `DuplicateDetector.Detect` covering size grouping, prefix-pass shortcutting, empty-file and symlink skipping, read-error tolerance, and worker-count idempotency.

### Detection algorithm (the non-obvious part)

`ArchiveDetector.Detect` does **not** look inside archives. It detects "directory `X` was created by unpacking `X.zip` next to it" by deriving the candidate directory name from the archive's filename and checking the filesystem:

1. For each file entry off the walk channel, the detector tests the path against the configured extensions, longest-first (so `.tar.gz` wins over `.tar`).
2. On a match, the detector strips that suffix and runs `os.Lstat` on the resulting path. If it exists and is a directory, an `ArchiveFinding` is emitted.
3. Directory entries from the walker are ignored.

Consequences worth knowing before changing this code:

- Detection is **stateless and order-independent** — entries can arrive in any order, which is what makes parallel traversal under `-j > 1` safe.
- Multi-part extensions like `.tar.gz` work because `NewArchiveDetector` sorts its extensions longest-first, so the detector tries the most specific match first and `break`s on hit. Don't "fix" this by switching to `filepath.Ext` — that would return `.gz` and miss `.tar.gz`.
- Each archive-extension match costs one extra `os.Lstat`. Non-matching files cost only `strings.HasSuffix` calls.
- Files whose basename is exactly an archive extension (e.g. `.zip`, `dir/.tar.gz`) are skipped — trimming would yield the parent directory and produce a false positive against it.
- The check is purely name-based — there is no verification that the directory's contents actually came from the archive. False positives are possible if a user happens to have `foo/` and `foo.zip` as unrelated siblings.
