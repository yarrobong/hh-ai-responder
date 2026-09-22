package runtime

import (
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestMigrationAuthorityDirectoriesAreIdentical(t *testing.T) {
	t.Parallel()

	embeddedDir := "migrations"
	rootDir := filepath.Join("..", "..", "migrations")
	embedded := migrationDirectoryFiles(t, embeddedDir)
	root := migrationDirectoryFiles(t, rootDir)

	if len(embedded) != len(root) {
		t.Fatalf("migration file count differs: embedded=%d root=%d", len(embedded), len(root))
	}
	for name, embeddedPath := range embedded {
		rootPath, ok := root[name]
		if !ok {
			t.Fatalf("migration %q exists in embedded authority but not root mirror", name)
		}
		embeddedBytes, err := os.ReadFile(embeddedPath)
		if err != nil {
			t.Fatalf("read embedded migration %q: %v", name, err)
		}
		rootBytes, err := os.ReadFile(rootPath)
		if err != nil {
			t.Fatalf("read root migration %q: %v", name, err)
		}
		if !bytes.Equal(embeddedBytes, rootBytes) {
			t.Fatalf("migration %q differs between embedded authority and root mirror", name)
		}
	}

	versions := make([]int, 0, len(embedded))
	for name := range embedded {
		version, err := migrationVersion(filepath.ToSlash(filepath.Join("migrations", name)))
		if err != nil {
			t.Fatal(err)
		}
		versions = append(versions, int(version))
	}
	sort.Ints(versions)
	for index, version := range versions {
		if version != index/2+1 {
			t.Fatalf("migration versions are not contiguous at index %d: got %d", index, version)
		}
	}
	for name := range embedded {
		if !strings.HasSuffix(name, ".up.sql") {
			continue
		}
		base := strings.TrimSuffix(name, ".up.sql")
		if _, ok := embedded[base+".down.sql"]; !ok {
			t.Fatalf("migration %q has no paired down file", name)
		}
	}
}

func migrationDirectoryFiles(t *testing.T, dir string) map[string]string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read migration directory %q: %v", dir, err)
	}
	result := make(map[string]string)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		result[entry.Name()] = filepath.Join(dir, entry.Name())
	}
	return result
}
