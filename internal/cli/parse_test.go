package cli

import (
	"strings"
	"testing"
)

func TestParseSupportedInvocations(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		command     CommandKind
		subcommand  string
		leadingArgs []string
		commandArgs []string
	}{
		{name: "default", command: CommandRun},
		{name: "help", args: []string{"-h"}, command: CommandHelp, leadingArgs: []string{"-h"}},
		{name: "long help", args: []string{"--help"}, command: CommandHelp, leadingArgs: []string{"--help"}},
		{name: "hh", args: []string{"hh"}, command: CommandHH},
		{name: "hh workflow", args: []string{"hh", "workflow"}, command: CommandHH, subcommand: "workflow", commandArgs: []string{"workflow"}},
		{name: "hh quality report", args: []string{"hh", "quality-report"}, command: CommandHH, subcommand: "quality-report", commandArgs: []string{"quality-report"}},
		{name: "hh write status", args: []string{"hh", "write-status"}, command: CommandHH, subcommand: "write-status", commandArgs: []string{"write-status"}},
		{name: "hh reliability applications", args: []string{"hh", "reliability", "applications", "--limit", "5"}, command: CommandHH, subcommand: "reliability", commandArgs: []string{"reliability", "applications", "--limit", "5"}},
		{name: "hh reliability applications reconcile", args: []string{"hh", "reliability", "applications", "reconcile", "attempt-1"}, command: CommandHH, subcommand: "reliability", commandArgs: []string{"reliability", "applications", "reconcile", "attempt-1"}},
		{name: "hh reliability applications manual confirmation", args: []string{"hh", "reliability", "applications", "manual-confirm", "attempt-1"}, command: CommandHH, subcommand: "reliability", commandArgs: []string{"reliability", "applications", "manual-confirm", "attempt-1"}},
		{name: "candidate", args: []string{"candidate", "semantic", "search", "python"}, command: CommandCandidate, subcommand: "semantic", commandArgs: []string{"semantic", "search", "python"}},
		{name: "profile", args: []string{"profile", "communication"}, command: CommandProfile, subcommand: "communication", commandArgs: []string{"communication"}},
		{name: "storage", args: []string{"storage", "migrate-postgres"}, command: CommandStorage, subcommand: "migrate-postgres", commandArgs: []string{"migrate-postgres"}},
		{name: "web", args: []string{"web", "--host", "127.0.0.1"}, command: CommandWeb, commandArgs: []string{"--host", "127.0.0.1"}},
		{name: "dashboard alias", args: []string{"dashboard"}, command: CommandWeb},
		{name: "monitor", args: []string{"monitor", "--run-once"}, command: CommandMonitor, commandArgs: []string{"--run-once"}},
		{name: "audit", args: []string{"audit"}, command: CommandAudit},
		{name: "hh doctor", args: []string{"hh-doctor"}, command: CommandHHDoctor},
		{name: "hh api auth", args: []string{"hh-api", "auth"}, command: CommandHHAPI, subcommand: "auth", commandArgs: []string{"auth"}},
		{name: "hh api doctor", args: []string{"hh-api", "doctor"}, command: CommandHHAPI, subcommand: "doctor", commandArgs: []string{"doctor"}},
		{name: "hh api logout", args: []string{"hh-api", "logout"}, command: CommandHHAPI, subcommand: "logout", commandArgs: []string{"logout"}},
		{name: "career agent shadow", args: []string{"career-agent", "--shadow"}, command: CommandCareerAgent, commandArgs: []string{"--shadow"}},
		{name: "career agent subcommand shadow", args: []string{"career-agent", "shadow"}, command: CommandCareerAgent, subcommand: "shadow", commandArgs: []string{"shadow"}},
		{name: "career agent feedback", args: []string{"career-agent", "feedback", "--vacancy", "1"}, command: CommandCareerAgent, subcommand: "feedback", commandArgs: []string{"feedback", "--vacancy", "1"}},
		{name: "career agent browser session", args: []string{"career-agent", "browser-session", "--status"}, command: CommandCareerAgent, subcommand: "browser-session", commandArgs: []string{"browser-session", "--status"}},
		{name: "config before command", args: []string{"--ai-model", "fixture", "hh", "workflow"}, command: CommandHH, subcommand: "workflow", leadingArgs: []string{"--ai-model", "fixture"}, commandArgs: []string{"workflow"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			invocation, err := Parse(test.args)
			if err != nil {
				t.Fatal(err)
			}
			if invocation.Command != test.command || invocation.Subcommand != test.subcommand {
				t.Fatalf("invocation = %#v, want command=%q subcommand=%q", invocation, test.command, test.subcommand)
			}
			if !equalStrings(invocation.LeadingArgs, test.leadingArgs) || !equalStrings(invocation.Args, test.commandArgs) {
				t.Fatalf("args = leading=%#v command=%#v, want leading=%#v command=%#v", invocation.LeadingArgs, invocation.Args, test.leadingArgs, test.commandArgs)
			}
		})
	}
}

