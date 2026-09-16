package config

import (
	"path/filepath"
	"time"
)

const (
	DefaultAIAttempts                   = 2
	DefaultAIBaseURL                    = "http://localhost:11434"
	DefaultAIModel                      = "llama3:8b"
	DefaultEmbeddingModel               = "text-embedding-3-small"
	DefaultEmbeddingDimensions          = 1536
	MaxEmbeddingDimensions              = 16000
	DefaultHHChatURL                    = "https://chatik.hh.ru"
	DefaultMinMatchScore                = 65
	DefaultMinSalaryCurrency            = "RUR"
	DefaultHHMaxWritesPerRun            = 1
	DefaultHHMaxWritesPerDay            = 5
	DefaultHHReadConcurrency            = 4
	DefaultMaxVacanciesPerRun           = 20
	DefaultMaxApplicationsPerRun        = 10
	DefaultSearchPeriodDays             = 7
	DefaultCareerAgentMaxSearchProfiles = 16
	DefaultCandidateID                  = "candidate-local"
	DefaultStorageBackend               = "json"
	DefaultChatMode                     = "review"
	DefaultLogLevel                     = "info"
)

const (
	DefaultAIConnectTimeout       = 5 * time.Second
	DefaultAITimeout              = 30 * time.Second
	DefaultRequestInterval        = 1200 * time.Millisecond
	DefaultConversationDisplayTTL = 60 * time.Second
	DefaultMonitorInterval        = 15 * time.Minute
	DefaultNotificationCooldown   = 15 * time.Minute
)

// DefaultFollowUpConfig is the primitive configuration counterpart of the
// domain's default FollowUpPolicy.
func DefaultFollowUpConfig() FollowUpConfig {
	return FollowUpConfig{
		AfterApplicationWithoutReply:      5 * 24 * time.Hour,
		AfterCandidateMessageWithoutReply: 3 * 24 * time.Hour,
		MaxFollowUps:                      2,
		MinimumInterval:                   3 * 24 * time.Hour,
	}
}

func defaults(workingDir string) Config {
	return Config{
		FollowUp:                     DefaultFollowUpConfig(),
		StorageBackend:               DefaultStorageBackend,
		CandidateID:                  DefaultCandidateID,
		EmbeddingModel:               DefaultEmbeddingModel,
		EmbeddingDimensions:          DefaultEmbeddingDimensions,
		DryRun:                       true,
		AutoApply:                    true,
		AutoApplyMode:                "off",
		AutoChat:                     true,
		AutoTouch:                    true,
		AutoJobStatus:                true,
		ChatMode:                     DefaultChatMode,
		LogLevel:                     DefaultLogLevel,
		MinMatchScore:                DefaultMinMatchScore,
		MinSalaryCurrency:            DefaultMinSalaryCurrency,
		CandidateProfilePath:         filepath.Join(workingDir, "candidate_profile.json"),
		CandidateStoriesPath:         filepath.Join(workingDir, "candidate_stories.json"),
		CareerAgentResultPath:        filepath.Join(workingDir, "career_agent_latest.json"),
		CareerAgentFeedbackPath:      filepath.Join(workingDir, "career_agent_feedback.json"),
		ResumeRegistryPath:           filepath.Join(workingDir, "resume_registry.json"),
		SearchPeriodDays:             DefaultSearchPeriodDays,
		CareerAgentMaxSearchProfiles: DefaultCareerAgentMaxSearchProfiles,
		HHSyncStatePath:              filepath.Join(workingDir, "hh_sync_state.json"),
		MonitorInterval:              DefaultMonitorInterval,
		NotificationCooldown:         DefaultNotificationCooldown,
		ConversationDisplayTTL:       DefaultConversationDisplayTTL,
		HHMaxWritesPerRun:            DefaultHHMaxWritesPerRun,
		HHMaxWritesPerDay:            DefaultHHMaxWritesPerDay,
	}
}
