package runtime

import (
	"bytes"
	stdcontext "context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"hh-ai-responder/internal/platform"
	llmport "hh-ai-responder/internal/ports/llm"
	applicationanswer "hh-ai-responder/internal/usecase/applicationanswer"
	coverletter "hh-ai-responder/internal/usecase/coverletter"
	employerreply "hh-ai-responder/internal/usecase/employerreply"
	employerreplyworkflow "hh-ai-responder/internal/usecase/employerreplyworkflow"
	followupdraft "hh-ai-responder/internal/usecase/followupdraft"
)

// StructuredAIClient is the smallest part of the existing AI integration that
// the orchestrator needs. Keeping it as an interface makes all orchestration
// tests local and prevents them from requiring an actual provider.
type StructuredAIClient interface {
	ChatStructuredWithSchema(systemPrompt, userPrompt string, maxTokens int, temperature float64, schema *ChatJSONSchema, validator func(string) error) (string, error)
}

type aiModelNamer interface {
	ModelName() string
}

type AIDraftType string

const (
	AIDraftFollowUp          AIDraftType = "follow_up"
	AIDraftEmployerReply     AIDraftType = "employer_reply"
	AIDraftCoverLetter       AIDraftType = "cover_letter"
	AIDraftApplicationAnswer AIDraftType = "application_answer"
)

type AIDraftStatus string

const (
	AIDraftGenerated  AIDraftStatus = "generated"
	AIDraftApproved   AIDraftStatus = "approved"
	AIDraftRejected   AIDraftStatus = "rejected"
	AIDraftSuperseded AIDraftStatus = "superseded"
	AIDraftSent       AIDraftStatus = "sent" // retained for a future explicit send flow
)

type AIDraftSource string

const (
	AIDraftSourceAI         AIDraftSource = "ai"
	AIDraftSourceUserEdited AIDraftSource = "user_edited"
)

type AIDraft struct {
	InputFingerprint      string        `json:"input_fingerprint,omitempty"`
	PromptVersion         string        `json:"prompt_version,omitempty"`
	EmployerMessageHash   string        `json:"employer_message_hash,omitempty"`
	RelevantKnowledgeHash string        `json:"relevant_knowledge_hash,omitempty"`
	ID                    string        `json:"id"`
	Type                  AIDraftType   `json:"type"`
	ApplicationID         string        `json:"application_id,omitempty"`
	ConversationID        string        `json:"conversation_id,omitempty"`
	InputMessageID        string        `json:"input_message_id,omitempty"`
	Text                  string        `json:"text"`
	OriginalText          string        `json:"original_text,omitempty"`
	EditedText            string        `json:"edited_text,omitempty"`
	Source                AIDraftSource `json:"source,omitempty"`
	Status                AIDraftStatus `json:"status"`
	Model                 string        `json:"model,omitempty"`
	CreatedAt             time.Time     `json:"created_at"`
	UpdatedAt             time.Time     `json:"updated_at"`
	DecisionReason        string        `json:"decision_reason"`
	UsedFacts             []string      `json:"used_facts"`
}

func (d AIDraft) validate() error {
	if strings.TrimSpace(d.ID) == "" || strings.TrimSpace(d.Text) == "" || d.CreatedAt.IsZero() || d.UpdatedAt.Before(d.CreatedAt) {
		return errors.New("AI draft requires identity, text and ordered timestamps")
	}
	switch d.Type {
	case AIDraftFollowUp, AIDraftEmployerReply, AIDraftCoverLetter, AIDraftApplicationAnswer:
	default:
		return errors.New("invalid AI draft type")
	}
	switch d.Status {
	case AIDraftGenerated, AIDraftApproved, AIDraftRejected, AIDraftSuperseded, AIDraftSent:
	default:
		return errors.New("invalid AI draft status")
	}
	if strings.TrimSpace(d.DecisionReason) == "" {
		return errors.New("AI draft requires a decision reason")
	}
	if d.Source != "" && d.Source != AIDraftSourceAI && d.Source != AIDraftSourceUserEdited {
		return errors.New("invalid AI draft source")
	}
	return nil
}

