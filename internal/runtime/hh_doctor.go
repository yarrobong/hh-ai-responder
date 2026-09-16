package runtime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	HHAuthSessionExpired       = "AUTH_SESSION_EXPIRED"
	HHAuthRequired             = "AUTH_REQUIRED"
	HHAccessForbidden          = "ACCESS_FORBIDDEN"
	HHAntiBotOrChallenge       = "ANTI_BOT_OR_CHALLENGE"
	HHRequestShapeRejected     = "REQUEST_SHAPE_REJECTED"
	HHRateLimitOrTempBlock     = "RATE_LIMIT_OR_TEMPORARY_BLOCK"
	HHCookieDomainPathMismatch = "COOKIE_DOMAIN_OR_PATH_MISMATCH"
	HHBadRedirect              = "BAD_REDIRECT"
	HHUnknown403               = "UNKNOWN_403"
)

var authCookieNames = map[string]struct{}{
	"crypted_hhuid": {}, "crypted_id": {}, "hhrole": {}, "hhtoken": {},
	"hhuid": {}, "hhul": {}, "_xsrf": {},
}

type HHRedirectHop struct {
	Status      int
	URL         string
	CookieNames []string
}

// HHReadDiagnostic is deliberately limited to request/response metadata.
// It never contains cookie values, authorization headers, or response bodies.
type HHReadDiagnostic struct {
	Purpose             string
	Method              string
	Endpoint            string
	InitialURL          string
	FinalURL            string
	Redirects           []HHRedirectHop
	Status              int
	ContentType         string
	ResponseSize        int
	SafeResponseHeaders map[string]string
	RequestCookieNames  []string
	BodySignals         []string
}

type HHReadAccessError struct {
	Diagnostic     HHReadDiagnostic
	Classification string
	Action         string
}

func (e *HHReadAccessError) Error() string {
	if e == nil {
		return "HH read access unavailable"
	}
	return fmt.Sprintf("HH read access unavailable: classification=%s action=%s request=%s %s final=%s status=%d content_type=%s response_size=%d diagnostic=%s writes_attempted=0",
		e.Classification, e.Action, e.Diagnostic.Method, e.Diagnostic.Endpoint, safeURLForOutput(e.Diagnostic.FinalURL), e.Diagnostic.Status, e.Diagnostic.ContentType, e.Diagnostic.ResponseSize, strings.Join(e.Diagnostic.BodySignals, ","))
}

type hhTraceContextKey struct{}

type hhRequestTrace struct {
	mu   sync.Mutex
	hops []HHRedirectHop
}

func withHHRequestTrace(ctx context.Context) (context.Context, *hhRequestTrace) {
	trace := &hhRequestTrace{}
	return context.WithValue(ctx, hhTraceContextKey{}, trace), trace
}

func hhRequestTraceFromContext(ctx context.Context) *hhRequestTrace {
	if ctx == nil {
		return nil
	}
	trace, _ := ctx.Value(hhTraceContextKey{}).(*hhRequestTrace)
	return trace
}

func (t *hhRequestTrace) record(hop HHRedirectHop) {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.hops = append(t.hops, hop)
	t.mu.Unlock()
}

func (t *hhRequestTrace) snapshot() []HHRedirectHop {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]HHRedirectHop(nil), t.hops...)
}

// hhDiagnosticTransport records every actual network hop while preserving the
// configured transport. Cookie values are intentionally not inspected.
type hhDiagnosticTransport struct {
	base http.RoundTripper
}

func (t *hhDiagnosticTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	resp, err := base.RoundTrip(req)
	if err == nil && resp != nil {
		if trace := hhRequestTraceFromContext(req.Context()); trace != nil {
			trace.record(HHRedirectHop{Status: resp.StatusCode, URL: req.URL.String(), CookieNames: cookieNamesList(req)})
		}
	}
	return resp, err
}

func safeResponseHeaders(headers http.Header) map[string]string {
	result := map[string]string{}
	for _, name := range []string{"Content-Type", "Content-Length", "Date", "Retry-After", "Server", "Via", "X-Correlation-ID", "X-Request-ID", "CF-Ray"} {
		if value := strings.TrimSpace(headers.Get(name)); value != "" && !strings.ContainsAny(value, "\r\n") && len(value) <= 256 {
			result[name] = value
		}
	}
	return result
}

func safeURLForOutput(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return "[invalid-url]"
	}
	value := parsed.Scheme + "://" + parsed.Host + parsed.EscapedPath()
	if value == parsed.Scheme+"://"+parsed.Host {
		value += "/"
	}
	if len(parsed.Query()) > 0 {
		keys := make([]string, 0, len(parsed.Query()))
		for key := range parsed.Query() {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		value += "?[query_keys=" + strings.Join(keys, ",") + "]"
	}
	return value
}

