package bootstrap

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	appcli "hh-ai-responder/internal/cli"
)

func testEnv(t *testing.T, handlers Handlers) Env {
	t.Helper()
	return Env{
		Stdout:     &bytes.Buffer{},
		Stderr:     &bytes.Buffer{},
		Stdin:      strings.NewReader(""),
		WorkingDir: t.TempDir(),
		LookupEnv:  func(string) (string, bool) { return "", false },
		Handlers:   handlers,
	}
}

func TestRunRejectsUnknownCommandBeforeConstruction(t *testing.T) {
	env := testEnv(t, Handlers{Run: func(Request) int { t.Fatal("runtime was constructed"); return 0 }})
	if got := Run(context.Background(), []string{"not-a-command"}, env); got != 2 {
		t.Fatalf("exit code: got %d, want 2", got)
	}
	if !strings.Contains(env.Stderr.(*bytes.Buffer).String(), `unknown command "not-a-command"`) {
		t.Fatalf("stderr=%q", env.Stderr.(*bytes.Buffer).String())
	}
}

func TestRunReportsConfigFailureWithoutConstructingRuntime(t *testing.T) {
	env := testEnv(t, Handlers{Run: func(Request) int { t.Fatal("runtime was constructed"); return 0 }})
	env.LookupEnv = func(name string) (string, bool) {
		if name == "STORAGE_BACKEND" {
			return "postgres", true
		}
		return "", false
	}
	if got := Run(context.Background(), nil, env); got != 2 {
		t.Fatalf("exit code: got %d, want 2", got)
	}
	if !strings.Contains(env.Stderr.(*bytes.Buffer).String(), "DATABASE_URL is required") {
		t.Fatalf("stderr=%q", env.Stderr.(*bytes.Buffer).String())
	}
}

func TestRunDispatchesRunOnceWithSignalAwareContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var signalParent context.Context
	env := testEnv(t, Handlers{Run: func(request Request) int {
		if !request.Config.RunOnce {
			t.Fatal("run-once flag was not loaded")
		}
		if request.Context == nil {
			t.Fatal("handler received nil context")
		}
		if err := request.Context.Err(); err != nil {
			t.Fatalf("unexpected handler context error: %v", err)
		}
		return 0
	}})
	env.SignalContext = func(parent context.Context) (context.Context, context.CancelFunc) {
		signalParent = parent
		return context.WithCancel(parent)
	}
	if got := Run(ctx, []string{"--run-once"}, env); got != 0 {
		t.Fatalf("exit code: got %d, want 0", got)
	}
	if signalParent != ctx {
		t.Fatal("signal context did not inherit caller context")
	}
}

func TestRunDispatchesCommandAndPreservesArguments(t *testing.T) {
	var got Request
	env := testEnv(t, Handlers{HH: func(request Request) int {
		got = request
		return 7
	}})
	if gotCode := Run(context.Background(), []string{"hh", "sync", "vacancies"}, env); gotCode != 7 {
		t.Fatalf("exit code: got %d, want 7", gotCode)
	}
	if string(got.Invocation.Command) != "hh" || got.Invocation.Subcommand != "sync" {
		t.Fatalf("invocation=%+v", got.Invocation)
	}
	if len(got.Invocation.Args) != 2 || got.Invocation.Args[1] != "vacancies" {
		t.Fatalf("args=%v", got.Invocation.Args)
	}
}

func TestRunDispatchesCareerAgentDailyBeforeRuntime(t *testing.T) {
	var got Request
	env := testEnv(t, Handlers{CareerAgent: func(request Request) int {
		got = request
		return 0
	}})
	if gotCode := Run(context.Background(), []string{"career-agent", "daily", "--json"}, env); gotCode != 0 {
		t.Fatalf("exit code: got %d, want 0; stderr=%q", gotCode, env.Stderr.(*bytes.Buffer).String())
	}
	if got.Invocation.Command != appcli.CommandCareerAgent || got.Invocation.Subcommand != "daily" {
		t.Fatalf("invocation=%+v", got.Invocation)
	}
	if len(got.Invocation.Args) != 2 || got.Invocation.Args[0] != "daily" || got.Invocation.Args[1] != "--json" {
		t.Fatalf("args=%v, want [daily --json]", got.Invocation.Args)
	}
}

func TestRunDispatchesHHAPICommand(t *testing.T) {
	called := false
	env := testEnv(t, Handlers{HHAPI: func(request Request) int {
		called = true
		if request.Invocation.Command != "hh-api" || request.Invocation.Subcommand != "doctor" {
			t.Fatalf("invocation=%+v", request.Invocation)
		}
		return 0
	}})
	if got := Run(context.Background(), []string{"hh-api", "doctor"}, env); got != 0 || !called {
		t.Fatalf("exit=%d called=%t", got, called)
	}
}

func TestRunPassesCancellationToRunHandler(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	called := false
	env := testEnv(t, Handlers{Run: func(request Request) int {
		called = true
		if !errors.Is(request.Context.Err(), context.Canceled) {
			t.Fatalf("context error=%v", request.Context.Err())
		}
		return 0
	}})
	env.SignalContext = func(parent context.Context) (context.Context, context.CancelFunc) {
		return parent, func() {}
	}
	if got := Run(ctx, nil, env); got != 0 || !called {
		t.Fatalf("exit=%d called=%t", got, called)
	}
}

func TestRunUsesWorkingDirectoryForConfigDefaults(t *testing.T) {
	dir := t.TempDir()
	env := testEnv(t, Handlers{Run: func(request Request) int {
		want := filepath.Join(dir, "cookies.txt")
		if request.Config.CookiesPath != want {
			t.Fatalf("cookies path=%q, want %q", request.Config.CookiesPath, want)
		}
		return 0
	}})
	env.WorkingDir = dir
	if got := Run(context.Background(), nil, env); got != 0 {
		t.Fatalf("exit code: got %d", got)
	}
}

func TestRunCommandHelpSkipsConfigurationAndDispatchesTypedIntent(t *testing.T) {
	called := false
	env := testEnv(t, Handlers{Profile: func(request Request) int {
		called = true
		if !request.Invocation.Help {
			t.Fatal("handler did not receive typed help intent")
		}
		return 0
	}})
	env.LookupEnv = func(name string) (string, bool) {
		if name == "STORAGE_BACKEND" {
			return "postgres", true
		}
		return "", false
	}
	if got := Run(context.Background(), []string{"profile", "--help"}, env); got != 0 || !called {
		t.Fatalf("exit=%d called=%t", got, called)
	}
}
