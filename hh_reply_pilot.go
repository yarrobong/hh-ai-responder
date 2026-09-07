package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"
)

const (
	PilotRecommended    = "RECOMMENDED"
	PilotPossible       = "POSSIBLE"
	PilotNotRecommended = "NOT_RECOMMENDED"
	PilotFresh          = "fresh"
	PilotAging          = "aging"
	PilotStale          = "stale"
	PilotSyncVerified   = "VERIFIED"
	PilotSyncUnknown    = "UNKNOWN"
	PilotAIEligible     = "ELIGIBLE"
	PilotAIReview       = "REVIEW_REQUIRED"
)

const HHPilotObservationsFilename = "hh_pilot_observations.json"

const (
	pilotFreshFor   = 72 * time.Hour
	pilotStaleAfter = 14 * 24 * time.Hour
)

// PilotCandidateReport is intentionally a compact, body-free report. Message
// text is only returned by the single-conversation review payload.
type PilotCandidateReport struct {
	ConversationID               string     `json:"conversation_id"`
	Company                      string     `json:"company"`
	Vacancy                      string     `json:"vacancy"`
	HHDestinationStatus          string     `json:"hh_destination_status"`
	LastHumanSender              string     `json:"last_human_sender"`
	LastHumanMessageAt           *time.Time `json:"last_human_message_timestamp,omitempty"`
	ConversationFreshness        string     `json:"conversation_freshness"`
	FreshSyncStatus              string     `json:"fresh_sync_status"`
	CandidateContextStatus       string     `json:"candidate_context_status"`
	AIReplyEligibility           string     `json:"ai_reply_eligibility"`
	ReplyRequirement             string     `json:"reply_requirement"`
	UnresolvedClarificationCount int        `json:"unresolved_clarification_count"`
	Warnings                     []string   `json:"warnings,omitempty"`
	ExistingDraftStatus          string     `json:"existing_draft_status"`
	Eligibility                  string     `json:"eligibility"`
	Suitability                  string     `json:"pilot_suitability"`
	SuitabilityReasons           []string   `json:"suitability_reasons,omitempty"`
	RankingReasons               []string   `json:"ranking_reasons,omitempty"`
	RankingScore                 int        `json:"ranking_score"`
}

type PilotCandidateReportSet struct {
	GeneratedAt    time.Time              `json:"generated_at"`
	Recommended    []PilotCandidateReport `json:"recommended"`
	Possible       []PilotCandidateReport `json:"possible"`
	NotRecommended []PilotCandidateReport `json:"not_recommended"`
}

// PilotCandidatePreview is the bounded, body-bearing review payload for the
// next manual pilot. The existing pilot-candidates report remains body-free;
// this separate projection is opt-in and never creates or approves a draft.
type PilotCandidatePreview struct {
	Rank                   int      `json:"rank"`
	Company                string   `json:"company"`
	Vacancy                string   `json:"vacancy"`
	ConversationID         string   `json:"conversation_id"`
	LastEmployerMessage    string   `json:"last_employer_message"`
	EmployerQuestion       string   `json:"employer_question"`
	CandidateFacts         []string `json:"candidate_facts"`
	CandidateContextStatus string   `json:"candidate_context_status"`
	ProposedDraft          string   `json:"proposed_draft"`
	Warnings               []string `json:"warnings,omitempty"`
	WhySuitable            []string `json:"why_suitable"`
}

type PilotShortlist struct {
	GeneratedAt time.Time               `json:"generated_at"`
	Candidates  []PilotCandidatePreview `json:"candidates"`
}

type PilotObservation struct {
	ActionID               string    `json:"action_id"`
	DraftGenerated         bool      `json:"draft_generated"`
	UserEdited             bool      `json:"user_edited"`
	PreflightPassed        bool      `json:"preflight_passed"`
	DeliveryState          string    `json:"delivery_state"`
	ReconciliationSuccess  bool      `json:"reconciliation_success"`
	ConversationStateAfter string    `json:"conversation_state_after"`
	Issues                 []string  `json:"issues,omitempty"`
	CreatedAt              time.Time `json:"created_at"`
}

