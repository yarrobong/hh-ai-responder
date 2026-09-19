package runtime

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	stdRuntime "runtime"
	"strconv"
	"strings"
	"time"

	hhapi "hh-ai-responder/internal/adapters/hh/api"
)

const (
	hhAPIMaxManualRedirectBytes = 16 << 10
)

// HHAPICommandDeps contains process-side seams for the OAuth command. The
// production handler uses the defaults; tests can inject browser and callback
// behavior without accepting codes through argv or environment variables.
type HHAPICommandDeps struct {
	OpenBrowser              func(context.Context, string) error
	ReceiveLocalhostCallback func(context.Context, string) (string, error)
	HTTPClient               *http.Client
	Now                      func() time.Time
}

func runHHAPICommand(ctx context.Context, args []string, cfg Config, stdin io.Reader, stdout, stderr io.Writer) error {
	return runHHAPICommandWithDeps(ctx, args, cfg, stdin, stdout, stderr, HHAPICommandDeps{})
}

func runHHAPICommandWithDeps(ctx context.Context, args []string, cfg Config, stdin io.Reader, stdout, stderr io.Writer, deps HHAPICommandDeps) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if stdout == nil {
		stdout = io.Discard
	}
	if stderr == nil {
		stderr = io.Discard
	}
	if len(args) != 1 || strings.HasPrefix(args[0], "-") {
		return errors.New("hh-api command requires exactly one subcommand: auth, doctor, or logout")
	}
	switch args[0] {
	case "auth":
		return runHHAPIAuth(ctx, cfg, stdin, stdout, deps)
	case "doctor":
		return runHHAPIDoctor(ctx, cfg, stdout, deps)
	case "logout":
		return runHHAPILogout(ctx, cfg, stdout)
	default:
		return errors.New("unknown hh-api subcommand")
	}
}

func runHHAPIAuth(ctx context.Context, cfg Config, stdin io.Reader, stdout io.Writer, deps HHAPICommandDeps) error {
	oauthConfig, err := hhAPIOAuthConfig(cfg)
	if err != nil {
		return err
	}
	if deps.Now == nil {
		deps.Now = time.Now
	}
	session, err := hhapi.NewAuthorizationSession(oauthConfig)
	if err != nil {
		return errors.New("HH API authorization configuration is invalid")
	}
	authorizationURL := session.AuthorizationURL()

	redirectURI := strings.TrimSpace(cfg.HHOAuthRedirectURI)
	if isLoopbackRedirectURI(redirectURI) {
		openBrowser := deps.OpenBrowser
		if openBrowser == nil {
			openBrowser = openHHAPIURL
		}
		if err := openBrowser(ctx, authorizationURL); err != nil {
			// A browser opener is best effort. The URL is the operator's
			// authorization input, not a callback/code input.
			_, _ = fmt.Fprintf(stdout, "Authorization URL: %s\n", authorizationURL)
		}
		receiveCallback := deps.ReceiveLocalhostCallback
		if receiveCallback == nil {
			receiveCallback = receiveHHAPILocalhostCallback
		}
		callbackURL, callbackErr := receiveCallback(ctx, redirectURI)
		if callbackErr != nil {
			return errors.New("HH API localhost callback was not received")
		}
		return exchangeHHAPICallback(ctx, cfg, oauthConfig, session, callbackURL, stdout, deps)
	}

	_, _ = fmt.Fprintf(stdout, "Authorization URL: %s\n", authorizationURL)
	_, _ = fmt.Fprint(stdout, "Paste the full redirect URL: ")
	callbackURL, err := readHHAPIRedirectURL(stdin)
	if err != nil {
		return errors.New("HH API manual callback input is invalid")
	}
	return exchangeHHAPICallback(ctx, cfg, oauthConfig, session, callbackURL, stdout, deps)
}

