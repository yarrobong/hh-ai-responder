package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"time"

	"hh-ai-responder/internal/platform"
)

const DefaultTokenExpirySkew = 30 * time.Second

var (
	errTokenFileConfig = errors.New("OAuth token file configuration is invalid")
	errTokenFileAbsent = errors.New("OAuth token file is absent")
	errTokenFileRead   = errors.New("OAuth token file could not be read")
	errTokenFileWrite  = errors.New("OAuth token file could not be written")
	errTokenFileShape  = errors.New("OAuth token file is malformed")
	errTokensInvalid   = errors.New("OAuth tokens are incomplete")
)

// OAuthTokens is the private token model used by the API adapter.
type OAuthTokens struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	TokenType    string    `json:"token_type"`
	ExpiresAt    time.Time `json:"expires_at"`
}

// TokenStore persists OAuth tokens without exposing them to reports or logs.
type TokenStore interface {
	Load(context.Context) (OAuthTokens, error)
	Save(context.Context, OAuthTokens) error
	Delete(context.Context) error
}

// FileTokenStore stores tokens in one operator-configured private JSON file.
type FileTokenStore struct {
	Path string
}

var _ TokenStore = (*FileTokenStore)(nil)

func NewFileTokenStore(path string) *FileTokenStore {
	return &FileTokenStore{Path: path}
}

func (s *FileTokenStore) Load(ctx context.Context) (OAuthTokens, error) {
	if err := contextError(ctx); err != nil {
		return OAuthTokens{}, err
	}
	if s == nil || s.Path == "" {
		return OAuthTokens{}, errTokenFileConfig
	}
	raw, err := os.ReadFile(s.Path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return OAuthTokens{}, errTokenFileAbsent
		}
		return OAuthTokens{}, errTokenFileRead
	}
	var tokens OAuthTokens
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&tokens); err != nil {
		return OAuthTokens{}, errTokenFileShape
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return OAuthTokens{}, errTokenFileShape
	}
	if err := validateTokens(tokens); err != nil {
		return OAuthTokens{}, errTokenFileShape
	}
	return tokens, nil
}

func (s *FileTokenStore) Save(ctx context.Context, tokens OAuthTokens) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if s == nil || s.Path == "" {
		return errTokenFileConfig
	}
	if err := validateTokens(tokens); err != nil {
		return errTokensInvalid
	}
	raw, err := json.Marshal(tokens)
	if err != nil {
		return errTokenFileWrite
	}
	if err := platform.WritePrivateFileAtomic(s.Path, raw, ".hh-api-token-*.tmp"); err != nil {
		return errTokenFileWrite
	}
	return nil
}

func (s *FileTokenStore) Delete(ctx context.Context) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if s == nil || s.Path == "" {
		return errTokenFileConfig
	}
	if err := os.Remove(s.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return errTokenFileWrite
	}
	return nil
}

// IsExpiredAt applies a safety skew so a request does not start with a token
// that is about to expire. An unknown expiry is treated as expired.
func (tokens OAuthTokens) IsExpiredAt(now time.Time, skew time.Duration) bool {
	if tokens.ExpiresAt.IsZero() {
		return true
	}
	if skew < 0 {
		skew = 0
	}
	return !now.Before(tokens.ExpiresAt.Add(-skew))
}

func (tokens OAuthTokens) IsExpired(now time.Time) bool {
	return tokens.IsExpiredAt(now, DefaultTokenExpirySkew)
}

func validateTokens(tokens OAuthTokens) error {
	if tokens.AccessToken == "" || tokens.TokenType == "" {
		return errTokensInvalid
	}
	return nil
}