type pilotObservationFile struct {
	Version      int                `json:"version"`
	Observations []PilotObservation `json:"observations"`
}

type PilotObservationStore struct {
	path         string
	observations []PilotObservation
}

func NewPilotObservationStore(path string) *PilotObservationStore {
	return &PilotObservationStore{path: path, observations: []PilotObservation{}}
}

func (s *PilotObservationStore) Load() error {
	if s == nil {
		return errors.New("pilot observation store is nil")
	}
	if strings.TrimSpace(s.path) == "" {
		s.observations = []PilotObservation{}
		return nil
	}
	raw, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		s.observations = []PilotObservation{}
		return nil
	}
	if err != nil || profileContainsSecret(raw) {
		return errors.New("cannot read pilot observations")
	}
	var file pilotObservationFile
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&file) != nil || decoder.Decode(new(any)) != io.EOF || file.Version != 1 || file.Observations == nil {
		return errors.New("invalid pilot observation store")
	}
	for _, value := range file.Observations {
		if err := validatePilotObservation(value); err != nil {
			return err
		}
	}
	s.observations = append([]PilotObservation{}, file.Observations...)
	return nil
}

func validatePilotObservation(value PilotObservation) error {
	if strings.TrimSpace(value.ActionID) == "" || strings.TrimSpace(value.DeliveryState) == "" || strings.TrimSpace(value.ConversationStateAfter) == "" || value.CreatedAt.IsZero() {
		return errors.New("invalid pilot observation")
	}
	raw, _ := json.Marshal(value)
	if profileContainsSecret(raw) || profileContainsSecret([]byte(strings.Join(value.Issues, "\n"))) {
		return errors.New("pilot observation contains a forbidden secret marker")
	}
	return nil
}

func (s *PilotObservationStore) Save() error {
	if s == nil {
		return errors.New("pilot observation store is nil")
	}
	for _, value := range s.observations {
		if err := validatePilotObservation(value); err != nil {
			return err
		}
	}
	if strings.TrimSpace(s.path) == "" {
		return nil
	}
	raw, err := json.MarshalIndent(pilotObservationFile{Version: 1, Observations: s.observations}, "", "  ")
	if err != nil {
		return errors.New("cannot encode pilot observations")
	}
	return atomicPrivateStoreWrite(s.path, raw, ".hh-pilot-observations-*.tmp")
}

func (s *PilotObservationStore) Append(value PilotObservation) error {
	if s == nil {
		return errors.New("pilot observation store is nil")
	}
	if value.CreatedAt.IsZero() {
		value.CreatedAt = time.Now().UTC()
	}
	if err := validatePilotObservation(value); err != nil {
		return err
	}
	return s.Upsert(value)
}

func (s *PilotObservationStore) Upsert(value PilotObservation) error {
	if s == nil {
		return errors.New("pilot observation store is nil")
	}
	if value.CreatedAt.IsZero() {
		value.CreatedAt = time.Now().UTC()
	}
	if err := validatePilotObservation(value); err != nil {
		return err
	}
	for i, old := range s.observations {
		if old.ActionID == value.ActionID {
			if value.CreatedAt.IsZero() {
				value.CreatedAt = old.CreatedAt
			}
			s.observations[i] = value
			return s.Save()
		}
	}
	s.observations = append(s.observations, value)
	return s.Save()
}

func (s *PilotObservationStore) List() []PilotObservation {
	if s == nil {
		return []PilotObservation{}
	}
	return append([]PilotObservation{}, s.observations...)
}

func pilotFreshness(at time.Time, now time.Time) string {
	if at.IsZero() || now.Before(at) {
		return "unknown"
	}
	age := now.Sub(at)
	switch {
	case age <= pilotFreshFor:
		return PilotFresh
	case age <= pilotStaleAfter:
		return PilotAging
	default:
		return PilotStale
	}
}

func pilotLatestHuman(c EmployerConversation) *ConversationMessage {
	values := deliveredMessages(c.Messages)
	if len(values) == 0 {
		return nil
	}
	value := values[len(values)-1]
	return &value
}

