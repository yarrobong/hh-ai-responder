package config

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

func NormalizeStorageBackend(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return DefaultStorageBackend, nil
	}
	switch value {
	case "json", "postgres":
		return value, nil
	default:
		return "", fmt.Errorf("unsupported storage backend %q: use json or postgres", value)
	}
}

func NormalizeBrowserTransport(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return DefaultBrowserTransport, nil
	}
	switch value {
	case "auto", "browser", "http":
		return value, nil
	default:
		return "", fmt.Errorf("unsupported browser transport %q: use auto, browser, or http", value)
	}
}

func NormalizeHHTransport(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return DefaultHHTransport, nil
	}
	switch value {
	case "browser", "api", "auto":
		return value, nil
	default:
		return "", fmt.Errorf("unsupported HH transport %q: use browser, api, or auto", value)
	}
}

func ParseNonNegativeInt(value, name string, fallback int) (int, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(trimmed)
	if err != nil || parsed < 0 {
		if err == nil {
			err = errors.New("must not be negative")
		}
		return 0, fmt.Errorf("%s must be a non-negative integer: %w", name, err)
	}
	return parsed, nil
}

func ParseEmbeddingDimensions(value string) (int, error) {
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || parsed <= 0 || parsed > MaxEmbeddingDimensions {
		if err == nil {
			err = fmt.Errorf("must be between 1 and %d", MaxEmbeddingDimensions)
		}
		return 0, fmt.Errorf("EMBEDDING_DIMENSIONS must be a positive integer within pgvector limits: %w", err)
	}
	return parsed, nil
}

func ParseMinSalary(value string) (int, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return 0, nil
	}
	parsed, err := strconv.Atoi(trimmed)
	if err != nil || parsed < 0 {
		if err == nil {
			err = errors.New("must not be negative")
		}
		return 0, fmt.Errorf("HH_MIN_SALARY must be a non-negative integer: %w", err)
	}
	return parsed, nil
}

func ParseMinMatchScore(value string) (int, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return DefaultMinMatchScore, nil
	}
	parsed, err := strconv.Atoi(trimmed)
	if err != nil || parsed < 0 || parsed > 100 {
		if err == nil {
			err = errors.New("must be between 0 and 100")
		}
		return 0, fmt.Errorf("HH_MIN_MATCH_SCORE must be an integer from 0 to 100: %w", err)
	}
	return parsed, nil
}

func NormalizeChatMode(mode string) (string, error) {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "" {
		return DefaultChatMode, nil
	}
	switch mode {
	case "off", "review", "auto":
		return mode, nil
	default:
		return "", fmt.Errorf("HH_CHAT_MODE must be one of: off, review, auto; got %q", mode)
	}
}

func NormalizeAutoApplyMode(mode string) (string, error) {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "" {
		return "off", nil
	}
	if mode != "off" && mode != "canary" {
		return "", fmt.Errorf("HH_AUTO_APPLY_MODE must be off or canary")
	}
	return mode, nil
}

func NormalizeSalaryCurrency(value string) (string, error) {
	currency := strings.ToUpper(strings.TrimSpace(value))
	if currency == "" {
		return DefaultMinSalaryCurrency, nil
	}
	if len(currency) != 3 {
		return "", fmt.Errorf("HH_MIN_SALARY_CURRENCY must be a 3-letter currency code: %q", value)
	}
	for _, character := range currency {
		if character < 'A' || character > 'Z' {
			return "", fmt.Errorf("HH_MIN_SALARY_CURRENCY must contain only letters: %q", value)
		}
	}
	return currency, nil
}

func ParseKeywordList(value string) []string {
	parts := strings.Split(value, ",")
	keywords := make([]string, 0, len(parts))
	for _, part := range parts {
		if keyword := strings.TrimSpace(part); keyword != "" {
			keywords = append(keywords, keyword)
		}
	}
	return keywords
}

func ConfiguredSearchURLs(multiple, fallback string) ([]string, error) {
	if strings.TrimSpace(multiple) != "" {
		parts := strings.Split(multiple, "||")
		searchURLs := make([]string, 0, len(parts))
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part != "" {
				searchURLs = append(searchURLs, part)
			}
		}
		if len(searchURLs) == 0 {
			return nil, errors.New("HH_SEARCH_URLS must contain at least one URL")
		}
		return searchURLs, nil
	}
	if strings.TrimSpace(fallback) == "" {
		return nil, nil
	}
	return []string{strings.TrimSpace(fallback)}, nil
}

