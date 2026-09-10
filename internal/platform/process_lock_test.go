package platform

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestProcessLockAcquireDuplicateReleaseAndTouch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "store.json")
	lock, err := AcquireProcessLock(path, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path + ".lock/owner"); err != nil {
		t.Fatal(err)
	}
	if err := lock.Touch(); err != nil {
		t.Fatal(err)
	}
	if _, err := AcquireProcessLock(path, time.Hour); err == nil || err.Error() != "local store is busy: store.json" {
		t.Fatalf("duplicate acquisition error = %v", err)
	}
	if err := lock.Release(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path + ".lock"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("lock directory after release: %v", err)
	}
}

func TestProcessLockStaleLockIsReclaimed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stale.json")
	lockPath := path + ".lock"
	if err := os.Mkdir(lockPath, 0o700); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(lockPath, old, old); err != nil {
		t.Fatal(err)
	}
	lock, err := AcquireProcessLock(path, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = lock.Release() })
	if _, err := os.Stat(lockPath + "/owner"); err != nil {
		t.Fatal(err)
	}
}

func TestProcessLockRequiresPath(t *testing.T) {
	if _, err := AcquireProcessLock(" ", time.Hour); err == nil || err.Error() != "lock path is required" {
		t.Fatalf("error = %v", err)
	}
	var lock *ProcessLock
	if err := lock.Release(); err != nil {
		t.Fatal(err)
	}
	if err := lock.Touch(); err == nil || err.Error() != "lock is nil" {
		t.Fatalf("nil touch error = %v", err)
	}
}
