package hhwebsession

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

const netscapeHeader = "# Netscape HTTP Cookie File\n"

type storedCookie struct {
	http.Cookie
	hostOnly bool
}

func parseNetscapeCookies(data []byte, now time.Time) ([]storedCookie, error) {
	var result []storedCookie
	scanner := bufio.NewScanner(bytes.NewReader(data))
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSuffix(scanner.Text(), "\r")
		if strings.TrimSpace(line) == "" || (strings.HasPrefix(line, "#") && !strings.HasPrefix(line, "#HttpOnly_")) {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) != 7 {
			return nil, fmt.Errorf("invalid Netscape cookie row at line %d", lineNo)
		}
		domain, hostOnly, httpOnly, err := parseNetscapeDomain(parts[0])
		if err != nil {
			return nil, fmt.Errorf("invalid cookie domain at line %d: %w", lineNo, err)
		}
		if parts[1] != "TRUE" && parts[1] != "FALSE" {
			return nil, fmt.Errorf("invalid cookie include-subdomains flag at line %d", lineNo)
		}
		if (parts[1] == "TRUE") == hostOnly {
			return nil, fmt.Errorf("cookie domain flag mismatch at line %d", lineNo)
		}
		if parts[2] == "" || !strings.HasPrefix(parts[2], "/") {
			return nil, fmt.Errorf("invalid cookie path at line %d", lineNo)
		}
		if parts[3] != "TRUE" && parts[3] != "FALSE" {
			return nil, fmt.Errorf("invalid cookie secure flag at line %d", lineNo)
		}
		expiresUnix, err := strconv.ParseInt(parts[4], 10, 64)
		if err != nil || expiresUnix < 0 {
			return nil, fmt.Errorf("invalid cookie expiry at line %d", lineNo)
		}
		if parts[5] == "" || strings.ContainsAny(parts[5], "\r\n\t") {
			return nil, fmt.Errorf("invalid cookie name at line %d", lineNo)
		}
		cookie := storedCookie{Cookie: http.Cookie{
			Name:     parts[5],
			Value:    parts[6],
			Domain:   domain,
			Path:     parts[2],
			Secure:   parts[3] == "TRUE",
			HttpOnly: httpOnly,
		}, hostOnly: hostOnly}
		if expiresUnix != 0 {
			cookie.Expires = time.Unix(expiresUnix, 0)
			if !cookie.Expires.After(now) {
				continue
			}
		}
		result = append(result, cookie)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func parseNetscapeDomain(raw string) (domain string, hostOnly, httpOnly bool, err error) {
	if strings.HasPrefix(raw, "#HttpOnly_") {
		httpOnly = true
		raw = strings.TrimPrefix(raw, "#HttpOnly_")
	}
	if raw == "" {
		return "", false, false, errors.New("empty domain")
	}
	includeSubdomains := strings.HasPrefix(raw, ".")
	domain = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(raw)), ".")
	if !isHHHost(domain) {
		return "", false, false, errors.New("domain is outside hh.ru")
	}
	return domain, !includeSubdomains, httpOnly, nil
}

func serializeNetscapeCookies(cookies []storedCookie) ([]byte, error) {
	return serializeNetscapeCookiesForHosts(cookies, []string{"hh.ru"})
}

func serializeNetscapeCookiesForHosts(cookies []storedCookie, allowedHosts []string) ([]byte, error) {
	ordered := append([]storedCookie(nil), cookies...)
	sort.SliceStable(ordered, func(i, j int) bool {
		left, right := ordered[i], ordered[j]
		if left.Domain != right.Domain {
			return left.Domain < right.Domain
		}
		if left.Path != right.Path {
			return left.Path < right.Path
		}
		return left.Name < right.Name
	})
	var builder strings.Builder
	builder.WriteString(netscapeHeader)
	builder.WriteString("# This is a generated file! Do not edit.\n\n")
	for _, item := range ordered {
		if item.Name == "" {
			return nil, errors.New("cookie name is required")
		}
		domain := strings.TrimPrefix(strings.ToLower(item.Domain), ".")
		if !hostMatchesAllowed(domain, allowedHosts) {
			return nil, errors.New("cookie domain is outside hh.ru")
		}
		path := item.Path
		if path == "" {
			path = "/"
		}
		if !strings.HasPrefix(path, "/") {
			return nil, errors.New("cookie path must start with /")
		}
		domainField := domain
		includeSubdomains := "FALSE"
		if !item.hostOnly {
			domainField = "." + domain
			includeSubdomains = "TRUE"
		}
		if item.HttpOnly {
			domainField = "#HttpOnly_" + domainField
		}
		expires := int64(0)
		if !item.Expires.IsZero() {
			expires = item.Expires.Unix()
		}
		secure := "FALSE"
		if item.Secure {
			secure = "TRUE"
		}
		builder.WriteString(strings.Join([]string{
			domainField,
			includeSubdomains,
			path,
			secure,
			strconv.FormatInt(expires, 10),
			item.Name,
			item.Value,
		}, "\t"))
		builder.WriteByte('\n')
	}
	return []byte(builder.String()), nil
}

func isHHHost(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), "."))
	return host == "hh.ru" || strings.HasSuffix(host, ".hh.ru")
}

func hostMatchesAllowed(host string, allowedHosts []string) bool {
	host = normalizedHost(host)
	for _, allowed := range allowedHosts {
		allowed = normalizedHost(allowed)
		if host == allowed || strings.HasSuffix(host, "."+allowed) {
			return true
		}
	}
	return false
}