func safeEndpoint(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "[invalid-endpoint]"
	}
	value := parsed.EscapedPath()
	if value == "" {
		value = "/"
	}
	if len(parsed.Query()) > 0 {
		keys := make([]string, 0, len(parsed.Query()))
		for key := range parsed.Query() {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		value += "?[query_keys=" + strings.Join(keys, ",") + "]"
	}
	return value
}

func isHHHost(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	return host == "hh.ru" || strings.HasSuffix(host, ".hh.ru")
}

func classifyHHReadResponse(response HHResponse, authCookies []string) (string, []string) {
	body := strings.ToLower(string(response.Body))
	forbiddenPage := strings.Contains(body, "forbiddenpage")
	loginPage := strings.Contains(body, "/account/login") || strings.Contains(body, "supernova-login-wrapper") || forbiddenPage
	loginRedirect := false
	for _, hop := range response.Redirects {
		loginRedirect = loginRedirect || strings.Contains(strings.ToLower(hop.URL), "/account/login")
	}
	challenge := strings.Contains(body, "cf-chl-") || strings.Contains(body, "cloudflare challenge") || strings.Contains(body, "ddos-guard challenge") || strings.Contains(body, "captcha-container")
	signals := []string{}
	if forbiddenPage {
		signals = append(signals, "ForbiddenPage")
	}
	if strings.Contains(body, "/account/login") {
		signals = append(signals, "account_login_link")
	}
	if challenge {
		signals = append(signals, "explicit_challenge_marker")
	}
	if response.Status == http.StatusTooManyRequests {
		return HHRateLimitOrTempBlock, signals
	}
	if response.Status == http.StatusUnauthorized {
		return HHAuthRequired, signals
	}
	if response.Status == http.StatusBadRequest || response.Status == http.StatusUnprocessableEntity {
		return HHRequestShapeRejected, signals
	}
	if response.Status == http.StatusForbidden {
		if challenge {
			return HHAntiBotOrChallenge, signals
		}
		if loginPage {
			if len(authCookies) > 0 {
				return HHAuthSessionExpired, signals
			}
			return HHAuthRequired, signals
		}
		if server := strings.ToLower(response.Headers["Server"]); strings.Contains(server, "ddos-guard") {
			return HHAntiBotOrChallenge, append(signals, "provider_guard_header")
		}
		return HHAccessForbidden, signals
	}
	if response.Status >= 300 && response.Status < 400 && !isHHHost(response.FinalURL) {
		return HHBadRedirect, signals
	}
	if response.Status >= 200 && response.Status < 300 {
		if loginRedirect {
			if len(authCookies) > 0 {
				return HHAuthSessionExpired, signals
			}
			return HHAuthRequired, signals
		}
		return "OK", signals
	}
	return HHUnknown403, signals
}

func hhReadAction(classification string) string {
	switch classification {
	case HHAuthSessionExpired:
		return "refresh HH authenticated session/cookies"
	case HHAuthRequired:
		return "authenticate in HH and provide the resulting session"
	case HHAntiBotOrChallenge:
		return "complete the provider's interactive challenge/login manually"
	case HHRateLimitOrTempBlock:
		return "wait and retry later; no aggressive retries"
	case HHBadRedirect:
		return "inspect HH redirect configuration/session"
	default:
		return "manual review required"
	}
}

func hhAccessError(purpose string, response HHResponse) error {
	classification, signals := classifyHHReadResponse(response, response.RequestCookieNames)
	initialURL := ""
	if response.URL != nil {
		initialURL = response.URL.String()
	}
	headers := map[string]string{}
	for key, value := range response.Headers {
		headers[key] = value
	}
	diagnostic := HHReadDiagnostic{Purpose: purpose, Method: http.MethodGet, Endpoint: safeEndpoint(initialURL), InitialURL: safeURLForOutput(initialURL), FinalURL: response.FinalURL, Redirects: append([]HHRedirectHop(nil), response.Redirects...), Status: response.Status, ContentType: response.ContentType, ResponseSize: len(response.Body), SafeResponseHeaders: headers, RequestCookieNames: append([]string(nil), response.RequestCookieNames...), BodySignals: signals}
	return &HHReadAccessError{Diagnostic: diagnostic, Classification: classification, Action: hhReadAction(classification)}
}

type hhDoctorCookieSummary struct {
	Parsed       int
	Expired      int
	HHDomains    int
	Secure       int
	AuthNames    []string
	AppliedNames []string
}