func exchangeHHAPICallback(ctx context.Context, cfg Config, oauthConfig hhapi.OAuthConfig, session *hhapi.AuthorizationSession, callbackURL string, stdout io.Writer, deps HHAPICommandDeps) error {
	callback, err := parseHHAPICallbackURL(callbackURL)
	if err != nil {
		return errors.New("HH API callback URL is invalid")
	}
	tokens, err := session.ExchangeCallback(ctx, callback)
	if err != nil {
		return fmt.Errorf("HH API authorization failed: %w", err)
	}
	tokenStore, client, err := newHHAPIClient(cfg, oauthConfig, deps)
	if err != nil {
		return err
	}
	if err := tokenStore.Save(ctx, tokens); err != nil {
		return errors.New("HH API token storage failed")
	}
	user, err := client.CurrentUser(ctx)
	if err != nil {
		return fmt.Errorf("HH API authorization verification failed: %w", err)
	}

	_, _ = fmt.Fprintln(stdout, "HH API OAuth")
	_, _ = fmt.Fprintln(stdout, "Authorized: YES")
	_, _ = fmt.Fprintf(stdout, "User type: %s\n", safeHHAPIUserType(user.AuthType))
	_, _ = fmt.Fprintln(stdout, "Token stored: YES")
	_, _ = fmt.Fprintln(stdout, "Access token logged: NO")
	_, _ = fmt.Fprintln(stdout, "Refresh token logged: NO")
	return nil
}

func runHHAPIDoctor(ctx context.Context, cfg Config, stdout io.Writer, deps HHAPICommandDeps) error {
	_, _ = fmt.Fprintln(stdout, "HH API Doctor")
	tokenPath := strings.TrimSpace(cfg.HHAPITokenFile)
	if tokenPath == "" {
		_, _ = fmt.Fprintln(stdout, "Token file: invalid")
		_, _ = fmt.Fprintln(stdout, "Overall: AUTH_REQUIRED")
		return errors.New("HH API token file is not configured")
	}
	if _, err := os.Stat(tokenPath); err != nil {
		if os.IsNotExist(err) {
			_, _ = fmt.Fprintln(stdout, "Token file: absent")
			_, _ = fmt.Fprintln(stdout, "Overall: AUTH_REQUIRED")
			return errors.New("HH API token file is absent")
		}
		_, _ = fmt.Fprintln(stdout, "Token file: unavailable")
		_, _ = fmt.Fprintln(stdout, "Overall: AUTH_REQUIRED")
		return errors.New("HH API token file is unavailable")
	}

	tokenStore := hhapi.NewFileTokenStore(tokenPath)
	tokens, err := tokenStore.Load(ctx)
	if err != nil {
		_, _ = fmt.Fprintln(stdout, "Token file: invalid")
		_, _ = fmt.Fprintln(stdout, "Overall: AUTH_REQUIRED")
		return errors.New("HH API token file is invalid")
	}
	_, _ = fmt.Fprintln(stdout, "Transport: API")
	_, _ = fmt.Fprintln(stdout, "Token file: present")
	now := time.Now()
	if deps.Now != nil {
		now = deps.Now()
	}
	expired := tokens.IsExpired(now)
	if expired {
		_, _ = fmt.Fprintln(stdout, "Token expired: yes")
		_, _ = fmt.Fprintln(stdout, "Overall: TOKEN_EXPIRED")
		return errors.New("HH API token is expired; run hh-api auth")
	}
	_, _ = fmt.Fprintln(stdout, "Token expired: no")

	oauthConfig := hhAPIReadOAuthConfig(cfg)
	_, client, err := newHHAPIClientWithStore(cfg, oauthConfig, deps, hhAPINoRefreshTokenStore{tokens: tokens})
	if err != nil {
		return err
	}
	user, err := client.CurrentUser(ctx)
	if err != nil {
		_, _ = fmt.Fprintln(stdout, "/me: ERROR")
		_, _ = fmt.Fprintln(stdout, "Overall: AUTH_REQUIRED")
		return fmt.Errorf("HH API doctor /me failed: %w", err)
	}
	_, _ = fmt.Fprintln(stdout, "/me: AUTH_OK")
	_, _ = fmt.Fprintf(stdout, "User type: %s\n", safeHHAPIUserType(user.AuthType))
	if !strings.EqualFold(strings.TrimSpace(user.AuthType), "applicant") {
		_, _ = fmt.Fprintln(stdout, "Applicant resumes: UNKNOWN")
		_, _ = fmt.Fprintln(stdout, "Overall: AUTH_REQUIRED")
		return errors.New("HH API current user is not an applicant")
	}
	if _, err := client.ReadResumes(ctx); err != nil {
		_, _ = fmt.Fprintln(stdout, "Applicant resumes: ERROR")
		_, _ = fmt.Fprintln(stdout, "Overall: AUTH_REQUIRED")
		return fmt.Errorf("HH API doctor resumes failed: %w", err)
	}
	_, _ = fmt.Fprintln(stdout, "Applicant resumes: OK")
	if err := readHHAPIDoctorVacancy(ctx, cfg, client); err != nil {
		_, _ = fmt.Fprintln(stdout, "Vacancy read: ERROR")
		_, _ = fmt.Fprintln(stdout, "Overall: AUTH_REQUIRED")
		return fmt.Errorf("HH API doctor vacancy read failed: %w", err)
	}
	_, _ = fmt.Fprintln(stdout, "Vacancy read: OK")
	_, _ = fmt.Fprintln(stdout, "Overall: AUTH_OK")
	return nil
}