type aiDraftStoreFile struct {
	Version int       `json:"version"`
	Drafts  []AIDraft `json:"drafts"`
}

// AIDraftStore is local persistence only. It has no method that sends or
// approves anything, and Save remains explicit like the other project stores.
type AIDraftStore struct {
	path   string
	drafts []AIDraft
}

func NewAIDraftStore(path string) *AIDraftStore {
	return &AIDraftStore{path: path, drafts: []AIDraft{}}
}

func (s *AIDraftStore) Load() error {
	if s == nil || strings.TrimSpace(s.path) == "" {
		return errors.New("AI draft store requires a path")
	}
	raw, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		s.drafts = []AIDraft{}
		return nil
	}
	if err != nil || profileContainsSecret(raw) {
		return errors.New("cannot read AI draft store")
	}
	var file aiDraftStoreFile
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&file) != nil || decoder.Decode(new(any)) != io.EOF || file.Version != 1 || file.Drafts == nil {
		return errors.New("invalid AI draft store")
	}
	if err := validateAIDrafts(file.Drafts); err != nil {
		return err
	}
	s.drafts = file.Drafts
	return nil
}

func validateAIDrafts(values []AIDraft) error {
	ids := map[string]bool{}
	for _, draft := range values {
		if err := draft.validate(); err != nil {
			return err
		}
		if ids[draft.ID] {
			return errors.New("duplicate AI draft id")
		}
		ids[draft.ID] = true
	}
	return nil
}

func (s *AIDraftStore) Save() error {
	if s == nil || strings.TrimSpace(s.path) == "" {
		return errors.New("AI draft store requires a path")
	}
	if err := validateAIDrafts(s.drafts); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(aiDraftStoreFile{Version: 1, Drafts: s.drafts}, "", "  ")
	if err != nil {
		return errors.New("cannot encode AI draft store")
	}
	return withStoreLock(s.path, func() error {
		return platform.WritePrivateFileAtomic(s.path, append(raw, '\n'), ".ai-drafts-*.tmp")
	})
}

func (s *AIDraftStore) Create(draft AIDraft) (AIDraft, error) {
	if s == nil {
		return AIDraft{}, errors.New("AI draft store is nil")
	}
	if draft.ID == "" {
		var err error
		draft.ID, err = newKnowledgeID("ai_draft")
		if err != nil {
			return AIDraft{}, err
		}
	}
	now := time.Now().UTC()
	if draft.CreatedAt.IsZero() {
		draft.CreatedAt = now
	}
	if draft.UpdatedAt.IsZero() {
		draft.UpdatedAt = draft.CreatedAt
	}
	if draft.Status == "" {
		draft.Status = AIDraftGenerated
	}
	if draft.Source == "" {
		draft.Source = AIDraftSourceAI
	}
	if draft.OriginalText == "" {
		draft.OriginalText = draft.Text
	}
	if profileContainsSecret([]byte(draft.Text)) {
		return AIDraft{}, errors.New("AI draft contains a forbidden secret marker")
	}
	if draft.UsedFacts == nil {
		draft.UsedFacts = []string{}
	}
	if err := draft.validate(); err != nil {
		return AIDraft{}, err
	}
	for _, old := range s.drafts {
		if old.ID == draft.ID {
			return AIDraft{}, errors.New("AI draft id already exists")
		}
	}
	s.drafts = append(s.drafts, draft)
	return cloneKnowledge(draft)
}

func (s *AIDraftStore) Get(id string) (AIDraft, error) {
	if s != nil {
		for _, draft := range s.drafts {
			if draft.ID == id {
				return cloneKnowledge(draft)
			}
		}
	}
	return AIDraft{}, errors.New("AI draft not found")
}

