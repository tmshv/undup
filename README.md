# undup

Walks a directory tree to help you keep a filesystem clean. Two detectors are available:

- **archives** (default) — reports unpacked archives that still sit next to their original `.zip`, `.7z`, `.rar`, `.tar`, or `.tar.gz` file.
- **duplicates** — reports files whose contents are byte-for-byte identical, grouped by SHA256.
- **all** — runs both detectors in a single walk of the tree.

```
./undup ~/Downloads/GIS
Unpacked archive ne_10m_populated_places.zip [4.7 MiB] (...GIS/natural_earth/ne_10m_populated_places)
Unpacked archive ne_10m_roads.zip [2.1 MiB] (...GIS/natural_earth/ne_10m_roads)
Unpacked archive ne_10m_time_zones.zip [318.2 KiB] (...GIS/natural_earth/ne_10m_time_zones)
```

Each archive line shows the archive file name, its size, and the sibling directory that appears to be the unpacked copy. Detection is name-based: it does not look inside the archive.

## Usage

1. Build

```sh
go build ./cmd/undup
```

2. Run

```sh
./undup <root>                            # --mode archives (default), 1 walker
./undup -j 4 <root>                       # 4 parallel walkers, archives mode
./undup --mode duplicates -j 10 <root>    # duplicate-file detector
./undup --mode all -j 10 <root>           # both detectors, single walk
./undup --help                            # full usage
```

`<root>` must be an existing directory. `-j / --workers` must be `>= 1`; the value sizes both the directory walker fan-out and the per-pass hash worker pool used by the duplicate detector.

### Duplicate-mode output

```
./undup --mode duplicates -j 10 ~/Downloads
b94d27b9  524288  /home/me/Downloads/photo-001.raw
b94d27b9  524288  /home/me/Downloads/backup/photo-001.raw
a591a6d4   12011  /home/me/Downloads/notes.txt
a591a6d4   12011  /home/me/Downloads/notes-copy.txt
```

Each group is one or more lines sharing the same short SHA256 prefix (8 hex chars), file size in bytes, and absolute path. Empty files and symlinks are skipped. Read errors are logged to stderr and the path is dropped from its candidate group.

### Combined `--mode all` output

```
./undup --mode all -j 10 ~/Downloads
archive: ne_10m_roads.zip [2.1 MiB] (/home/me/Downloads/ne_10m_roads)
archive: dataset.tar.gz [318.2 KiB] (/home/me/Downloads/dataset)
dupe: b94d27b9  524288  /home/me/Downloads/photo-001.raw
dupe: b94d27b9  524288  /home/me/Downloads/backup/photo-001.raw
```

In `--mode all` lines are prefixed (`archive:` / `dupe:`) so output is easy to grep. Archive findings stream while the walk is in progress; duplicate findings appear together once all files have been hashed.
