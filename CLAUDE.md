# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build & run

```sh
go build ./cmd/fsdup        # produces ./fsdup binary
./fsdup <root_dir>          # walk root_dir, print directories that look like in-place unpacked archives
go run ./cmd/fsdup <root>   # run without producing a binary
```

There are no tests yet. `go vet ./...` and `go build ./...` are the only static checks.

## Architecture

Single-purpose CLI. Three files:

- `cmd/fsdup/main.go` — argv parsing (positional `os.Args[1]`, no flags), wires `scan.New(scan.Extensions)` and calls `Walk`.
- `internal/scan/scan.go` — the detection algorithm.
- `internal/scan/extensions.go` — hard-coded list of archive extensions (`.zip`, `.7z`, `.rar`, `.tar`, `.tar.gz`).

### Detection algorithm (the non-obvious part)

`Scanner.Walk` does **not** look inside archives. It detects "directory `X` was created by unpacking `X.zip` next to it" purely by name correlation:

1. `filepath.Walk` visits entries in lexical order. When a directory `X` is visited, the scanner registers candidate archive paths `X.zip`, `X.7z`, … into a map keyed by full path.
2. When a regular file is later visited, its path is looked up in that map. A hit means a sibling directory shares the file's basename minus extension, and the file is reported as an unpacked archive.

Consequences worth knowing before changing this code:

- Detection is **order-sensitive**: it relies on `filepath.Walk` visiting the directory before the sibling file. Lexical ordering happens to put `foo/` before `foo.zip` in most cases, but a refactor to `filepath.WalkDir` or concurrent traversal must preserve this invariant or switch to a two-pass design.
- The map grows unbounded for the lifetime of the walk (every directory contributes `len(Extensions)` entries). Fine for `~/Downloads`, not fine for huge trees — keep this in mind if adding recursion limits or parallelism.
- Multi-part extensions like `.tar.gz` work only because the candidate is built by string concatenation; `filepath.Ext` would return `.gz` and miss them. Don't "fix" extensions.go to use `filepath.Ext`.
- The check is purely name-based — there is no verification that the directory's contents actually came from the archive. False positives are possible if a user happens to have `foo/` and `foo.zip` as unrelated siblings.