func (s *AIDraftStore) List() ([]AIDraft, error) {
	if s == nil {
		return nil, errors.New("AI draft store is nil")
	}
	return cloneKnowledge(s.drafts)
}

// SetStatus supports the future review UI. Even the sent enum only changes a
// local draft record; this store has no transport or HH client dependency.
func (s *AIDraftStore) SetStatus(id string, status AIDraftStatus) error {
	draft, err := s.Get(id)
	if err != nil {
		return err
	}
	switch status {
	case AIDraftGenerated, AIDraftApproved, AIDraftRejected, AIDraftSuperseded, AIDraftSent:
	default:
		return errors.New("invalid AI draft status")
	}
	draft.Status = status
	draft.UpdatedAt = time.Now().UTC()
	for i := range s.drafts {
		if s.drafts[i].ID == id {
			s.drafts[i] = draft
			return nil
		}
	}
	return errors.New("AI draft not found")
}

func (s *AIDraftStore) updateText(id, text string, source AIDraftSource) error {
	if s == nil {
		return errors.New("AI draft store is nil")
	}
	if strings.TrimSpace(text) == "" || len([]rune(text)) > 600 {
		return errors.New("draft text is empty or too long")
	}
	if source != AIDraftSourceAI && source != AIDraftSourceUserEdited {
		return errors.New("invalid AI draft source")
	}
	for i := range s.drafts {
		if s.drafts[i].ID != id {
			continue
		}
		updated := s.drafts[i]
		if updated.OriginalText == "" {
			updated.OriginalText = updated.Text
		}
		updated.Text, updated.Source = text, source
		if source == AIDraftSourceUserEdited {
			updated.EditedText = text
		}
		updated.Status = AIDraftGenerated
		updated.UpdatedAt = time.Now().UTC()
		if updated.UpdatedAt.Before(updated.CreatedAt) {
			updated.UpdatedAt = updated.CreatedAt
		}
		if err := updated.validate(); err != nil {
			return err
		}
		s.drafts[i] = updated
		return nil
	}
	return errors.New("AI draft not found")
}

type AIReplyOrchestrator struct {
	ai                       StructuredAIClient
	replyService             *employerreply.Service
	followUpService          *followupdraft.Service
	employerWorkflow         *employerreplyworkflow.Service
	coverLetterService       *coverletter.Service
	applicationAnswerService *applicationanswer.Service
	conversationBuilder      *ConversationContextBuilder
	applicationBuilder       *ApplicationContextBuilder
	applications             *ApplicationStore
	clarifications           *CandidateClarificationStore
	drafts                   *AIDraftStore
	updater                  CandidateKnowledgeMutationWriter
	stories                  []CandidateStory
	extraPrompt              string
	model                    string
	semanticRetriever        CandidateSemanticRetriever
	acquisition              *CandidateKnowledgeAcquisitionService
}

// AIReplyOrchestratorOptions is the explicit composition boundary for the
// runtime façade. Dependency selection stays visible at each call site.
type AIReplyOrchestratorOptions struct {
	ConversationBuilder *ConversationContextBuilder
	ApplicationBuilder  *ApplicationContextBuilder
	ConversationStore   *ConversationStore
	Resolver            *CandidateContextResolver
	Applications        *ApplicationStore
	Clarifications      *CandidateClarificationStore
	Drafts              *AIDraftStore
	Updater             CandidateKnowledgeMutationWriter
	Stories             []CandidateStory
	ExtraPrompt         string
	SemanticRetriever   CandidateSemanticRetriever
	Acquisition         *CandidateKnowledgeAcquisitionService
}

