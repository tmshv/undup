package scan

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
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
	root := t.TempDir()
	d := NewArchiveDetector(Extensions)
	out := d.Detect(Walk(root, 1))
	for range out {
	}
	if _, ok := <-out; ok {
		t.Fatal("Detect channel should be closed after entries channel closes")
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