func pilotDraftStatus(drafts *AIDraftStore, conversationID string) string {
	if drafts == nil {
		return "none"
	}
	values, err := drafts.List()
	if err != nil {
		return "unavailable"
	}
	var latest *AIDraft
	for i := range values {
		value := values[i]
		if value.ConversationID != conversationID || value.Type != AIDraftEmployerReply {
			continue
		}
		if latest == nil || value.UpdatedAt.After(latest.UpdatedAt) {
			copy := value
			latest = &copy
		}
	}
	if latest == nil {
		return "none"
	}
	return string(latest.Status)
}

func latestEmployerReplyDraft(drafts *AIDraftStore, conversationID string) (AIDraft, error) {
	if drafts == nil {
		return AIDraft{}, errors.New("AI draft store is unavailable")
	}
	values, err := drafts.List()
	if err != nil {
		return AIDraft{}, err
	}
	var latest AIDraft
	found := false
	for _, value := range values {
		if value.ConversationID == conversationID && value.Type == AIDraftEmployerReply && (!found || value.UpdatedAt.After(latest.UpdatedAt)) {
			latest, found = value, true
		}
	}
	if !found {
		return AIDraft{}, errors.New("draft not found")
	}
	return latest, nil
}

func pilotClarificationCount(ctx ConversationContext, clarifications *CandidateClarificationStore, conversationID string) int {
	seen := map[string]bool{}
	for _, item := range ctx.UnresolvedQuestions {
		key := item.MessageID + "\x00" + item.Question
		seen[key] = true
	}
	if clarifications != nil {
		if values, err := clarifications.List(); err == nil {
			for _, value := range values {
				if value.ConversationID == conversationID && value.Status == ClarificationPending {
					seen[value.ID] = true
				}
			}
		}
	}
	return len(seen)
}

func pilotWarnings(eligibility ConversationEligibilityReport, ctx ConversationContext) []string {
	result := []string{}
	for _, finding := range append(append([]EligibilityFinding{}, eligibility.Warnings...), eligibility.Blockers...) {
		if finding.Code != "" {
			result = append(result, finding.Code)
		}
	}
	for _, warning := range ctx.ConsistencyWarnings {
		if warning.Code != "" {
			result = append(result, warning.Code)
		}
	}
	return uniqueStrings(result)
}

func pilotIntentReason(ctx ConversationContext, message *ConversationMessage) (string, bool) {
	if ctx.CandidateContext.MessageIntent == EmployerMessageIntentInterviewInvitation {
		return "требуется ручная координация интервью", true
	}
	if ctx.CandidateContext.MessageIntent == EmployerMessageIntentStatusMessage || ctx.CandidateContext.MessageIntent == EmployerMessageIntentAcknowledgement {
		return "сообщение выглядит informational/acknowledgement", true
	}
	if message == nil {
		return "последнее человеческое сообщение не определено", true
	}
	text := strings.ToLower(message.Text)
	if strings.Contains(text, "интервью") || strings.Contains(text, "собеседован") || strings.Contains(text, "созвон") {
		return "сообщение требует координации интервью", true
	}
	if len([]rune(strings.TrimSpace(message.Text))) > 420 || strings.Contains(text, "анкета") || strings.Contains(text, "опрос") || strings.Contains(text, "тестовое") {
		return "сообщение требует длинного или сложного ответа", true
	}
	return "", false
}

