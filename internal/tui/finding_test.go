package tui

import (
	"testing"

	"github.com/tmshv/undup/internal/scan"
)

func TestFromArchive(t *testing.T) {
	in := scan.ArchiveFinding{
		ArchivePath: "/root/sub/foo.zip",
		DirPath:     "/root/sub/foo",
		Size:        1234,
	}
	got := FromArchive(in)
	if got.Source != SourceArchive {
		t.Fatalf("Source = %v, want SourceArchive", got.Source)
	}
	if got.Label != "foo.zip" {
		t.Errorf("Label = %q, want %q", got.Label, "foo.zip")
	}
	if len(got.Members) != 2 {
		t.Fatalf("len(Members) = %d, want 2", len(got.Members))
	}
	if got.Members[0].Path != "/root/sub/foo.zip" || !got.Members[0].Selected || got.Members[0].IsDir {
		t.Errorf("archive member = %+v; want path/foo.zip, selected, !IsDir", got.Members[0])
	}
	if got.Members[0].Size != 1234 {
		t.Errorf("archive member size = %d, want 1234", got.Members[0].Size)
	}
	if got.Members[1].Path != "/root/sub/foo" || got.Members[1].Selected || !got.Members[1].IsDir {
		t.Errorf("dir member = %+v; want path/foo, !selected, IsDir", got.Members[1])
	}
	if got.Members[1].Size != -1 {
		t.Errorf("dir member size = %d, want -1 (unresolved)", got.Members[1].Size)
	}
}

func TestFromDuplicate(t *testing.T) {
	g := scan.DuplicateGroup{
		SHA256: [32]byte{0xbe, 0xa0, 0xaa, 0x1c, 0xff},
		Size:   2048,
		Paths:  []string{"/a/x.bin", "/b/x.bin", "/c/x.bin"},
	}
	got := FromDuplicate(g)
	if got.Source != SourceDuplicate {
		t.Fatalf("Source = %v, want SourceDuplicate", got.Source)
	}
	if got.Label != "bea0aa1c" {
		t.Errorf("Label = %q, want %q", got.Label, "bea0aa1c")
	}
	if len(got.Members) != 3 {
		t.Fatalf("len(Members) = %d, want 3", len(got.Members))
	}
	wantSelected := []bool{false, true, true}
	for i, w := range wantSelected {
		if got.Members[i].Selected != w {
			t.Errorf("Members[%d].Selected = %v, want %v", i, got.Members[i].Selected, w)
		}
		if got.Members[i].Size != 2048 {
			t.Errorf("Members[%d].Size = %d, want 2048", i, got.Members[i].Size)
		}
		if got.Members[i].IsDir {
			t.Errorf("Members[%d].IsDir = true, want false", i)
		}
	}
}

func TestFromDuplicate_TwoCopies(t *testing.T) {
	g := scan.DuplicateGroup{
		SHA256: [32]byte{0x11, 0x22, 0x33, 0x44},
		Size:   10,
		Paths:  []string{"/a", "/b"},
	}
	got := FromDuplicate(g)
	if got.Members[0].Selected || !got.Members[1].Selected {
		t.Errorf("default selection wrong: %+v", got.Members)
	}
}
