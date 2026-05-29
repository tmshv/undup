package tui

import (
	"os"
	"path/filepath"
	"testing"
)

func mustWrite(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestDeleteAction_File(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "x.bin")
	mustWrite(t, p, []byte("hello"))

	if err := (DeleteAction{}).Apply(Member{Path: p, IsDir: false}, root); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if _, err := os.Lstat(p); !os.IsNotExist(err) {
		t.Errorf("file still exists or unexpected err: %v", err)
	}
}

func TestDeleteAction_Directory(t *testing.T) {
	root := t.TempDir()
	d := filepath.Join(root, "tree")
	mustWrite(t, filepath.Join(d, "a/b.bin"), []byte("hi"))

	if err := (DeleteAction{}).Apply(Member{Path: d, IsDir: true}, root); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if _, err := os.Lstat(d); !os.IsNotExist(err) {
		t.Errorf("dir still exists or unexpected err: %v", err)
	}
}

func TestValidateMoveTarget(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()

	tests := []struct {
		name    string
		target  string
		wantErr bool
	}{
		{"outside is allowed", outside, false},
		{"equal to root is refused", root, true},
		{"inside root is refused", filepath.Join(root, "sub"), true},
		{"parent of root is allowed", filepath.Dir(root), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateMoveTarget(tt.target, root)
			if (err != nil) != tt.wantErr {
				t.Errorf("err = %v, wantErr = %v", err, tt.wantErr)
			}
		})
	}
}

func TestMoveAction_RelativeLayout(t *testing.T) {
	root := t.TempDir()
	target := t.TempDir()
	src := filepath.Join(root, "sub/dir/file.bin")
	mustWrite(t, src, []byte("payload"))

	a := MoveAction{Target: target}
	if err := a.Apply(Member{Path: src}, root); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	dest := filepath.Join(target, "sub/dir/file.bin")
	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("dest missing: %v", err)
	}
	if string(data) != "payload" {
		t.Errorf("dest content = %q, want %q", data, "payload")
	}
	if _, err := os.Lstat(src); !os.IsNotExist(err) {
		t.Errorf("source still exists: %v", err)
	}
}

func TestMoveAction_CreatesParents(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "newly/nested/parent")
	src := filepath.Join(root, "f.bin")
	mustWrite(t, src, []byte("x"))

	if err := (MoveAction{Target: target}).Apply(Member{Path: src}, root); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if _, err := os.Stat(filepath.Join(target, "f.bin")); err != nil {
		t.Errorf("target file not created: %v", err)
	}
}

func TestMoveAction_RelEscapeFallback(t *testing.T) {
	// member outside the scan root → falls back to filepath.Base
	root := t.TempDir()
	other := t.TempDir()
	target := t.TempDir()
	src := filepath.Join(other, "stray.bin")
	mustWrite(t, src, []byte("y"))

	if err := (MoveAction{Target: target}).Apply(Member{Path: src}, root); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if _, err := os.Stat(filepath.Join(target, "stray.bin")); err != nil {
		t.Errorf("fallback target missing: %v", err)
	}
}

func TestMoveAction_RefusesToOverwrite(t *testing.T) {
	root := t.TempDir()
	target := t.TempDir()
	src := filepath.Join(root, "sub/file.bin")
	mustWrite(t, src, []byte("new"))
	dest := filepath.Join(target, "sub/file.bin")
	mustWrite(t, dest, []byte("existing"))

	err := (MoveAction{Target: target}).Apply(Member{Path: src}, root)
	if err == nil {
		t.Fatal("expected error when destination exists, got nil")
	}
	data, readErr := os.ReadFile(dest)
	if readErr != nil {
		t.Fatalf("dest disappeared: %v", readErr)
	}
	if string(data) != "existing" {
		t.Errorf("dest content = %q, want %q (unmodified)", data, "existing")
	}
	if _, err := os.Lstat(src); err != nil {
		t.Errorf("source removed despite failed move: %v", err)
	}
}

// copyDir is the cross-device fallback for directory members. Verifying it
// directly is enough: the surrounding MoveAction logic only reaches this
// path when os.Rename returns EXDEV, which we can't synthesize in tests
// without crossing real filesystems.
func TestCopyDir_RecursiveCopy(t *testing.T) {
	src := t.TempDir()
	mustWrite(t, filepath.Join(src, "a/b.bin"), []byte("hello"))
	mustWrite(t, filepath.Join(src, "a/c/d.bin"), []byte("world"))
	mustWrite(t, filepath.Join(src, "top.bin"), []byte("top"))

	dst := filepath.Join(t.TempDir(), "out")
	if err := copyDir(src, dst); err != nil {
		t.Fatalf("copyDir: %v", err)
	}

	checks := map[string]string{
		"a/b.bin":   "hello",
		"a/c/d.bin": "world",
		"top.bin":   "top",
	}
	for rel, want := range checks {
		got, err := os.ReadFile(filepath.Join(dst, rel))
		if err != nil {
			t.Errorf("read %s: %v", rel, err)
			continue
		}
		if string(got) != want {
			t.Errorf("%s = %q, want %q", rel, got, want)
		}
	}
}

func TestCopyDir_PreservesSymlinks(t *testing.T) {
	src := t.TempDir()
	mustWrite(t, filepath.Join(src, "real.bin"), []byte("payload"))
	link := filepath.Join(src, "link.bin")
	if err := os.Symlink("real.bin", link); err != nil {
		t.Skipf("symlinks unsupported on this platform: %v", err)
	}
	dst := filepath.Join(t.TempDir(), "out")
	if err := copyDir(src, dst); err != nil {
		t.Fatalf("copyDir: %v", err)
	}
	info, err := os.Lstat(filepath.Join(dst, "link.bin"))
	if err != nil {
		t.Fatalf("lstat link: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("expected symlink at dst, got mode %v", info.Mode())
	}
	got, err := os.Readlink(filepath.Join(dst, "link.bin"))
	if err != nil {
		t.Fatalf("readlink: %v", err)
	}
	if got != "real.bin" {
		t.Errorf("link target = %q, want %q", got, "real.bin")
	}
}

func TestApplyAction_DedupsByAbsPath(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "shared.bin")
	mustWrite(t, p, []byte("z"))

	// Same path shows up in two findings (e.g. archive + duplicate).
	findings := []Finding{
		{Members: []Member{{Path: p, Size: 1, Selected: true}}},
		{Members: []Member{{Path: p, Size: 1, Selected: true}}},
	}
	res := ApplyAction(DeleteAction{}, root, findings)
	if res.Ok != 1 {
		t.Errorf("Ok = %d, want 1 (deduped)", res.Ok)
	}
	if res.Failed != 0 {
		t.Errorf("Failed = %d, want 0", res.Failed)
	}
	if !res.Succeeded[p] {
		t.Errorf("Succeeded[%q] = false, want true", p)
	}
}

func TestApplyAction_SkipsUnselected(t *testing.T) {
	root := t.TempDir()
	keep := filepath.Join(root, "keep.bin")
	gone := filepath.Join(root, "gone.bin")
	mustWrite(t, keep, []byte("k"))
	mustWrite(t, gone, []byte("g"))

	findings := []Finding{
		{Members: []Member{
			{Path: keep, Size: 1, Selected: false},
			{Path: gone, Size: 1, Selected: true},
		}},
	}
	res := ApplyAction(DeleteAction{}, root, findings)
	if res.Ok != 1 || res.Failed != 0 {
		t.Errorf("res = %+v, want Ok=1 Failed=0", res)
	}
	if _, err := os.Lstat(keep); err != nil {
		t.Errorf("keep was deleted: %v", err)
	}
}
