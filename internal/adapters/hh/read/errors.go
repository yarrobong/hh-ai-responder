package hhread

import (
	"errors"
	"fmt"
	"net/http"
)

var (
	ErrNotConfigured       = errors.New("HH read client is not configured")
	ErrInvalidCursor       = errors.New("invalid HH read cursor")
	ErrInvalidConversation = errors.New("invalid HH conversation identity")
)

type HTTPStatusError struct {
	Status int
}

func (e *HTTPStatusError) Error() string {
	if e == nil {
		return "unexpected HH HTTP status"
	}
	return fmt.Sprintf("unexpected HTTP status %d %s", e.Status, http.StatusText(e.Status))
}

func unexpectedStatus(status int) error { return &HTTPStatusError{Status: status} }
