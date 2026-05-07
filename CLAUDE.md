# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build & run

```sh
go build ./cmd/undup            # produces ./undup binary
./undup <root>                  # default: 1 walker goroutine
./undup -j 4 <root>             # 4 parallel walker goroutines
./undup --help                  # kong-generated usage
go run ./cmd/undup <root>       # run without producing a binary
```

There are no tests yet. `go vet ./...` and `go build ./...` are the only static checks.

## Architecture

Single-purpose CLI. Four files:

- `cmd/undup/main.go` — kong-based CLI parsing (`Root` positional and `-j / --workers` flag, default 1) that wires `scan.Walk` to a `scan.ArchiveDetector` constructed from `scan.Extensions`.
- `internal/scan/scan.go` — the filesystem walker — `Walk(root, workers)` returns `<-chan Entry`. With `workers == 1` it runs a single `filepath.Walk` (lexical order). With `workers > 1` it fans out across the immediate subdirectories of `root`; emission order is not guaranteed.
- `internal/scan/archives.go` — the archive detector — stateless per-entry transformer. For each file it tests known extensions (longest-first), strips the suffix to derive a candidate directory path, runs `os.Lstat`, and emits a finding when the candidate exists and is a directory.
- `internal/scan/extensions.go` — hard-coded list of archive extensions (`.zip`, `.7z`, `.rar`, `.tar`, `.tar.gz`).

### Detection algorithm (the non-obvious part)

`ArchiveDetector.Detect` does **not** look inside archives. It detects "directory `X` was created by unpacking `X.zip` next to it" by deriving the candidate directory name from the archive's filename and checking the filesystem:

1. For each file entry off the walk channel, the detector tests the path against the configured extensions, longest-first (so `.tar.gz` wins over `.tar`).
2. On a match, the detector strips that suffix and runs `os.Lstat` on the resulting path. If it exists and is a directory, an `ArchiveFinding` is emitted.
3. Directory entries from the walker are ignored.

Consequences worth knowing before changing this code:

- Detection is **stateless and order-independent** — entries can arrive in any order, which is what makes parallel traversal under `-j > 1` safe.
- Multi-part extensions like `.tar.gz` work because `NewArchiveDetector` sorts its extensions longest-first, so the detector tries the most specific match first and `break`s on hit. Don't "fix" this by switching to `filepath.Ext` — that would return `.gz` and miss `.tar.gz`.
- Each archive-extension match costs one extra `os.Lstat`. Non-matching files cost only `strings.HasSuffix` calls.
- The check is purely name-based — there is no verification that the directory's contents actually came from the archive. False positives are possible if a user happens to have `foo/` and `foo.zip` as unrelated siblings.
