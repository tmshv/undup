# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build & run

```sh
go build ./cmd/undup        # produces ./undup binary
./undup <root_dir>          # walk root_dir, print directories that look like in-place unpacked archives
go run ./cmd/undup <root>   # run without producing a binary
```

There are no tests yet. `go vet ./...` and `go build ./...` are the only static checks.

## Architecture

Single-purpose CLI. Four files:

- `cmd/undup/main.go` — argv parsing (positional `os.Args[1]`, no flags), wires `scan.Walk` to a `scan.ArchiveDetector` constructed from `scan.Extensions`.
- `internal/scan/scan.go` — the filesystem walker — `Walk(root)` returns `<-chan Entry` populated by a background goroutine running `filepath.Walk`.
- `internal/scan/archives.go` — the archive detector — consumes the entry channel produced by `scan.Walk` and prints unpacked-archive findings.
- `internal/scan/extensions.go` — hard-coded list of archive extensions (`.zip`, `.7z`, `.rar`, `.tar`, `.tar.gz`).

### Detection algorithm (the non-obvious part)

`ArchiveDetector.Detect` does **not** look inside archives. It detects "directory `X` was created by unpacking `X.zip` next to it" purely by name correlation:

1. `filepath.Walk` visits entries in lexical order. When a directory `X` is visited, the scanner registers candidate archive paths `X.zip`, `X.7z`, … into a map keyed by full path.
2. When a regular file is later visited, its path is looked up in that map. A hit means a sibling directory shares the file's basename minus extension, and the file is reported as an unpacked archive.

Consequences worth knowing before changing this code:

- Detection is **order-sensitive**: `Walk` preserves `filepath.Walk`'s lexical ordering when emitting on the channel, which is what makes the directory-before-archive detection work. A switch to `filepath.WalkDir` or any concurrent traversal must preserve this ordering or move detection to a two-pass design.
- The map grows unbounded for the lifetime of the walk (every directory contributes `len(Extensions)` entries). Fine for `~/Downloads`, not fine for huge trees — keep this in mind if adding recursion limits or parallelism.
- Multi-part extensions like `.tar.gz` work only because the candidate is built by string concatenation; `filepath.Ext` would return `.gz` and miss them. Don't "fix" extensions.go to use `filepath.Ext`.
- The check is purely name-based — there is no verification that the directory's contents actually came from the archive. False positives are possible if a user happens to have `foo/` and `foo.zip` as unrelated siblings.
