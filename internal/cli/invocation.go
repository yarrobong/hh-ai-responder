// Package cli owns command-line syntax and routing.
//
// It deliberately contains no application services. Invocation is a typed
// description of what the user requested; bootstrap and its explicitly wired
// compatibility handlers construct and execute the corresponding runtime.
package cli

// CommandKind identifies a top-level command.
type CommandKind string

const (
	CommandRun         CommandKind = "run"
	CommandHelp        CommandKind = "help"
	CommandHH          CommandKind = "hh"
	CommandCandidate   CommandKind = "candidate"
	CommandProfile     CommandKind = "profile"
	CommandStorage     CommandKind = "storage"
	CommandWeb         CommandKind = "web"
	CommandReconcile   CommandKind = "reconcile"
	CommandMonitor     CommandKind = "monitor"
	CommandAudit       CommandKind = "audit"
	CommandCareerAgent CommandKind = "career-agent"
	CommandHHDoctor    CommandKind = "hh-doctor"
	CommandHHAPI       CommandKind = "hh-api"
)

// Invocation is the parsed CLI intent. Args are the arguments after the
// top-level command. LeadingArgs contains application flags that appeared
// before a top-level command and are passed to the configuration boundary.
// Neither field contains application/domain state.
type Invocation struct {
	Command     CommandKind
	Subcommand  string
	Args        []string
	LeadingArgs []string
	// Help reports an explicit help flag immediately after a recognized
	// top-level command. Bootstrap uses it to bypass configuration loading and
	// dispatch directly to command-specific help.
	Help bool
}
