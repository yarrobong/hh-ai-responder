package main

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

type localProcessLock struct{ path string }

var localPathMutexes sync.Map

func acquireLocalProcessLock(path string, staleAfter time.Duration) (*localProcessLock, error) {
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

func writeLockOwner(lockPath string) (*localProcessLock, error) {
	if err := os.WriteFile(filepath.Join(lockPath, "owner"), []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
		_ = os.RemoveAll(lockPath)
		return nil, err
	}
	return &localProcessLock{path: lockPath}, nil
}

func (l *localProcessLock) Release() error {
	if l == nil {
		return nil
	}
	return os.RemoveAll(l.path)
}

func (l *localProcessLock) Touch() error {
	if l == nil {
		return errors.New("lock is nil")
	}
	now := time.Now()
	return os.Chtimes(l.path, now, now)
}

func withStoreLock(path string, fn func() error) error {
	value, _ := localPathMutexes.LoadOrStore(path, &sync.Mutex{})
	mutex := value.(*sync.Mutex)
	waitStart := time.Now()
	mutex.Lock()
	perfRecord("lock.process_wait", waitStart, 1)
	defer mutex.Unlock()
	lock, err := acquireLocalProcessLock(path, 30*time.Minute)
	if err != nil {
		return err
	}
	defer lock.Release()
	start := time.Now()
	defer perfRecord("lock.critical_section", start, 1)
	return fn()
}