// hhAPINoRefreshTokenStore gives doctor a read-only view of the already
// inspected token pair. Removing the refresh token from this view prevents
// APIHHClient's normal 401 refresh path and makes token-file mutation
// impossible during diagnostics.
type hhAPINoRefreshTokenStore struct {
	tokens hhapi.OAuthTokens
}

var _ hhapi.TokenStore = (*hhAPINoRefreshTokenStore)(nil)

func (s hhAPINoRefreshTokenStore) Load(ctx context.Context) (hhapi.OAuthTokens, error) {
	if ctx == nil {
		return hhapi.OAuthTokens{}, errors.New("doctor token context is invalid")
	}
	if err := ctx.Err(); err != nil {
		return hhapi.OAuthTokens{}, err
	}
	tokens := s.tokens
	tokens.RefreshToken = ""
	return tokens, nil
}

func (hhAPINoRefreshTokenStore) Save(context.Context, hhapi.OAuthTokens) error {
	return errors.New("doctor token storage is disabled")
}

func (hhAPINoRefreshTokenStore) Delete(context.Context) error {
	return errors.New("doctor token deletion is disabled")
}

func runHHAPILogout(ctx context.Context, cfg Config, stdout io.Writer) error {
	_, _ = fmt.Fprintln(stdout, "HH API Logout")
	tokenPath := strings.TrimSpace(cfg.HHAPITokenFile)
	if tokenPath == "" {
		_, _ = fmt.Fprintln(stdout, "Token file: absent")
		_, _ = fmt.Fprintln(stdout, "Deleted: NO")
		return errors.New("HH API token file is not configured")
	}
	_, statErr := os.Stat(tokenPath)
	present := statErr == nil
	if statErr != nil && !os.IsNotExist(statErr) {
		_, _ = fmt.Fprintln(stdout, "Token file: unavailable")
		_, _ = fmt.Fprintln(stdout, "Deleted: NO")
		return errors.New("HH API token file is unavailable")
	}
	if present {
		_, _ = fmt.Fprintln(stdout, "Token file: present")
	} else {
		_, _ = fmt.Fprintln(stdout, "Token file: absent")
	}
	if err := hhapi.NewFileTokenStore(tokenPath).Delete(ctx); err != nil {
		_, _ = fmt.Fprintln(stdout, "Deleted: NO")
		return errors.New("HH API token file could not be deleted")
	}
	if present {
		_, _ = fmt.Fprintln(stdout, "Deleted: YES")
	} else {
		_, _ = fmt.Fprintln(stdout, "Deleted: NO")
	}
	_, _ = fmt.Fprintln(stdout, "Access token logged: NO")
	_, _ = fmt.Fprintln(stdout, "Refresh token logged: NO")
	return nil
}

