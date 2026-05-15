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