func buildPilotCandidateReport(c EmployerConversation, eligibility ConversationEligibilityReport, ctx ConversationContext, drafts *AIDraftStore, clarifications *CandidateClarificationStore, now time.Time) PilotCandidateReport {
	latest := pilotLatestHuman(c)
	lastSender, lastAt := "none", (*time.Time)(nil)
	if latest != nil {
		lastSender, lastAt = string(latest.Sender), &latest.Timestamp
	}
	freshness := pilotFreshness(derefTime(lastAt, time.Time{}), now)
	if lastAt == nil {
		freshness = "unknown"
	}
	contextStatus := "REVIEW_REQUIRED"
	if !ctx.CandidateContext.RequiresCandidateInput() && len(ctx.ConsistencyWarnings) == 0 {
		contextStatus = "ANSWERABLE"
	}
	clarificationsCount := pilotClarificationCount(ctx, clarifications, c.ID)
	warnings := pilotWarnings(eligibility, ctx)
	aiEligibility := PilotAIReview
	if eligibility.Classification == EligibilitySafe && lastSender == string(ConversationSenderEmployer) && contextStatus == "ANSWERABLE" && clarificationsCount == 0 && len(warnings) == 0 {
		aiEligibility = PilotAIEligible
	}
	syncStatus := PilotSyncUnknown
	if !c.HHUpdatedAt.IsZero() && !c.HHUpdatedAt.After(now) {
		syncStatus = PilotSyncVerified
	}
	result := PilotCandidateReport{
		ConversationID: c.ID, Company: c.CompanyName, Vacancy: c.VacancyTitle,
		HHDestinationStatus: func() string {
			if destinationStatus(c) == "STRONG" {
				return PilotSyncVerified
			}
			return destinationStatus(c)
		}(),
		LastHumanSender: lastSender, LastHumanMessageAt: lastAt, ConversationFreshness: freshness,
		FreshSyncStatus: syncStatus, CandidateContextStatus: contextStatus, AIReplyEligibility: aiEligibility,
		ReplyRequirement:             string(ctx.ReplyRequirement),
		UnresolvedClarificationCount: clarificationsCount, Warnings: warnings, ExistingDraftStatus: pilotDraftStatus(drafts, c.ID),
		Eligibility: string(eligibility.Classification), Suitability: PilotPossible, RankingScore: 0,
	}
	if eligibility.Classification != EligibilitySafe {
		result.Suitability = PilotNotRecommended
		result.SuitabilityReasons = []string{"conversation is not SAFE_FOR_MANUAL_REPLY"}
		return result
	}
	if freshness == PilotStale {
		result.Suitability = PilotNotRecommended
		result.SuitabilityReasons = []string{"разговор слишком старый для первого pilot"}
		return result
	}
	if reason, informational := pilotIntentReason(ctx, latest); informational && strings.Contains(reason, "informational") {
		result.Suitability = PilotNotRecommended
		result.SuitabilityReasons = []string{reason}
		return result
	}
	if latest == nil || lastSender != string(ConversationSenderEmployer) {
		result.SuitabilityReasons = append(result.SuitabilityReasons, "последнее человеческое сообщение не от работодателя")
	}
	if freshness != PilotFresh {
		result.SuitabilityReasons = append(result.SuitabilityReasons, "сообщение не относительно свежее")
	}
	if result.HHDestinationStatus != PilotSyncVerified {
		result.SuitabilityReasons = append(result.SuitabilityReasons, "HH destination не подтверждён")
	}
	if syncStatus != PilotSyncVerified {
		result.SuitabilityReasons = append(result.SuitabilityReasons, "последний HH sync не подтверждён локальной меткой")
	}
	if contextStatus != "ANSWERABLE" {
		result.SuitabilityReasons = append(result.SuitabilityReasons, "CandidateContext требует review")
	}
	if clarificationsCount > 0 {
		result.SuitabilityReasons = append(result.SuitabilityReasons, "есть unresolved clarification")
	}
	if len(warnings) > 0 {
		result.SuitabilityReasons = append(result.SuitabilityReasons, "есть consistency/audit warning")
	}
	if reason, complex := pilotIntentReason(ctx, latest); complex {
		result.SuitabilityReasons = append(result.SuitabilityReasons, reason)
	}
	result.SuitabilityReasons = uniqueStrings(result.SuitabilityReasons)
	if len(result.SuitabilityReasons) == 0 && result.AIReplyEligibility == PilotAIEligible {
		result.Suitability = PilotRecommended
		result.RankingScore = 100
		result.RankingReasons = []string{"последнее сообщение работодателя", "сообщение свежее", "destination verified", "CandidateContext answerable", "нет clarification или warning", "короткий фактический ответ"}
	} else {
		result.RankingScore = 50 - len(result.SuitabilityReasons)
	}
	return result
}

