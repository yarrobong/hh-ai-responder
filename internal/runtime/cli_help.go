package runtime

import (
	"fmt"
	"io"

	appbootstrap "hh-ai-responder/internal/bootstrap"
	appcli "hh-ai-responder/internal/cli"
)

// withCommandHelp is the single runtime boundary for immediate top-level
// command help. The typed invocation has already classified the request, so
// no command configuration or business dependency is constructed first.
func withCommandHelp(command appcli.CommandKind, handler appbootstrap.Handler) appbootstrap.Handler {
	return func(request appbootstrap.Request) int {
		if request.Invocation.Help {
			return commandResult(writeCommandHelp(command, request.Stdout), request.Stderr, 1)
		}
		return handler(request)
	}
}

func writeCommandHelp(command appcli.CommandKind, out io.Writer) error {
	usage := map[appcli.CommandKind]string{
		appcli.CommandCandidate:   "usage: candidate migrate-postgres ... | status | semantic status | semantic reindex [--dry-run|--apply] | semantic search QUERY",
		appcli.CommandProfile:     "usage: profile [show|questions|bootstrap|import file|stories|communication|knowledge proposals|knowledge confirm <id>|knowledge reject <id>] [-candidate-profile path] [-candidate-stories path]",
		appcli.CommandStorage:     "usage: storage migrate-postgres [--dry-run] [--apply] [--source-dir DIR] [--report FILE]",
		appcli.CommandReconcile:   "usage: reconcile [--dry-run]",
		appcli.CommandMonitor:     "usage: monitor [--run-once] [application flags]",
		appcli.CommandAudit:       "usage: audit [application flags]",
		appcli.CommandCareerAgent: "usage: career-agent run | career-agent --shadow | career-agent --canary | career-agent browser-doctor [--headless|--headed] | career-agent resumes | career-agent resume enable|disable --id <resume-id> | career-agent feedback --vacancy <id> --type <type> | career-agent pilot --search [--max-scan 1..1000] [--max-candidates 1..20] | career-agent pilot --vacancy <id> | career-agent pilot send --nonce <nonce> | career-agent web-trace --known <id,id> --unknown <id,id,...>",
		appcli.CommandHHDoctor:    "usage: hh-doctor (GET/read-only HH access diagnostics)",
		appcli.CommandHHAPI:       "usage: hh-api auth | doctor | logout | preflight <vacancy-id> [--resume-id ID]",
		appcli.CommandHH:          "usage: hh sync [vacancies|applications|conversations] | hh sync conversation <conversation-id-or-chat-id> | hh inbox | hh workflow | hh draft <conversation-id> | hh pilot-candidates | hh pilot-shortlist | hh pilot-show <conversation-id> | hh write-status | hh eligible | hh eligibility-report | hh eligibility-summary | hh quality-report | hh action preflight|request-preview <action-id> | hh reliability applications|autochat [--limit N] [--state STATE] [--all] [--json] | hh reliability applications|autochat reconcile <attempt-id> [--json]",
	}
	value, ok := usage[command]
	if !ok {
		return fmt.Errorf("help is not available for command %q", command)
	}
	_, err := fmt.Fprintln(out, value)
	return err
}

func isCLIHelpFlag(value string) bool {
	return appcli.IsHelpFlag(value)
}
