package tui

import "github.com/charmbracelet/bubbles/key"

type KeyMap struct {
	Up           key.Binding
	Down         key.Binding
	PageUp       key.Binding
	PageDown     key.Binding
	First        key.Binding
	Last         key.Binding
	Toggle       key.Binding
	Expand       key.Binding
	SelectGroup  key.Binding
	ApplyDefault key.Binding
	ClearAll     key.Binding
	Delete       key.Binding
	Move         key.Binding
	Help         key.Binding
	Confirm      key.Binding
	Cancel       key.Binding
	Quit         key.Binding
}

func DefaultKeyMap() KeyMap {
	return KeyMap{
		Up:           key.NewBinding(key.WithKeys("up", "k")),
		Down:         key.NewBinding(key.WithKeys("down", "j")),
		PageUp:       key.NewBinding(key.WithKeys("pgup", "ctrl+b")),
		PageDown:     key.NewBinding(key.WithKeys("pgdown", "ctrl+f")),
		First:        key.NewBinding(key.WithKeys("g")),
		Last:         key.NewBinding(key.WithKeys("G")),
		Toggle:       key.NewBinding(key.WithKeys(" ")),
		Expand:       key.NewBinding(key.WithKeys("enter")),
		SelectGroup:  key.NewBinding(key.WithKeys("a")),
		ApplyDefault: key.NewBinding(key.WithKeys("A")),
		ClearAll:     key.NewBinding(key.WithKeys("c")),
		Delete:       key.NewBinding(key.WithKeys("d")),
		Move:         key.NewBinding(key.WithKeys("m")),
		Help:         key.NewBinding(key.WithKeys("?")),
		Confirm:      key.NewBinding(key.WithKeys("y")),
		Cancel:       key.NewBinding(key.WithKeys("n", "esc")),
		Quit:         key.NewBinding(key.WithKeys("q", "ctrl+c")),
	}
}
