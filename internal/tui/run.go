package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/tmshv/undup/internal/scan"
)

// Run launches the bubbletea program over the provided detector channels.
// archCh / dupCh may be nil (corresponding detector inactive).
func Run(archCh <-chan scan.ArchiveFinding, dupCh <-chan scan.DuplicateGroup, scanRoot string) error {
	model := NewModel(archCh, dupCh, scanRoot)
	prog := tea.NewProgram(model, tea.WithAltScreen())
	_, err := prog.Run()
	return err
}