func (j *MemoryPersistentJar) safeSummary(now time.Time, u *url.URL) hhDoctorCookieSummary {
	result := hhDoctorCookieSummary{}
	if j == nil {
		return result
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	auth := map[string]struct{}{}
	if j.parsed > 0 {
		result.Parsed = j.parsed
	}
	for domain, cookies := range j.cookies {
		for _, cookie := range cookies {
			if j.parsed == 0 {
				result.Parsed++
			}
			if isHHCookieDomain(domain) {
				result.HHDomains++
			}
			if cookie.Secure {
				result.Secure++
			}
			if !cookie.Expires.IsZero() && cookie.Expires.Before(now) {
				result.Expired++
			}
			if _, ok := authCookieNames[cookie.Name]; ok {
				auth[cookie.Name] = struct{}{}
			}
		}
	}
	for name := range auth {
		result.AuthNames = append(result.AuthNames, name)
	}
	sort.Strings(result.AuthNames)
	if u != nil {
		host := strings.ToLower(u.Hostname())
		for domain, cookies := range j.cookies {
			domainNoDot := strings.ToLower(strings.TrimPrefix(domain, "."))
			if domain != host && host != domainNoDot && !strings.HasSuffix(host, "."+domainNoDot) {
				continue
			}
			for _, cookie := range cookies {
				if !cookie.Expires.IsZero() && cookie.Expires.Before(now) {
					continue
				}
				if cookie.Secure && u.Scheme != "https" {
					continue
				}
				result.AppliedNames = append(result.AppliedNames, cookie.Name)
			}
		}
		sort.Strings(result.AppliedNames)
	}
	return result
}

func isHHCookieDomain(domain string) bool {
	domain = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(domain), "."))
	return domain == "hh.ru" || strings.HasSuffix(domain, ".hh.ru")
}

func applyHHReadHeaders(request *http.Request) {
	request.Header.Set("User-Agent", userAgent)
	request.Header.Set("Accept-Language", acceptLanguageHeader)
	request.Header.Set("Accept", acceptHeader)
	request.Header.Set("Sec-CH-UA", secCHUAHeader)
	request.Header.Set("Sec-CH-UA-Mobile", "?0")
	request.Header.Set("Sec-CH-UA-Platform", `"Windows"`)
	request.Header.Set("Sec-Fetch-Site", "same-origin")
	request.Header.Set("Sec-Fetch-Mode", "cors")
	request.Header.Set("Sec-Fetch-Dest", "empty")
}

func doctorBaseURL(cfg Config) (*url.URL, error) {
	raw := strings.TrimSpace(cfg.SearchURL)
	if len(cfg.SearchURLs) > 0 {
		raw = strings.TrimSpace(cfg.SearchURLs[0])
	}
	if raw == "" {
		raw = "https://hh.ru"
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, errors.New("HH doctor base URL is invalid")
	}
	return &url.URL{Scheme: parsed.Scheme, Host: parsed.Host}, nil
}

func doctorProbe(ctx context.Context, client *http.Client, base *url.URL, purpose, endpoint string) (HHResponse, error) {
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return HHResponse{}, err
	}
	requestURL := base.ResolveReference(parsed)
	traceContext, trace := withHHRequestTrace(ctx)
	request, err := http.NewRequestWithContext(traceContext, http.MethodGet, requestURL.String(), nil)
	if err != nil {
		return HHResponse{}, err
	}
	applyHHReadHeaders(request)
	response, err := client.Do(request)
	if err != nil {
		return HHResponse{}, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return HHResponse{}, err
	}
	hops := trace.snapshot()
	finalURL := requestURL.String()
	if response.Request != nil && response.Request.URL != nil {
		finalURL = response.Request.URL.String()
	}
	if len(hops) == 0 {
		hops = []HHRedirectHop{{Status: response.StatusCode, URL: finalURL, CookieNames: cookieNamesList(response.Request)}}
	}
	cookies := firstHopCookieNames(hops)
	return HHResponse{URL: requestURL, FinalURL: finalURL, Redirects: hops, Status: response.StatusCode, ContentType: response.Header.Get("Content-Type"), Body: body, Headers: safeResponseHeaders(response.Header), RequestCookieNames: cookies, Purpose: purpose}, nil
}

func firstHopCookieNames(hops []HHRedirectHop) []string {
	if len(hops) == 0 {
		return nil
	}
	return append([]string(nil), hops[0].CookieNames...)
}

