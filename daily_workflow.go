package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// CareerWorkflowState is the small, user-facing state vocabulary for the
// daily Inbox. Stored HH/application lifecycle states remain available in
// Diagnostics, but are deliberately not used as the primary Inbox UX.
type CareerWorkflowState string

const (
	WorkflowNeedsReply         CareerWorkflowState = "NEEDS_REPLY"
	WorkflowNeedsUserAction    CareerWorkflowState = "NEEDS_USER_ACTION"
	WorkflowWaitingForEmployer CareerWorkflowState = "WAITING_FOR_EMPLOYER"
	WorkflowNoReplyNeeded      CareerWorkflowState = "NO_REPLY_NEEDED"
	WorkflowInterview          CareerWorkflowState = "INTERVIEW"
	WorkflowExternalAction     CareerWorkflowState = "EXTERNAL_ACTION"
	WorkflowNeedsClarification CareerWorkflowState = "NEEDS_CLARIFICATION"
	WorkflowTerminal           CareerWorkflowState = "TERMINAL"
)

const dailyWorkflowPromptVersion = "stage24-v1"

type CareerWorkflowProjection struct {
	State                   CareerWorkflowState        `json:"state"`
	Label                   string                     `json:"label"`
	WhatIsHappening         string                     `json:"what_is_happening"`
	WhatToDo                string                     `json:"what_to_do"`
	AIProposal              string                     `json:"ai_proposal,omitempty"`
	Priority                int                        `json:"priority"`
	LastEmployerMessageHash string                     `json:"last_employer_message_hash,omitempty"`
	DraftCacheKey           string                     `json:"draft_cache_key,omitempty"`
	ExternalAction          *ExternalActionRequirement `json:"external_action,omitempty"`
	Waiting                 *WorkflowWaitingDetails    `json:"waiting,omitempty"`
	FollowUp                *WorkflowFollowUpDetails   `json:"follow_up,omitempty"`
	Diagnostics             []string                   `json:"diagnostics,omitempty"`
}

type WorkflowWaitingDetails struct {
	LastCandidateReplyAt *time.Time `json:"last_candidate_reply_at,omitempty"`
	WaitingSince         *time.Time `json:"waiting_since,omitempty"`
	ElapsedSeconds       int64      `json:"elapsed_seconds"`
}

type WorkflowFollowUpDetails struct {
	Eligible                bool       `json:"eligible"`
	RecommendedFollowUpDate *time.Time `json:"recommended_follow_up_date,omitempty"`
	Reason                  string     `json:"reason,omitempty"`
}

type InboxSectionSummary struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Count int    `json:"count"`
}

var inboxSectionOrder = []struct {
	ID     string
	Label  string
	States []CareerWorkflowState
}{
	{ID: "needs_reply", Label: "Нужно ответить", States: []CareerWorkflowState{WorkflowNeedsReply}},
	{ID: "needs_action", Label: "Нужно сделать", States: []CareerWorkflowState{WorkflowNeedsUserAction, WorkflowNeedsClarification}},
	{ID: "waiting", Label: "Ждём работодателя", States: []CareerWorkflowState{WorkflowWaitingForEmployer}},
	{ID: "interviews", Label: "Интервью / тесты", States: []CareerWorkflowState{WorkflowInterview, WorkflowExternalAction}},
	{ID: "no_action", Label: "Не требует действий", States: []CareerWorkflowState{WorkflowNoReplyNeeded, WorkflowTerminal}},
}

func workflowLabel(state CareerWorkflowState) string {
	switch state {
	case WorkflowNeedsReply:
		return "Нужно ответить"
	case WorkflowNeedsUserAction:
		return "Нужно сделать"
	case WorkflowWaitingForEmployer:
		return "Ждём работодателя"
	case WorkflowNoReplyNeeded:
		return "Не требует действий"
	case WorkflowInterview:
		return "Интервью"
	case WorkflowExternalAction:
		return "Внешнее действие"
	case WorkflowNeedsClarification:
		return "Нужно уточнение"
	case WorkflowTerminal:
		return "Завершено"
	default:
		return "Требует проверки"
	}
}

