package hhwebsession

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

var (
	ErrMethodNotAllowed = errors.New("HH session client does not permit this method")
	ErrExternalRedirect = errors.New("HH session rejected an external redirect")
	ErrTooManyRedirects = errors.New("HH session exceeded redirect limit")
)

type RedirectError struct {
	From *url.URL
	To   *url.URL
	Kind string
}

func (e *RedirectError) Error() string {
	if e == nil {
		return "redirect rejected"
	}
	return fmt.Sprintf("HH redirect rejected (%s)", e.Kind)
}

func (e *RedirectError) Unwrap() error { return ErrExternalRedirect }

const (
	RedirectLogin   = "login"
	RedirectCaptcha = "captcha"
	RedirectUnknown = "unknown"
)

func redirectKind(u *url.URL) string {
	if u == nil {
		return RedirectUnknown
	}
	value := strings.ToLower(u.Path + "?" + u.RawQuery)
	if strings.Contains(value, "captcha") || strings.Contains(value, "challenge") {
		return RedirectCaptcha
	}
	if strings.Contains(value, "login") || strings.Contains(value, "auth") {
		return RedirectLogin
	}
	return RedirectUnknown
}
