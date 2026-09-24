package config

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func lookupFrom(values map[string]string) LookupEnv {
	return func(name string) (string, bool) {
		value, ok := values[name]
		return value, ok
	}
}

func loadForTest(t *testing.T, args []string, values map[string]string) Config {
	t.Helper()
	cfg, err := Load(args, lookupFrom(values), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestLoadSafeDefaults(t *testing.T) {
	cfg := loadForTest(t, nil, nil)
	if !cfg.DryRun {
		t.Fatal("DryRun default is not safe")
	}
	if cfg.HHWriteEnabled {
		t.Fatal("HHWriteEnabled default is enabled")
	}
	if cfg.HHMaxWritesPerRun != 1 || cfg.HHMaxWritesPerDay != 5 {
		t.Fatalf("write limits = %d/%d, want 1/5", cfg.HHMaxWritesPerRun, cfg.HHMaxWritesPerDay)
	}
	if cfg.ChatMode != "review" {
		t.Fatalf("ChatMode = %q, want review", cfg.ChatMode)
	}
	if cfg.StorageBackend != "json" {
		t.Fatalf("StorageBackend = %q, want json", cfg.StorageBackend)
	}
	if cfg.MaxConversationsPerRun != 0 {
		t.Fatalf("MaxConversationsPerRun = %d, want 0", cfg.MaxConversationsPerRun)
	}
	if cfg.BrowserProfilePath == "" || filepath.Base(cfg.BrowserProfilePath) != DefaultProfileDir {
		t.Fatalf("BrowserProfilePath = %q, want %q", cfg.BrowserProfilePath, DefaultProfileDir)
	}
	if filepath.Base(cfg.CareerAgentWorkflowPath) != "career_agent_workflow.json" {
		t.Fatalf("CareerAgentWorkflowPath = %q, want career_agent_workflow.json", cfg.CareerAgentWorkflowPath)
	}
	if cfg.SearchPeriodDays != DefaultSearchPeriodDays || cfg.CareerAgentMaxSearchProfiles != DefaultCareerAgentMaxSearchProfiles || cfg.AutoApplyMode != "off" {
		t.Fatalf("unexpected Career Agent defaults: period=%d profiles=%d mode=%q", cfg.SearchPeriodDays, cfg.CareerAgentMaxSearchProfiles, cfg.AutoApplyMode)
	}
	if cfg.HHReadConcurrency != 4 || cfg.MonitorInterval != 15*time.Minute || cfg.ConversationDisplayTTL != time.Minute {
		t.Fatalf("unexpected runtime defaults: concurrency=%d interval=%s ttl=%s", cfg.HHReadConcurrency, cfg.MonitorInterval, cfg.ConversationDisplayTTL)
	}
	if cfg.CareerAgentDailySchedulerEnabled || cfg.CareerAgentDailyInterval != DefaultCareerAgentDailyInterval {
		t.Fatalf("unexpected daily scheduler defaults: enabled=%t interval=%s", cfg.CareerAgentDailySchedulerEnabled, cfg.CareerAgentDailyInterval)
	}
}

func TestCareerAgentDailySchedulerConfigurationIsDefaultOffAndCLIOverridesEnv(t *testing.T) {
	values := map[string]string{"HH_CAREER_AGENT_DAILY_ENABLED": "true", "HH_CAREER_AGENT_DAILY_INTERVAL": "2h"}
	cfg := loadForTest(t, nil, values)
	if !cfg.CareerAgentDailySchedulerEnabled || cfg.CareerAgentDailyInterval != 2*time.Hour {
		t.Fatalf("environment daily scheduler config=%+v", cfg)
	}
	cfg = loadForTest(t, []string{"--career-agent-daily-scheduler=false", "--career-agent-daily-interval", "30m"}, values)
	if cfg.CareerAgentDailySchedulerEnabled || cfg.CareerAgentDailyInterval != 30*time.Minute {
		t.Fatalf("CLI daily scheduler precedence config=%+v", cfg)
	}
}

func TestLoadParsesHHAPIConfiguration(t *testing.T) {
	lookup := lookupFrom(map[string]string{
		"HH_TRANSPORT":           "api",
		"HH_API_BASE_URL":        "http://api.test",
		"HH_OAUTH_AUTHORIZE_URL": "http://authorize.test",
		"HH_OAUTH_TOKEN_URL":     "http://token.test",
		"HH_OAUTH_CLIENT_ID":     "test-client",
		"HH_OAUTH_CLIENT_SECRET": "test-secret",
		"HH_OAUTH_REDIRECT_URI":  "http://127.0.0.1/callback",
		"HH_OAUTH_USER_AGENT":    "test-agent",
		"HH_API_TOKEN_FILE":      ".test-token.json",
	})
	cfg, err := Load(nil, lookup, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HHTransport != "api" || cfg.HHAPIBaseURL != "http://api.test" || cfg.HHOAuthAuthorizeURL != "http://authorize.test" || cfg.HHOAuthTokenURL != "http://token.test" || cfg.HHOAuthClientID != "test-client" || cfg.HHOAuthClientSecret != "test-secret" || cfg.HHOAuthRedirectURI != "http://127.0.0.1/callback" || cfg.HHOAuthUserAgent != "test-agent" || cfg.HHAPITokenFile != ".test-token.json" {
		t.Fatal("HH API environment values were not parsed")
	}
}

func TestLoadHHAPIConfigurationDefaultsKeepCredentialsEmpty(t *testing.T) {
	cfg := loadForTest(t, nil, nil)
	if cfg.HHTransport != "browser" || cfg.HHAPIBaseURL != "https://api.hh.ru" || cfg.HHOAuthAuthorizeURL != "https://hh.ru/oauth/authorize" || cfg.HHOAuthTokenURL != "https://api.hh.ru/token" || cfg.HHAPITokenFile != ".hh-api-token.json" {
		t.Fatalf("unexpected HH API defaults: %+v", cfg)
	}
	if cfg.HHOAuthClientID != "" || cfg.HHOAuthClientSecret != "" || cfg.HHOAuthRedirectURI != "" || cfg.HHOAuthUserAgent != "" {
		t.Fatalf("HH API credentials must default to empty: %+v", cfg)
	}
}

func TestLoadHHTransportValidationAndCLIPrecedence(t *testing.T) {
	for _, transport := range []string{"browser", "api", "auto"} {
		t.Run(transport, func(t *testing.T) {
			cfg := loadForTest(t, nil, map[string]string{"HH_TRANSPORT": transport})
			if cfg.HHTransport != transport {
				t.Fatalf("HHTransport = %q, want %q", cfg.HHTransport, transport)
			}
		})
	}
	if _, err := Load(nil, lookupFrom(map[string]string{"HH_TRANSPORT": "http"}), t.TempDir()); err == nil {
		t.Fatal("invalid HH transport was accepted")
	}
	cfg := loadForTest(t, []string{"--hh-transport", "browser"}, map[string]string{"HH_TRANSPORT": "api"})
	if cfg.HHTransport != "browser" {
		t.Fatalf("CLI HH transport did not override environment: %q", cfg.HHTransport)
	}
}

func TestConversationRunLimitParsingAndValidation(t *testing.T) {
	workDir := t.TempDir()
	cfg, err := Load(nil, lookupFrom(map[string]string{"HH_MAX_CONVERSATIONS_PER_RUN": "3"}), workDir)
	if err != nil || cfg.MaxConversationsPerRun != 3 {
		t.Fatalf("environment limit=%d err=%v", cfg.MaxConversationsPerRun, err)
	}
	cfg, err = Load([]string{"--max-conversations-per-run", "5"}, lookupFrom(map[string]string{"HH_MAX_CONVERSATIONS_PER_RUN": "3"}), workDir)
	if err != nil || cfg.MaxConversationsPerRun != 5 {
		t.Fatalf("CLI limit=%d err=%v", cfg.MaxConversationsPerRun, err)
	}
	for name, test := range map[string]struct {
		args []string
		env  map[string]string
	}{
		"negative flag": {args: []string{"--max-conversations-per-run", "-1"}},
		"invalid env":   {env: map[string]string{"HH_MAX_CONVERSATIONS_PER_RUN": "many"}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Load(test.args, lookupFrom(test.env), workDir); err == nil {
				t.Fatal("invalid conversation limit was accepted")
			}
		})
	}
}

func TestLoadEnvironmentParsing(t *testing.T) {
	cfg := loadForTest(t, nil, map[string]string{
		"HH_DRY_RUN":                  "false",
		"HH_WRITE_ENABLED":            "true",
		"HH_MAX_WRITES_PER_RUN":       "3",
		"HH_MAX_WRITES_PER_DAY":       "7",
		"HH_READ_CONCURRENCY":         "2",
		"HH_SYNC_INTERVAL":            "2h",
		"HH_CONVERSATION_DISPLAY_TTL": "45s",
		"HH_BACKGROUND_INBOX_REFRESH": "true",
		"HH_INCLUDE_KEYWORDS":         " Python, Django, REST API ",
		"HH_SEARCH_URL":               "https://hh.example/search/vacancy?text=python",
		"HH_SEARCH_URLS":              "https://hh.example/search/vacancy?text=python|| https://hh.example/search/vacancy?text=django ",
		"HH_SEARCH_PERIOD_DAYS":       "3",
		"HH_MAX_SEARCH_PROFILES":      "9",
		"HH_AUTO_APPLY_MODE":          "canary",
		"HH_CANDIDATE_PROFILE":        filepath.Join(t.TempDir(), "profile.json"),
		"HH_AI_BASE_URL":              "https://ai.example/v1",
		"HH_AI_MODEL":                 "fixture-model",
	})
	if cfg.DryRun || !cfg.HHWriteEnabled || cfg.HHMaxWritesPerRun != 3 || cfg.HHMaxWritesPerDay != 7 {
		t.Fatalf("boolean/limit parsing failed: %+v", cfg)
	}
	if cfg.HHReadConcurrency != 2 || cfg.MonitorInterval != 2*time.Hour || cfg.ConversationDisplayTTL != 45*time.Second || !cfg.BackgroundInboxRefresh {
		t.Fatalf("integer/duration parsing failed: %+v", cfg)
	}
	if got := strings.Join(cfg.IncludeKeywords, "|"); got != "Python|Django|REST API" {
		t.Fatalf("keywords = %q", got)
	}
	if len(cfg.SearchURLs) != 2 || cfg.SearchURLs[1] != "https://hh.example/search/vacancy?text=django" {
		t.Fatalf("search URLs = %#v", cfg.SearchURLs)
	}
	if cfg.SearchPeriodDays != 3 || cfg.CareerAgentMaxSearchProfiles != 9 || cfg.AutoApplyMode != "canary" {
		t.Fatalf("Career Agent config = period=%d profiles=%d mode=%q", cfg.SearchPeriodDays, cfg.CareerAgentMaxSearchProfiles, cfg.AutoApplyMode)
	}
	if cfg.CandidateProfilePath == "" || cfg.AIBaseURL != "https://ai.example/v1" || cfg.AIModel != "fixture-model" {
		t.Fatalf("path/URL parsing failed: %+v", cfg)
	}
}

func TestLoadCLIOverridesEnvironment(t *testing.T) {
	workDir := t.TempDir()
	cfg, err := Load([]string{
		"--ai-model", "cli-model",
		"--storage-backend", "json",
		"--candidate-id", "cli-candidate",
		"--candidate-profile", filepath.Join(workDir, "cli-profile.json"),
		"--dry-run=false",
		"--hh-write-enabled=true",
		"--min-salary", "90000",
		"--chat-mode", "off",
		"--sync-interval", "30s",
		"--conversation-display-ttl", "5s",
		"--career-agent-workflow", filepath.Join(workDir, "cli-workflow.json"),
	}, lookupFrom(map[string]string{
		"HH_AI_MODEL":                 "env-model",
		"STORAGE_BACKEND":             "postgres",
		"HH_CANDIDATE_ID":             "env-candidate",
		"HH_CANDIDATE_PROFILE":        filepath.Join(workDir, "env-profile.json"),
		"HH_DRY_RUN":                  "true",
		"HH_WRITE_ENABLED":            "false",
		"HH_MIN_SALARY":               "60000",
		"HH_CHAT_MODE":                "review",
		"HH_SYNC_INTERVAL":            "2h",
		"HH_CONVERSATION_DISPLAY_TTL": "2m",
		"HH_CAREER_AGENT_WORKFLOW":    filepath.Join(workDir, "env-workflow.json"),
	}), workDir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AIModel != "cli-model" || cfg.StorageBackend != "json" || cfg.CandidateID != "cli-candidate" || cfg.CandidateProfilePath != filepath.Join(workDir, "cli-profile.json") || cfg.CareerAgentWorkflowPath != filepath.Join(workDir, "cli-workflow.json") {
		t.Fatalf("CLI did not override env: %+v", cfg)
	}
	if cfg.DryRun || !cfg.HHWriteEnabled || cfg.MinSalary != 90000 || cfg.ChatMode != "off" || cfg.MonitorInterval != 30*time.Second || cfg.ConversationDisplayTTL != 5*time.Second {
		t.Fatalf("CLI precedence values incorrect: %+v", cfg)
	}
}

func TestLoadRejectsInvalidCareerWorkflowPath(t *testing.T) {
	for _, args := range [][]string{{"--career-agent-workflow", ""}, {"--career-agent-workflow", "bad\x00path"}} {
		if _, err := Load(args, lookupFrom(nil), t.TempDir()); err == nil {
			t.Fatalf("invalid workflow path %q was accepted", args[1])
		}
	}
}

func TestLoadDotEnvTreatsValuesAsData(t *testing.T) {
	workDir := t.TempDir()
	marker := filepath.Join(workDir, "should-not-exist")
	dotenv := strings.Join([]string{
		"HH_AI_MODEL='model $(touch " + marker + ")'",
		"HH_INCLUDE_KEYWORDS=Python,Go,REST API # data comment",
		"HH_SEARCH_URLS=\"https://one.example/search||https://two.example/search\"",
		"export HH_DRY_RUN=false",
	}, "\n")
	if err := os.WriteFile(filepath.Join(workDir, ".env"), []byte(dotenv), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(nil, lookupFrom(map[string]string{"HH_AI_MODEL": "shell-model", "HH_DRY_RUN": "true"}), workDir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AIModel != "model $(touch "+marker+")" {
		t.Fatalf("dotenv special characters changed: %q", cfg.AIModel)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("dotenv value was executed: stat error=%v", err)
	}
	if got := strings.Join(cfg.IncludeKeywords, "|"); got != "Python|Go|REST API" {
		t.Fatalf("dotenv comma data = %q", got)
	}
	if cfg.DryRun {
		t.Fatal("dotenv boolean was not parsed")
	}
	if len(cfg.SearchURLs) != 2 {
		t.Fatalf("dotenv search URLs = %#v", cfg.SearchURLs)
	}
}

func TestEmbeddingConfigurationIsIndependentWithExplicitPrecedence(t *testing.T) {
	cfg := loadForTest(t, nil, map[string]string{
		"HH_AI_BASE_URL":       "https://chat.example/v1",
		"HH_AI_API_KEY":        "chat-key",
		"HH_AI_MODEL":          "chat-model",
		"EMBEDDING_PROVIDER":   "openai-compatible",
		"EMBEDDING_BASE_URL":   "https://embed.example/v1",
		"EMBEDDING_API_KEY":    "embed-key",
		"EMBEDDING_MODEL":      "embed-model",
		"EMBEDDING_DIMENSIONS": "1024",
	})
	if cfg.AIBaseURL != "https://chat.example/v1" || cfg.AIAPIKey != "chat-key" || cfg.AIModel != "chat-model" {
		t.Fatalf("chat config changed: %+v", cfg)
	}
	if cfg.EmbeddingBaseURL != "https://embed.example/v1" || cfg.EmbeddingAPIKey != "embed-key" || cfg.EmbeddingModel != "embed-model" || cfg.EmbeddingDimensions != 1024 {
		t.Fatalf("embedding config was not independent: %+v", cfg)
	}
}

func TestEmbeddingConfigurationFallsBackToChatEndpointAndKey(t *testing.T) {
	cfg := loadForTest(t, nil, map[string]string{
		"HH_AI_BASE_URL":       "https://chat.example/v1",
		"HH_AI_API_KEY":        "chat-key",
		"EMBEDDING_PROVIDER":   "openai-compatible",
		"EMBEDDING_MODEL":      "embed-model",
		"EMBEDDING_DIMENSIONS": "1536",
	})
	if cfg.EmbeddingBaseURL != cfg.AIBaseURL || cfg.EmbeddingAPIKey != cfg.AIAPIKey {
		t.Fatalf("compatibility fallback mismatch: embedding=%q/%q chat=%q/%q", cfg.EmbeddingBaseURL, cfg.EmbeddingAPIKey, cfg.AIBaseURL, cfg.AIAPIKey)
	}
}

func TestEmbeddingDimensionsAreValidatedOnlyByContractBounds(t *testing.T) {
	if _, err := ParseEmbeddingDimensions("0"); err == nil {
		t.Fatal("zero embedding dimensions must fail")
	}
	if _, err := ParseEmbeddingDimensions("16001"); err == nil {
		t.Fatal("dimensions above pgvector practical limit must fail")
	}
	cfg := loadForTest(t, nil, map[string]string{"EMBEDDING_PROVIDER": "openai-compatible", "EMBEDDING_DIMENSIONS": "1024"})
	if cfg.EmbeddingDimensions != 1024 {
		t.Fatalf("dimensions=%d", cfg.EmbeddingDimensions)
	}
}

func TestConfigDefaultsBoundSearchPagination(t *testing.T) {
	cfg, err := Load(nil, lookupFrom(map[string]string{}), ".")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MaxSearchPagesPerProfile != 3 || cfg.MaxSearchPagesPerRun != 48 {
		t.Fatalf("unexpected search read bounds: profile=%d run=%d", cfg.MaxSearchPagesPerProfile, cfg.MaxSearchPagesPerRun)
	}
}

func TestConfigRejectsNonPositiveSearchPaginationBounds(t *testing.T) {
	_, err := Load(nil, lookupFrom(map[string]string{"HH_MAX_SEARCH_PAGES_PER_PROFILE": "0"}), ".")
	if err == nil || !strings.Contains(err.Error(), "max-search-pages-per-profile") {
		t.Fatalf("non-positive page bound was accepted: %v", err)
	}
}

func TestConfigCLIOverridesSearchPaginationEnv(t *testing.T) {
	cfg, err := Load([]string{"--max-search-pages-per-profile=5", "--max-search-pages-per-run=9"}, lookupFrom(map[string]string{
		"HH_MAX_SEARCH_PAGES_PER_PROFILE": "2",
		"HH_MAX_SEARCH_PAGES_PER_RUN":     "4",
	}), ".")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MaxSearchPagesPerProfile != 5 || cfg.MaxSearchPagesPerRun != 9 {
		t.Fatalf("CLI did not override env: profile=%d run=%d", cfg.MaxSearchPagesPerProfile, cfg.MaxSearchPagesPerRun)
	}
}

func TestLoadInvalidValuesFailClosed(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		args []string
	}{
		{"boolean", map[string]string{"HH_DRY_RUN": "sometimes"}, nil},
		{"duration", map[string]string{"HH_SYNC_INTERVAL": "soon"}, nil},
		{"integer", map[string]string{"HH_MAX_WRITES_PER_DAY": "-1"}, nil},
		{"concurrency", map[string]string{"HH_READ_CONCURRENCY": "9"}, nil},
		{"chat mode", map[string]string{"HH_CHAT_MODE": "send"}, nil},
		{"storage", map[string]string{"STORAGE_BACKEND": "sqlite"}, nil},
		{"postgres DSN", map[string]string{"STORAGE_BACKEND": "postgres", "DATABASE_URL": ""}, nil},
		{"flag score", nil, []string{"--min-match-score", "101"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := Load(test.args, lookupFrom(test.env), t.TempDir())
			if err == nil {
				t.Fatal("invalid configuration was accepted")
			}
		})
	}
}

func TestLoadDoesNotTouchGlobalParserState(t *testing.T) {
	original := flag.CommandLine
	defer func() { flag.CommandLine = original }()
	before := flag.CommandLine
	if _, err := Load([]string{"--ai-model", "fixture"}, lookupFrom(nil), t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if flag.CommandLine != before {
		t.Fatal("config.Load mutated global flag.CommandLine")
	}
}

func TestSplitLeadingArgsUsesCanonicalFlags(t *testing.T) {
	leading, remainder, err := SplitLeadingArgs([]string{"--ai-model", "fixture", "hh", "workflow"}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(leading, "|") != "--ai-model|fixture" || strings.Join(remainder, "|") != "hh|workflow" {
		t.Fatalf("split = leading=%#v remainder=%#v", leading, remainder)
	}
}

func TestLoadSecretValuesDoNotAppearInValidationErrors(t *testing.T) {
	secret := "postgres://user:password@example.invalid/db?token=secret"
	_, err := Load(nil, lookupFrom(map[string]string{
		"STORAGE_BACKEND": "postgres",
		"DATABASE_URL":    secret,
		"HH_CHAT_MODE":    "invalid",
	}), t.TempDir())
	if err == nil || strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), "password") {
		t.Fatalf("secret leaked or validation unexpectedly succeeded: %v", err)
	}
}