// NewAIReplyOrchestratorWithOptions is the typed composition path. It only
// assembles the extracted AI leaves/workflows; policy remains in those
// use-case packages.
func NewAIReplyOrchestratorWithOptions(ai StructuredAIClient, options AIReplyOrchestratorOptions) *AIReplyOrchestrator {
	o := &AIReplyOrchestrator{
		ai:                  ai,
		conversationBuilder: options.ConversationBuilder,
		applicationBuilder:  options.ApplicationBuilder,
		applications:        options.Applications,
		clarifications:      options.Clarifications,
		drafts:              options.Drafts,
		updater:             options.Updater,
		stories:             append([]CandidateStory{}, options.Stories...),
		extraPrompt:         options.ExtraPrompt,
		semanticRetriever:   options.SemanticRetriever,
		acquisition:         options.Acquisition,
	}
	conversationStore := options.ConversationStore
	resolver := options.Resolver
	if resolver == nil && o.conversationBuilder != nil {
		resolver = o.conversationBuilder.resolver
	}
	return finishAIReplyOrchestrator(ai, o, conversationStore, resolver)
}

func finishAIReplyOrchestrator(ai StructuredAIClient, o *AIReplyOrchestrator, conversationStore *ConversationStore, resolver *CandidateContextResolver) *AIReplyOrchestrator {
	if o.acquisition == nil && resolver != nil && o.clarifications != nil && o.updater != nil {
		var extractor CandidateKnowledgeProposalExtractor
		if ai != nil {
			extractor = StructuredCandidateKnowledgeExtractor{AI: ai}
		}
		o.acquisition = NewCandidateKnowledgeAcquisitionService(resolver, o.updater, o.clarifications, extractor)
	}
	if o.acquisition != nil {
		o.acquisition.SetStories(o.stories)
	}
	if o.conversationBuilder == nil && conversationStore != nil {
		o.conversationBuilder = NewConversationContextBuilder(conversationStore, resolver, o.semanticRetriever)
	} else if o.conversationBuilder != nil && o.semanticRetriever != nil {
		o.conversationBuilder.semanticRetriever = o.semanticRetriever
	}
	if o.applications != nil && o.applicationBuilder == nil {
		o.applicationBuilder = NewApplicationContextBuilder(o.applications, o.applications.conversationStore, o.applications.resolver, o.semanticRetriever)
	} else if o.applicationBuilder != nil && o.semanticRetriever != nil {
		o.applicationBuilder.semanticRetriever = o.semanticRetriever
	}
	if named, ok := ai.(aiModelNamer); ok {
		o.model = named.ModelName()
	}
	var completion llmport.CompletionProvider
	if provider, ok := any(ai).(llmport.CompletionProvider); ok {
		completion = provider
	} else if ai != nil {
		completion = legacyCompletionProvider{client: ai}
	}
	attempts := 1
	if client, ok := any(ai).(*AIClient); ok && client != nil && client.attempts > 0 {
		attempts = client.attempts
	}
	o.replyService = employerreply.NewService(employerreply.Dependencies{Completion: completion}, employerreply.Options{Model: o.model, Attempts: attempts, ExtraPrompt: o.extraPrompt, SemanticRetryDelay: aiRetryDelay})
	o.followUpService = followupdraft.NewService(followupdraft.Dependencies{Completion: completion}, followupdraft.Options{Model: o.model, Attempts: attempts, ExtraPrompt: o.extraPrompt, SemanticRetryDelay: aiRetryDelay})
	o.coverLetterService = coverletter.NewService(coverletter.Dependencies{Completion: completion}, coverletter.Options{Model: o.model, MaxTokens: 512, Temperature: 0.5})
	o.applicationAnswerService = applicationanswer.NewService(applicationanswer.Dependencies{Completion: completion}, applicationanswer.Options{Model: o.model, Attempts: attempts, ExtraPrompt: o.extraPrompt, SemanticRetryDelay: aiRetryDelay})
	if o.conversationBuilder != nil {
		var drafts employerreplyworkflow.DraftStore
		if o.drafts != nil {
			drafts = rootEmployerDraftStore{store: o.drafts, model: o.model}
		}
		var clarifications employerreplyworkflow.ClarificationWriter
		if o.clarifications != nil {
			clarifications = rootEmployerClarificationWriter{store: o.clarifications, acquisition: o.acquisition}
		}
		o.employerWorkflow = employerreplyworkflow.NewService(employerreplyworkflow.Dependencies{
			Conversations: employerReplyWorkflowLoader{builder: o.conversationBuilder},
			Reply:         rootEmployerReplyPreparer{orchestrator: o}, Drafts: drafts, Clarifications: clarifications,
		}, employerreplyworkflow.Options{PromptVersion: dailyWorkflowPromptVersion})
	}
	return o
}

