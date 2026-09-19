package api

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestFileTokenStoreSavesLoadsPrivatelyAndAtomicallyReplaces(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "oauth.json")
	store := &FileTokenStore{Path: path}
	first := OAuthTokens{AccessToken: "access-one", RefreshToken: "refresh-one", TokenType: "Bearer", ExpiresAt: time.Unix(100, 0).UTC()}
	second := OAuthTokens{AccessToken: "access-two", RefreshToken: "refresh-two", TokenType: "Bearer", ExpiresAt: time.Unix(200, 0).UTC()}
	if err := store.Save(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(context.Background(), second); err != nil {
		t.Fatal(err)
	}
	got, err := store.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got != second {
		t.Fatalf("loaded=%+v, want %+v", got, second)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if mode := info.Mode().Perm(); mode != 0o600 {
			t.Fatalf("mode=%o, want 600", mode)
		}
	}
	matches, err := filepath.Glob(filepath.Join(filepath.Dir(path), ".hh-api-token-*.tmp"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("temporary files remain: %v", matches)
	}
}

func TestFileTokenStoreRejectsMalformedAndInvalidTokenFilesWithoutSecrets(t *testing.T) {
	for name, contents := range map[string]string{
		"malformed":      `{not-json`,
		"missing access": `{"refresh_token":"private-refresh","token_type":"Bearer"}`,
		"missing type":   `{"access_token":"private-access","token_type":""}`,
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "oauth.json")
			if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
				t.Fatal(err)
			}
			_, err := (&FileTokenStore{Path: path}).Load(context.Background())
			if err == nil || strings.Contains(err.Error(), "private-") || strings.Contains(err.Error(), "access_token") {
				t.Fatalf("unsafe malformed-file error=%v", err)
			}
		})
	}
}

func TestFileTokenStoreDeleteRemovesOnlyConfiguredFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "oauth.json")
	neighbor := filepath.Join(dir, "neighbor.txt")
	if err := os.WriteFile(path, []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(neighbor, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := &FileTokenStore{Path: path}
	if err := store.Delete(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("token file stat error=%v", err)
	}
	if raw, err := os.ReadFile(neighbor); err != nil || string(raw) != "keep" {
		t.Fatalf("neighbor changed: raw=%q err=%v", raw, err)
	}
	if err := store.Delete(context.Background()); err != nil {
		t.Fatalf("missing-file delete should be idempotent: %v", err)
	}
}

func TestOAuthTokensExpiryUsesSafetySkew(t *testing.T) {
	now := time.Date(2026, 9, 19, 10, 0, 0, 0, time.UTC)
	tests := []struct {
		name      string
		expiresAt time.Time
		want      bool
	}{
		{"already expired", now.Add(-time.Second), true},
		{"within skew", now.Add(29 * time.Second), true},
		{"outside skew", now.Add(31 * time.Second), false},
		{"unknown expiry", time.Time{}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := (OAuthTokens{AccessToken: "access", TokenType: "Bearer", ExpiresAt: tt.expiresAt}).IsExpiredAt(now, 30*time.Second); got != tt.want {
				t.Fatalf("IsExpiredAt=%v, want %v", got, tt.want)
			}
		})
	}
}

func TestFileTokenStoreValidatesRequiredFieldsOnSave(t *testing.T) {
	store := &FileTokenStore{Path: filepath.Join(t.TempDir(), "oauth.json")}
	for name, tokens := range map[string]OAuthTokens{
		"missing access": {TokenType: "Bearer"},
		"missing type":   {AccessToken: "access"},
	} {
		t.Run(name, func(t *testing.T) {
			if err := store.Save(context.Background(), tokens); err == nil || strings.Contains(err.Error(), "access") {
				t.Fatalf("unsafe or missing validation error=%v", err)
			}
		})
	}
}
