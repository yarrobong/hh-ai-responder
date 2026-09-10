package runtime

import (
	"time"

	"hh-ai-responder/internal/platform"
)

func withStoreLock(path string, fn func() error) error {
	waitStart := time.Now()
	return platform.WithPrivateFileLock(path, 30*time.Minute, func() error {
		perfRecord("lock.process_wait", waitStart, 1)
		start := time.Now()
		defer perfRecord("lock.critical_section", start, 1)
		return fn()
	})
}