func ValidQuietHours(spec string) bool {
	parts := strings.Split(strings.TrimSpace(spec), "-")
	if len(parts) != 2 {
		return false
	}
	for _, value := range parts {
		if _, err := time.Parse("15:04", strings.TrimSpace(value)); err != nil {
			return false
		}
	}
	return true
}

func Validate(c Config) error {
	workflowPath := strings.TrimSpace(c.CareerAgentWorkflowPath)
	if workflowPath == "" {
		return errors.New("career-agent-workflow path must not be empty")
	}
	if strings.ContainsRune(workflowPath, '\x00') {
		return errors.New("career-agent-workflow path contains an invalid NUL byte")
	}
	if c.StorageBackend == "postgres" && strings.TrimSpace(c.DatabaseURL) == "" {
		return errors.New("DATABASE_URL is required when STORAGE_BACKEND=postgres")
	}
	if strings.TrimSpace(c.EmbeddingProvider) != "" {
		provider := strings.ToLower(strings.TrimSpace(c.EmbeddingProvider))
		if provider != "openai" && provider != "openai-compatible" {
			return fmt.Errorf("unsupported embedding provider %q", c.EmbeddingProvider)
		}
		if strings.TrimSpace(c.EmbeddingBaseURL) == "" {
			return errors.New("EMBEDDING_BASE_URL (or HH_AI_BASE_URL fallback) is required when semantic embeddings are enabled")
		}
		if strings.TrimSpace(c.EmbeddingModel) == "" {
			return errors.New("EMBEDDING_MODEL is required when semantic embeddings are enabled")
		}
		if c.EmbeddingDimensions <= 0 || c.EmbeddingDimensions > MaxEmbeddingDimensions {
			return fmt.Errorf("EMBEDDING_DIMENSIONS must be between 1 and %d", MaxEmbeddingDimensions)
		}
	}
	if c.HHReadConcurrency < 1 || c.HHReadConcurrency > 8 {
		return errors.New("hh-read-concurrency must be between 1 and 8")
	}
	if c.MinMatchScore < 0 || c.MinMatchScore > 100 {
		return errors.New("min-match-score must be between 0 and 100")
	}
	if c.MinSalary < 0 {
		return errors.New("min-salary must not be negative")
	}
	if c.MaxVacanciesPerRun < 0 {
		return errors.New("max-vacancies-per-run must not be negative")
	}
	if c.MaxApplicationsPerRun < 0 {
		return errors.New("max-applications-per-run must not be negative")
	}
	if c.MaxConversationsPerRun < 0 {
		return errors.New("max-conversations-per-run must not be negative")
	}
	if c.SearchPeriodDays <= 0 {
		return errors.New("search-period-days must be greater than 0")
	}
	if c.CareerAgentMaxSearchProfiles <= 0 {
		return errors.New("max-search-profiles must be greater than 0")
	}
	if c.MaxSearchPagesPerProfile <= 0 {
		return errors.New("max-search-pages-per-profile must be greater than 0")
	}
	if c.MaxSearchPagesPerRun <= 0 {
		return errors.New("max-search-pages-per-run must be greater than 0")
	}
	if _, err := NormalizeAutoApplyMode(c.AutoApplyMode); err != nil {
		return err
	}
	if _, err := NormalizeBrowserTransport(c.BrowserTransport); err != nil {
		return err
	}
	if _, err := NormalizeHHTransport(c.HHTransport); err != nil {
		return err
	}
	if c.AIAttempts < 1 {
		return errors.New("ai-attempts must be greater than 0")
	}
	if c.AITimeout <= 0 {
		return errors.New("ai-timeout must be greater than 0")
	}
	if c.AIConnectTimeout <= 0 {
		return errors.New("ai-connect-timeout must be greater than 0")
	}
	if c.AIConnectTimeout > c.AITimeout {
		return errors.New("ai-connect-timeout must not exceed ai-timeout")
	}
	if c.RequestInterval <= 0 {
		return errors.New("request-interval must be greater than 0")
	}
	if c.MonitorInterval <= 0 || c.NotificationCooldown <= 0 || c.ConversationDisplayTTL <= 0 {
		return errors.New("monitor intervals must be greater than 0")
	}
	if c.MonitorQuietHours != "" && !ValidQuietHours(c.MonitorQuietHours) {
		return errors.New("quiet-hours must use HH:MM-HH:MM")
	}
	if c.FollowUp.AfterApplicationWithoutReply <= 0 || c.FollowUp.AfterCandidateMessageWithoutReply <= 0 || c.FollowUp.MinimumInterval <= 0 || c.FollowUp.MaxFollowUps < 0 {
		return errors.New("follow-up delays must be positive and max-followups non-negative")
	}
	return nil
}