func renderHHDoctorProbe(label string, response HHResponse, classification string, signals []string) string {
	redirects := make([]string, 0, len(response.Redirects))
	for _, hop := range response.Redirects {
		redirects = append(redirects, strconv.Itoa(hop.Status)+" "+safeURLForOutput(hop.URL))
	}
	if len(redirects) == 0 {
		redirects = []string{"none"}
	}
	state := "FAILED"
	if response.Status >= 200 && response.Status < 300 {
		state = "OK"
	}
	line := fmt.Sprintf("%s: %s\n  request: %s %s\n  final_url: %s\n  status: %d\n  content_type: %s\n  response_size: %d\n  redirect_chain: %s\n  cookies_attached: %d [%s]\n  classification: %s",
		label, state, http.MethodGet, safeEndpoint(response.URL.String()), safeURLForOutput(response.FinalURL), response.Status, response.ContentType, len(response.Body), strings.Join(redirects, " -> "), len(response.RequestCookieNames), strings.Join(response.RequestCookieNames, ","), classification)
	headerNames := make([]string, 0, len(response.Headers))
	for name := range response.Headers {
		headerNames = append(headerNames, name)
	}
	sort.Strings(headerNames)
	headerValues := make([]string, 0, len(headerNames))
	for _, name := range headerNames {
		headerValues = append(headerValues, name+"="+response.Headers[name])
	}
	line += "\n  safe_response_headers: " + strings.Join(headerValues, "; ")
	if len(signals) > 0 {
		line += "\n  sanitized_diagnostic: " + strings.Join(signals, ",")
	}
	return line
}

func runHHDoctor(cfg Config, stdout, stderr io.Writer) error {
	base, err := doctorBaseURL(cfg)
	if err != nil {
		return err
	}
	jar, err := NewMemoryPersistentJar(cfg.CookiesPath)
	if err != nil {
		return err
	}
	// A diagnostic must not persist Set-Cookie changes or prune local data.
	jar.persistPath = ""
	summary := jar.safeSummary(time.Now(), base)
	client := &http.Client{Jar: jar, Timeout: 30 * time.Second, Transport: &hhDiagnosticTransport{base: http.DefaultTransport}}
	public, err := doctorProbe(context.Background(), client, base, "public vacancy search", "/search/vacancy?items_on_page=1&search_period=1&order_by=publication_time")
	if err != nil {
		return err
	}
	publicClass, publicSignals := classifyHHReadResponse(public, summary.AppliedNames)
	profile, err := doctorProbe(context.Background(), client, base, "authenticated profile read", "/applicant/my_resumes")
	if err != nil {
		return err
	}
	profileClass, profileSignals := classifyHHReadResponse(profile, profile.RequestCookieNames)

	fmt.Fprintln(stdout, "HH Read Doctor")
	cookieFileState := "MISSING"
	if strings.TrimSpace(cfg.CookiesPath) != "" {
		if _, statErr := os.Stat(cfg.CookiesPath); statErr == nil {
			cookieFileState = "FOUND"
		}
	}
	fmt.Fprintf(stdout, "Cookie file: %s\nCookie format: Netscape HTTP Cookie File\nCookies parsed: %d\nExpired cookies: %d\nHH-domain cookies: %d\nSecure cookies: %d\nhttpOnly: unavailable in Netscape format\nsameSite: unavailable in Netscape format\nAuth-related cookie names: %s\n", cookieFileState, summary.Parsed, summary.Expired, summary.HHDomains, summary.Secure, strings.Join(summary.AuthNames, ","))
	fmt.Fprintln(stdout, renderHHDoctorProbe("Public HH read", public, publicClass, publicSignals))
	fmt.Fprintln(stdout, renderHHDoctorProbe("Authenticated profile read", profile, profileClass, profileSignals))
	if profileClass == "OK" {
		fmt.Fprintln(stdout, "Vacancy search: NOT RUN (doctor probe only)")
		fmt.Fprintln(stdout, "Overall: AUTHENTICATED_READ_OK")
	} else {
		fmt.Fprintln(stdout, "Vacancy search: NOT RUN (authenticated read failed; fail-fast)")
		fmt.Fprintf(stdout, "Overall: %s\nUser action: %s\n", profileClass, hhReadAction(profileClass))
	}
	fmt.Fprintln(stdout, "Writes: DISABLED")
	fmt.Fprintln(stdout, "HH writes attempted: 0")
	return nil
}

func (r *HHAIResponder) configureReadDiagnostics() {
	if r == nil || r.client == nil {
		return
	}
	base := r.client.Transport
	if _, ok := base.(*hhDiagnosticTransport); ok {
		return
	}
	r.client.Transport = &hhDiagnosticTransport{base: base}
}

func (r *HHAIResponder) profileReadAccessError(response HHResponse) error {
	return hhAccessError("authenticated profile read", response)
}
