package platform

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ProcessLock is a filesystem-backed process lock. The lock directory is the
// coordination primitive; its owner file is diagnostic only.
type ProcessLock struct{ path string }

var processLockMutexes sync.Map

// AcquireProcessLock creates path+".lock" and records the owning process.
// Existing stale locks are removed using the same caller-provided threshold.
func AcquireProcessLock(path string, staleAfter time.Duration) (*ProcessLock, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("lock path is required")
	}
	lockPath := path + ".lock"
	if err := os.Mkdir(lockPath, 0o700); err != nil {
		if !os.IsExist(err) {
			return nil, err
		}
		info, statErr := os.Stat(lockPath)
		if statErr == nil && staleAfter > 0 && time.Since(info.ModTime()) > staleAfter {
			if removeErr := os.RemoveAll(lockPath); removeErr == nil {
				if retryErr := os.Mkdir(lockPath, 0o700); retryErr == nil {
					return writeLockOwner(lockPath)
				}
			}
		}
		return nil, fmt.Errorf("local store is busy: %s", filepath.Base(path))
	}
	return writeLockOwner(lockPath)
}

func writeLockOwner(lockPath string) (*ProcessLock, error) {
	if err := os.WriteFile(filepath.Join(lockPath, "owner"), []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
		_ = os.RemoveAll(lockPath)
		return nil, err
	}
	return &ProcessLock{path: lockPath}, nil
}

// Release removes the lock directory. A nil lock is already released.
func (l *ProcessLock) Release() error {
	if l == nil {
		return nil
	}
	return os.RemoveAll(l.path)
}

// Touch refreshes the lock directory modification time for long-running work.
func (l *ProcessLock) Touch() error {
	if l == nil {
		return errors.New("lock is nil")
	}
	now := time.Now()
	return os.Chtimes(l.path, now, now)
}

// WithPrivateFileLock serializes callers in-process and across processes while
// preserving the lock directory and stale-lock semantics of AcquireProcessLock.
func WithPrivateFileLock(path string, staleAfter time.Duration, fn func() error) error {
	value, _ := processLockMutexes.LoadOrStore(path, &sync.Mutex{})
	mutex := value.(*sync.Mutex)
	mutex.Lock()
	defer mutex.Unlock()
	lock, err := AcquireProcessLock(path, staleAfter)
	if err != nil {
		return err
	}
	defer lock.Release()
	return fn()
}
