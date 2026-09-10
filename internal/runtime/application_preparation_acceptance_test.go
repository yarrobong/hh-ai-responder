package runtime

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	appconfig "hh-ai-responder/internal/config"
	"hh-ai-responder/internal/ports"
	"hh-ai-responder/internal/usecase/applicationprocessing"
	"hh-ai-responder/internal/usecase/candidatecontext"
	"hh-ai-responder/internal/usecase/coverletter"
	"hh-ai-responder/internal/usecase/vacancyanalysis"
	"hh-ai-responder/internal/vacancy"
)

// TestV2ApplicationPreparationAcceptance is deliberately skipped unless the
// caller opts in. It is an acceptance harness, not normal package coverage:
// the opt-in run reads the configured PostgreSQL database and calls the real
// configured completion/embedding services.
func TestV2ApplicationPreparationAcceptance(t *testing.T) {
	if os.Getenv("V2_APPLICATION_PREPARATION_ACCEPTANCE") != "1" {
		t.Skip("V2_APPLICATION_PREPARATION_ACCEPTANCE=1 is required")
	}

	ctx := context.Background()
	root := acceptanceRepositoryRoot(t)
	loaded, err := appconfig.Load(nil, os.LookupEnv, root)
	if err != nil {
		t.Fatal(err)
	}
	cfg := legacyConfigFromPackage(loaded)
	validateAcceptanceConfig(t, cfg)

	pool, err := OpenPostgres(ctx, PostgresConfig{DatabaseURL: cfg.DatabaseURL})
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	assertAcceptanceDatabase(t, ctx, pool)
	countsBefore := readAcceptanceCounts(t, ctx, pool)
	if countsBefore.Candidate != 1 || countsBefore.SemanticDocuments != 3 || countsBefore.Attempts != 0 || countsBefore.AutoChatAttempts != 0 {
		t.Fatalf("unexpected canonical PostgreSQL baseline: %+v", countsBefore)
	}

	candidateRepository := NewPostgresCandidateRepositoryForID(pool, cfg.CandidateID)
	candidateBefore, err := candidateRepository.CurrentCandidate(ctx)
	if err != nil {
		t.Fatal(err)
	}
	fingerprintBefore, err := candidateFingerprint(candidateBefore)
	if err != nil {
		t.Fatal(err)
	}
	if candidateBefore.ID != "candidate-local" {
		t.Fatalf("acceptance candidate=%q, want candidate-local", candidateBefore.ID)
	}

	semanticRepository := NewPostgresCandidateSemanticRepository(pool)
	documentsBefore, err := semanticRepository.ListDocuments(ctx, candidateBefore.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(documentsBefore) != 3 {
		t.Fatalf("semantic documents=%d, want 3", len(documentsBefore))
	}
	semanticProvider, err := configuredEmbeddingProvider(cfg)
	if err != nil {
		t.Fatal(err)
	}
	semanticContract, err := embeddingContractForAcceptance(semanticProvider)
	if err != nil {
		t.Fatal(err)
	}
	for _, document := range documentsBefore {
		if document.EmbeddingDimensions != 1024 || len(document.Embedding) != 1024 {
			t.Fatalf("semantic document %d has dimensions=%d/vector=%d, want 1024", document.ID, document.EmbeddingDimensions, len(document.Embedding))
		}
		if document.EmbeddingSpaceID != semanticContract.SpaceID {
			t.Fatalf("semantic document %d is in space %q, want active configured space", document.ID, document.EmbeddingSpaceID)
		}
	}

	vacancyRepository := NewPostgresVacancyRepository(pool)
	selectedVacancy := selectAcceptanceVacancy(t, ctx, vacancyRepository)
	if selectedVacancy.ID <= 0 || strings.TrimSpace(selectedVacancy.ExternalID) == "" || strings.TrimSpace(selectedVacancy.Description) == "" {
		t.Fatalf("selected vacancy is not a complete real PostgreSQL vacancy: id=%d external_id_present=%t description_present=%t", selectedVacancy.ID, strings.TrimSpace(selectedVacancy.ExternalID) != "", strings.TrimSpace(selectedVacancy.Description) != "")
	}

	var semanticRetriever CandidateSemanticRetriever = NewCandidateSemanticSearchService(semanticRepository, semanticProvider, candidateRepository)
	semanticEvidence := &acceptanceSemanticEvidence{}
	semanticRetriever = &acceptanceRecordingSemanticRetriever{delegate: semanticRetriever, evidence: semanticEvidence}
	ai := NewAIClient(ctx, cfg.AIBaseURL, cfg.AIModel, cfg.AIAPIKey, cfg.AITimeout, cfg.AIConnectTimeout, cfg.AIAttempts)
	candidateResponder := &HHAIResponder{
		ctx:                     ctx,
		candidateRepository:     candidateRepository,
		semanticRetriever:       semanticRetriever,
		contacts:                cfg.Contacts,
		githubURL:               cfg.GithubURL,
		ai:                      ai,
		minMatchScore:           cfg.MinMatchScore,
		minSalary:               cfg.MinSalary,
		minSalaryCurrency:       cfg.MinSalaryCurrency,
		excludeKeywords:         append([]string(nil), cfg.ExcludeKeywords...),
		extraLetterPrompt:       cfg.ExtraLetterPrompt,
		extraTestSolutionPrompt: cfg.ExtraTestSolutionPrompt,
	}

	resume := ResumeItem{Hash: strings.TrimSpace(cfg.Resume), Title: firstAcceptanceResumeTitle(candidateBefore)}
	legacyCandidate, resolver, err := candidateResponder.canonicalCandidateContext(resume)
	if err != nil {
		t.Fatal(err)
	}

	var hhReader *rootApplicationReader
	if selectedVacancy.UserTestPresent {
		hhReader = newAcceptanceHHReadReader(t, ctx, cfg)
	}
	reader := acceptanceVacancyReader{repository: vacancyRepository, testReader: hhReader}
	policyEvidence := &acceptancePolicyEvidence{}
	productionPolicy := rootApplicationPolicy{responder: candidateResponder}
	service := applicationprocessing.NewService(applicationprocessing.Dependencies{
		Vacancies:   reader,
		Candidate:   rootApplicationCandidate{resolver: resolver},
		Analyzer:    &acceptanceRecordingAnalyzer{delegate: rootApplicationAnalyzer{client: ai}},
		CoverLetter: rootApplicationCoverLetter{client: ai},
		TestAnswer:  rootApplicationTestAnswer{client: ai},
		Semantic:    rootApplicationSemanticHints{responder: candidateResponder, resolver: resolver},
		// No Clarifier and no HH writer are supplied. Unknowns therefore remain
		// a typed safe outcome and cannot mutate Candidate truth.
		Policy: acceptancePolicy{delegate: productionPolicy, evidence: policyEvidence},
	})

	request := candidateResponder.applicationProcessingRequest(vacancyDomain(selectedVacancy), resume, legacyCandidate, 0)
	request.ForceLetter = false
	request.ApplicationLimitReached = false
	result, err := service.Prepare(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != applicationprocessing.OutcomePrepared && result.Outcome != applicationprocessing.OutcomeNeedsCandidateInput {
		t.Fatalf("application preparation did not reach a safe preparation outcome: outcome=%s reason=%s", result.Outcome, result.Reason)
	}
	if result.Analysis == nil {
		t.Fatal("application preparation returned no typed analysis")
	}
	if result.Outcome == applicationprocessing.OutcomePrepared && result.Prepared == nil {
		t.Fatal("prepared outcome returned no in-memory prepared application")
	}
	if result.Prepared != nil && result.Prepared.VacancyID != selectedVacancy.ID {
		t.Fatalf("prepared vacancy=%d, want %d", result.Prepared.VacancyID, selectedVacancy.ID)
	}
	if policyEvidence.decide == "" {
		t.Fatal("production matching policy was not evaluated")
	}

	countsAfter := readAcceptanceCounts(t, ctx, pool)
	if countsAfter.Attempts != countsBefore.Attempts || countsAfter.AutoChatAttempts != countsBefore.AutoChatAttempts || countsAfter.Applications != countsBefore.Applications {
		t.Fatalf("preparation changed durable application state: before=%+v after=%+v", countsBefore, countsAfter)
	}
	candidateAfter, err := candidateRepository.CurrentCandidate(ctx)
	if err != nil {
		t.Fatal(err)
	}
	fingerprintAfter, err := candidateFingerprint(candidateAfter)
	if err != nil {
		t.Fatal(err)
	}
	if fingerprintAfter != fingerprintBefore {
		t.Fatal("preparation changed canonical Candidate truth")
	}
	documentsAfter, err := semanticRepository.ListDocuments(ctx, candidateBefore.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(documentsAfter) != len(documentsBefore) {
		t.Fatalf("preparation changed semantic document count: before=%d after=%d", len(documentsBefore), len(documentsAfter))
	}

	t.Logf("V2.Fix-3c acceptance: database=hh_ai_responder_s3 candidate=%s vacancy=%d production_policy=%s preparation=%s semantic_results=%d semantic_space=%s writer_available=NO attempts=%d->%d hh_mutations=0", candidateBefore.ID, selectedVacancy.ID, policyEvidence.result(), result.Outcome, semanticEvidence.results, semanticContract.SpaceID, countsBefore.Attempts, countsAfter.Attempts)
}

// TestApplicationPreparationCompositionHasNoHHWriteCapability exercises the
// same preparation service with fakes. The dependency set has no writer or
// reservation port, so a preparation call cannot reach HH transport by
// construction. This guards the acceptance boundary independently of live
// credentials.
func TestApplicationPreparationCompositionHasNoHHWriteCapability(t *testing.T) {
	reader := &acceptanceFakeReader{description: "Python API integration", applicability: applicationprocessing.Applicability{Available: true, CanApply: true, CanApplyKnown: true, LetterRequired: true, LetterRequiredKnown: true}}
	analyzer := &acceptanceFakeAnalyzer{assessment: vacancyanalysis.Assessment{Apply: true, Score: 1}}
	letter := &acceptanceFakeLetter{result: coverletter.Result{Letter: "Known candidate fact only."}}
	service := applicationprocessing.NewService(applicationprocessing.Dependencies{
		Vacancies: reader, Candidate: acceptanceFakeCandidate{}, Analyzer: analyzer, CoverLetter: letter,
		Policy: acceptanceMatchPolicy{},
	})
	result, err := service.Prepare(context.Background(), applicationprocessing.Request{Vacancy: vacancy.Vacancy{ID: 1, Name: "API integration", Links: map[string]string{"desktop": "https://hh.ru/vacancy/1"}}, ResumeID: "read-only", ResumeTitle: "canonical"})
	if err != nil || result.Outcome != applicationprocessing.OutcomePrepared || letter.calls != 1 {
		t.Fatalf("preparation composition result=%+v err=%v letter_calls=%d", result, err, letter.calls)
	}
	if reader.testCalls != 0 || reader.writeCalls != 0 {
		t.Fatalf("preparation used an unexpected mutation-shaped reader operation: test=%d writes=%d", reader.testCalls, reader.writeCalls)
	}
}

type acceptanceCounts struct {
	Candidate         int
	Vacancies         int
	Applications      int
	Conversations     int
	Attempts          int
	AutoChatAttempts  int
	SemanticDocuments int
}

func readAcceptanceCounts(t *testing.T, ctx context.Context, pool *pgxpool.Pool) acceptanceCounts {
	t.Helper()
	// This helper is intentionally kept on the read-only acceptance path.
	var counts acceptanceCounts
	queries := []struct {
		name   string
		target *int
		sql    string
	}{
		{"candidates", &counts.Candidate, `SELECT count(*) FROM candidates`},
		{"vacancies", &counts.Vacancies, `SELECT count(*) FROM vacancies`},
		{"applications", &counts.Applications, `SELECT count(*) FROM applications`},
		{"conversations", &counts.Conversations, `SELECT count(*) FROM conversations`},
		{"automatic_application_attempts", &counts.Attempts, `SELECT count(*) FROM automatic_application_attempts`},
		{"legacy_auto_chat_attempts", &counts.AutoChatAttempts, `SELECT count(*) FROM legacy_auto_chat_attempts`},
		{"candidate_semantic_documents", &counts.SemanticDocuments, `SELECT count(*) FROM candidate_semantic_documents`},
	}
	for _, query := range queries {
		if err := pool.QueryRow(ctx, query.sql).Scan(query.target); err != nil {
			t.Fatalf("read %s count: %v", query.name, err)
		}
	}
	return counts
}

func assertAcceptanceDatabase(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	var database string
	if err := pool.QueryRow(ctx, `SELECT current_database()`).Scan(&database); err != nil {
		t.Fatal(err)
	}
	if database != "hh_ai_responder_s3" {
		t.Fatalf("refusing acceptance run against database %q; want hh_ai_responder_s3", database)
	}
}

func selectAcceptanceVacancy(t *testing.T, ctx context.Context, repository VacancyRepository) Vacancy {
	t.Helper()
	if rawID := strings.TrimSpace(os.Getenv("V2_ACCEPTANCE_VACANCY_ID")); rawID != "" {
		id, err := strconv.Atoi(rawID)
		if err != nil || id <= 0 {
			t.Fatalf("V2_ACCEPTANCE_VACANCY_ID must be a positive integer")
		}
		value, err := repository.Get(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	values, err := repository.List(ctx, VacancyQuery{})
	if err != nil {
		t.Fatal(err)
	}
	sort.SliceStable(values, func(i, j int) bool {
		left := acceptanceVacancyRank(values[i])
		right := acceptanceVacancyRank(values[j])
		if left != right {
			return left > right
		}
		return values[i].UpdatedAt.After(values[j].UpdatedAt)
	})
	for _, value := range values {
		if value.ID > 0 && strings.TrimSpace(value.ExternalID) != "" && strings.TrimSpace(value.Description) != "" && strings.TrimSpace(value.Links["desktop"]) != "" && !value.Archived && strings.TrimSpace(value.ResponseURL) == "" {
			return value
		}
	}
	t.Fatal("no complete, unresponded PostgreSQL vacancy is available for acceptance")
	return Vacancy{}
}

func acceptanceVacancyRank(value Vacancy) int {
	rank := 0
	if value.ResponseLetterRequired {
		rank += 4
	}
	if !value.UserTestPresent {
		rank += 2
	}
	if value.DataCompleteness == DataCompletenessFull {
		rank++
	}
	if len([]rune(value.Description)) >= 500 {
		rank++
	}
	return rank
}

type acceptanceVacancyReader struct {
	repository VacancyReader
	testReader *rootApplicationReader
}

func (r acceptanceVacancyReader) ReadDescription(ctx context.Context, id int) (string, error) {
	value, err := r.repository.Get(ctx, id)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(value.Description) == "" {
		return "", errors.New("selected PostgreSQL vacancy has no stored description")
	}
	return value.Description, nil
}

func (r acceptanceVacancyReader) ReadApplicability(ctx context.Context, value vacancy.Vacancy) (applicationprocessing.Applicability, error) {
	if err := ctx.Err(); err != nil {
		return applicationprocessing.Applicability{}, err
	}
	alreadyResponded := strings.TrimSpace(value.ResponseURL) != ""
	canApply := !value.Archived && !alreadyResponded
	return applicationprocessing.Applicability{
		Available: canApply,
		Archived:  value.Archived, ArchivedKnown: true,
		AlreadyResponded: alreadyResponded, AlreadyRespondedKnown: true,
		TestPresent: value.UserTestPresent, TestPresentKnown: true,
		LetterRequired: value.ResponseLetterRequired, LetterRequiredKnown: true,
		CanApply: canApply, CanApplyKnown: true,
		Area: value.Area.Name, AreaKnown: strings.TrimSpace(value.Area.Name) != "",
		WorkSchedule: value.WorkSchedule, WorkScheduleKnown: strings.TrimSpace(value.WorkSchedule) != "",
		WorkExperience: value.WorkExperience, WorkExperienceKnown: strings.TrimSpace(value.WorkExperience) != "",
		ResponseURL: value.ResponseURL,
	}, nil
}

func (r acceptanceVacancyReader) ReadTest(ctx context.Context, id int) (applicationprocessing.TestSnapshot, error) {
	if r.testReader == nil {
		return applicationprocessing.TestSnapshot{}, fmt.Errorf("test snapshot for vacancy %d requires a GET-only HH reader", id)
	}
	return r.testReader.ReadTest(ctx, id)
}

type acceptancePolicyEvidence struct {
	decide    string
	reconcile string
}

func (e *acceptancePolicyEvidence) result() string {
	if e.reconcile != "" {
		return e.reconcile
	}
	return e.decide
}

type acceptancePolicy struct {
	delegate rootApplicationPolicy
	evidence *acceptancePolicyEvidence
}

func (p acceptancePolicy) EarlyReject(value vacancy.Vacancy) string {
	return p.delegate.EarlyReject(value)
}
func (p acceptancePolicy) DescriptionReject(value vacancy.Vacancy, description string) string {
	return p.delegate.DescriptionReject(value, description)
}
func (p acceptancePolicy) Decide(assessment vacancyanalysis.Assessment) (applicationprocessing.Decision, string) {
	decision, reason := p.delegate.Decide(assessment)
	if p.evidence != nil {
		p.evidence.decide = string(decision)
	}
	// Test-only upstream eligibility bypass. Hard requirements and unknown
	// handling remain authoritative in ReconcileApplicability below.
	if decision == applicationprocessing.DecisionReject && len(hardRequirementsMissing(assessment)) == 0 && len(hardRequirementsUnknown(assessment)) == 0 {
		return applicationprocessing.DecisionMatch, "acceptance harness selected vacancy after production policy evaluation"
	}
	return decision, reason
}
func (p acceptancePolicy) ReconcileApplicability(value vacancy.Vacancy, app applicationprocessing.Applicability, facts vacancyanalysis.CandidateFacts, assessment vacancyanalysis.Assessment) (vacancyanalysis.Assessment, applicationprocessing.Decision, string, error) {
	assessment, decision, reason, err := p.delegate.ReconcileApplicability(value, app, facts, assessment)
	if p.evidence != nil {
		p.evidence.reconcile = string(decision)
	}
	// Applicability is a read-only safety gate, not the match override. Keep
	// every preflight block/review and every hard requirement outcome intact.
	if decision == applicationprocessing.DecisionReject && !strings.HasPrefix(reason, "preflight:") && len(hardRequirementsMissing(assessment)) == 0 && len(hardRequirementsUnknown(assessment)) == 0 {
		return assessment, applicationprocessing.DecisionMatch, "acceptance harness selected vacancy after production policy evaluation", nil
	}
	return assessment, decision, reason, err
}

type acceptanceRecordingAnalyzer struct {
	delegate rootApplicationAnalyzer
}

func (a *acceptanceRecordingAnalyzer) Analyze(ctx context.Context, input vacancyanalysis.Input) (vacancyanalysis.Assessment, error) {
	result, err := a.delegate.Analyze(ctx, input)
	return result, err
}

type acceptanceSemanticEvidence struct {
	results int
}

type acceptanceRecordingSemanticRetriever struct {
	delegate CandidateSemanticRetriever
	evidence *acceptanceSemanticEvidence
}

func (r *acceptanceRecordingSemanticRetriever) Retrieve(ctx context.Context, request SemanticRetrievalRequest) ([]CandidateSemanticResult, error) {
	results, err := r.delegate.Retrieve(ctx, request)
	if err == nil && r.evidence != nil {
		r.evidence.results = len(results)
	}
	return results, err
}

type acceptanceGetOnlyTransport struct {
	base   http.RoundTripper
	gets   atomic.Int64
	writes atomic.Int64
}

func (t *acceptanceGetOnlyTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.Method == http.MethodGet || request.Method == http.MethodHead {
		t.gets.Add(1)
	} else {
		t.writes.Add(1)
		return nil, errors.New("acceptance HH transport rejects non-GET/HEAD requests")
	}
	return t.base.RoundTrip(request)
}

func newAcceptanceHHReadReader(t *testing.T, ctx context.Context, cfg Config) *rootApplicationReader {
	t.Helper()
	jar, err := NewMemoryPersistentJar(cfg.CookiesPath)
	if err != nil {
		t.Fatal(err)
	}
	transport := &acceptanceGetOnlyTransport{base: http.DefaultTransport}
	client := &http.Client{Jar: jar, Timeout: cfg.AITimeout, Transport: transport}
	baseURL := &url.URL{Scheme: "https", Host: "hh.ru"}
	responder := &HHAIResponder{ctx: ctx, baseURL: baseURL, client: client, jar: jar, requester: NewHHRequester(ctx, client, cfg.RequestInterval), preflightCache: map[int]VacancyPreflight{}}
	responder.requester.readOnly = true
	return &rootApplicationReader{responder: responder}
}

func validateAcceptanceConfig(t *testing.T, cfg Config) {
	t.Helper()
	if err := acceptanceConfigError(cfg); err != nil {
		t.Fatal(err)
	}
}

func acceptanceConfigError(cfg Config) error {
	if cfg.StorageBackend != storageBackendPostgres || strings.TrimSpace(cfg.DatabaseURL) == "" {
		return errors.New("acceptance requires STORAGE_BACKEND=postgres and DATABASE_URL")
	}
	if cfg.CandidateID != "candidate-local" {
		return fmt.Errorf("acceptance requires HH_CANDIDATE_ID=candidate-local, got %q", cfg.CandidateID)
	}
	if !cfg.DryRun || cfg.HHWriteEnabled {
		return errors.New("acceptance requires HH_DRY_RUN=true and HH_WRITE_ENABLED=false")
	}
	if strings.TrimSpace(cfg.AIBaseURL) == "" {
		return errors.New("acceptance requires HH_AI_BASE_URL")
	}
	if strings.TrimSpace(cfg.AIAPIKey) == "" {
		return errors.New("acceptance requires HH_AI_API_KEY")
	}
	if strings.TrimSpace(cfg.AIModel) == "" {
		return errors.New("acceptance requires a non-empty HH_AI_MODEL")
	}
	completion := NewAIClient(context.Background(), cfg.AIBaseURL, cfg.AIModel, cfg.AIAPIKey, cfg.AITimeout, cfg.AIConnectTimeout, cfg.AIAttempts)
	if completion == nil || completion.provider == nil {
		return errors.New("acceptance could not construct the configured completion provider")
	}
	if strings.ToLower(cfg.EmbeddingProvider) != "openai-compatible" || cfg.EmbeddingModel != "mistral-embed" || cfg.EmbeddingDimensions != 1024 {
		return fmt.Errorf("acceptance requires openai-compatible/mistral-embed/1024 embeddings, got %s/%s/%d", cfg.EmbeddingProvider, cfg.EmbeddingModel, cfg.EmbeddingDimensions)
	}
	if _, err := configuredEmbeddingProvider(cfg); err != nil {
		return fmt.Errorf("acceptance requires a constructible embedding provider: %w", err)
	}
	return nil
}

func TestAcceptanceConfigAllowsConfiguredCompletionModelNames(t *testing.T) {
	for _, model := range []string{"ministral-8b-latest", "mistral-small-latest", "provider-specific-model-v2"} {
		t.Run(model, func(t *testing.T) {
			cfg := acceptanceConfigFixture()
			cfg.AIModel = model
			if err := acceptanceConfigError(cfg); err != nil {
				t.Fatalf("model %q rejected: %v", model, err)
			}
		})
	}
}

func TestAcceptanceConfigRejectsUnsafeOrIncompleteConfiguration(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Config)
	}{
		{name: "empty model", mutate: func(cfg *Config) { cfg.AIModel = "" }},
		{name: "missing base URL", mutate: func(cfg *Config) { cfg.AIBaseURL = "" }},
		{name: "missing API key", mutate: func(cfg *Config) { cfg.AIAPIKey = "" }},
		{name: "dry run disabled", mutate: func(cfg *Config) { cfg.DryRun = false }},
		{name: "HH writes enabled", mutate: func(cfg *Config) { cfg.HHWriteEnabled = true }},
		{name: "embedding dimensions differ", mutate: func(cfg *Config) { cfg.EmbeddingDimensions = 1536 }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := acceptanceConfigFixture()
			test.mutate(&cfg)
			if err := acceptanceConfigError(cfg); err == nil {
				t.Fatal("configuration was accepted")
			}
		})
	}
}

func acceptanceConfigFixture() Config {
	return Config{
		StorageBackend:      storageBackendPostgres,
		DatabaseURL:         "postgres://acceptance.example/hh_ai_responder_s3",
		CandidateID:         "candidate-local",
		AIBaseURL:           "https://ai.example/v1",
		AIAPIKey:            "configured-key",
		AIModel:             "ministral-8b-latest",
		EmbeddingProvider:   "openai-compatible",
		EmbeddingModel:      "mistral-embed",
		EmbeddingDimensions: 1024,
		DryRun:              true,
		HHWriteEnabled:      false,
	}
}

func embeddingContractForAcceptance(provider EmbeddingProvider) (EmbeddingContract, error) {
	return ports.EmbeddingContractFor(provider)
}

func firstAcceptanceResumeTitle(candidate Candidate) string {
	if value := strings.TrimSpace(candidate.Profile.WorkPreferences.PrimaryRoles.Value); value != "" {
		return value
	}
	if value := strings.TrimSpace(candidate.Identity.FullName); value != "" {
		return value
	}
	return "canonical candidate"
}

func acceptanceRepositoryRoot(t *testing.T) string {
	t.Helper()
	directory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for range 5 {
		if _, err := os.Stat(filepath.Join(directory, "go.mod")); err == nil {
			return directory
		}
		directory = filepath.Dir(directory)
	}
	t.Fatal("repository root not found")
	return ""
}

type acceptanceFakeReader struct {
	description   string
	applicability applicationprocessing.Applicability
	testCalls     int
	writeCalls    int
}

func (r *acceptanceFakeReader) ReadDescription(context.Context, int) (string, error) {
	return r.description, nil
}
func (r *acceptanceFakeReader) ReadApplicability(context.Context, vacancy.Vacancy) (applicationprocessing.Applicability, error) {
	return r.applicability, nil
}
func (r *acceptanceFakeReader) ReadTest(context.Context, int) (applicationprocessing.TestSnapshot, error) {
	r.testCalls++
	return applicationprocessing.TestSnapshot{}, errors.New("test not applicable")
}

type acceptanceFakeCandidate struct{}

func (acceptanceFakeCandidate) ResolveForVacancy(context.Context, vacancy.Vacancy, string) (candidatecontext.CandidateContext, error) {
	return candidatecontext.CandidateContext{}, nil
}

type acceptanceFakeAnalyzer struct{ assessment vacancyanalysis.Assessment }

func (a *acceptanceFakeAnalyzer) Analyze(context.Context, vacancyanalysis.Input) (vacancyanalysis.Assessment, error) {
	return a.assessment, nil
}

type acceptanceFakeLetter struct {
	result coverletter.Result
	calls  int
}

func (a *acceptanceFakeLetter) Generate(context.Context, coverletter.Input) (coverletter.Result, error) {
	a.calls++
	return a.result, nil
}

type acceptanceMatchPolicy struct{}

func (acceptanceMatchPolicy) EarlyReject(vacancy.Vacancy) string               { return "" }
func (acceptanceMatchPolicy) DescriptionReject(vacancy.Vacancy, string) string { return "" }
func (acceptanceMatchPolicy) Decide(vacancyanalysis.Assessment) (applicationprocessing.Decision, string) {
	return applicationprocessing.DecisionMatch, ""
}
func (acceptanceMatchPolicy) ReconcileApplicability(vacancy.Vacancy, applicationprocessing.Applicability, vacancyanalysis.CandidateFacts, vacancyanalysis.Assessment) (vacancyanalysis.Assessment, applicationprocessing.Decision, string, error) {
	return vacancyanalysis.Assessment{Apply: true, Score: 1}, applicationprocessing.DecisionMatch, "", nil
}

func vacancyDomain(value Vacancy) vacancy.Vacancy { return value }
