package runtime

import (
	"testing"

	appbootstrap "hh-ai-responder/internal/bootstrap"
)

func TestRootBootstrapHandlerGraphIsExplicit(t *testing.T) {
	handlers := NewHandlers()
	for name, handler := range map[string]appbootstrap.Handler{
		"run":       handlers.Run,
		"candidate": handlers.Candidate,
		"storage":   handlers.Storage,
		"reconcile": handlers.Reconcile,
		"monitor":   handlers.Monitor,
		"audit":     handlers.Audit,
		"web":       handlers.Web,
		"hh":        handlers.HH,
		"profile":   handlers.Profile,
	} {
		if handler == nil {
			t.Fatalf("%s handler is not wired", name)
		}
	}
}
