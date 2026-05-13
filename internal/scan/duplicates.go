package scan

type DuplicateGroup struct {
	SHA256 [32]byte
	Size   int64
	Paths  []string
}

type DuplicateDetector struct {
	workers    int
	prefixSize int
	minSize    int64
}

func NewDuplicateDetector(workers, prefixSize int, minSize int64) *DuplicateDetector {
	if workers < 1 {
		workers = 1
	}
	if prefixSize < 1 {
		prefixSize = 4096
	}
	if minSize < 0 {
		minSize = 0
	}
	return &DuplicateDetector{workers: workers, prefixSize: prefixSize, minSize: minSize}
}

func (d *DuplicateDetector) Detect(entries <-chan Entry) <-chan DuplicateGroup {
	out := make(chan DuplicateGroup)
	go func() {
		defer close(out)
		for range entries {
		}
	}()
	return out
}
