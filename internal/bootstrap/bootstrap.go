// Package bootstrap owns the importable process boundary for the HH responder.
//
// It deliberately contains only startup concerns: CLI classification, config
// loading, signal context creation, command dispatch, and exit-code mapping.
// Concrete runtime compatibility adapters are supplied explicitly by the
// importable internal/runtime package.
package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	appcli "hh-ai-responder/internal/cli"
	appconfig "hh-ai-responder/internal/config"
)

// Request is the complete, typed input to one top-level command handler.
// Handlers receive already-classified CLI intent and validated configuration;
// they do not parse os.Args or read process environment directly.
type Request struct {
	Context    context.Context
	Invocation appcli.Invocation
	Config     appconfig.Config
	Stdin      io.Reader
	Stdout     io.Writer
	Stderr     io.Writer
}

// Handler executes one command and returns the process exit code. Handlers
// own command-specific output so bootstrap does not move stdout/stderr data
// or rewrite established human and JSON output.
type Handler func(Request) int

// Handlers is explicit command wiring. It is intentionally not a registry or
// dependency container: every supported top-level command has a named slot.
type Handlers struct {
	Run         Handler
	Candidate   Handler
	Storage     Handler
	Reconcile   Handler
	Monitor     Handler
	Audit       Handler
	Web         Handler
	HH          Handler
	Profile     Handler
	CareerAgent Handler
	HHDoctor    Handler
	HHAPI       Handler
}

// Env contains process seams required by bootstrap. Nil streams and lookup
// functions use the normal process values. SignalContext is injectable only
// so dispatch and cancellation can be tested without OS signals.
type Env struct {
	Stdout        io.Writer
	Stderr        io.Writer
	Stdin         io.Reader
	WorkingDir    string
	LookupEnv     appconfig.LookupEnv
	SignalContext func(context.Context) (context.Context, context.CancelFunc)
	Handlers      Handlers
}

// Run is the importable executable boundary. It preserves the historical
// exit classes: malformed/unknown invocation and config failures return 2;
// command/runtime failures are returned by the selected handler; successful
// commands return 0.
func Run(ctx context.Context, args []string, env Env) int {
	if ctx == nil {
		ctx = context.Background()
	}
	if env.Stdout == nil {
		env.Stdout = os.Stdout
	}
	if env.Stderr == nil {
		env.Stderr = os.Stderr
	}
	if env.Stdin == nil {
		env.Stdin = os.Stdin
	}
	if env.LookupEnv == nil {
		env.LookupEnv = os.LookupEnv
	}
	if strings.TrimSpace(env.WorkingDir) == "" {
		env.WorkingDir, _ = os.Getwd()
		if strings.TrimSpace(env.WorkingDir) == "" {
			env.WorkingDir = "."
		}
	}

	invocation, err := appcli.Parse(args)
	if err != nil {
		return reportError(env.Stderr, err, 2)
	}

	configArgs := invocation.LeadingArgs
	switch invocation.Command {
	case appcli.CommandCandidate, appcli.CommandProfile, appcli.CommandStorage:
		// These commands own their command-specific flags. The historical
		// entrypoint loaded application config from env/dotenv only here.
	case appcli.CommandReconcile, appcli.CommandMonitor, appcli.CommandAudit:
		configArgs = append(append([]string(nil), invocation.LeadingArgs...), invocation.Args...)
	}
	cfg := appconfig.Config{}
	if !invocation.Help {
		cfg, err = appconfig.Load(configArgs, env.LookupEnv, env.WorkingDir)
	}
	if err != nil {
		if errors.Is(err, appconfig.ErrHelp) {
			return 0
		}
		// Storage migration historically remains usable without the runtime
		// DATABASE_URL requirement; its own command flags own that concern.
		if (invocation.Command != appcli.CommandStorage && invocation.Command != appcli.CommandProfile) || !strings.Contains(err.Error(), "DATABASE_URL is required when STORAGE_BACKEND=postgres") {
			return reportError(env.Stderr, err, 2)
		}
		cfg = appconfig.Config{}
	}

	request := Request{
		Invocation: invocation,
		Config:     cfg,
		Stdin:      env.Stdin,
		Stdout:     env.Stdout,
		Stderr:     env.Stderr,
	}
	if invocation.Command == appcli.CommandRun {
		if env.SignalContext == nil {
			env.SignalContext = defaultSignalContext
		}
		var stop context.CancelFunc
		request.Context, stop = env.SignalContext(ctx)
		defer stop()
	} else {
		request.Context = ctx
	}

	handler := handlerFor(invocation.Command, env.Handlers)
	if handler == nil {
		return reportError(env.Stderr, fmt.Errorf("no handler configured for command %q", invocation.Command), 1)
	}
	return handler(request)
}

func handlerFor(command appcli.CommandKind, handlers Handlers) Handler {
	switch command {
	case appcli.CommandRun:
		return handlers.Run
	case appcli.CommandCandidate:
		return handlers.Candidate
	case appcli.CommandStorage:
		return handlers.Storage
	case appcli.CommandReconcile:
		return handlers.Reconcile
	case appcli.CommandMonitor:
		return handlers.Monitor
	case appcli.CommandAudit:
		return handlers.Audit
	case appcli.CommandWeb:
		return handlers.Web
	case appcli.CommandHH:
		return handlers.HH
	case appcli.CommandProfile:
		return handlers.Profile
	case appcli.CommandCareerAgent:
		return handlers.CareerAgent
	case appcli.CommandHHDoctor:
		return handlers.HHDoctor
	case appcli.CommandHHAPI:
		return handlers.HHAPI
	default:
		return nil
	}
}

func reportError(stderr io.Writer, err error, code int) int {
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err)
	}
	return code
}

func defaultSignalContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
}