func (c *AIClient) ModelName() string {
	if c == nil {
		return ""
	}
	return c.model
}

func (o *AIReplyOrchestrator) aiContext() stdcontext.Context {
	if o != nil {
		if client, ok := any(o.ai).(*AIClient); ok && client != nil && client.ctx != nil {
			return client.ctx
		}
	}
	return stdcontext.Background()
}

func (o *AIReplyOrchestrator) PrepareEmployerReply(conversationID string) (AIResponseDecision, error) {
	if o == nil || o.employerWorkflow == nil {
		return AIResponseDecision{}, errors.New("reply orchestrator conversation builder is not configured")
	}
	result, err := o.employerWorkflow.Prepare(o.aiContext(), employerreplyworkflow.Input{ConversationID: conversationID, Task: "ответ работодателю"})
	return result.Decision, err
}

func (o *AIReplyOrchestrator) AnalyzeConversation(conversationID string) (AIResponseDecision, error) {
	if o == nil || o.conversationBuilder == nil || o.replyService == nil {
		return AIResponseDecision{}, errors.New("reply orchestrator conversation builder is not configured")
	}
	context, err := o.conversationBuilder.BuildForReply(conversationID)
	if err != nil {
		return AIResponseDecision{}, err
	}
	return o.replyService.Prepare(o.aiContext(), employerReplyInput(context, "анализ разговора"))
}

func (o *AIReplyOrchestrator) PrepareCoverLetter(applicationID string) (AIResponseDecision, error) {
	if o == nil || o.applicationBuilder == nil {
		return AIResponseDecision{}, errors.New("reply orchestrator application builder is not configured")
	}
	context, err := o.applicationBuilder.Build(applicationID)
	if err != nil {
		return AIResponseDecision{}, err
	}
	if o.applicationBuilder.resolver != nil {
		query := context.Application.VacancyTitle + " " + strings.Join(context.MatchResult.MatchedSkills, " ") + " " + strings.Join(context.MatchResult.MatchedProjects, " ")
		if context.Conversation.ID != "" {
			query += "\n" + context.Conversation.VacancyDescription
		}
		resolved, resolveErr := o.applicationBuilder.resolver.ResolveForVacancy(Vacancy{ID: context.Application.VacancyID, Name: context.Application.VacancyTitle}, query)
		if resolveErr != nil {
			return AIResponseDecision{}, resolveErr
		}
		context.CandidateContext = resolved
	}
	o.attachCoverLetterSemanticContext(&context)
	if o.coverLetterService == nil {
		return AIResponseDecision{}, errors.New("reply orchestrator cover-letter service is not configured")
	}
	replyContext := stdcontext.Background()
	if client, ok := any(o.ai).(*AIClient); ok && client != nil && client.ctx != nil {
		replyContext = client.ctx
	}
	result, err := o.coverLetterService.Generate(replyContext, coverLetterInputFromApplication(context, o.stories, o.extraPrompt))
	if err != nil {
		return AIResponseDecision{}, err
	}
	if err := validateStoryClaims(result.Letter, context.CandidateContext, o.stories, context.Application, context.Conversation.VacancyDescription); err != nil {
		return o.manualReviewDecision("черновик использует неподтверждённое содержание story", []string{err.Error()}, nil)
	}
	decision := AIResponseDecision{Action: AIActionDraftReply, Draft: result.Letter, Reason: "cover letter generated", Confidence: 1, UsedFacts: []string{}, MissingInformation: []AIMissingInformation{}, ForbiddenClaimsChecked: true, ConversationTopicsUsed: []string{}, Warnings: []string{}}
	if err := o.persistDraft(AIDraft{Type: AIDraftCoverLetter, ApplicationID: applicationID, Text: decision.Draft, DecisionReason: decision.Reason, UsedFacts: decision.UsedFacts, RelevantKnowledgeHash: RelevantKnowledgeHash(context.RelevantKnowledge)}); err != nil {
		return AIResponseDecision{}, err
	}
	return decision, nil
}

