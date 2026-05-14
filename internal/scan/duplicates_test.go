package scan

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"testing"
	"time"
)

func TestDuplicateDetectorClosesChannelWhenEntriesClose(t *testing.T) {
	in := make(chan Entry)
	close(in)
	d := NewDuplicateDetector(1, 4096, 1)
	out := d.Detect(in)
	select {
	case _, ok := <-out:
		if ok {
			t.Fatal("Detect emitted a group from an empty input")
		}
	case <-time.After(time.Second):
		t.Fatal("Detect did not close output channel within 1s of input close")
	}
}

func collectGroups(ch <-chan DuplicateGroup) [][]string {
	var out [][]string
	for g := range ch {
		paths := append([]string(nil), g.Paths...)
		sort.Strings(paths)
		out = append(out, paths)
	}
	sort.Slice(out, func(i, j int) bool { return out[i][0] < out[j][0] })
	return out
}

func TestDuplicateDetectorBasic(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T, root string)
		want  [][]string // sorted paths per group, sorted by first path; relative to root
	}{
		{
			name: "two identical files",
			setup: func(t *testing.T, root string) {
				mustWrite(t, filepath.Join(root, "a.bin"), []byte("hello world"))
				mustWrite(t, filepath.Join(root, "b.bin"), []byte("hello world"))
			},
			want: [][]string{{"a.bin", "b.bin"}},
		},
		{
			name: "same size, different content",
			setup: func(t *testing.T, root string) {
				mustWrite(t, filepath.Join(root, "a.bin"), []byte("hello world"))
				mustWrite(t, filepath.Join(root, "b.bin"), []byte("HELLO WORLD"))
			},
			want: nil,
		},
		{
			name: "different sizes",
			setup: func(t *testing.T, root string) {
				mustWrite(t, filepath.Join(root, "a.bin"), []byte("short"))
				mustWrite(t, filepath.Join(root, "b.bin"), []byte("longer content"))
			},
			want: nil,
		},
		{
			name: "three files, two identical plus one same-size near-miss",
			setup: func(t *testing.T, root string) {
				mustWrite(t, filepath.Join(root, "a.bin"), []byte("hello world"))
				mustWrite(t, filepath.Join(root, "b.bin"), []byte("hello world"))
				mustWrite(t, filepath.Join(root, "c.bin"), []byte("HELLO WORLD"))
			},
			want: [][]string{{"a.bin", "b.bin"}},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			tc.setup(t, root)

			d := NewDuplicateDetector(2, 4096, 1)
			got := collectGroups(d.Detect(Walk(root, 1)))

			wantAbs := absolutize(root, tc.want)
			if !equalGroupings(got, wantAbs) {
				t.Fatalf("got %v, want %v", got, wantAbs)
			}
		})
	}
}

func absolutize(root string, groups [][]string) [][]string {
	if groups == nil {
		return nil
	}
	out := make([][]string, len(groups))
	for i, g := range groups {
		out[i] = make([]string, len(g))
		for j, p := range g {
			out[i][j] = filepath.Join(root, p)
		}
	}
	return out
}

func equalGroupings(a, b [][]string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if len(a[i]) != len(b[i]) {
			return false
		}
		for j := range a[i] {
			if a[i][j] != b[i][j] {
				return false
			}
		}
	}
	return true
}

func TestDuplicateDetectorIgnoresEmptyFiles(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "a.empty"), nil)
	mustWrite(t, filepath.Join(root, "b.empty"), nil)

	d := NewDuplicateDetector(2, 4096, 1)
	got := collectGroups(d.Detect(Walk(root, 1)))
	if len(got) != 0 {
		t.Fatalf("expected no groups for empty files, got %v", got)
	}
}

