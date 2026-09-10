package runtime_test

import (
	"testing"

	appbootstrap "hh-ai-responder/internal/bootstrap"
	appruntime "hh-ai-responder/internal/runtime"
)

func TestHandlersAreImportableFromOutsideRuntimePackage(t *testing.T) {
	handlers := appruntime.NewHandlers()
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