func workflowPriority(state CareerWorkflowState, followUpEligible bool) int {
	switch state {
	case WorkflowInterview, WorkflowExternalAction:
		return 0
	case WorkflowNeedsReply:
		return 1
	case WorkflowNeedsClarification:
		return 2
	case WorkflowWaitingForEmployer:
		if followUpEligible {
			return 3
		}
		return 4
	case WorkflowNoReplyNeeded, WorkflowTerminal:
		return 5
	case WorkflowNeedsUserAction:
		return 2
	default:
		return 5
	}
}

func latestEmployerMessageHash(c EmployerConversation) string {
	message := latestEmployerMessage(c.Messages)
	if message == nil {
		return ""
	}
	raw := message.ID + "\x00" + message.Text + "\x00" + message.Timestamp.UTC().Format(time.RFC3339Nano)
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func workflowDraftCacheKey(messageHash, knowledgeHash string) string {
	sum := sha256.Sum256([]byte(messageHash + "\x00" + knowledgeHash + "\x00" + dailyWorkflowPromptVersion))
	return hex.EncodeToString(sum[:])
}

func employerDraftMetadata(resolver *CandidateContextResolver, context ConversationContext) (messageHash, knowledgeHash, cacheKey string) {
	message := latestEmployerMessage(context.RecentMessages)
	if message != nil {
		raw := message.ID + "\x00" + message.Text + "\x00" + message.Timestamp.UTC().Format(time.RFC3339Nano)
		sum := sha256.Sum256([]byte(raw))
		messageHash = hex.EncodeToString(sum[:])
	}
	knowledgeHash = workflowKnowledgeHash(resolver, context.CandidateContext, context.RelevantKnowledge)
	return messageHash, knowledgeHash, workflowDraftCacheKey(messageHash, knowledgeHash)
}

func workflowKnowledgeHash(resolver *CandidateContextResolver, context CandidateContext, semanticSnapshot RelevantKnowledgeSnapshot) string {
	if len(semanticSnapshot.SemanticSelections) > 0 {
		return RelevantKnowledgeHash(semanticSnapshot)
	}
	if resolver != nil && (resolver.kb != nil || resolver.candidate != nil) {
		if snapshot, err := relevantKnowledgeSnapshotForResolver(resolver, context, "", nil); err == nil {
			return RelevantKnowledgeHash(snapshot)
		}
	}
	raw, _ := json.Marshal(struct {
		Allowed   []string `json:"allowed"`
		Forbidden []string `json:"forbidden"`
	}{context.AllowedFacts, context.ForbiddenClaims})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func workflowActionText(message ConversationMessage, state CareerWorkflowState, external *ExternalActionRequirement) (string, string) {
	if external != nil {
		return "Работодатель пригласил на внешнее действие.", external.RequiredAction
	}
	switch state {
	case WorkflowNeedsReply:
		return "Работодатель ждёт ответ в переписке.", "Проверьте готовый draft и при необходимости отредактируйте его."
	case WorkflowNeedsUserAction:
		return "Работодатель просит выполнить конкретное действие.", userActionInstruction(message.Text)
	case WorkflowNeedsClarification:
		return "Для truthful ответа не хватает подтверждённого факта.", "Ответьте на уточняющий вопрос кандидата; данные пройдут через CandidateKnowledgeUpdater."
	case WorkflowInterview:
		return "Работодатель предложил следующий этап.", "Откройте детали интервью и решите, какое действие выполнить."
	case WorkflowWaitingForEmployer:
		return "Последний шаг был за вами; теперь ждём работодателя.", "Следите за follow-up suggestion и не отправляйте его автоматически."
	case WorkflowNoReplyNeeded:
		return "Последнее сообщение информационное или courtesy-only.", "Сейчас ничего делать не нужно."
	case WorkflowTerminal:
		return "Разговор завершён или вакансия больше недоступна.", "Никаких действий по этому диалогу не требуется."
	default:
		return "Состояние диалога требует проверки.", "Откройте Diagnostics и проверьте локальные данные."
	}
}

func userActionInstruction(text string) string {
	lower := strings.ToLower(text)
	switch {
	case strings.Contains(lower, "напишите именно") || strings.Contains(lower, "ответьте именно"):
		return "Напишите работодателю именно тот ответ, который указан в сообщении. AI не отправляет его за вас."
	case instructionNeedsUserConfirmation(text):
		return userConfirmationQuestion(text)
	case strings.Contains(lower, "заполните") || strings.Contains(lower, "анкет") || strings.Contains(lower, "форм"):
		return "Заполните указанную работодателем форму или анкету."
	case strings.Contains(lower, "выберите дату") || strings.Contains(lower, "выбрать дату") || strings.Contains(lower, "запишитесь"):
		return "Выберите удобную дату и подтвердите её работодателю самостоятельно."
	case strings.Contains(lower, "пройдите тест") || strings.Contains(lower, "выполните тест"):
		return "Пройдите тест; отправка результата остаётся под вашим контролем."
	default:
		return "Откройте сообщение и выполните указанное действие самостоятельно."
	}
}

func classifyCareerWorkflow(a JobApplication, c EmployerConversation, pending []CandidateClarificationRequest, now time.Time, followUp *FollowUpCandidate, resolver *CandidateContextResolver) CareerWorkflowProjection {
	projection := CareerWorkflowProjection{Label: "Требует проверки", Diagnostics: []string{}}
	latest := latestDeliveredMessage(c)
	messageHash := latestEmployerMessageHash(c)
	projection.LastEmployerMessageHash = messageHash
	if now.IsZero() {
		now = time.Now().UTC()
	}

	state := (ConversationStateResolver{}).Resolve(a, c, nil, false, nil, now)
	if len(state.Warnings) > 0 {
		projection.Diagnostics = append(projection.Diagnostics, state.Warnings...)
	}
	if terminal, ok := terminalConversationState(c); ok || state.Status == ConversationRejected || state.Status == ConversationClosed || state.Status == ConversationOffer {
		_ = terminal
		projection.State = WorkflowTerminal
	} else {
		for _, q := range pending {
			conversationMatch := q.ConversationID == c.ID
			applicationMatch := q.ConversationID == "" && q.ApplicationID != "" && a.ID != "" && q.ApplicationID == a.ID
			if q.Status == ClarificationPending && (conversationMatch || applicationMatch) {
				projection.State = WorkflowNeedsClarification
				break
			}
		}
		if projection.State == "" && len(c.Summary.PendingQuestions) > 0 {
			projection.State = WorkflowNeedsClarification
		}
		if latest != nil && latest.Sender == ConversationSenderEmployer {
			intent := classifyEmployerMessage(latest.Text)
			external := externalInterviewAction(latest.Text, latest.Timestamp, now)
			if external != nil {
				projection.State, projection.ExternalAction = WorkflowExternalAction, external
			} else if projection.State == "" && (c.Status == ConversationInterview || state.Status == ConversationInterview || intent == EmployerMessageIntentInterviewInvitation) {
				projection.State = WorkflowInterview
			} else if projection.State == "" && intent == EmployerMessageIntentInstruction {
				projection.State = WorkflowNeedsUserAction
			} else if projection.State == "" && conversationReplyRequirement(c, latest, intent) == ReplyRequired {
				projection.State = WorkflowNeedsReply
			} else if projection.State == "" && (intent == EmployerMessageIntentAcknowledgement || intent == EmployerMessageIntentStatusMessage || intent == EmployerMessageIntentGeneralMessage) {
				projection.State = WorkflowNoReplyNeeded
			}
		}
		if projection.State == "" && (state.Status == ConversationWaitingEmployer || latest != nil && latest.Sender == ConversationSenderCandidate || a.Status == ApplicationApplied) {
			projection.State = WorkflowWaitingForEmployer
		}
		if projection.State == "" {
			projection.State = WorkflowNoReplyNeeded
		}
	}

	if latest != nil && latest.Sender == ConversationSenderEmployer {
		projection.AIProposal = "AI подготовит draft только на основе подтверждённых данных."
	}
	if projection.State == WorkflowNeedsUserAction {
		projection.AIProposal = "AI не подтверждает действие за вас."
	}
	if projection.State == WorkflowNeedsClarification {
		projection.AIProposal = "AI не придумывает ответ; сначала нужно уточнение."
	}
	if projection.State == WorkflowWaitingForEmployer {
		waitingSince := state.WaitingSince
		if waitingSince == nil {
			waitingSince = c.WaitingSince
		}
		projection.Waiting = &WorkflowWaitingDetails{LastCandidateReplyAt: c.LastCandidateMessageAt, WaitingSince: waitingSince}
		if waitingSince != nil && !waitingSince.After(now) {
			projection.Waiting.ElapsedSeconds = int64(now.Sub(*waitingSince).Seconds())
		}
	}
	if followUp != nil {
		projection.FollowUp = &WorkflowFollowUpDetails{Eligible: followUp.Status == FollowUpEligible, RecommendedFollowUpDate: followUp.EligibleAt, Reason: followUp.Reason}
	}
	projection.Label = workflowLabel(projection.State)
	projection.Priority = workflowPriority(projection.State, projection.FollowUp != nil && projection.FollowUp.Eligible)
	if latest != nil {
		projection.WhatIsHappening, projection.WhatToDo = workflowActionText(*latest, projection.State, projection.ExternalAction)
	} else {
		projection.WhatIsHappening, projection.WhatToDo = workflowActionText(ConversationMessage{}, projection.State, projection.ExternalAction)
	}
	return projection
}

func workflowSectionFor(state CareerWorkflowState) InboxSectionSummary {
	for _, section := range inboxSectionOrder {
		for _, candidate := range section.States {
			if candidate == state {
				return InboxSectionSummary{ID: section.ID, Label: section.Label}
			}
		}
	}
	return InboxSectionSummary{ID: "no_action", Label: "Не требует действий"}
}

func buildInboxSections(items []CandidateInboxItem) []InboxSectionSummary {
	counts := map[string]int{}
	for _, item := range items {
		counts[workflowSectionFor(item.Workflow.State).ID]++
	}
	sections := make([]InboxSectionSummary, 0, len(inboxSectionOrder))
	for _, section := range inboxSectionOrder {
		sections = append(sections, InboxSectionSummary{ID: section.ID, Label: section.Label, Count: counts[section.ID]})
	}
	return sections
}

func sortWorkflowInbox(items []CandidateInboxItem) {
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Workflow.Priority != items[j].Workflow.Priority {
			return items[i].Workflow.Priority < items[j].Workflow.Priority
		}
		left, right := conversationActivity(items[i].Conversation), conversationActivity(items[j].Conversation)
		if !left.Equal(right) {
			return left.After(right)
		}
		return items[i].Conversation.ID < items[j].Conversation.ID
	})
}

