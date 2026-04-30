package scan

import (
	"os"
	"path/filepath"
)

type Entry struct {
	Path string
	Info os.FileInfo
	Err  error
}

// Walk traverses root and emits one Entry per filesystem entry in
// filepath.Walk order. The returned channel is closed when traversal
// finishes. Per-entry errors from filepath.Walk are surfaced via Entry.Err
// rather than aborting the walk.
func Walk(root string) <-chan Entry {
	out := make(chan Entry)
	go func() {
		defer close(out)
		filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			out <- Entry{Path: path, Info: info, Err: err}
			return nil
		})
	}()
	return out
}
