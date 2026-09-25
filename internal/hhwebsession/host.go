package hhwebsession

import (
	"errors"
	"net/url"
	"strings"
)

// ValidateHHWebBaseURL is the production boundary for cookie-web transport
// destinations. Test servers must be injected through an explicit transport
// seam; they must not be accepted through ordinary HH configuration.
func ValidateHHWebBaseURL(base *url.URL) error {
	if base == nil || base.Scheme != "https" || base.Host == "" || base.User != nil || base.Port() != "" {
		return errors.New("HH cookie-web base URL must be an HTTPS HH host without userinfo or port")
	}
	if !validHHHostname(base.Hostname()) {
		return errors.New("HH cookie-web base URL host is outside hh.ru")
	}
	return nil
}

func validHHHostname(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" || strings.HasSuffix(host, ".") || len(host) > 253 {
		return false
	}
	if host != "hh.ru" && !strings.HasSuffix(host, ".hh.ru") {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, r := range label {
			if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' {
				return false
			}
		}
	}
	return true
}
