package tui

import (
	"errors"
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

func TestFinding_TotalSize(t *testing.T) {
	dup := FromDuplicate(scan.DuplicateGroup{Size: 100, Paths: []string{"/a", "/b", "/c"}})
	if got := dup.totalSize(); got != 300 {
		t.Errorf("duplicate totalSize = %d, want 300", got)
	}

	arc := FromArchive(scan.ArchiveFinding{ArchivePath: "/x.zip", DirPath: "/x", Size: 50})
	// dir size unresolved (-1) counts as 0 until it is walked.
	if got := arc.totalSize(); got != 50 {
		t.Errorf("archive totalSize (unresolved dir) = %d, want 50", got)
	}
	arc.Members[1].Size = 70 // dir size resolves
	if got := arc.totalSize(); got != 120 {
		t.Errorf("archive totalSize (resolved dir) = %d, want 120", got)
	}
}

func TestCycleGroupSelection_Duplicate(t *testing.T) {
	f := FromDuplicate(scan.DuplicateGroup{Size: 10, Paths: []string{"/a", "/b", "/c", "/d"}})
	// FromDuplicate starts in the default "all-except-one" state (keep /a).
	assertSelected := func(label string, want ...bool) {
		t.Helper()
		for i, w := range want {
			if f.Members[i].Selected != w {
				t.Fatalf("%s: member[%d].Selected = %v, want %v (%+v)", label, i, f.Members[i].Selected, w, f.Members)
			}
		}
	}

	cycleGroupSelection(&f) // except-one → all
	assertSelected("after press 1 (all)", true, true, true, true)

	cycleGroupSelection(&f) // all → none
	assertSelected("after press 2 (none)", false, false, false, false)

	cycleGroupSelection(&f) // none → all-except-one (default keep = first)
	assertSelected("after press 3 (default)", false, true, true, true)

	cycleGroupSelection(&f) // wrap: except-one → all
	assertSelected("after press 4 (wrap to all)", true, true, true, true)
}

func TestCycleGroupSelection_SkipsNonSelectable(t *testing.T) {
	f := Finding{Source: SourceDuplicate, Members: []Member{
		{Path: "/a", Size: 10},
		{Path: "/b", Size: 10, Selected: true},
		{Path: "/c", SizeErr: errors.New("walk failed")}, // non-selectable
	}}
	// Selectable members = {a, b}: sel = 1 = n-1 → press goes to "all".
	cycleGroupSelection(&f)
	if !f.Members[0].Selected || !f.Members[1].Selected {
		t.Fatalf("selectable members should be selected: %+v", f.Members)
	}
	if f.Members[2].Selected {
		t.Fatalf("non-selectable member must not be selected: %+v", f.Members[2])
	}
}
