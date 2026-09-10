package platform

import (
	"fmt"
	"os"
	"path/filepath"
)

// WritePrivateFileAtomic replaces path with data through a same-directory
// private temporary file. The temporary file is synced before it is closed and
// renamed, and is removed on every failure path. CreateTemp gives the file
// 0600 permissions; the containing directory is created with 0700.
//
// tempPattern is kept explicit so callers can preserve their existing
// temporary-file naming and cleanup contracts.
func WritePrivateFileAtomic(path string, data []byte, tempPattern string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create private file directory: %w", err)
	}
	tmp, err := os.CreateTemp(dir, tempPattern)
	if err != nil {
		return fmt.Errorf("stage private file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if _, err = tmp.Write(data); err == nil {
		err = tmp.Sync()
	}
	closeErr := tmp.Close()
	if err != nil {
		return fmt.Errorf("write private file: %w", err)
	}
	if closeErr != nil {
		return fmt.Errorf("close private file: %w", closeErr)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("replace private file: %w", err)
	}
	return nil
}