func BuildPilotCandidateReports(conversations *ConversationStore, applications *ApplicationStore, clarifications *CandidateClarificationStore, resolver *CandidateContextResolver, drafts *AIDraftStore, now time.Time) (PilotCandidateReportSet, error) {
	if conversations == nil || resolver == nil {
		return PilotCandidateReportSet{}, errors.New("pilot report dependencies are unavailable")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	reports, err := BuildConversationEligibilityReports(conversations, applications, clarifications, resolver)
	if err != nil {
		return PilotCandidateReportSet{}, err
	}
	values, err := conversations.ListConversations()
	if err != nil {
		return PilotCandidateReportSet{}, err
	}
	byID := map[string]EmployerConversation{}
	for _, value := range values {
		byID[value.ID] = value
	}
	result := PilotCandidateReportSet{GeneratedAt: now, Recommended: []PilotCandidateReport{}, Possible: []PilotCandidateReport{}, NotRecommended: []PilotCandidateReport{}}
	builder := NewConversationContextBuilder(conversations, resolver)
	for _, eligibility := range reports {
		if eligibility.Classification != EligibilitySafe {
			continue
		}
		conversation, ok := byID[eligibility.ConversationID]
		if !ok {
			continue
		}
		ctx, ctxErr := builder.BuildForReply(conversation.ID)
		if ctxErr != nil {
			continue
		}
		candidate := buildPilotCandidateReport(conversation, eligibility, ctx, drafts, clarifications, now)
		switch candidate.Suitability {
		case PilotRecommended:
			result.Recommended = append(result.Recommended, candidate)
		case PilotNotRecommended:
			result.NotRecommended = append(result.NotRecommended, candidate)
		default:
			result.Possible = append(result.Possible, candidate)
		}
	}
	order := func(items []PilotCandidateReport) {
		sort.SliceStable(items, func(i, j int) bool {
			if items[i].RankingScore != items[j].RankingScore {
				return items[i].RankingScore > items[j].RankingScore
			}
			if items[i].LastHumanMessageAt != nil && items[j].LastHumanMessageAt != nil && !items[i].LastHumanMessageAt.Equal(*items[j].LastHumanMessageAt) {
				return items[i].LastHumanMessageAt.After(*items[j].LastHumanMessageAt)
			}
			return items[i].ConversationID < items[j].ConversationID
		})
	}
	order(result.Recommended)
	order(result.Possible)
	order(result.NotRecommended)
	return result, nil
}

func pilotPreviewFacts(ctx ConversationContext) []string {
	result := append([]string{}, ctx.CandidateContext.AllowedFacts...)
	for _, fact := range ctx.CandidateContext.ResolvedFacts {
		if fact.Status == ResolvedFactAnswerable && strings.TrimSpace(fact.Value) != "" {
			result = append(result, fact.Value)
		}
	}
	return uniqueStrings(result)
}

func pilotProposedDraft(ctx ConversationContext, message *ConversationMessage) string {
	if message == nil {
		return ""
	}
	for _, fact := range ctx.CandidateContext.ResolvedFacts {
		if fact.Status != ResolvedFactAnswerable || strings.TrimSpace(fact.Value) == "" {
			continue
		}
		switch fact.Topic {
		case "salary":
			if strings.TrimSpace(fact.Value) == "Минимум 40-50 тыс., цель 50-100 тыс., 100+ интересно" {
				return "Рассматриваю предложения от 40–50 тыс. рублей, целевой диапазон — 50–100 тыс. рублей. Варианты выше 100 тыс. рублей тоже готов обсудить."
			}
			return "Рассматриваю предложения: " + strings.TrimSuffix(fact.Value, ".") + "."
		case "relocation":
			if strings.Contains(strings.ToLower(fact.Value), "не готов") {
				if location := relocationLocation(message.Text); location != "" {
					return "Спасибо за вопрос. На данный момент к релокации " + location + " не готов."
				}
				return "Спасибо за вопрос. На данный момент к релокации не готов."
			}
			return "Спасибо за вопрос. " + strings.TrimSuffix(fact.Value, ".") + "."
		}
	}
	if ctx.CandidateContext.UserConfirmationRequired {
		return ""
	}
	return "Спасибо за обратную связь! Буду рад узнать о результатах рассмотрения резюме."
}

// relocationLocation extracts only the location explicitly present in the
// employer's question. It is deliberately narrow so a draft cannot invent a
// destination from vacancy or candidate context.
func relocationLocation(text string) string {
	lower := strings.ToLower(text)
	for _, topic := range []string{"релокац", "переезд", "переехать", "переезжать"} {
		start := strings.Index(lower, topic)
		if start < 0 {
			continue
		}
		rest := text[start:]
		lowerRest := lower[start:]
		for _, preposition := range []string{" в ", " во "} {
			at := strings.Index(lowerRest, preposition)
			if at < 0 {
				continue
			}
			location := strings.TrimSpace(rest[at+len(preposition):])
			if end := strings.IndexAny(location, "?!.,;:\n\r"); end >= 0 {
				location = location[:end]
			}
			location = strings.TrimSpace(location)
			if location != "" {
				return strings.TrimSpace(preposition + location)
			}
		}
	}
	return ""
}

func pilotTopicWarnings(ctx ConversationContext, message string) []string {
	reason := classifyHighRiskChatMessage(message)
	if reason == "" {
		return nil
	}
	if reason == "relocation" {
		for _, fact := range ctx.CandidateContext.ResolvedFacts {
			if fact.Topic == "relocation" && fact.Status == ResolvedFactAnswerable && strings.TrimSpace(fact.Value) != "" {
				// Relocation is fact-sensitive, but a confirmed preference is a
				// bounded answer, not an actually high-risk claim. Approval and
				// dry-run gates still apply to the eventual HH write.
				return nil
			}
		}
	}
	return []string{"HIGH_RISK_TOPIC_MANUAL_REVIEW"}
}

// BuildPilotShortlistPreviews is read-only. It deliberately uses the local
// eligibility projection and trusted vacancy archive flag, then renders a
// bounded draft suggestion without calling the AI or mutating AIDraftStore.
func BuildPilotShortlistPreviews(conversations *ConversationStore, applications *ApplicationStore, vacancies *VacancyStore, clarifications *CandidateClarificationStore, resolver *CandidateContextResolver, now time.Time, limit int) (PilotShortlist, error) {
	if conversations == nil || resolver == nil {
		return PilotShortlist{}, errors.New("pilot shortlist dependencies are unavailable")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if limit <= 0 || limit > 5 {
		limit = 5
	}
	reports, err := BuildConversationEligibilityReports(conversations, applications, clarifications, resolver)
	if err != nil {
		return PilotShortlist{}, err
	}
	values, err := conversations.ListConversations()
	if err != nil {
		return PilotShortlist{}, err
	}
	byID := map[string]EmployerConversation{}
	for _, value := range values {
		byID[value.ID] = value
	}
	builder := NewConversationContextBuilder(conversations, resolver)
	result := PilotShortlist{GeneratedAt: now, Candidates: []PilotCandidatePreview{}}
	type rankedPreview struct {
		item  PilotCandidatePreview
		score int
		at    time.Time
	}
	ranked := []rankedPreview{}
	for _, eligibility := range reports {
		if eligibility.Classification != EligibilitySafe || eligibility.StateStatus != string(ConversationCandidateActionRequired) {
			continue
		}
		conversation, ok := byID[eligibility.ConversationID]
		if !ok {
			continue
		}
		if _, terminalState := terminalConversationState(conversation); terminalState {
			continue
		}
		if vacancies != nil {
			vacancy, vacancyErr := vacancies.Get(conversation.VacancyID)
			if vacancyErr != nil || vacancy.Archived {
				continue
			}
		}
		latest := pilotLatestHuman(conversation)
		if latest == nil || latest.Sender != ConversationSenderEmployer {
			continue
		}
		ctx, contextErr := builder.BuildForReply(conversation.ID)
		if contextErr != nil || ctx.ReplyRequirement != ReplyRequired || ctx.CandidateContext.UnsupportedIntent || ctx.CandidateContext.RequiresCandidateInput() || len(ctx.ConsistencyWarnings) > 0 {
			continue
		}
		if reason, complex := pilotIntentReason(ctx, latest); complex {
			_ = reason
			continue
		}
		candidateFacts := pilotPreviewFacts(ctx)
		warnings := pilotWarnings(eligibility, ctx)
		messageLower := strings.ToLower(latest.Text)
		warnings = append(warnings, pilotTopicWarnings(ctx, latest.Text)...)
		if !looksLikeFactualQuestion(messageLower) && !strings.Contains(messageLower, "доход") {
			warnings = append(warnings, "STATUS_OR_COURTESY_MESSAGE")
		}
		if count := pilotClarificationCount(ctx, clarifications, conversation.ID); count > 0 {
			warnings = append(warnings, "NON_CRITICAL_CLARIFICATION_PENDING")
		}
		if freshness := pilotFreshness(latest.Timestamp, now); freshness != PilotFresh {
			warnings = append(warnings, "MESSAGE_NOT_FRESH")
		}
		why := []string{"REPLY_REQUIRED", "SAFE_FOR_MANUAL_REPLY", "non-terminal vacancy", "latest human message is from employer", "no candidate reply after the employer message", "history is sufficiently complete", "candidate context is ANSWERABLE"}
		if len(candidateFacts) > 0 {
			why = append(why, "confirmed candidate facts are available")
		}
		if len(warnings) == 0 {
			why = append(why, "no critical clarification, conflict, or complex flow")
		} else if len(ctx.ConsistencyWarnings) == 0 && !ctx.CandidateContext.RequiresCandidateInput() {
			why = append(why, "no critical clarification, conflict, or complex flow; warnings are non-critical")
		}
		item := PilotCandidatePreview{
			Company: conversation.CompanyName, Vacancy: conversation.VacancyTitle, ConversationID: conversation.ID,
			LastEmployerMessage: latest.Text, EmployerQuestion: latest.Text, CandidateFacts: candidateFacts,
			CandidateContextStatus: "ANSWERABLE", ProposedDraft: pilotProposedDraft(ctx, latest), Warnings: uniqueStrings(warnings), WhySuitable: why,
		}
		score := 0
		if looksLikeFactualQuestion(messageLower) || strings.Contains(messageLower, "доход") {
			score += 40
		}
		if ctx.CandidateContext.MessageIntent == EmployerMessageIntentFactualQuestion {
			score += 10
		}
		if len(candidateFacts) > 0 {
			score += 25
		}
		if pilotFreshness(latest.Timestamp, now) == PilotFresh {
			score += 20
		} else {
			score -= 10
		}
		if len(warnings) == 0 {
			score += 10
		} else {
			score -= len(warnings)
		}
		ranked = append(ranked, rankedPreview{item: item, score: score, at: latest.Timestamp})
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].score != ranked[j].score {
			return ranked[i].score > ranked[j].score
		}
		return ranked[i].at.After(ranked[j].at)
	})
	for _, candidate := range ranked {
		if len(result.Candidates) >= limit {
			break
		}
		result.Candidates = append(result.Candidates, candidate.item)
	}
	for i := range result.Candidates {
		result.Candidates[i].Rank = i + 1
	}
	return result, nil
}

