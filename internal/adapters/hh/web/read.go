package hhweb

import (
	"errors"
	"net/url"

	hhreadadapter "hh-ai-responder/internal/adapters/hh/read"
	"hh-ai-responder/internal/hhwebsession"
)

// NewCookieWebReadClient composes the existing GET-only HH parser/client with
// the shared persistent cookie session. The write capability is intentionally
// not passed across this boundary.
func NewCookieWebReadClient(session *hhwebsession.Session, baseURL *url.URL, searchParams url.Values) (*hhreadadapter.Client, error) {
	if session == nil {
		return nil, errors.New("cookie web read session is required")
	}
	return hhreadadapter.NewClient(hhreadadapter.Options{
		BaseURL:      baseURL,
		SearchParams: searchParams,
		HTTPClient:   session.ReadClient(),
	})
}
