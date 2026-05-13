package scan

import (
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
