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
		Help:        (len(commandArgs) > 0 && IsHelpFlag(commandArgs[0])) || ((command == CommandCareerAgent || command == CommandHHAPI) && hasHelpFlag(commandArgs)),
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
	case "hh-api":
		return CommandHHAPI, true
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
	if len(args) == 0 {
		if command == CommandHHAPI {
			return errors.New("hh-api requires a subcommand: auth, doctor, logout, preflight, approval, or apply")
		}
		return nil
	}
	if strings.HasPrefix(args[0], "-") {
		if command == CommandHHAPI && !IsHelpFlag(args[0]) {
			return errors.New("hh-api requires a subcommand: auth, doctor, logout, preflight, approval, or apply")
		}
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
		if args[0] == "reliability" && len(args) > 2 && !strings.HasPrefix(args[2], "-") && !known(args[2], "reconcile", "manual-confirm") {
			return fmt.Errorf("unknown hh reliability action %q", args[2])
		}
	case CommandHHAPI:
		if !known(args[0], "auth", "doctor", "logout", "preflight", "approval", "apply") {
			return errors.New("unknown hh-api subcommand")
		}
		if args[0] == "apply" {
			return validateHHAPIApplyArgs(args[1:])
		}
		if args[0] == "preflight" {
			if len(args) > 1 && IsHelpFlag(args[1]) {
				return nil
			}
			if len(args) < 2 || strings.HasPrefix(args[1], "-") {
				return errors.New("hh-api preflight requires a vacancy ID")
			}
			hasResumeID, hasAllResumes := false, false
			for index := 2; index < len(args); index++ {
				arg := args[index]
				if arg == "--resume-id" {
					hasResumeID = true
					if index+1 >= len(args) || strings.HasPrefix(args[index+1], "-") || args[index+1] == "" {
						return errors.New("hh-api preflight --resume-id requires a value")
					}
					index++
					continue
				}
				if strings.HasPrefix(arg, "--resume-id=") && strings.TrimPrefix(arg, "--resume-id=") != "" {
					hasResumeID = true
					continue
				}
				if arg == "--all-resumes" {
					hasAllResumes = true
					continue
				}
				if IsHelpFlag(arg) {
					continue
				}
				if strings.HasPrefix(arg, "-") {
					return errors.New("hh-api preflight accepts only --resume-id or --all-resumes")
				}
				return errors.New("hh-api preflight accepts one vacancy ID")
			}
			if hasResumeID && hasAllResumes {
				return errors.New("hh-api preflight cannot combine --resume-id with --all-resumes")
			}
			return nil
		}
		if args[0] == "approval" {
			return validateHHAPIApprovalArgs(args[1:])
		}
		for _, arg := range args[1:] {
			if !strings.HasPrefix(arg, "-") {
				return fmt.Errorf("hh-api %s does not accept positional arguments", args[0])
			}
			if !IsHelpFlag(arg) {
				return fmt.Errorf("hh-api %s does not accept flags; authorization input is stdin-only", args[0])
			}
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
		if !known(args[0], "run", "autopilot", "shadow", "canary", "feedback", "resumes", "resume", "pilot", "web-trace", "browser-session", "browser-doctor") {
			return fmt.Errorf("unknown career-agent command %q", args[0])
		}
		if args[0] == "pilot" && len(args) > 1 && !strings.HasPrefix(args[1], "-") && args[1] != "send" {
			return fmt.Errorf("unknown career-agent pilot action %q", args[1])
		}
	}
	return nil
}

func validateHHAPIApprovalArgs(args []string) error {
	if len(args) == 0 {
		return errors.New("hh-api approval requires the export or review action")
	}
	action := args[0]
	if action != "export" && action != "review" {
		return errors.New("hh-api approval requires the export or review action")
	}
	pilot, output, letter := false, false, false
	for index := 1; index < len(args); index++ {
		arg := args[index]
		switch {
		case arg == "--pilot", arg == "--out", arg == "--letter-file":
			if index+1 >= len(args) || strings.HasPrefix(args[index+1], "-") || strings.TrimSpace(args[index+1]) == "" {
				return fmt.Errorf("hh-api approval export %s requires a value", arg)
			}
			if arg == "--pilot" {
				if pilot {
					return errors.New("hh-api approval export accepts exactly one --pilot")
				}
				pilot = true
			} else if arg == "--out" {
				if output {
					return errors.New("hh-api approval export accepts exactly one --out")
				}
				output = true
			} else {
				if action != "review" {
					return errors.New("hh-api approval export accepts only --pilot and --out")
				}
				if letter {
					return errors.New("hh-api approval review accepts exactly one --letter-file")
				}
				letter = true
			}
			index++
		case strings.HasPrefix(arg, "--pilot="):
			if pilot || strings.TrimSpace(strings.TrimPrefix(arg, "--pilot=")) == "" {
				return errors.New("hh-api approval export requires exactly one --pilot")
			}
			pilot = true
		case strings.HasPrefix(arg, "--out="):
			if output || strings.TrimSpace(strings.TrimPrefix(arg, "--out=")) == "" {
				return errors.New("hh-api approval export requires exactly one --out")
			}
			output = true
		case strings.HasPrefix(arg, "--letter-file="):
			if action != "review" || letter || strings.TrimSpace(strings.TrimPrefix(arg, "--letter-file=")) == "" {
				return errors.New("hh-api approval review requires exactly one --letter-file value")
			}
			letter = true
		default:
			return fmt.Errorf("hh-api approval %s accepts only --pilot, --out, and optional --letter-file", action)
		}
	}
	if !pilot || !output {
		return fmt.Errorf("hh-api approval %s requires --pilot and --out", action)
	}
	if action == "export" && letter {
		return errors.New("hh-api approval export accepts only --pilot and --out")
	}
	return nil
}

func validateHHAPIApplyArgs(args []string) error {
	if len(args) > 0 && IsHelpFlag(args[0]) {
		return nil
	}
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return errors.New("hh-api apply requires a vacancy ID")
	}
	resumeID, approvalFile := false, false
	for index := 1; index < len(args); index++ {
		arg := args[index]
		switch {
		case arg == "--resume-id":
			if resumeID || index+1 >= len(args) || strings.HasPrefix(args[index+1], "-") || strings.TrimSpace(args[index+1]) == "" {
				return errors.New("hh-api apply requires exactly one --resume-id value")
			}
			resumeID = true
			index++
		case strings.HasPrefix(arg, "--resume-id="):
			if resumeID || strings.TrimSpace(strings.TrimPrefix(arg, "--resume-id=")) == "" {
				return errors.New("hh-api apply requires exactly one --resume-id value")
			}
			resumeID = true
		case arg == "--approval-file":
			if approvalFile || index+1 >= len(args) || strings.HasPrefix(args[index+1], "-") || strings.TrimSpace(args[index+1]) == "" {
				return errors.New("hh-api apply requires exactly one explicit --approval-file value")
			}
			approvalFile = true
			index++
		case strings.HasPrefix(arg, "--approval-file="):
			if approvalFile || strings.TrimSpace(strings.TrimPrefix(arg, "--approval-file=")) == "" {
				return errors.New("hh-api apply requires exactly one explicit --approval-file value")
			}
			approvalFile = true
		case IsHelpFlag(arg):
			return nil
		default:
			return errors.New("hh-api apply accepts one vacancy ID, --resume-id, and --approval-file only")
		}
	}
	if !resumeID || !approvalFile {
		return errors.New("hh-api apply requires --resume-id and explicit --approval-file")
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