func hhAPIOAuthConfig(cfg Config) (hhapi.OAuthConfig, error) {
	if strings.TrimSpace(cfg.HHOAuthAuthorizeURL) == "" || strings.TrimSpace(cfg.HHOAuthTokenURL) == "" ||
		strings.TrimSpace(cfg.HHOAuthClientID) == "" || strings.TrimSpace(cfg.HHOAuthClientSecret) == "" ||
		strings.TrimSpace(cfg.HHOAuthRedirectURI) == "" || strings.TrimSpace(cfg.HHOAuthUserAgent) == "" {
		return hhapi.OAuthConfig{}, errors.New("HH API OAuth configuration is incomplete")
	}
	return hhapi.OAuthConfig{
		AuthorizeURL: cfg.HHOAuthAuthorizeURL,
		TokenURL:     cfg.HHOAuthTokenURL,
		ClientID:     cfg.HHOAuthClientID,
		ClientSecret: cfg.HHOAuthClientSecret,
		RedirectURI:  cfg.HHOAuthRedirectURI,
		UserAgent:    cfg.HHOAuthUserAgent,
	}, nil
}

func hhAPIReadOAuthConfig(cfg Config) hhapi.OAuthConfig {
	return hhapi.OAuthConfig{
		AuthorizeURL: cfg.HHOAuthAuthorizeURL,
		TokenURL:     cfg.HHOAuthTokenURL,
		ClientID:     cfg.HHOAuthClientID,
		ClientSecret: cfg.HHOAuthClientSecret,
		RedirectURI:  cfg.HHOAuthRedirectURI,
		UserAgent:    cfg.HHOAuthUserAgent,
	}
}

func newHHAPIClient(cfg Config, oauthConfig hhapi.OAuthConfig, deps HHAPICommandDeps) (*hhapi.FileTokenStore, *hhapi.APIHHClient, error) {
	store := hhapi.NewFileTokenStore(strings.TrimSpace(cfg.HHAPITokenFile))
	_, client, err := newHHAPIClientWithStore(cfg, oauthConfig, deps, store)
	return store, client, err
}

func newHHAPIClientWithStore(cfg Config, oauthConfig hhapi.OAuthConfig, deps HHAPICommandDeps, store hhapi.TokenStore) (*hhapi.FileTokenStore, *hhapi.APIHHClient, error) {
	baseURL, err := url.Parse(strings.TrimSpace(cfg.HHAPIBaseURL))
	if err != nil || baseURL.Scheme == "" || baseURL.Host == "" || (baseURL.Scheme != "http" && baseURL.Scheme != "https") {
		return nil, nil, errors.New("HH API base URL is invalid")
	}
	if strings.TrimSpace(cfg.HHOAuthUserAgent) == "" {
		return nil, nil, errors.New("HH API User-Agent is required")
	}
	if oauthConfig.HTTPClient == nil {
		oauthConfig.HTTPClient = deps.HTTPClient
	}
	if oauthConfig.UserAgent == "" {
		oauthConfig.UserAgent = cfg.HHOAuthUserAgent
	}
	if oauthConfig.Now == nil && deps.Now != nil {
		oauthConfig.Now = deps.Now
	}
	client, err := hhapi.NewAPIHHClient(hhapi.APIClientOptions{
		BaseURL: baseURL, HTTPClient: deps.HTTPClient, TokenStore: store, OAuthConfig: oauthConfig,
		UserAgent: cfg.HHOAuthUserAgent, Now: deps.Now,
	})
	if err != nil {
		return nil, nil, errors.New("HH API client configuration is invalid")
	}
	fileStore, _ := store.(*hhapi.FileTokenStore)
	return fileStore, client, nil
}