func TestParseRejectsUnknownCommands(t *testing.T) {
	for _, args := range [][]string{{"unknown"}, {"hh", "unknown"}} {
		if _, err := Parse(args); err == nil {
			t.Fatalf("Parse(%#v) accepted an unsupported command", args)
		}
	}
}

func TestParseHHAPIUnknownSubcommandRedactsRawArgument(t *testing.T) {
	for _, code := range []string{"authorization-code-cli-sentinel", "unknown-hh-api-sentinel"} {
		_, err := Parse([]string{"hh-api", code})
		if err == nil {
			t.Fatalf("Parse accepted unknown hh-api subcommand %q", code)
		}
		if got, want := err.Error(), "unknown hh-api subcommand"; got != want {
			t.Fatalf("error = %q, want fixed redacted error %q", got, want)
		}
		if strings.Contains(err.Error(), code) {
			t.Fatalf("error exposed raw subcommand %q: %v", code, err)
		}
	}
}

func TestParseRejectsHHAPIWithoutSubcommand(t *testing.T) {
	if _, err := Parse([]string{"hh-api"}); err == nil {
		t.Fatal("bare hh-api was accepted")
	}
}

func TestParseRejectsHHAPIExtraPositionalArguments(t *testing.T) {
	for _, args := range [][]string{
		{"hh-api", "auth", "unexpected"},
		{"hh-api", "doctor", "unexpected"},
		{"hh-api", "logout", "unexpected"},
		{"hh-api", "auth", "--code=authorization-code-sentinel"},
		{"hh-api", "auth", "--authorization-code=authorization-code-sentinel"},
	} {
		if _, err := Parse(args); err == nil {
			t.Fatalf("Parse(%#v) accepted an extra positional argument", args)
		}
		if _, err := Parse(args); err != nil && strings.Contains(err.Error(), "authorization-code-sentinel") {
			t.Fatalf("Parse(%#v) exposed authorization code: %v", args, err)
		}
	}
}

func TestParseClassifiesImmediateCommandHelp(t *testing.T) {
	for _, args := range [][]string{
		{"profile", "--help"},
		{"monitor", "-h"},
		{"audit", "--help"},
		{"reconcile", "-h"},
		{"hh", "--help"},
		{"candidate", "-h"},
		{"storage", "--help"},
	} {
		invocation, err := Parse(args)
		if err != nil {
			t.Fatalf("Parse(%#v): %v", args, err)
		}
		if !invocation.Help {
			t.Fatalf("Parse(%#v) did not classify command help: %#v", args, invocation)
		}
	}
	if invocation, err := Parse([]string{"career-agent", "resumes", "--help"}); err != nil || !invocation.Help {
		t.Fatalf("Career Agent nested help classification: invocation=%#v err=%v", invocation, err)
	}
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