func (o *AIReplyOrchestrator) PrepareApplicationAnswer(applicationID, question string) (AIResponseDecision, error) {
	if strings.TrimSpace(question) == "" {
		return AIResponseDecision{}, errors.New("application question is required")
	}
	if o == nil || o.applicationBuilder == nil {
		return AIResponseDecision{}, errors.New("reply orchestrator application builder is not configured")
	}
	context, err := o.applicationBuilder.Build(applicationID)
	if err != nil {
		return AIResponseDecision{}, err
	}
	if o.applicationBuilder.resolver != nil {
		resolved, resolveErr := o.applicationBuilder.resolver.ResolveForVacancy(Vacancy{ID: context.Application.VacancyID, Name: context.Application.VacancyTitle}, question)
		if resolveErr != nil {
			return AIResponseDecision{}, resolveErr
		}
		context.CandidateContext = resolved
	}
	if o.applicationAnswerService == nil {
		return AIResponseDecision{}, errors.New("reply orchestrator application-answer service is not configured")
	}
	result, err := o.applicationAnswerService.Prepare(applicationAnswerContext(o.ai), applicationAnswerInput(context, question, o.stories))
	if err != nil {
		return AIResponseDecision{}, err
	}
	decision := result.Decision
	if decision.Action == AIActionNeedCandidate {
		if err := o.persistClarifications("", context.Application.ID, decision); err != nil {
			return AIResponseDecision{}, err
		}
	}
	if decision.Action == AIActionDraftReply {
		if err := o.persistDraft(AIDraft{Type: AIDraftApplicationAnswer, ApplicationID: applicationID, Text: decision.Draft, DecisionReason: decision.Reason, UsedFacts: decision.UsedFacts, RelevantKnowledgeHash: RelevantKnowledgeHash(context.RelevantKnowledge)}); err != nil {
			return AIResponseDecision{}, err
		}
	}
	return decision, nil
}

func conversationNeedsCandidateInput(context ConversationContext) bool {
	return context.CandidateContext.RequiresCandidateInput()
}

func (o *AIReplyOrchestrator) attachCoverLetterSemanticContext(ctx *ApplicationContext) {
	if o == nil || ctx == nil || o.semanticRetriever == nil || ctx.Application.VacancyTitle == "" {
		return
	}
	resolver := o.applicationBuilder.resolver
	if resolver == nil {
		return
	}
	candidate, diagnostics, err := resolver.canonicalCandidate()
	if err != nil || len(diagnostics.Conflicts) > 0 || strings.TrimSpace(candidate.ID) == "" {
		return
	}
	query := buildCoverLetterSemanticQuery(ctx.Application.VacancyTitle, ctx.Conversation.VacancyDescription, ctx.MatchResult)
	results, err := o.semanticRetriever.Retrieve(stdcontext.Background(), SemanticRetrievalRequest{CandidateID: candidate.ID, Query: query, EntityTypes: semanticEntityTypesForPurpose(SemanticRetrievalPurposeCoverLetter), Limit: semanticRetrievalTopK, Purpose: SemanticRetrievalPurposeCoverLetter})
	if err != nil {
		if logger != nil {
			logger.Warn("semantic retrieval skipped for cover letter: %v", err)
		}
		return
	}
	ctx.RelevantExamples = BuildSafeSemanticContext(candidate, results)
	if len(ctx.RelevantExamples) == 0 {
		return
	}
	ctx.RelevantKnowledge, _ = relevantKnowledgeSnapshotForResolverWithSemantic(resolver, ctx.CandidateContext, "", nil, ctx.RelevantExamples)
	if logger != nil {
		logger.Debug("semantic retrieval used purpose=%s candidates=%d selected=%d ids=%s", SemanticRetrievalPurposeCoverLetter, len(results), len(ctx.RelevantExamples), semanticSelectionIDs(ctx.RelevantExamples))
	}
}