func WritePilotShortlistText(out io.Writer, shortlist PilotShortlist) error {
	if _, err := fmt.Fprintf(out, "Stage 21 shortlist (read-only)\nCandidates: %d\n", len(shortlist.Candidates)); err != nil {
		return err
	}
	for _, item := range shortlist.Candidates {
		if _, err := fmt.Fprintf(out, "\n%d. %s · %s\nConversation: %s\nLast employer message: %s\nEmployer asks: %s\nCandidate facts: %s\nCandidate context: %s\nProposed draft preview: %s\nWarnings: %s\nWhy suitable: %s\n", item.Rank, item.Company, item.Vacancy, item.ConversationID, item.LastEmployerMessage, item.EmployerQuestion, strings.Join(item.CandidateFacts, "; "), item.CandidateContextStatus, item.ProposedDraft, strings.Join(item.Warnings, "; "), strings.Join(item.WhySuitable, "; ")); err != nil {
			return err
		}
	}
	return nil
}

func WritePilotCandidateText(out io.Writer, set PilotCandidateReportSet) error {
	if _, err := fmt.Fprintf(out, "Pilot candidates (read-only)\nRECOMMENDED: %d\nPOSSIBLE: %d\nNOT_RECOMMENDED: %d\n", len(set.Recommended), len(set.Possible), len(set.NotRecommended)); err != nil {
		return err
	}
	index := 0
	for _, group := range [][]PilotCandidateReport{set.Recommended, set.Possible} {
		for _, item := range group {
			index++
			if _, err := fmt.Fprintf(out, "\n#%d\nConversation: %s\nCompany: %s\nVacancy: %s\nState: %s\nReply policy: %s\nKnowledge: %s\nDestination: %s\nFreshness: %s\nPilot suitability: %s\nRanking: %s\nReasons: %s\nDraft: %s\n", index, item.ConversationID, item.Company, item.Vacancy, item.Eligibility, item.ReplyRequirement, item.CandidateContextStatus, item.HHDestinationStatus, item.ConversationFreshness, item.Suitability, strings.Join(item.RankingReasons, "; "), strings.Join(item.SuitabilityReasons, "; "), item.ExistingDraftStatus); err != nil {
				return err
			}
		}
	}
	if len(set.NotRecommended) > 0 {
		if _, err := fmt.Fprintln(out, "\nNOT_RECOMMENDED details"); err != nil {
			return err
		}
		for _, item := range set.NotRecommended {
			if _, err := fmt.Fprintf(out, "%s · %s · %s\n", item.Company, item.Vacancy, strings.Join(item.SuitabilityReasons, "; ")); err != nil {
				return err
			}
		}
	}
	return nil
}

