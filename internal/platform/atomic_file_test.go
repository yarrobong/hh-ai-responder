package platform

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

func TestWritePrivateFileAtomicSuccessAndReplacement(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "private.json")
	pattern := ".private-*.tmp"
	if err := WritePrivateFileAtomic(path, []byte("first\n"), pattern); err != nil {
		t.Fatal(err)
	}
	if err := WritePrivateFileAtomic(path, []byte("second\n"), pattern); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "second\n" {
		t.Fatalf("content = %q", raw)
	}
	if runtime.GOOS != "windows" {
		if mode := fileMode(t, path).Perm(); mode != 0o600 {
			t.Fatalf("mode = %o, want 600", mode)
		}
	}
	assertNoTempFiles(t, dir, pattern)
}

func TestWritePrivateFileAtomicFailureCleansTemporaryFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "destination")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	pattern := ".failure-*.tmp"
	if err := WritePrivateFileAtomic(path, []byte("data"), pattern); err == nil {
		t.Fatal("rename onto directory unexpectedly succeeded")
	}
	assertNoTempFiles(t, dir, pattern)
}

func TestWritePrivateFileAtomicConcurrentWritersLeaveCompleteValue(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "concurrent.json")
	pattern := ".concurrent-*.tmp"
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			value := []byte(strings.Repeat(string(rune('a'+i)), 1024))
			if err := WritePrivateFileAtomic(path, value, pattern); err != nil {
				t.Errorf("writer %d: %v", i, err)
			}
		}(i)
	}
	wg.Wait()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) != 1024 || strings.Trim(string(raw), string(raw[0])) != "" {
		t.Fatalf("concurrent write produced partial data: length=%d", len(raw))
	}
	assertNoTempFiles(t, dir, pattern)
}

func assertNoTempFiles(t *testing.T, dir, pattern string) {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, pattern))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("temporary files remain: %v", matches)
	}
}

func fileMode(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode()
}
