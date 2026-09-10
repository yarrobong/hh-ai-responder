package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

type entrypointResult struct {
	exitCode int
	stdout   string
	stderr   string
}

func TestCanonicalEntrypointSafeBehavior(t *testing.T) {
	cases := []struct {
		name string
		args []string
		code int
	}{
		{name: "help", args: []string{"--help"}, code: 0},
		{name: "unknown command", args: []string{"not-a-command"}, code: 1}, // go run reports the child code as its own failure.
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			canonical := runEntrypoint(t, "./cmd/hh-ai-responder", tc.args...)
			if canonical.exitCode != tc.code {
				t.Fatalf("exit code=%d, want %d; stdout=%q stderr=%q", canonical.exitCode, tc.code, canonical.stdout, canonical.stderr)
			}
			if tc.name == "help" && !strings.Contains(canonical.stderr, "Usage of hh-ai-responder:") {
				t.Fatalf("help output is missing: stdout=%q stderr=%q", canonical.stdout, canonical.stderr)
			}
			if tc.name == "unknown command" && !strings.Contains(canonical.stderr, "unknown command") {
				t.Fatalf("unknown invocation did not report an error: %q", canonical.stderr)
			}
		})
	}
}

func TestCanonicalEntrypointHelpMatrix(t *testing.T) {
	cases := []struct {
		name  string
		args  []string
		match string
	}{
		{name: "root long", args: []string{"--help"}, match: "Usage of hh-ai-responder:"},
		{name: "root short", args: []string{"-h"}, match: "Usage of hh-ai-responder:"},
		{name: "profile long", args: []string{"profile", "--help"}, match: "usage: profile"},
		{name: "profile short", args: []string{"profile", "-h"}, match: "usage: profile"},
		{name: "monitor long", args: []string{"monitor", "--help"}, match: "usage: monitor"},
		{name: "monitor short", args: []string{"monitor", "-h"}, match: "usage: monitor"},
		{name: "audit long", args: []string{"audit", "--help"}, match: "usage: audit"},
		{name: "audit short", args: []string{"audit", "-h"}, match: "usage: audit"},
		{name: "reconcile long", args: []string{"reconcile", "--help"}, match: "usage: reconcile"},
		{name: "reconcile short", args: []string{"reconcile", "-h"}, match: "usage: reconcile"},
		{name: "hh long", args: []string{"hh", "--help"}, match: "usage: hh"},
		{name: "hh short", args: []string{"hh", "-h"}, match: "usage: hh"},
		{name: "reliability long", args: []string{"hh", "reliability", "--help"}, match: "usage: hh reliability"},
		{name: "reliability short", args: []string{"hh", "reliability", "-h"}, match: "usage: hh reliability"},
		{name: "applications long", args: []string{"hh", "reliability", "applications", "--help"}, match: "Usage of hh reliability applications:"},
		{name: "applications short", args: []string{"hh", "reliability", "applications", "-h"}, match: "Usage of hh reliability applications:"},
		{name: "autochat long", args: []string{"hh", "reliability", "autochat", "--help"}, match: "Usage of hh reliability autochat:"},
		{name: "autochat short", args: []string{"hh", "reliability", "autochat", "-h"}, match: "Usage of hh reliability autochat:"},
		{name: "application reconciliation long", args: []string{"hh", "reliability", "applications", "reconcile", "--help"}, match: "Usage of hh reliability applications reconcile:"},
		{name: "autochat reconciliation short", args: []string{"hh", "reliability", "autochat", "reconcile", "-h"}, match: "Usage of hh reliability autochat reconcile:"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := runEntrypoint(t, "./cmd/hh-ai-responder", tc.args...)
			if result.exitCode != 0 {
				t.Fatalf("exit code=%d, want 0; stdout=%q stderr=%q", result.exitCode, result.stdout, result.stderr)
			}
			output := result.stdout + result.stderr
			if !strings.Contains(output, tc.match) {
				t.Fatalf("help output does not contain %q: %q", tc.match, output)
			}
			if strings.Contains(output, "flag: help requested") {
				t.Fatalf("help was reported as an application error: %q", output)
			}
			if strings.Count(output, tc.match) != 1 {
				t.Fatalf("help was not printed exactly once: count=%d output=%q", strings.Count(output, tc.match), output)
			}
		})
	}
}

func TestCanonicalEntrypointRejectsInvalidCLI(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
	}{
		{name: "unknown root command", args: []string{"unknown-root"}},
		{name: "unknown hh subcommand", args: []string{"hh", "unknown"}},
		{name: "unknown reliability subcommand", args: []string{"hh", "reliability", "unknown"}},
		{name: "invalid profile flag", args: []string{"profile", "--definitely-invalid"}},
		{name: "invalid reliability flag", args: []string{"hh", "reliability", "applications", "--invalid"}},
		{name: "missing reliability attempt", args: []string{"hh", "reliability", "applications", "reconcile"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := runEntrypoint(t, "./cmd/hh-ai-responder", tc.args...)
			if result.exitCode == 0 {
				t.Fatalf("invalid invocation succeeded: stdout=%q stderr=%q", result.stdout, result.stderr)
			}
		})
	}
}

func runEntrypoint(t *testing.T, target string, args ...string) entrypointResult {
	t.Helper()
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(source), "..", ".."))
	commandArgs := append([]string{"run", target}, args...)
	command := exec.Command("go", commandArgs...)
	command.Dir = repoRoot
	command.Env = safeEntrypointEnvironment()
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	result := entrypointResult{stdout: stdout.String(), stderr: stderr.String()}
	if err == nil {
		result.exitCode = 0
		return result
	}
	if exitError, ok := err.(*exec.ExitError); ok {
		result.exitCode = exitError.ExitCode()
		return result
	}
	t.Fatalf("run %q: %v", target, err)
	return entrypointResult{}
}

func safeEntrypointEnvironment() []string {
	values := make(map[string]string)
	for _, entry := range os.Environ() {
		key, value, ok := strings.Cut(entry, "=")
		if ok {
			values[key] = value
		}
	}
	values["HH_DRY_RUN"] = "true"
	values["HH_WRITE_ENABLED"] = "false"
	env := make([]string, 0, len(values))
	for key, value := range values {
		env = append(env, key+"="+value)
	}
	return env
}
