package hhwebsession

import (
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type persistentJar struct {
	mu           sync.Mutex
	cookies      []storedCookie
	persistPath  string
	allowedHosts []string
	now          func() time.Time
	persistErr   error
}

func newPersistentJar(path string, now time.Time) *persistentJar {
	return &persistentJar{persistPath: path, now: func() time.Time { return now }, allowedHosts: []string{"hh.ru"}}
}

func loadPersistentJar(path string, options Options) (*persistentJar, error) {
	now := options.Now()
	jar := &persistentJar{persistPath: path, allowedHosts: normalizeAllowedHosts(options.AllowedHosts), now: options.Now}
	if len(jar.allowedHosts) == 0 {
		jar.allowedHosts = []string{"hh.ru"}
	}
	if strings.TrimSpace(path) == "" {
		return jar, nil
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return jar, nil
	}
	if err != nil {
		return nil, err
	}
	cookies, err := parseNetscapeCookies(data, now)
	if err != nil {
		return nil, err
	}
	for _, cookie := range cookies {
		if !jar.domainAllowed(cookie.Domain) {
			return nil, errors.New("cookie file contains a domain outside the configured HH host")
		}
	}
	jar.cookies = cookies
	return jar, nil
}

func (j *persistentJar) Cookies(u *url.URL) []*http.Cookie {
	if u == nil {
		return nil
	}
	now := j.now()
	j.mu.Lock()
	defer j.mu.Unlock()
	host := normalizedHost(u.Hostname())
	matched := make([]*http.Cookie, 0)
	active := j.cookies[:0]
	for _, item := range j.cookies {
		if !item.Expires.IsZero() && !item.Expires.After(now) {
			continue
		}
		active = append(active, item)
		if !cookieDomainMatches(item, host) || !cookiePathMatches(item.Path, u.EscapedPath()) || (item.Secure && u.Scheme != "https") {
			continue
		}
		copy := item.Cookie
		matched = append(matched, &copy)
	}
	j.cookies = active
	return matched
}

func (j *persistentJar) SetCookies(u *url.URL, cookies []*http.Cookie) {
	if u == nil || len(cookies) == 0 {
		return
	}
	now := j.now()
	host := normalizedHost(u.Hostname())
	j.mu.Lock()
	defer j.mu.Unlock()
	changed := false
	for _, incoming := range cookies {
		if incoming == nil {
			continue
		}
		if !j.hostAllowed(host) {
			j.markUnsafeLocked(errors.New("cookie response origin is outside the configured host scope"))
			continue
		}
		if incoming.Name == "" {
			j.markUnsafeLocked(errors.New("cookie response contains an empty cookie name"))
			continue
		}
		item := storedCookie{Cookie: *incoming}
		path := incoming.Path
		if path == "" {
			path = defaultCookiePath(u.Path)
		}
		if !strings.HasPrefix(path, "/") {
			j.markUnsafeLocked(errors.New("cookie response contains an invalid cookie path"))
			continue
		}
		item.Path = path
		if incoming.Domain == "" {
			item.Domain = host
			item.hostOnly = true
		} else {
			item.Domain = normalizedHost(incoming.Domain)
			item.hostOnly = false
			if !j.domainAllowed(item.Domain) || item.Domain != host {
				j.markUnsafeLocked(errors.New("cookie response attempts to widen its domain scope"))
				continue
			}
			if j.hasActiveHostOnlyCookie(item.Name, item.Path, host) {
				j.markUnsafeLocked(errors.New("cookie response attempts to convert a host-only cookie"))
				continue
			}
		}
		item.Raw = ""
		item.RawExpires = ""
		item.Unparsed = nil
		index := j.cookieIndex(item)
		deleteCookie := item.MaxAge < 0 || (!item.Expires.IsZero() && !item.Expires.After(now))
		if deleteCookie {
			if index >= 0 {
				j.cookies = append(j.cookies[:index], j.cookies[index+1:]...)
				changed = true
			}
			continue
		}
		item.MaxAge = 0
		if index >= 0 {
			if !sameStoredCookie(j.cookies[index], item) {
				j.cookies[index] = item
				changed = true
			}
		} else {
			j.cookies = append(j.cookies, item)
			changed = true
		}
	}
	if changed {
		j.persistErr = j.persistLocked()
	}
}

func (j *persistentJar) markUnsafeLocked(err error) {
	if j.persistErr == nil {
		j.persistErr = err
	}
}

func (j *persistentJar) hasActiveHostOnlyCookie(name, path, host string) bool {
	now := j.now()
	for _, existing := range j.cookies {
		if existing.Name == name && existing.Path == path && existing.hostOnly && existing.Domain == host && (existing.Expires.IsZero() || existing.Expires.After(now)) {
			return true
		}
	}
	return false
}

func (j *persistentJar) cookieIndex(item storedCookie) int {
	for index, existing := range j.cookies {
		if existing.Name == item.Name && existing.Path == item.Path && existing.Domain == item.Domain && existing.hostOnly == item.hostOnly {
			return index
		}
	}
	return -1
}

func (j *persistentJar) PersistenceError() error {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.persistErr
}

func (j *persistentJar) persistLocked() error {
	if strings.TrimSpace(j.persistPath) == "" {
		return nil
	}
	data, err := serializeNetscapeCookiesForHosts(j.cookies, j.allowedHosts)
	if err != nil {
		return err
	}
	dir := filepath.Dir(j.persistPath)
	tmp, err := os.CreateTemp(dir, ".cookies-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if err := tmp.Chmod(0600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, j.persistPath); err != nil {
		return err
	}
	return nil
}

func (j *persistentJar) safeMetadata(now time.Time) Metadata {
	j.mu.Lock()
	defer j.mu.Unlock()
	metadata := Metadata{Cookies: make([]CookieMetadata, 0, len(j.cookies))}
	for _, item := range j.cookies {
		if !item.Expires.IsZero() && !item.Expires.After(now) {
			continue
		}
		metadata.Cookies = append(metadata.Cookies, CookieMetadata{
			Name:     item.Name,
			Domain:   item.Domain,
			Path:     item.Path,
			Expires:  item.Expires,
			Session:  item.Expires.IsZero(),
			Secure:   item.Secure,
			HttpOnly: item.HttpOnly,
		})
	}
	return metadata
}

func sameStoredCookie(left, right storedCookie) bool {
	return left.Name == right.Name && left.Value == right.Value && left.Domain == right.Domain && left.Path == right.Path && left.Secure == right.Secure && left.HttpOnly == right.HttpOnly && left.Expires.Equal(right.Expires) && left.hostOnly == right.hostOnly
}

func cookieDomainMatches(cookie storedCookie, host string) bool {
	if cookie.hostOnly {
		return host == cookie.Domain
	}
	return domainMatches(host, cookie.Domain)
}

func domainMatches(host, domain string) bool {
	host = normalizedHost(host)
	domain = normalizedHost(domain)
	return host == domain || strings.HasSuffix(host, "."+domain)
}

func cookiePathMatches(cookiePath, requestPath string) bool {
	if cookiePath == "" {
		cookiePath = "/"
	}
	if requestPath == "" {
		requestPath = "/"
	}
	if requestPath == cookiePath {
		return true
	}
	if !strings.HasPrefix(requestPath, cookiePath) {
		return false
	}
	return strings.HasSuffix(cookiePath, "/") || (len(requestPath) > len(cookiePath) && requestPath[len(cookiePath)] == '/')
}

func defaultCookiePath(requestPath string) string {
	if requestPath == "" || requestPath[0] != '/' {
		return "/"
	}
	index := strings.LastIndex(requestPath, "/")
	if index <= 0 {
		return "/"
	}
	return requestPath[:index]
}

func normalizeAllowedHosts(hosts []string) []string {
	result := make([]string, 0, len(hosts))
	for _, host := range hosts {
		if normalized := normalizedHost(host); normalized != "" {
			result = append(result, normalized)
		}
	}
	return result
}

func normalizedHost(host string) string {
	host = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), "."))
	return strings.TrimPrefix(host, ".")
}

func (j *persistentJar) domainAllowed(domain string) bool {
	domain = normalizedHost(domain)
	for _, allowed := range j.allowedHosts {
		if domain == allowed || strings.HasSuffix(domain, "."+allowed) {
			return true
		}
	}
	return false
}

func (j *persistentJar) hostAllowed(host string) bool {
	for _, allowed := range j.allowedHosts {
		if host == allowed || strings.HasSuffix(host, "."+allowed) {
			return true
		}
	}
	return false
}
