package config

import (
	"errors"
	"flag"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// ErrHelp is returned when the explicit FlagSet handles -h. The process
// entrypoint uses it to preserve the historical successful help exit.
var ErrHelp = flag.ErrHelp

// Load parses application flags and environment values without touching
// global flag.CommandLine or os.Args. CLI values take precedence over env;
// dotenv values retain the historical application behavior of being loaded
// before environment lookup.
func Load(args []string, lookup LookupEnv, workingDir string) (Config, error) {
	if lookup == nil {
		lookup = os.LookupEnv
	}
	if strings.TrimSpace(workingDir) == "" {
		workingDir, _ = os.Getwd()
		if workingDir == "" {
			workingDir = "."
		}
	}

	dotenv, _ := readDotEnv(filepath.Join(workingDir, ".env"))
	cfg := defaults(workingDir)
	var includeKeywordsRaw, excludeKeywordsRaw string
	fs := flag.NewFlagSet("hh-ai-responder", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	registerFlags(fs, &cfg, &includeKeywordsRaw, &excludeKeywordsRaw, workingDir)
	if err := fs.Parse(args); err != nil {
		return Config{}, err
	}
	flags := visitedFlags(fs)
	var err error
	get := func(name, fallback string) string { return getEnv(lookup, dotenv, name, fallback) }
	getBool := func(name string, fallback bool) (bool, error) {
		return getEnvBool(lookup, dotenv, name, fallback)
	}
	parseDuration := func(name string, fallback time.Duration) (time.Duration, error) {
		return parseDurationEnv(lookup, dotenv, name, fallback)
	}
	lookupValue := func(name string) (string, bool) { return envValue(lookup, dotenv, name) }

	if !isWebCommand(args) {
		var err error
		if cfg.FollowUp, err = followUpEnv(cfg.FollowUp, lookupValue, flags); err != nil {
			return Config{}, err
		}
	}
	if !flags["u"] {
		fallbackSearchURL := get("HH_SEARCH_URL", cfg.SearchURL)
		var err error
		multiple, _ := lookupValue("HH_SEARCH_URLS")
		if cfg.SearchURLs, err = ConfiguredSearchURLs(multiple, fallbackSearchURL); err != nil {
			return Config{}, err
		}
		if strings.TrimSpace(multiple) == "" {
			cfg.SearchURL = fallbackSearchURL
		}
	}
	if !flags["r"] {
		cfg.Resume = get("HH_RESUME", cfg.Resume)
	}
	if !flags["search-period-days"] {
		if raw, ok := lookupValue("HH_SEARCH_PERIOD_DAYS"); ok && raw != "" {
			cfg.SearchPeriodDays, err = atoiEnv(raw, "HH_SEARCH_PERIOD_DAYS")
			if err != nil {
				return Config{}, err
			}
		}
	}
	if !flags["max-search-profiles"] {
		if raw, ok := lookupValue("HH_MAX_SEARCH_PROFILES"); ok && raw != "" {
			cfg.CareerAgentMaxSearchProfiles, err = atoiEnv(raw, "HH_MAX_SEARCH_PROFILES")
			if err != nil {
				return Config{}, err
			}
		}
	}
	if !flags["storage-backend"] {
		cfg.StorageBackend = get("STORAGE_BACKEND", cfg.StorageBackend)
	}
	if !flags["database-url"] {
		cfg.DatabaseURL = get("DATABASE_URL", cfg.DatabaseURL)
	}
	if !flags["candidate-id"] {
		cfg.CandidateID = get("HH_CANDIDATE_ID", cfg.CandidateID)
	}
	if cfg.StorageBackend, err = NormalizeStorageBackend(cfg.StorageBackend); err != nil {
		return Config{}, err
	}
	if !flags["already-responded-state"] {
		cfg.AlreadyRespondedStatePath = get("HH_ALREADY_RESPONDED_STATE", cfg.AlreadyRespondedStatePath)
	}
	if !flags["candidate-profile"] {
		cfg.CandidateProfilePath = get("HH_CANDIDATE_PROFILE", cfg.CandidateProfilePath)
	}
	if !flags["candidate-stories"] {
		cfg.CandidateStoriesPath = get("HH_CANDIDATE_STORIES", cfg.CandidateStoriesPath)
	}
	if !flags["career-agent-result"] {
		cfg.CareerAgentResultPath = get("HH_CAREER_AGENT_RESULT", cfg.CareerAgentResultPath)
	}
	if !flags["career-agent-feedback"] {
		cfg.CareerAgentFeedbackPath = get("HH_CAREER_AGENT_FEEDBACK", cfg.CareerAgentFeedbackPath)
	}
	if !flags["resume-registry"] {
		cfg.ResumeRegistryPath = get("HH_RESUME_REGISTRY", cfg.ResumeRegistryPath)
	}
	if !flags["browser-profile"] {
		cfg.BrowserProfilePath = get("HH_BROWSER_PROFILE", cfg.BrowserProfilePath)
	}
	if !flags["browser-trace-vacancy"] {
		cfg.BrowserTraceVacancyURL = get("HH_BROWSER_TRACE_VACANCY", cfg.BrowserTraceVacancyURL)
	}
	if !flags["browser-transport"] {
		cfg.BrowserTransport = get("HH_BROWSER_TRANSPORT", cfg.BrowserTransport)
	}
	if !flags["browser-headless"] {
		if cfg.BrowserHeadless, err = getBool("HH_BROWSER_HEADLESS", cfg.BrowserHeadless); err != nil {
			return Config{}, err
		}
	}
	if cfg.BrowserTransport, err = NormalizeBrowserTransport(cfg.BrowserTransport); err != nil {
		return Config{}, err
	}
	if !flags["hh-read-concurrency"] {
		if raw, ok := lookupValue("HH_READ_CONCURRENCY"); ok && raw != "" {
			cfg.HHReadConcurrency, err = atoiEnv(raw, "HH_READ_CONCURRENCY")
			if err != nil {
				return Config{}, err
			}
		}
	}
	if !flags["hh-sync-state"] {
		cfg.HHSyncStatePath = get("HH_SYNC_STATE", cfg.HHSyncStatePath)
	}
	if !flags["sync-interval"] {
		if cfg.MonitorInterval, err = parseDuration("HH_SYNC_INTERVAL", cfg.MonitorInterval); err != nil {
			return Config{}, err
		}
	}
	if !flags["quiet-hours"] {
		cfg.MonitorQuietHours = get("HH_QUIET_HOURS", cfg.MonitorQuietHours)
	}
	if !flags["notification-cooldown"] {
		if cfg.NotificationCooldown, err = parseDuration("HH_NOTIFICATION_COOLDOWN", cfg.NotificationCooldown); err != nil {
			return Config{}, err
		}
	}
	if !flags["conversation-display-ttl"] {
		if cfg.ConversationDisplayTTL, err = parseDuration("HH_CONVERSATION_DISPLAY_TTL", cfg.ConversationDisplayTTL); err != nil {
			return Config{}, err
		}
	}
	if !flags["background-inbox-refresh"] {
		if cfg.BackgroundInboxRefresh, err = getBool("HH_BACKGROUND_INBOX_REFRESH", cfg.BackgroundInboxRefresh); err != nil {
			return Config{}, err
		}
	}
	if !flags["ai-base-url"] {
		cfg.AIBaseURL = get("HH_AI_BASE_URL", cfg.AIBaseURL)
	}
	if !flags["ai-model"] {
		cfg.AIModel = get("HH_AI_MODEL", cfg.AIModel)
	}
	if !flags["ai-api-key"] {
		cfg.AIAPIKey = get("HH_AI_API_KEY", cfg.AIAPIKey)
	}
	cfg.EmbeddingProvider = get("EMBEDDING_PROVIDER", cfg.EmbeddingProvider)
	cfg.EmbeddingBaseURL = get("EMBEDDING_BASE_URL", cfg.AIBaseURL)
	cfg.EmbeddingAPIKey = get("EMBEDDING_API_KEY", cfg.AIAPIKey)
	cfg.EmbeddingModel = get("EMBEDDING_MODEL", cfg.EmbeddingModel)
	if raw, ok := lookupValue("EMBEDDING_DIMENSIONS"); ok && strings.TrimSpace(raw) != "" {
		if cfg.EmbeddingDimensions, err = ParseEmbeddingDimensions(raw); err != nil {
			return Config{}, err
		}
	}
	if !flags["letter-prompt"] {
		cfg.ExtraLetterPrompt = get("HH_LETTER_PROMPT", cfg.ExtraLetterPrompt)
	}
	if !flags["solution-prompt"] {
		cfg.ExtraTestSolutionPrompt = get("HH_SOLUTION_PROMPT", cfg.ExtraTestSolutionPrompt)
	}
	if !flags["chat-reply-prompt"] {
		cfg.ExtraChatReplyPrompt = get("HH_CHAT_REPLY_PROMPT", cfg.ExtraChatReplyPrompt)
	}
	if !flags["contacts"] {
		cfg.Contacts = get("HH_CONTACTS", cfg.Contacts)
	}
	if !flags["github-url"] {
		cfg.GithubURL = get("HH_GITHUB_URL", cfg.GithubURL)
	}
	if !flags["min-salary"] {
		value, _ := lookupValue("HH_MIN_SALARY")
		if cfg.MinSalary, err = ParseMinSalary(value); err != nil {
			return Config{}, err
		}
	}
	if !flags["min-salary-currency"] {
		cfg.MinSalaryCurrency = get("HH_MIN_SALARY_CURRENCY", cfg.MinSalaryCurrency)
	}
	if cfg.MinSalaryCurrency, err = NormalizeSalaryCurrency(cfg.MinSalaryCurrency); err != nil {
		return Config{}, err
	}
	if !flags["include-keywords"] {
		includeKeywordsRaw = get("HH_INCLUDE_KEYWORDS", includeKeywordsRaw)
	}
	if !flags["exclude-keywords"] {
		excludeKeywordsRaw = get("HH_EXCLUDE_KEYWORDS", excludeKeywordsRaw)
	}
	cfg.IncludeKeywords = ParseKeywordList(includeKeywordsRaw)
	cfg.ExcludeKeywords = ParseKeywordList(excludeKeywordsRaw)
	if !flags["dry-run"] {
		if cfg.DryRun, err = getBool("HH_DRY_RUN", cfg.DryRun); err != nil {
			return Config{}, err
		}
	}
	if !flags["hh-write-enabled"] {
		if cfg.HHWriteEnabled, err = getBool("HH_WRITE_ENABLED", cfg.HHWriteEnabled); err != nil {
			return Config{}, err
		}
	}
	if !flags["hh-chat-url"] {
		cfg.HHChatURL = get("HH_CHAT_URL", cfg.HHChatURL)
	}
	if !flags["hh-max-writes-per-run"] {
		value, _ := lookupValue("HH_MAX_WRITES_PER_RUN")
		if cfg.HHMaxWritesPerRun, err = ParseNonNegativeInt(value, "HH_MAX_WRITES_PER_RUN", cfg.HHMaxWritesPerRun); err != nil {
			return Config{}, err
		}
	}
	if !flags["hh-max-writes-per-day"] {
		value, _ := lookupValue("HH_MAX_WRITES_PER_DAY")
		if cfg.HHMaxWritesPerDay, err = ParseNonNegativeInt(value, "HH_MAX_WRITES_PER_DAY", cfg.HHMaxWritesPerDay); err != nil {
			return Config{}, err
		}
	}
	if !flags["auto-apply"] {
		if cfg.AutoApply, err = getBool("HH_AUTO_APPLY", cfg.AutoApply); err != nil {
			return Config{}, err
		}
	}
	if !flags["auto-apply-mode"] {
		cfg.AutoApplyMode = get("HH_AUTO_APPLY_MODE", cfg.AutoApplyMode)
	}
	if cfg.AutoApplyMode, err = NormalizeAutoApplyMode(cfg.AutoApplyMode); err != nil {
		return Config{}, err
	}
	if !flags["auto-chat"] {
		if cfg.AutoChat, err = getBool("HH_AUTO_CHAT", cfg.AutoChat); err != nil {
			return Config{}, err
		}
	}
	if !flags["auto-touch"] {
		if cfg.AutoTouch, err = getBool("HH_AUTO_TOUCH", cfg.AutoTouch); err != nil {
			return Config{}, err
		}
	}
	if !flags["auto-job-status"] {
		if cfg.AutoJobStatus, err = getBool("HH_AUTO_JOB_STATUS", cfg.AutoJobStatus); err != nil {
			return Config{}, err
		}
	}
	if !flags["run-once"] {
		if cfg.RunOnce, err = getBool("HH_RUN_ONCE", cfg.RunOnce); err != nil {
			return Config{}, err
		}
	}
	if !flags["max-vacancies-per-run"] {
		value, _ := lookupValue("HH_MAX_VACANCIES_PER_RUN")
		if cfg.MaxVacanciesPerRun, err = ParseNonNegativeInt(value, "HH_MAX_VACANCIES_PER_RUN", cfg.MaxVacanciesPerRun); err != nil {
			return Config{}, err
		}
	}
	if !flags["max-applications-per-run"] {
		value, _ := lookupValue("HH_MAX_APPLICATIONS_PER_RUN")
		if cfg.MaxApplicationsPerRun, err = ParseNonNegativeInt(value, "HH_MAX_APPLICATIONS_PER_RUN", cfg.MaxApplicationsPerRun); err != nil {
			return Config{}, err
		}
	}
	if !flags["max-conversations-per-run"] {
		value, _ := lookupValue("HH_MAX_CONVERSATIONS_PER_RUN")
		if cfg.MaxConversationsPerRun, err = ParseNonNegativeInt(value, "HH_MAX_CONVERSATIONS_PER_RUN", cfg.MaxConversationsPerRun); err != nil {
			return Config{}, err
		}
	}
	chatModeFromEnv, hasChatModeEnv := lookupValue("HH_CHAT_MODE")
	if !flags["chat-mode"] && hasChatModeEnv && strings.TrimSpace(chatModeFromEnv) != "" {
		cfg.ChatMode = chatModeFromEnv
	} else if !flags["chat-mode"] && !hasChatModeEnv {
		// Legacy HH_AUTO_CHAT is only a fallback when explicit chat mode is absent.
		if flags["auto-chat"] {
			if cfg.AutoChat {
				cfg.ChatMode = "auto"
			} else {
				cfg.ChatMode = "off"
			}
		} else if autoChat, ok := lookupValue("HH_AUTO_CHAT"); ok && autoChat != "" {
			if cfg.AutoChat {
				cfg.ChatMode = "auto"
			} else {
				cfg.ChatMode = "off"
			}
		}
	}
	if cfg.ChatMode, err = NormalizeChatMode(cfg.ChatMode); err != nil {
		return Config{}, err
	}
	cfg.AutoChat = cfg.ChatMode == "auto"
	if !flags["min-match-score"] {
		value, _ := lookupValue("HH_MIN_MATCH_SCORE")
		if cfg.MinMatchScore, err = ParseMinMatchScore(value); err != nil {
			return Config{}, err
		}
	}
	if err := Validate(cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// SplitLeadingArgs identifies application flags that precede a top-level
// command. It uses the same canonical flag registration as Load and does not
// read environment values or mutate global flag/os.Args state. The returned
// remainder starts at the first positional command.
func SplitLeadingArgs(args []string, workingDir string) (leading, remainder []string, err error) {
	if strings.TrimSpace(workingDir) == "" {
		workingDir, _ = os.Getwd()
		if workingDir == "" {
			workingDir = "."
		}
	}
	cfg := defaults(workingDir)
	var includeKeywordsRaw, excludeKeywordsRaw string
	fs := flag.NewFlagSet("hh-ai-responder", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	registerFlags(fs, &cfg, &includeKeywordsRaw, &excludeKeywordsRaw, workingDir)
	if err := fs.Parse(args); err != nil {
		return nil, nil, err
	}
	remainder = append([]string(nil), fs.Args()...)
	consumed := len(args) - len(remainder)
	return append([]string(nil), args[:consumed]...), remainder, nil
}

func registerFlags(fs *flag.FlagSet, cfg *Config, includeKeywordsRaw, excludeKeywordsRaw *string, wd string) {
	fs.StringVar(&cfg.SearchURL, "u", "", "URL для поиска вакансий")
	fs.IntVar(&cfg.SearchPeriodDays, "search-period-days", DefaultSearchPeriodDays, "Период свежести HH search в днях")
	fs.IntVar(&cfg.CareerAgentMaxSearchProfiles, "max-search-profiles", DefaultCareerAgentMaxSearchProfiles, "Максимум автоматически построенных search profiles")
	fs.StringVar(&cfg.StorageBackend, "storage-backend", cfg.StorageBackend, "Хранилище вакансий: json или postgres")
	fs.StringVar(&cfg.DatabaseURL, "database-url", "", "PostgreSQL connection URL (только при storage-backend=postgres)")
	fs.StringVar(&cfg.CandidateID, "candidate-id", cfg.CandidateID, "Стабильный canonical ID кандидата")
	fs.StringVar(&cfg.CookiesPath, "c", filepath.Join(wd, "cookies.txt"), "Путь к файлу cookies")
	fs.StringVar(&cfg.LogLevel, "l", DefaultLogLevel, "Уровень логирования: debug, info, warn, error")
	fs.StringVar(&cfg.Resume, "r", "", "ID резюме (если не указан — используется последнее)")
	fs.StringVar(&cfg.OutputPath, "o", "", "Файл для вывода результатов (по умолчанию — в STDOUT)")
	fs.IntVar(&cfg.MaxResponses, "mr", 0, "Пропускать вакансии с количеством откликов больше N")
	fs.BoolVar(&cfg.ListResumes, "R", false, "Показать список резюме и выйти")
	fs.BoolVar(&cfg.ForceLetter, "force-letter", false, "Всегда генерировать сопроводительное письмо")
	fs.DurationVar(&cfg.AITimeout, "ai-timeout", DefaultAITimeout, "Общий таймаут AI-запроса: соединение и чтение ответа")
	fs.DurationVar(&cfg.AIConnectTimeout, "ai-connect-timeout", DefaultAIConnectTimeout, "Таймаут соединения с AI-сервером")
	fs.IntVar(&cfg.HHReadConcurrency, "hh-read-concurrency", DefaultHHReadConcurrency, "Maximum independent HH reads (1..8; existing request interval still applies)")
	fs.DurationVar(&cfg.RequestInterval, "request-interval", DefaultRequestInterval, "Минимальный интервал между запросами к hh.ru")
	fs.IntVar(&cfg.AIAttempts, "ai-attempts", DefaultAIAttempts, "Количество попыток отправить запрос к ИИ")
	fs.StringVar(&cfg.AIAPIKey, "ai-api-key", "", "API-ключ AI")
	fs.StringVar(&cfg.AIBaseURL, "ai-base-url", DefaultAIBaseURL, "Базовый URL ИИ")
	fs.StringVar(&cfg.AIModel, "ai-model", DefaultAIModel, "Название модели")
	fs.StringVar(&cfg.Contacts, "contacts", "", "Контакты для передачи работодателю")
	fs.StringVar(&cfg.ExtraTestSolutionPrompt, "solution-prompt", "", "Дополнительный промпт для решения тестов при отклике")
	fs.StringVar(&cfg.ExtraChatReplyPrompt, "chat-reply-prompt", "", "Дополнительный промпт для сообщений в чатах с работодателями")
	fs.StringVar(&cfg.ExtraLetterPrompt, "letter-prompt", "", "Дополнительный промпт для сопроводительного письма")
	fs.StringVar(&cfg.GithubURL, "github-url", "", "Ссылка на GitHub кандидата (не указывать, если не настроена)")
	fs.BoolVar(&cfg.DryRun, "dry-run", true, "Не выполнять записи в HH (по умолчанию включено)")
	fs.BoolVar(&cfg.HHWriteEnabled, "hh-write-enabled", false, "Разрешить только подтверждённые отправки через HH Write Gateway")
	fs.StringVar(&cfg.HHChatURL, "hh-chat-url", DefaultHHChatURL, "HH Chatik URL для offline request preview")
	fs.IntVar(&cfg.HHMaxWritesPerRun, "hh-max-writes-per-run", DefaultHHMaxWritesPerRun, "Максимум ручных HH-отправок за один процесс; 0 — без лимита")
	fs.IntVar(&cfg.HHMaxWritesPerDay, "hh-max-writes-per-day", DefaultHHMaxWritesPerDay, "Максимум ручных HH-отправок за UTC-день; 0 — без лимита")
	fs.BoolVar(&cfg.AutoApply, "auto-apply", true, "Разрешить автоматические отклики")
	fs.StringVar(&cfg.AutoApplyMode, "auto-apply-mode", "off", "Режим откликов: off или canary")
	fs.BoolVar(&cfg.AutoChat, "auto-chat", true, "Разрешить автоматические ответы в чатах")
	fs.BoolVar(&cfg.AutoTouch, "auto-touch", true, "Разрешить поднятие резюме")
	fs.BoolVar(&cfg.AutoJobStatus, "auto-job-status", true, "Разрешить обновление статуса поиска работы")
	fs.StringVar(&cfg.ChatMode, "chat-mode", DefaultChatMode, "Режим чатов: off, review, auto")
	fs.IntVar(&cfg.MinSalary, "min-salary", 0, "Минимальная зарплата вакансии")
	fs.StringVar(&cfg.MinSalaryCurrency, "min-salary-currency", DefaultMinSalaryCurrency, "Валюта минимальной зарплаты (например, RUR)")
	fs.StringVar(includeKeywordsRaw, "include-keywords", "", "Дополнительные позитивные ключевые слова через запятую")
	fs.StringVar(excludeKeywordsRaw, "exclude-keywords", "", "Нежелательные ключевые слова через запятую")
	fs.IntVar(&cfg.MinMatchScore, "min-match-score", DefaultMinMatchScore, "Минимальный AI score для отклика: 0–100")
	fs.BoolVar(&cfg.RunOnce, "run-once", false, "Выполнить разрешённые задачи один раз и завершиться")
	fs.IntVar(&cfg.MaxVacanciesPerRun, "max-vacancies-per-run", DefaultMaxVacanciesPerRun, "Максимум вакансий для pipeline за один проход; 0 — без лимита")
	fs.IntVar(&cfg.MaxApplicationsPerRun, "max-applications-per-run", DefaultMaxApplicationsPerRun, "Максимум успешных/предпросмотренных откликов за один проход; 0 — без лимита")
	fs.IntVar(&cfg.MaxConversationsPerRun, "max-conversations-per-run", 0, "Максимум чатов для Career one-run; 0 — полный scope")
	fs.StringVar(&cfg.AlreadyRespondedStatePath, "already-responded-state", filepath.Join(wd, ".hh-already-responded.json"), "Локальный JSON-файл подтверждённых предыдущих откликов")
	fs.StringVar(&cfg.CandidateProfilePath, "candidate-profile", filepath.Join(wd, "candidate_profile.json"), "Локальный профиль кандидата")
	fs.StringVar(&cfg.CandidateStoriesPath, "candidate-stories", filepath.Join(wd, "candidate_stories.json"), "Примеры опыта кандидата")
	fs.StringVar(&cfg.CareerAgentResultPath, "career-agent-result", filepath.Join(wd, "career_agent_latest.json"), "Последний Career Agent shadow report")
	fs.StringVar(&cfg.CareerAgentFeedbackPath, "career-agent-feedback", filepath.Join(wd, "career_agent_feedback.json"), "Career Agent feedback store")
	fs.StringVar(&cfg.ResumeRegistryPath, "resume-registry", filepath.Join(wd, "resume_registry.json"), "Локальные enabled/disabled overrides резюме")
	fs.StringVar(&cfg.BrowserProfilePath, "browser-profile", filepath.Join(wd, DefaultProfileDir), "Persistent headed Chromium profile for HH session")
	fs.StringVar(&cfg.BrowserTraceVacancyURL, "browser-trace-vacancy", "", "One safe vacancy URL for browser/HTTP read trace")
	fs.StringVar(&cfg.BrowserTransport, "browser-transport", DefaultBrowserTransport, "HH web read transport: auto, browser, or http")
	fs.BoolVar(&cfg.BrowserHeadless, "browser-headless", false, "Run Playwright browser headless")
	fs.StringVar(&cfg.HHSyncStatePath, "hh-sync-state", filepath.Join(wd, "hh_sync_state.json"), "Состояние read-only синхронизации HH")
	fs.DurationVar(&cfg.MonitorInterval, "sync-interval", DefaultMonitorInterval, "Интервал background monitor")
	fs.StringVar(&cfg.MonitorQuietHours, "quiet-hours", "", "Тихие часы уведомлений, например 23:00-07:00")
	fs.DurationVar(&cfg.NotificationCooldown, "notification-cooldown", DefaultNotificationCooldown, "Cooldown одинаковых уведомлений")
	fs.DurationVar(&cfg.ConversationDisplayTTL, "conversation-display-ttl", DefaultConversationDisplayTTL, "TTL display freshness targeted refresh")
	fs.BoolVar(&cfg.BackgroundInboxRefresh, "background-inbox-refresh", false, "Периодически обновлять Inbox metadata в фоне")
	fs.DurationVar(&cfg.FollowUp.AfterApplicationWithoutReply, "follow-up-after-application", cfg.FollowUp.AfterApplicationWithoutReply, "Follow-up delay after application (e.g. 120h)")
	fs.DurationVar(&cfg.FollowUp.AfterCandidateMessageWithoutReply, "follow-up-after-message", cfg.FollowUp.AfterCandidateMessageWithoutReply, "Follow-up delay after candidate message")
	fs.DurationVar(&cfg.FollowUp.MinimumInterval, "follow-up-minimum-interval", cfg.FollowUp.MinimumInterval, "Minimum interval between confirmed follow-ups")
	fs.IntVar(&cfg.FollowUp.MaxFollowUps, "follow-up-max", cfg.FollowUp.MaxFollowUps, "Maximum confirmed follow-ups; 0 disables suggestions")
}

func visitedFlags(fs *flag.FlagSet) map[string]bool {
	flags := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { flags[f.Name] = true })
	return flags
}

func isWebCommand(args []string) bool {
	return len(args) > 0 && (args[0] == "web" || args[0] == "dashboard")
}

func atoiEnv(value, name string) (int, error) {
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, errors.New("invalid " + name)
	}
	return parsed, nil
}

func followUpEnv(p FollowUpConfig, lookup func(string) (string, bool), flags map[string]bool) (FollowUpConfig, error) {
	for _, option := range []struct {
		flag   string
		env    string
		target *time.Duration
	}{
		{"follow-up-after-application", "HH_FOLLOW_UP_AFTER_APPLICATION", &p.AfterApplicationWithoutReply},
		{"follow-up-after-message", "HH_FOLLOW_UP_AFTER_MESSAGE", &p.AfterCandidateMessageWithoutReply},
		{"follow-up-minimum-interval", "HH_FOLLOW_UP_MINIMUM_INTERVAL", &p.MinimumInterval},
	} {
		if value, ok := lookup(option.env); ok && value != "" && !flags[option.flag] {
			parsed, err := time.ParseDuration(value)
			if err != nil {
				return p, errors.New("invalid " + option.env)
			}
			*option.target = parsed
		}
	}
	if value, ok := lookup("HH_FOLLOW_UP_MAX"); ok && value != "" && !flags["follow-up-max"] {
		parsed, err := strconv.Atoi(value)
		if err != nil {
			return p, errors.New("invalid HH_FOLLOW_UP_MAX")
		}
		p.MaxFollowUps = parsed
	}
	if p.AfterApplicationWithoutReply <= 0 || p.AfterCandidateMessageWithoutReply <= 0 || p.MinimumInterval <= 0 || p.MaxFollowUps < 0 {
		return p, errors.New("follow-up delays must be positive and max-followups non-negative")
	}
	return p, nil
}
