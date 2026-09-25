package hhwebsession

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type RequestDoer interface {
	Do(*http.Request) (*http.Response, error)
}

type Options struct {
	BaseURL      *url.URL
	AllowedHosts []string
	Now          func() time.Time
	Transport    http.RoundTripper
	UserAgent    string
}

type Session struct {
	jar     *persistentJar
	baseURL *url.URL
	read    RequestDoer
	write   RequestDoer
	maxHops int
}

type CookieMetadata struct {
	Name     string
	Domain   string
	Path     string
	Expires  time.Time
	Session  bool
	Secure   bool
	HttpOnly bool
}

type Metadata struct {
	Cookies []CookieMetadata
}

func New(path string, options Options) (*Session, error) {
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.BaseURL != nil && options.BaseURL.Scheme != "" && options.BaseURL.Hostname() == "" {
		return nil, errors.New("HH session base URL has no host")
	}
	jar, err := loadPersistentJar(path, options)
	if err != nil {
		return nil, fmt.Errorf("load HH cookies: %w", err)
	}
	baseURL := options.BaseURL
	if baseURL == nil {
		baseURL = &url.URL{Scheme: "https", Host: "hh.ru"}
	}
	transport := options.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	maxHops := 10
	readClient := &http.Client{Jar: jar, Transport: transport, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= maxHops {
			return ErrTooManyRedirects
		}
		if !jar.hostAllowed(req.URL.Hostname()) {
			from := (*url.URL)(nil)
			if len(via) > 0 {
				from = via[len(via)-1].URL
			}
			return &RedirectError{From: from, To: req.URL, Kind: redirectKind(req.URL)}
		}
		return nil
	}}
	writeClient := &http.Client{Jar: jar, Transport: transport, CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	return &Session{
		jar:     jar,
		baseURL: baseURL,
		read:    restrictedClient{client: readClient, methods: map[string]struct{}{http.MethodGet: {}, http.MethodHead: {}}},
		write:   restrictedClient{client: writeClient, methods: map[string]struct{}{http.MethodPost: {}}},
		maxHops: maxHops,
	}, nil
}

func (s *Session) ReadClient() RequestDoer { return s.read }

func (s *Session) WriteClient() RequestDoer { return s.write }

func (s *Session) XSRFToken(base *url.URL) (string, error) {
	if base == nil {
		base = s.baseURL
	}
	if base == nil {
		return "", errors.New("HH session XSRF base URL is required")
	}
	for _, cookie := range s.jar.Cookies(base) {
		if cookie.Name == "_xsrf" {
			if cookie.Value == "" {
				return "", errors.New("HH session XSRF cookie is empty")
			}
			return cookie.Value, nil
		}
	}
	return "", errors.New("HH session XSRF cookie is missing")
}

func (s *Session) SafeMetadata(now time.Time) Metadata {
	return s.jar.safeMetadata(now)
}

func (s *Session) PersistenceError() error {
	return s.jar.PersistenceError()
}

type restrictedClient struct {
	client  *http.Client
	methods map[string]struct{}
}

func (c restrictedClient) Do(req *http.Request) (*http.Response, error) {
	if req == nil {
		return nil, ErrMethodNotAllowed
	}
	if _, ok := c.methods[strings.ToUpper(req.Method)]; !ok {
		return nil, ErrMethodNotAllowed
	}
	return c.client.Do(req)
}