func decodeAIResponseDecision(raw string) (AIResponseDecision, error) {
	return employerreply.ParseDecision(raw)
}

func validateAIResponseDecision(value AIResponseDecision) error {
	return employerreply.ValidateDecision(value)
}

func aiReplySystemPrompt(task, extra string) string {
	return employerreply.SystemPrompt(task, extra)
}

func aiResponseDecisionSchema() *ChatJSONSchema {
	return employerReplySchema()
}

func marshalSafeContext(context ConversationContext) string {
	return employerreply.MarshalContext(employerReplyInput(context, "").Context)
}

func (c ConversationContext) ConsistencyWarningsText() []string {
	result := []string{}
	for _, warning := range c.ConsistencyWarnings {
		result = append(result, warning.Message)
	}
	return result
}

func (o *AIReplyOrchestrator) manualReviewDecision(reason string, warnings, topics []string) (AIResponseDecision, error) {
	return AIResponseDecision{Action: AIActionManualReview, Reason: reason, Confidence: 1, UsedFacts: []string{}, MissingInformation: []AIMissingInformation{}, ForbiddenClaimsChecked: true, ConversationTopicsUsed: uniqueStrings(topics), Warnings: uniqueStrings(warnings)}, nil
}

func decisionForMissing(values []CandidateMissingInformation, reason string, topics []string) AIResponseDecision {
	missing := make([]AIMissingInformation, 0, len(values))
	for _, value := range values {
		question := strings.TrimSpace(value.Question)
		if question == "" {
			continue
		}
		missing = append(missing, AIMissingInformation{Topic: clarificationTopic(question), Question: question})
	}
	return AIResponseDecision{Action: AIActionNeedCandidate, Reason: reason, Confidence: 1, UsedFacts: []string{}, MissingInformation: missing, ForbiddenClaimsChecked: true, ConversationTopicsUsed: uniqueStrings(topics), Warnings: []string{}}
}

func clarificationTopic(question string) string {
	for _, name := range contextTechnologyNames {
		if contextMentions(question, name) {
			return name
		}
	}
	words := strings.Fields(question)
	if len(words) > 5 {
		words = words[:5]
	}
	return strings.Join(words, " ")
}

func (o *AIReplyOrchestrator) persistClarifications(conversationID, applicationID string, decision AIResponseDecision) error {
	if o == nil || o.clarifications == nil {
		return nil
	}
	existingValues, err := o.clarifications.List()
	if err != nil {
		return err
	}
	for _, missing := range decision.MissingInformation {
		alreadyPending := false
		for _, existing := range existingValues {
			if existing.Status == ClarificationPending && existing.ConversationID == conversationID && existing.ApplicationID == applicationID && existing.Topic == missing.Topic && existing.Question == missing.Question {
				alreadyPending = true
				break
			}
		}
		if alreadyPending {
			continue
		}
		created, err := o.clarifications.Create(CandidateClarificationRequest{ConversationID: conversationID, ApplicationID: applicationID, Topic: missing.Topic, Question: missing.Question, Reason: decision.Reason})
		if err != nil {
			return err
		}
		existingValues = append(existingValues, created)
	}
	return nil
}

func (o *AIReplyOrchestrator) persistDraft(draft AIDraft) error {
	if o == nil || o.drafts == nil {
		return nil
	}
	draft.Model = o.model
	_, err := o.drafts.Create(draft)
	return err
}

