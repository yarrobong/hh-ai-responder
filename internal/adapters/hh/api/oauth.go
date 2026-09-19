package api

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	pkceRandomBytes = 32
	maxOAuthBody    = 1 << 20
)

var (
	errInvalidOAuthConfig = errors.New("OAuth configuration is incomplete or invalid")
	errInvalidOAuthInput  = errors.New("OAuth input is incomplete or invalid")
	errOAuthState         = errors.New("OAuth callback state mismatch")
	errOAuthProvider      = errors.New("OAuth provider returned an error")
	errOAuthTokenRequest  = errors.New("OAuth token request failed")
	errOAuthTokenResponse = errors.New("OAuth token response is invalid")
)

// OAuthConfig contains only operator-supplied OAuth endpoints and credentials.
// It is intentionally not serializable or printable as a diagnostic value.
type OAuthConfig struct {
	AuthorizeURL string
	TokenURL     string
	ClientID     string
	ClientSecret string
	RedirectURI  string
	UserAgent    string
	HTTPClient   *http.Client
	Now          func() time.Time
}

// AuthorizationCallback is the provider's callback result. ErrorDescription is
// retained for callers that need to inspect a callback, but is never included
// in an error returned by this package.
type AuthorizationCallback struct {
	Code             string
	State            string
	Error            string
	ErrorDescription string
}

// AuthorizationSession keeps state and the PKCE verifier in memory for one
// authorization-code flow. The verifier is never placed in the URL or token
// store.
type AuthorizationSession struct {
	config       OAuthConfig
	state        string
	codeVerifier string
	authorizeURL string
}

// NewAuthorizationState returns a cryptographically random state value.
func NewAuthorizationState() (string, error) {
	return randomBase64URL(pkceRandomBytes)
}

// NewCodeVerifier returns a high-entropy RFC 7636 code verifier.
func NewCodeVerifier() (string, error) {
	return randomBase64URL(pkceRandomBytes)
}

// CodeChallengeS256 derives the RFC 7636 S256 challenge from a verifier.
func CodeChallengeS256(verifier string) (string, error) {
	if verifier == "" {
		return "", errInvalidOAuthInput
	}
	digest := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(digest[:]), nil
}

// BuildAuthorizationURL creates an authorization URL with a fresh ephemeral
// PKCE verifier. Callers that need to exchange the matching verifier should use
// NewAuthorizationSession; this helper is useful for URL construction and
// inspection without exposing verifier material.
func BuildAuthorizationURL(config OAuthConfig, state string) (string, error) {
	verifier, err := NewCodeVerifier()
	if err != nil {
		return "", errInvalidOAuthInput
	}
	return buildAuthorizationURL(config, state, verifier)
}

// NewAuthorizationSession starts a stateful, in-memory authorization flow.
func NewAuthorizationSession(config OAuthConfig) (*AuthorizationSession, error) {
	if err := validateAuthorizationConfig(config); err != nil {
		return nil, err
	}
	state, err := NewAuthorizationState()
	if err != nil {
		return nil, errInvalidOAuthInput
	}
	verifier, err := NewCodeVerifier()
	if err != nil {
		return nil, errInvalidOAuthInput
	}
	authorizeURL, err := buildAuthorizationURL(config, state, verifier)
	if err != nil {
		return nil, err
	}
	return &AuthorizationSession{config: config, state: state, codeVerifier: verifier, authorizeURL: authorizeURL}, nil
}

// AuthorizationURL returns the URL to open in the operator's browser.
func (s *AuthorizationSession) AuthorizationURL() string {
	if s == nil {
		return ""
	}
	return s.authorizeURL
}

// State returns the state that must be supplied by the callback.
func (s *AuthorizationSession) State() string {
	if s == nil {
		return ""
	}
	return s.state
}

// ExchangeCallback validates provider errors and state before exchanging the
// authorization code with the session's in-memory verifier.
func (s *AuthorizationSession) ExchangeCallback(ctx context.Context, callback AuthorizationCallback) (OAuthTokens, error) {
	if s == nil || s.codeVerifier == "" {
		return OAuthTokens{}, errInvalidOAuthInput
	}
	if err := ValidateAuthorizationCallback(s.state, callback); err != nil {
		return OAuthTokens{}, err
	}
	return ExchangeAuthorizationCode(ctx, s.config, callback.Code, s.codeVerifier)
}

// ValidateAuthorizationCallback rejects provider errors, missing values, and
// any callback whose state is not an exact match.
func ValidateAuthorizationCallback(expectedState string, callback AuthorizationCallback) error {
	if expectedState == "" || callback.State == "" || callback.State != expectedState {
		return errOAuthState
	}
	if callback.Error != "" {
		return errOAuthProvider
	}
	if callback.Code == "" {
		return errInvalidOAuthInput
	}
	return nil
}

