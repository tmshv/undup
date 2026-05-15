package tui

import (
	"encoding/hex"
	"path/filepath"

	"github.com/tmshv/undup/internal/scan"
)

type Source int

const (
	SourceArchive Source = iota
	SourceDuplicate
)

func (s Source) Tag() string {
	switch s {
	case SourceArchive:
		return "arc"
	case SourceDuplicate:
		return "dup"
	}
	return "?"
}

// Member is one selectable path inside a Finding.
type Member struct {
	Path     string
	Size     int64 // -1 if unresolved (archive directory pending dir-size walk)
	IsDir    bool
	Selected bool
	// SizeErr non-nil means this member is non-selectable (size walk failed).
	SizeErr error
}

// Selectable reports whether the member can participate in selection / actions.
// A member with a failed size walk is excluded from selection so its bytes
// don't pollute the reclaim total.
func (m Member) Selectable() bool { return m.SizeErr == nil }

// Finding groups together a set of members that the user can act on.
type Finding struct {
	Source   Source
	Label    string
	Members  []Member
	Expanded bool
}

func FromArchive(f scan.ArchiveFinding) Finding {
	return Finding{
		Source: SourceArchive,
		Label:  filepath.Base(f.ArchivePath),
		Members: []Member{
			{Path: f.ArchivePath, Size: f.Size, IsDir: false, Selected: true},
			{Path: f.DirPath, Size: -1, IsDir: true, Selected: false},
		},
	}
}

func FromDuplicate(g scan.DuplicateGroup) Finding {
	members := make([]Member, len(g.Paths))
	for i, p := range g.Paths {
		members[i] = Member{
			Path:     p,
			Size:     g.Size,
			IsDir:    false,
			Selected: i != 0, // keep the lexicographically-first path
		}
	}
	return Finding{
		Source:  SourceDuplicate,
		Label:   hex.EncodeToString(g.SHA256[:4]),
		Members: members,
	}
}
