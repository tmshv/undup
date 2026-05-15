package tui

import (
	"fmt"

	"github.com/tmshv/undup/internal/scan"
)

// Run is the entrypoint for the TUI. archCh / dupCh may be nil if the
// corresponding detector is not active for this run.
func Run(archCh <-chan scan.ArchiveFinding, dupCh <-chan scan.DuplicateGroup, scanRoot string) error {
	done := make(chan struct{}, 2)
	go func() {
		if archCh != nil {
			for range archCh {
			}
		}
		done <- struct{}{}
	}()
	go func() {
		if dupCh != nil {
			for range dupCh {
			}
		}
		done <- struct{}{}
	}()
	<-done
	<-done
	fmt.Println("(tui stub — full UI lands in subsequent tasks)")
	return nil
}
