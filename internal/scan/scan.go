package scan

import (
	"os"
	"path/filepath"
	"sync"
)

type Entry struct {
	Path string
	Info os.FileInfo
	Err  error
}

// Walk traverses root and emits one Entry per filesystem entry on the
// returned channel, which is closed when traversal finishes.
//
// With workers <= 1, Walk runs a single filepath.Walk goroutine and
// preserves filepath.Walk's lexical ordering.
//
// With workers > 1, Walk lists the immediate children of root, emits
// root-level files inline, and distributes the immediate subdirectories
// across the worker pool — each worker runs filepath.Walk over its
// assigned subtree and emits onto the shared channel. Emission order is
// not guaranteed in this mode.
//
// Per-entry errors from filepath.Walk are surfaced via Entry.Err rather
// than aborting the walk.
func Walk(root string, workers int) <-chan Entry {
	if workers < 1 {
		workers = 1
	}
	out := make(chan Entry)

	if workers == 1 {
		go func() {
			defer close(out)
			filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
				out <- Entry{Path: path, Info: info, Err: err}
				return nil
			})
		}()
		return out
	}

	go func() {
		defer close(out)

		rootInfo, err := os.Lstat(root)
		if err != nil {
			out <- Entry{Path: root, Err: err}
			return
		}
		out <- Entry{Path: root, Info: rootInfo}
		if !rootInfo.IsDir() {
			return
		}

		children, err := os.ReadDir(root)
		if err != nil {
			// os.ReadDir may return a partial slice alongside the error;
			// surface the error but still process whatever was read.
			out <- Entry{Path: root, Err: err}
		}
		if len(children) == 0 {
			return
		}

		jobs := make(chan string, len(children))
		var wg sync.WaitGroup
		for i := 0; i < workers; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for sub := range jobs {
					filepath.Walk(sub, func(path string, info os.FileInfo, err error) error {
						out <- Entry{Path: path, Info: info, Err: err}
						return nil
					})
				}
			}()
		}

		for _, c := range children {
			full := filepath.Join(root, c.Name())
			if c.IsDir() {
				jobs <- full
				continue
			}
			info, err := c.Info()
			if err != nil {
				out <- Entry{Path: full, Err: err}
				continue
			}
			out <- Entry{Path: full, Info: info}
		}
		close(jobs)
		wg.Wait()
	}()
	return out
}