func workflowExampleText(item CandidateInboxItem) string {
	message := ""
	if item.LatestMessage != nil {
		message = strings.Join(strings.Fields(item.LatestMessage.Text), " ")
		if len([]rune(message)) > 100 {
			message = string([]rune(message)[:100]) + "…"
		}
	}
	return fmt.Sprintf("%s — %s — %s", firstNonEmpty(item.Conversation.CompanyName, "Компания не указана"), firstNonEmpty(item.Conversation.VacancyTitle, "Вакансия не указана"), message)
}

type DailyWorkflowReport struct {
	GeneratedAt       time.Time                        `json:"generated_at"`
	ConversationCount int                              `json:"conversation_count"`
	ImportantCount    int                              `json:"important_count"`
	Distribution      map[CareerWorkflowState]int      `json:"distribution"`
	Sections          []InboxSectionSummary            `json:"sections"`
	Examples          map[CareerWorkflowState][]string `json:"examples"`
	FollowUpEligible  int                              `json:"follow_up_eligible"`
}

func BuildDailyWorkflowReport(snapshot CareerSnapshot, resolver *CandidateContextResolver, policy FollowUpPolicy, now time.Time) DailyWorkflowReport {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	applications := map[string]JobApplication{}
	for _, application := range snapshot.Applications {
		applications[application.ConversationID] = application
	}
	followUps := map[string]FollowUpCandidate{}
	if policy == (FollowUpPolicy{}) {
		policy = DefaultFollowUpPolicy()
	}
	for _, followUp := range snapshot.FollowUps(policy, now) {
		followUps[followUp.ConversationID] = followUp
	}
	pending := snapshot.Clarifications
	report := DailyWorkflowReport{GeneratedAt: now, ConversationCount: len(snapshot.Conversations), Distribution: map[CareerWorkflowState]int{}, Examples: map[CareerWorkflowState][]string{}}
	items := make([]CandidateInboxItem, 0, len(snapshot.Conversations))
	for _, conversation := range snapshot.Conversations {
		followUp, ok := followUps[conversation.ID]
		if !ok {
			followUp = FollowUpCandidate{ConversationID: conversation.ID, Status: FollowUpNotEligible}
		}
		item := CandidateInboxItem{Conversation: conversation}
		if timeline := deliveredMessages(conversation.Messages); len(timeline) > 0 {
			latest := timeline[len(timeline)-1]
			item.LatestMessage = &latest
		}
		for _, clarification := range pending {
			if clarification.Status == ClarificationPending && clarification.ConversationID == conversation.ID {
				item.PendingClarifications = append(item.PendingClarifications, clarification)
			}
		}
		item.Workflow = classifyCareerWorkflow(applications[conversation.ID], conversation, item.PendingClarifications, now, &followUp, resolver)
		items = append(items, item)
		report.Distribution[item.Workflow.State]++
		if item.Workflow.State != WorkflowNoReplyNeeded && item.Workflow.State != WorkflowTerminal {
			report.ImportantCount++
		}
		if item.Workflow.State == WorkflowWaitingForEmployer && followUp.Status == FollowUpEligible {
			report.FollowUpEligible++
		}
		if len(report.Examples[item.Workflow.State]) < 5 {
			report.Examples[item.Workflow.State] = append(report.Examples[item.Workflow.State], workflowExampleText(item))
		}
	}
	report.Sections = buildInboxSections(items)
	return report
}

func WriteDailyWorkflowReport(w interface{ Write([]byte) (int, error) }, report DailyWorkflowReport) error {
	order := []CareerWorkflowState{WorkflowNeedsReply, WorkflowNeedsUserAction, WorkflowWaitingForEmployer, WorkflowInterview, WorkflowExternalAction, WorkflowNeedsClarification, WorkflowNoReplyNeeded, WorkflowTerminal}
	if _, err := fmt.Fprintf(w, "DAILY WORKFLOW\n\nConversations: %d\nImportant: %d\nFollow-up eligible: %d\n\nDistribution:\n", report.ConversationCount, report.ImportantCount, report.FollowUpEligible); err != nil {
		return err
	}
	for _, state := range order {
		if _, err := fmt.Fprintf(w, "%s: %d\n", state, report.Distribution[state]); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(w, "\nRepresentative examples (up to 5 each):"); err != nil {
		return err
	}
	for _, state := range order {
		if _, err := fmt.Fprintf(w, "\n%s:\n", state); err != nil {
			return err
		}
		for _, example := range report.Examples[state] {
			if _, err := fmt.Fprintf(w, "- %s\n", example); err != nil {
				return err
			}
		}
	}
	return nil
}