func readHHAPIDoctorVacancy(ctx context.Context, cfg Config, client *hhapi.APIHHClient) error {
	if id, ok := hhAPIProbeVacancyID(cfg.BrowserTraceVacancyURL); ok {
		_, err := client.ReadVacancyDetail(ctx, id)
		return err
	}
	_, err := client.ReadVacancies(ctx, "")
	return err
}

func hhAPIProbeVacancyID(raw string) (int, bool) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return 0, false
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	for index := 0; index+1 < len(parts); index++ {
		if parts[index] != "vacancy" {
			continue
		}
		id, err := strconv.Atoi(parts[index+1])
		return id, err == nil && id > 0
	}
	return 0, false
}

func parseHHAPICallbackURL(raw string) (hhapi.AuthorizationCallback, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || !parsed.IsAbs() || parsed.Host == "" {
		return hhapi.AuthorizationCallback{}, errors.New("callback URL is invalid")
	}
	query := parsed.Query()
	return hhapi.AuthorizationCallback{
		Code:             query.Get("code"),
		State:            query.Get("state"),
		Error:            query.Get("error"),
		ErrorDescription: query.Get("error_description"),
	}, nil
}

func readHHAPIRedirectURL(input io.Reader) (string, error) {
	if input == nil {
		return "", errors.New("stdin is unavailable")
	}
	reader := bufio.NewReader(io.LimitReader(input, hhAPIMaxManualRedirectBytes))
	line, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", errors.New("redirect URL could not be read")
	}
	line = strings.TrimSpace(line)
	if line == "" || len(line) > hhAPIMaxManualRedirectBytes {
		return "", errors.New("redirect URL is empty or too large")
	}
	return line, nil
}

func isLoopbackRedirectURI(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

func receiveHHAPILocalhostCallback(ctx context.Context, redirectURI string) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	parsed, err := url.Parse(strings.TrimSpace(redirectURI))
	if err != nil || !isLoopbackRedirectURI(redirectURI) || parsed.Port() == "" {
		return "", errors.New("localhost callback configuration is invalid")
	}
	listener, err := net.Listen("tcp", parsed.Host)
	if err != nil {
		return "", errors.New("localhost callback listener could not be started")
	}
	defer listener.Close()

	callback := make(chan string, 1)
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != parsed.Path {
			http.NotFound(w, r)
			return
		}
		callbackURL := *r.URL
		callbackURL.Scheme = parsed.Scheme
		callbackURL.Host = parsed.Host
		select {
		case callback <- callbackURL.String():
		default:
		}
		_, _ = io.WriteString(w, "Authorization received. You may close this window.")
	})}
	serveErr := make(chan error, 1)
	go func() {
		err := server.Serve(listener)
		if !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
		}
	}()
	defer server.Shutdown(context.Background())

	select {
	case value := <-callback:
		return value, nil
	case <-serveErr:
		return "", errors.New("localhost callback receiver failed")
	case <-ctx.Done():
		return "", errors.New("localhost callback receiver canceled")
	}
}

func openHHAPIURL(ctx context.Context, value string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	var command string
	var args []string
	switch stdRuntime.GOOS {
	case "darwin":
		command, args = "open", []string{value}
	case "windows":
		command, args = "rundll32", []string{"url.dll,FileProtocolHandler", value}
	default:
		command, args = "xdg-open", []string{value}
	}
	if err := exec.CommandContext(ctx, command, args...).Run(); err != nil {
		return errors.New("browser opener failed")
	}
	return nil
}

func safeHHAPIUserType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "applicant":
		return "applicant"
	case "employer":
		return "employer"
	default:
		return "UNKNOWN"
	}
}