// ExchangeAuthorizationCode performs a form-encoded authorization-code
// exchange. The verifier is supplied by the in-memory authorization session.
func ExchangeAuthorizationCode(ctx context.Context, config OAuthConfig, code, verifier string) (OAuthTokens, error) {
	if err := validateTokenConfig(config); err != nil || code == "" || verifier == "" {
		return OAuthTokens{}, errInvalidOAuthInput
	}
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"code_verifier": {verifier},
		"client_id":     {config.ClientID},
		"client_secret": {config.ClientSecret},
		"redirect_uri":  {config.RedirectURI},
	}
	return postTokenForm(ctx, config, form, OAuthTokens{})
}

// RefreshTokens performs one form-encoded refresh request. A valid provider
// response that omits refresh_token keeps the prior refresh token; a malformed
// or failed response never produces a partially updated token pair.
func RefreshTokens(ctx context.Context, config OAuthConfig, previous OAuthTokens) (OAuthTokens, error) {
	if err := validateTokenConfig(config); err != nil || previous.RefreshToken == "" {
		return OAuthTokens{}, errInvalidOAuthInput
	}
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {previous.RefreshToken},
		"client_id":     {config.ClientID},
		"client_secret": {config.ClientSecret},
	}
	return postTokenForm(ctx, config, form, previous)
}

func buildAuthorizationURL(config OAuthConfig, state, verifier string) (string, error) {
	if err := validateAuthorizationConfig(config); err != nil || state == "" {
		return "", errInvalidOAuthInput
	}
	challenge, err := CodeChallengeS256(verifier)
	if err != nil {
		return "", errInvalidOAuthInput
	}
	u, err := url.Parse(config.AuthorizeURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", errInvalidOAuthConfig
	}
	query := u.Query()
	query.Set("response_type", "code")
	query.Set("client_id", config.ClientID)
	query.Set("redirect_uri", config.RedirectURI)
	query.Set("state", state)
	query.Set("code_challenge", challenge)
	query.Set("code_challenge_method", "S256")
	u.RawQuery = query.Encode()
	return u.String(), nil
}

func postTokenForm(ctx context.Context, config OAuthConfig, form url.Values, previous OAuthTokens) (OAuthTokens, error) {
	if err := contextError(ctx); err != nil {
		return OAuthTokens{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, config.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return OAuthTokens{}, errInvalidOAuthConfig
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	if config.UserAgent != "" {
		req.Header.Set("User-Agent", config.UserAgent)
	}
	client := config.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		if ctxErr := contextError(ctx); ctxErr != nil {
			return OAuthTokens{}, ctxErr
		}
		return OAuthTokens{}, errOAuthTokenRequest
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxOAuthBody))
	if err != nil {
		return OAuthTokens{}, errOAuthTokenResponse
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return OAuthTokens{}, errOAuthTokenRequest
	}
	var wire tokenResponse
	if err := json.Unmarshal(body, &wire); err != nil {
		return OAuthTokens{}, errOAuthTokenResponse
	}
	if wire.AccessToken == "" || wire.TokenType == "" || wire.ExpiresIn == nil || *wire.ExpiresIn < 0 {
		return OAuthTokens{}, errOAuthTokenResponse
	}
	now := time.Now
	if config.Now != nil {
		now = config.Now
	}
	refresh := wire.RefreshToken
	if refresh == "" {
		refresh = previous.RefreshToken
	}
	return OAuthTokens{
		AccessToken: wire.AccessToken, RefreshToken: refresh, TokenType: wire.TokenType,
		ExpiresAt: now().Add(time.Duration(*wire.ExpiresIn) * time.Second),
	}, nil
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    *int64 `json:"expires_in"`
}

func validateAuthorizationConfig(config OAuthConfig) error {
	if !validEndpoint(config.AuthorizeURL) || config.ClientID == "" || config.RedirectURI == "" {
		return errInvalidOAuthConfig
	}
	return nil
}

func validateTokenConfig(config OAuthConfig) error {
	if !validEndpoint(config.TokenURL) || config.ClientID == "" {
		return errInvalidOAuthConfig
	}
	return nil
}

func validEndpoint(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

func randomBase64URL(size int) (string, error) {
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return "", errInvalidOAuthInput
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func contextError(ctx context.Context) error {
	if ctx == nil {
		return errInvalidOAuthInput
	}
	return ctx.Err()
}
