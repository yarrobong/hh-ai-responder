package cli

import (
	"errors"
	"fmt"
	"strings"

	appconfig "hh-ai-responder/internal/config"
)

// Parse classifies argv without executing a command or contacting any
// external service. Application flag syntax is delegated to internal/config
// so this package does not duplicate flag definitions.
func Parse(args []string) (Invocation, error) {
	args = append([]string(nil), args...)
	if len(args) == 0 {
		return Invocation{Command: CommandRun}, nil
	}

	// Keep help as an explicit intent so callers can test it without invoking
	// os.Exit. The original config FlagSet still owns the actual help text.
	if IsHelpFlag(args[0]) {
		return Invocation{Command: CommandHelp, LeadingArgs: args}, nil
	}

	leading, remainder, err := appconfig.SplitLeadingArgs(args, "")
	if err != nil {
		if errors.Is(err, appconfig.ErrHelp) {
			return Invocation{Command: CommandHelp, LeadingArgs: args}, nil
		}
		return Invocation{}, err
	}
	if len(remainder) == 0 {
		return Invocation{Command: CommandRun, LeadingArgs: leading}, nil
	}

	command, ok := topLevelCommand(remainder[0])
	if !ok {
		return Invocation{}, fmt.Errorf("unknown command %q", remainder[0])
	}
	commandArgs := append([]string(nil), remainder[1:]...)
	if err := validateCommandArgs(command, commandArgs); err != nil {
		return Invocation{}, err
	}
	return Invocation{
		Command:     command,
		Subcommand:  firstSubcommand(commandArgs),
		Args:        commandArgs,
		LeadingArgs: leading,
		Help:        (len(commandArgs) > 0 && IsHelpFlag(commandArgs[0])) || (command == CommandCareerAgent && hasHelpFlag(commandArgs)),
	}, nil
}

func hasHelpFlag(args []string) bool {
	for _, value := range args {
		if IsHelpFlag(value) {
			return true
		}
	}
	return false
}

func topLevelCommand(value string) (CommandKind, bool) {
	switch value {
	case "hh":
		return CommandHH, true
	case "candidate":
		return CommandCandidate, true
	case "profile":
		return CommandProfile, true
	case "storage":
		return CommandStorage, true
	case "web", "dashboard":
		return CommandWeb, true
	case "reconcile":
		return CommandReconcile, true
	case "monitor":
		return CommandMonitor, true
	case "audit":
		return CommandAudit, true
	case "career-agent":
		return CommandCareerAgent, true
	case "hh-doctor":
		return CommandHHDoctor, true
	default:
		return "", false
	}
}

func firstSubcommand(args []string) string {
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		return args[0]
	}
	return ""
}

func validateCommandArgs(command CommandKind, args []string) error {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return nil
	}
	known := func(value string, values ...string) bool {
		for _, candidate := range values {
			if value == candidate {
				return true
			}
		}
		return false
	}
	switch command {
	case CommandHHDoctor:
		if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
			return fmt.Errorf("hh-doctor does not accept positional arguments")
		}
	case CommandHH:
		if !known(args[0], "sync", "inbox", "workflow", "draft", "pilot-candidates", "pilot-shortlist", "pilot-show", "write-status", "eligible", "eligibility-report", "eligibility-summary", "quality-report", "action", "reliability") {
			return fmt.Errorf("unknown hh command %q", args[0])
		}
		if args[0] == "sync" && len(args) > 1 && !strings.HasPrefix(args[1], "-") && !known(args[1], "vacancies", "applications", "conversations", "conversation") {
			return fmt.Errorf("unknown hh sync target %q", args[1])
		}
		if args[0] == "action" && len(args) > 1 && !strings.HasPrefix(args[1], "-") && !known(args[1], "preflight", "request-preview") {
			return fmt.Errorf("unknown hh action %q", args[1])
		}
		if args[0] == "reliability" && len(args) > 1 && !strings.HasPrefix(args[1], "-") && !known(args[1], "applications", "autochat") {
			return fmt.Errorf("unknown hh reliability target %q", args[1])
		}
		if args[0] == "reliability" && len(args) > 2 && !strings.HasPrefix(args[2], "-") && args[2] != "reconcile" {
			return fmt.Errorf("unknown hh reliability action %q", args[2])
		}
	case CommandCandidate:
		if !known(args[0], "migrate-postgres", "status", "semantic") {
			return fmt.Errorf("unknown candidate command %q", args[0])
		}
		if args[0] == "semantic" && len(args) > 1 && !strings.HasPrefix(args[1], "-") && !known(args[1], "status", "reindex", "search") {
			return fmt.Errorf("unknown candidate semantic command %q", args[1])
		}
	case CommandProfile:
		if !known(args[0], "show", "questions", "bootstrap", "import", "stories", "communication", "knowledge") {
			return fmt.Errorf("unknown profile command %q", args[0])
		}
		if args[0] == "knowledge" && len(args) > 1 && !strings.HasPrefix(args[1], "-") && !known(args[1], "proposals", "sync-profile", "confirm", "reject") {
			return fmt.Errorf("unknown profile knowledge command %q", args[1])
		}
	case CommandStorage:
		if args[0] != "migrate-postgres" {
			return fmt.Errorf("unknown storage command %q", args[0])
		}
	case CommandCareerAgent:
		if !known(args[0], "shadow", "canary", "feedback", "resumes", "resume", "pilot", "web-trace") {
			return fmt.Errorf("unknown career-agent command %q", args[0])
		}
		if args[0] == "pilot" && len(args) > 1 && !strings.HasPrefix(args[1], "-") && args[1] != "send" {
			return fmt.Errorf("unknown career-agent pilot action %q", args[1])
		}
	}
	return nil
}

// IsHelpFlag identifies the standard help spellings accepted by the CLI.
// Keeping this lexical rule in the CLI package lets command handlers share
// the same typed policy without inspecting rendered parser errors.
func IsHelpFlag(value string) bool {
	switch value {
	case "-h", "--help", "-help":
		return true
	default:
		return false
	}
}
