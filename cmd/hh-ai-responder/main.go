// Command hh-ai-responder is the canonical production executable.
package main

import (
	"context"
	"os"

	appbootstrap "hh-ai-responder/internal/bootstrap"
	appruntime "hh-ai-responder/internal/runtime"
)

func main() {
	os.Exit(appbootstrap.Run(context.Background(), os.Args[1:], appbootstrap.Env{
		Stdout:   os.Stdout,
		Stderr:   os.Stderr,
		Stdin:    os.Stdin,
		Handlers: appruntime.NewHandlers(),
	}))
}