// ResolveCandidateClarification deliberately records an unconfirmed answer
// through the existing updater. It never calls ConfirmKnowledge and cannot
// turn a candidate answer into confirmed employer-safe knowledge by itself.
func (o *AIReplyOrchestrator) ResolveCandidateClarification(id, answer string) (KnowledgeUpdateResult, error) {
	return o.ResolveCandidateClarificationAnswer(id, CandidateAnswer{Kind: "free_text", Raw: answer})
}

func (o *AIReplyOrchestrator) ResolveCandidateClarificationAnswer(id string, candidateAnswer CandidateAnswer) (KnowledgeUpdateResult, error) {
	if o == nil || o.clarifications == nil || o.updater == nil {
		return KnowledgeUpdateResult{}, errors.New("clarification pipeline is not configured")
	}
	request, err := o.clarifications.Get(id)
	if err != nil {
		return KnowledgeUpdateResult{}, err
	}
	if request.Status != ClarificationPending {
		return KnowledgeUpdateResult{}, errors.New("clarification is already resolved")
	}
	if strings.TrimSpace(candidateAnswer.Raw) == "" {
		return KnowledgeUpdateResult{}, errors.New("clarification answer is required")
	}
	if candidateAnswer.Kind == "" {
		candidateAnswer.Kind = "free_text"
	}
	if o.acquisition != nil {
		request, requestErr := o.clarifications.Get(id)
		// Old clarification records predate typed gaps and retain their legacy
		// safe behavior. Only structured acquisition requests enter the new
		// interpretation loop.
		if requestErr == nil && (request.Category != "" || request.UnknownID != "" || request.GapKey != "") {
			result, err := o.acquisition.SubmitAnswer(stdcontext.Background(), id, candidateAnswer)
			return KnowledgeUpdateResult{EntityID: result.UnknownID, ProposalID: firstNonEmptyString(result.ProposalIDs), QuestionID: result.UnknownID}, err
		}
	}
	if candidateAnswer.Kind != "free_text" {
		return KnowledgeUpdateResult{}, errors.New("choice answers require a typed knowledge clarification")
	}
	result, err := o.updater.UpdateUnknown(CandidateUnknown{Question: request.Question, Hypothesis: candidateAnswer.Raw, Status: CandidateUnknownNeedsConfirmation}, KnowledgeUpdate{Source: KnowledgeSourceRecord{Type: KnowledgeSourceCandidateInterview, Evidence: []string{"candidate clarification response"}}, Reason: "Ответ кандидата на clarification; требуется отдельное подтверждение факта."})
	if err != nil {
		return KnowledgeUpdateResult{}, err
	}
	if err := o.clarifications.SetStatus(id, ClarificationAnswered); err != nil {
		return KnowledgeUpdateResult{}, err
	}
	return result, nil
}

// ValidateAIDraft is intentionally conservative. It rejects explicit
// forbidden/unsupported technology claims and untrusted numerical/seniority
// assertions before a draft can be stored.
func (o *AIReplyOrchestrator) ValidateAIDraft(draft string, context CandidateContext, claims []CandidateConversationClaim) error {
	// Claims already sent to the employer are not permission to repeat a fact;
	// they are checked for conflicts by the context builder. Keep this argument
	// explicit so callers cannot accidentally validate against raw history.
	_ = claims
	return employerreply.ValidateDraft(draft, context)
}

func ValidateAIDraft(draft string, context CandidateContext, claims []CandidateConversationClaim) error {
	return (&AIReplyOrchestrator{}).ValidateAIDraft(draft, context, claims)
}

func latestEmployerMessage(messages []ConversationMessage) *ConversationMessage {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Sender == ConversationSenderEmployer {
			value := messages[i]
			return &value
		}
	}
	return nil
}
func latestEmployerMessageID(messages []ConversationMessage) string {
	value := latestEmployerMessage(messages)
	if value == nil {
		return ""
	}
	return value.ID
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	result := []string{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	sort.Strings(result)
	return result
}
