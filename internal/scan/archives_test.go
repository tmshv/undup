package scan

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"
)

func TestDetectEmitsFindingsForUnpackedArchives(t *testing.T) {
	root := t.TempDir()

	mustMkdir(t, filepath.Join(root, "foo"))
	mustWrite(t, filepath.Join(root, "foo.zip"), []byte("zipdata"))
	mustTouch(t, filepath.Join(root, "foo", "inside.txt"))

	mustMkdir(t, filepath.Join(root, "bar"))
	mustWrite(t, filepath.Join(root, "bar.tar.gz"), []byte("targzdata!"))

	mustTouch(t, filepath.Join(root, "unrelated.txt"))

	d := NewArchiveDetector(Extensions)
	var got []ArchiveFinding
	for f := range d.Detect(Walk(root, 1)) {
		got = append(got, f)
	}
	sort.Slice(got, func(i, j int) bool { return got[i].ArchivePath < got[j].ArchivePath })

	want := []ArchiveFinding{
		{ArchivePath: filepath.Join(root, "bar.tar.gz"), DirPath: filepath.Join(root, "bar"), Size: 10},
		{ArchivePath: filepath.Join(root, "foo.zip"), DirPath: filepath.Join(root, "foo"), Size: 7},
	}

	if len(got) != len(want) {
		t.Fatalf("got %d findings, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("finding %d: got %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestDetectClosesChannelWhenEntriesClose(t *testing.T) {
	in := make(chan Entry)
	close(in)
	d := NewArchiveDetector(Extensions)
	out := d.Detect(in)
	select {
	case _, ok := <-out:
		if ok {
			t.Fatal("Detect emitted a finding from an empty input")
		}
	case <-time.After(time.Second):
		t.Fatal("Detect did not close output channel within 1s of input close")
	}
}

// Regression: a file whose basename is exactly an archive extension
// (e.g. ".zip") used to trim to the parent directory path and emit a
// false-positive finding pointing at the parent.
func TestDetectIgnoresFilesWhoseBasenameIsJustAnExtension(t *testing.T) {
	root := t.TempDir()

	mustTouch(t, filepath.Join(root, ".zip"))
	mustTouch(t, filepath.Join(root, ".tar.gz"))

	d := NewArchiveDetector(Extensions)
	var got []ArchiveFinding
	for f := range d.Detect(Walk(root, 1)) {
		got = append(got, f)
	}
	if len(got) != 0 {
		t.Fatalf("expected no findings, got %d: %+v", len(got), got)
	}
}

// Regression: filenames like "..zip" and "...zip" used to slip past the
// basename check, trim to "." or "..", and resolve via os.Lstat to the
// parent (or grandparent) directory — emitting a false-positive finding
// that could even point outside the scanned root.
func TestDetectIgnoresFilesWithDotPrefixedExtensionNames(t *testing.T) {
	root := t.TempDir()
	inner := filepath.Join(root, "inner")
	mustMkdir(t, inner)

	mustTouch(t, filepath.Join(inner, "..zip"))
	mustTouch(t, filepath.Join(inner, "...zip"))
	mustTouch(t, filepath.Join(inner, "..tar.gz"))
	mustTouch(t, filepath.Join(inner, "...tar.gz"))

	d := NewArchiveDetector(Extensions)
	var got []ArchiveFinding
	for f := range d.Detect(Walk(root, 1)) {
		got = append(got, f)
	}
	if len(got) != 0 {
		t.Fatalf("expected no findings, got %d: %+v", len(got), got)
	}
}

// Regression: a symlink whose name matches an archive extension would
// otherwise be reported as an archive finding, and the cross-device
// MoveAction fallback would silently replace the link with a regular copy
// of the target's bytes. Non-regular files are not eligible findings.
func TestDetectIgnoresNonRegularFiles(t *testing.T) {
	root := t.TempDir()

	mustMkdir(t, filepath.Join(root, "foo"))
	target := filepath.Join(root, "elsewhere.zip")
	mustWrite(t, target, []byte("zipdata"))
	if err := os.Symlink(target, filepath.Join(root, "foo.zip")); err != nil {
		t.Skipf("symlink unsupported on this platform: %v", err)
	}

	d := NewArchiveDetector(Extensions)
	var got []ArchiveFinding
	for f := range d.Detect(Walk(root, 1)) {
		got = append(got, f)
	}
	for _, f := range got {
		if filepath.Base(f.ArchivePath) == "foo.zip" {
			t.Errorf("symlink foo.zip emitted as finding: %+v", f)
		}
	}
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

func mustTouch(t *testing.T, path string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
}

func mustWrite(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}
