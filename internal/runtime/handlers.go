package runtime

import (
	"errors"
	"os"

	appbootstrap "hh-ai-responder/internal/bootstrap"
	appcli "hh-ai-responder/internal/cli"
)

// NewHandlers constructs the single production command graph consumed by
// bootstrap. It has no process-global input beyond the legacy command
// adapters' existing compatibility seams, so both executable entrypoints can
// share this composition without importing package main.
func NewHandlers() appbootstrap.Handlers {
	return appbootstrap.Handlers{
		Run: func(request appbootstrap.Request) int {
			return runDefaultConfig(request.Context, legacyConfigFromPackage(request.Config), request.Stdout, request.Stderr)
		},
		Candidate: withCommandHelp(appcli.CommandCandidate, func(request appbootstrap.Request) int {
			_ = loadDotEnv(".env")
			return commandResult(runCandidateCommand(request.Invocation.Args, legacyConfigFromPackage(request.Config), request.Stdout), request.Stderr, 1)
		}),
		Storage: withCommandHelp(appcli.CommandStorage, func(request appbootstrap.Request) int {
			return commandResult(runStorageCommand(request.Invocation.Args, legacyConfigFromPackage(request.Config), request.Stdout), request.Stderr, 1)
		}),
		Reconcile: withCommandHelp(appcli.CommandReconcile, func(request appbootstrap.Request) int {
			commandArgs := append(append([]string(nil), request.Invocation.LeadingArgs...), request.Invocation.Args...)
			return commandResult(runReconcileCommand(legacyConfigFromPackage(request.Config), reconcileDryRun(commandArgs), request.Stdout), request.Stderr, 1)
		}),
		Monitor: withCommandHelp(appcli.CommandMonitor, func(request appbootstrap.Request) int {
			// Monitor is permanently read-only, regardless of legacy write flags.
			return commandResult(runMonitorCommand(legacyConfigFromPackage(request.Config), request.Stdout), request.Stderr, 1)
		}),
		Audit: withCommandHelp(appcli.CommandAudit, func(request appbootstrap.Request) int {
			return commandResult(runAuditCommand(legacyConfigFromPackage(request.Config), request.Stdout), request.Stderr, 1)
		}),
		Web: func(request appbootstrap.Request) int {
			_ = loadDotEnv(".env")
			cfg := legacyConfigFromPackage(request.Config)
			logger = NewLogger(request.Stderr, parseLogLevel(cfg.LogLevel))
			return commandResult(runDashboardCommand(request.Invocation.Args, cfg, request.Stdout), request.Stderr, 1)
		},
		HH: withCommandHelp(appcli.CommandHH, func(request appbootstrap.Request) int {
			cfg := legacyConfigFromPackage(request.Config)
			return commandResult(runHHCommand(request.Invocation.Args, cfg, request.Stdout, request.Stderr), request.Stderr, 1)
		}),
		Profile: withCommandHelp(appcli.CommandProfile, func(request appbootstrap.Request) int {
			_ = loadDotEnv(".env")
			if profileCLIUsesPostgres(request.Invocation.Args) {
				cfg := Config{StorageBackend: storageBackendPostgres, DatabaseURL: os.Getenv("DATABASE_URL"), CandidateID: firstNonEmpty(os.Getenv("HH_CANDIDATE_ID"), "candidate-local"), CandidateProfilePath: firstNonEmpty(os.Getenv("HH_CANDIDATE_PROFILE"), "candidate_profile.json")}
				if len(request.Invocation.Args) > 0 && request.Invocation.Args[0] == "knowledge" {
					return commandResult(runKnowledgeCommandWithConfig(request.Invocation.Args[1:], cfg, request.Stdout), request.Stderr, 2)
				}
				return commandResult(errors.New("profile write commands are unsupported in STORAGE_BACKEND=postgres; use canonical candidate knowledge commands"), request.Stderr, 2)
			}
			return commandResult(runProfileCommand(request.Invocation.Args, request.Stdin, request.Stdout), request.Stderr, 2)
		}),
		CareerAgent: withCommandHelp(appcli.CommandCareerAgent, func(request appbootstrap.Request) int {
			cfg := legacyConfigFromPackage(request.Config)
			return commandResult(runCareerAgentCommand(request.Invocation.Args, cfg, request.Stdout, request.Stderr), request.Stderr, 1)
		}),
	}
}