func pilotDraftQuality(decision AIResponseDecision, draft *AIDraft, ctx ConversationContext) map[string]any {
	usedFacts := append([]string{}, decision.UsedFacts...)
	if draft != nil && len(usedFacts) == 0 {
		usedFacts = append(usedFacts, draft.UsedFacts...)
	}
	projects := []string{}
	for _, project := range ctx.CandidateContext.RelevantProjects {
		if contextMentions(strings.Join(usedFacts, " "), project.Name) || contextMentions(strings.Join(ctx.ReplyGuidance.MentionedProjects, " "), project.Name) {
			projects = append(projects, project.Name)
		}
	}
	warnings := append([]string{}, decision.Warnings...)
	for _, warning := range ctx.ConsistencyWarnings {
		warnings = append(warnings, warning.Code)
	}
	for _, question := range ctx.UnresolvedQuestions {
		warnings = append(warnings, question.Question)
	}
	source := "AI generated"
	if draft != nil && draft.Source == AIDraftSourceUserEdited {
		source = "User edited"
	}
	return map[string]any{"facts_used": uniqueStrings(usedFacts), "projects_used": uniqueStrings(projects), "conversation_context_used": len(ctx.RecentMessages) > 0, "forbidden_claims_checked": decision.ForbiddenClaimsChecked, "warnings": uniqueStrings(warnings), "source": source}
}

func PilotShowPayload(conversation EmployerConversation, pilot PilotCandidateReport, ctx ConversationContext, ai map[string]any) map[string]any {
	return map[string]any{"pilot": pilot, "conversation": conversation, "context": ctx, "ai": ai}
}

// Keep the JSON encoder available to callers that want a local machine-readable
// report without exposing an alternate persistence path.
func encodePilotReport(value any) ([]byte, error) { return json.Marshal(value) }
