// Package config owns application configuration parsing and validation.
//
// The shape is intentionally flat for this transitional stage. The root
// package still consumes a legacy Config value, while this package owns the
// defaults and all env/flag parsing. FollowUp is the primitive representation
// used to construct the domain FollowUpPolicy at the composition boundary.
package config

import "time"

// LookupEnv is the small environment boundary used by Load and its tests.
type LookupEnv func(string) (string, bool)

// FollowUpConfig contains only primitive settings. Follow-up eligibility and
// policy behavior remain in the domain package until its extraction stage.
type FollowUpConfig struct {
	AfterApplicationWithoutReply      time.Duration
	AfterCandidateMessageWithoutReply time.Duration
	MaxFollowUps                      int
	MinimumInterval                   time.Duration
}

// Config is the validated runtime configuration consumed by the current
// composition root. Secrets are stored only as values; this package does not
// provide a String method or any other dumping representation.
type Config struct {
	HHReadOnly                   bool
	StorageBackend               string
	DatabaseURL                  string
	CandidateID                  string
	FollowUp                     FollowUpConfig
	SearchURL                    string
	SearchURLs                   []string
	SearchPeriodDays             int
	CareerAgentMaxSearchProfiles int
	CareerAgentResultPath        string
	CareerAgentFeedbackPath      string
	ResumeRegistryPath           string
	CookiesPath                  string
	BrowserProfilePath           string
	BrowserTraceVacancyURL       string
	BrowserTransport             string
	BrowserHeadless              bool
	LogLevel                     string
	Resume                       string
	MaxResponses                 int
	AIBaseURL                    string
	AIModel                      string
	AIAPIKey                     string
	EmbeddingProvider            string
	EmbeddingBaseURL             string
	EmbeddingAPIKey              string
	EmbeddingModel               string
	EmbeddingDimensions          int
	AITimeout                    time.Duration
	AIConnectTimeout             time.Duration
	AIAttempts                   int
	ExtraLetterPrompt            string
	ExtraTestSolutionPrompt      string
	HHReadConcurrency            int
	RequestInterval              time.Duration
	OutputPath                   string
	Contacts                     string
	ListResumes                  bool
	ForceLetter                  bool
	ExtraChatReplyPrompt         string
	GithubURL                    string
	DryRun                       bool
	HHWriteEnabled               bool
	HHChatURL                    string
	HHMaxWritesPerRun            int
	HHMaxWritesPerDay            int
	AutoApply                    bool
	AutoApplyMode                string
	AutoChat                     bool
	AutoTouch                    bool
	AutoJobStatus                bool
	ChatMode                     string
	MinSalary                    int
	MinSalaryCurrency            string
	IncludeKeywords              []string
	ExcludeKeywords              []string
	MinMatchScore                int
	RunOnce                      bool
	MaxVacanciesPerRun           int
	MaxApplicationsPerRun        int
	MaxConversationsPerRun       int
	AlreadyRespondedStatePath    string
	CandidateProfilePath         string
	CandidateStoriesPath         string
	HHSyncStatePath              string
	MonitorInterval              time.Duration
	MonitorQuietHours            string
	NotificationCooldown         time.Duration
	ConversationDisplayTTL       time.Duration
	BackgroundInboxRefresh       bool
}