func TestDuplicateDetectorIgnoresSymlinks(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "a.bin"), []byte("payload"))
	mustWrite(t, filepath.Join(root, "b.bin"), []byte("payload"))
	if err := os.Symlink(filepath.Join(root, "a.bin"), filepath.Join(root, "link.bin")); err != nil {
		t.Skipf("symlinks unsupported on this platform: %v", err)
	}

	d := NewDuplicateDetector(2, 4096, 1)
	got := collectGroups(d.Detect(Walk(root, 1)))

	want := [][]string{{filepath.Join(root, "a.bin"), filepath.Join(root, "b.bin")}}
	if !equalGroupings(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestDuplicateDetectorPrefixDifferentialAvoidsFullHash(t *testing.T) {
	root := t.TempDir()
	a := bytes.Repeat([]byte{'A'}, 8192)
	b := bytes.Repeat([]byte{'B'}, 8192)
	mustWrite(t, filepath.Join(root, "a.bin"), a)
	mustWrite(t, filepath.Join(root, "b.bin"), b)

	d := NewDuplicateDetector(2, 4096, 1)
	got := collectGroups(d.Detect(Walk(root, 1)))
	if len(got) != 0 {
		t.Fatalf("expected no groups, got %v", got)
	}
}

func TestDuplicateDetectorPrefixMatchTailDivergesGivesNoGroup(t *testing.T) {
	root := t.TempDir()
	prefix := bytes.Repeat([]byte{'P'}, 4096)
	a := append(append([]byte(nil), prefix...), bytes.Repeat([]byte{'A'}, 1024)...)
	b := append(append([]byte(nil), prefix...), bytes.Repeat([]byte{'B'}, 1024)...)
	mustWrite(t, filepath.Join(root, "a.bin"), a)
	mustWrite(t, filepath.Join(root, "b.bin"), b)

	d := NewDuplicateDetector(2, 4096, 1)
	got := collectGroups(d.Detect(Walk(root, 1)))
	if len(got) != 0 {
		t.Fatalf("expected no groups, got %v", got)
	}
}

func TestDuplicateDetectorTolerantOfUnreadableFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod 0 only toggles read-only on Windows; cannot make file unreadable")
	}
	if os.Geteuid() == 0 {
		t.Skip("running as root bypasses permission checks")
	}
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "a.bin"), []byte("payload"))
	mustWrite(t, filepath.Join(root, "b.bin"), []byte("payload"))
	unreadable := filepath.Join(root, "c.bin")
	mustWrite(t, unreadable, []byte("payload"))
	if err := os.Chmod(unreadable, 0); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(unreadable, 0o644) })

	d := NewDuplicateDetector(2, 4096, 1)
	got := collectGroups(d.Detect(Walk(root, 1)))

	want := [][]string{{filepath.Join(root, "a.bin"), filepath.Join(root, "b.bin")}}
	if !equalGroupings(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestDuplicateDetectorIdempotentAcrossWorkerCounts(t *testing.T) {
	root := t.TempDir()
	for i := range 6 {
		payload := fmt.Appendf(nil, "payload-%d", i)
		mustWrite(t, filepath.Join(root, fmt.Sprintf("a-%d.bin", i)), payload)
		mustWrite(t, filepath.Join(root, fmt.Sprintf("b-%d.bin", i)), payload)
	}
	for i := range 4 {
		mustWrite(t, filepath.Join(root, fmt.Sprintf("unique-%d.bin", i)), fmt.Appendf(nil, "unique-%d", i))
	}

	d1 := NewDuplicateDetector(1, 4096, 1)
	d8 := NewDuplicateDetector(8, 4096, 1)
	got1 := collectGroups(d1.Detect(Walk(root, 1)))
	got8 := collectGroups(d8.Detect(Walk(root, 8)))

	if !equalGroupings(got1, got8) {
		t.Fatalf("worker count changed groupings:\n  workers=1: %v\n  workers=8: %v", got1, got8)
	}
	if len(got1) != 6 {
		t.Fatalf("expected 6 groups, got %d: %v", len(got1), got1)
	}
}
