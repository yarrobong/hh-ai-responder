package runtime

import (
	"context"
	"errors"
	"strconv"
	"strings"

	llmport "hh-ai-responder/internal/ports/llm"
	autochatorchestration "hh-ai-responder/internal/usecase/autochatorchestration"
	"hh-ai-responder/internal/usecase/autochatreply"
	hhwritegateway "hh-ai-responder/internal/usecase/hhwritegateway"
)

// legacyAutoChatSource adapts the existing read-only HH façade and preserves
// the old selection/filtering/resource rules. It exposes only detached values
// to the extracted iteration service.
type legacyAutoChatSource struct{ responder *HHAIResponder }

func (s legacyAutoChatSource) AwaitingChats(ctx context.Context, maxPages int) ([]autochatorchestration.Chat, error) {
	legacy, err := s.responder.getChatsAwaitingReply(ctx, maxPages)
	if err != nil {
		return nil, err
	}
	result := make([]autochatorchestration.Chat, 0, len(legacy))
	for _, chat := range legacy {
		options := make([]autochatreply.Button, 0, len(chat.ReplyOptions))
		for _, option := range chat.ReplyOptions {
			options = append(options, autochatreply.Button{Text: option})
		}
		result = append(result, autochatorchestration.Chat{
			ID: chat.ChatId, TriggerMessageID: chat.TriggerMessageID, ContactName: chat.ContactName, EmployerMessage: chat.ReplyToMessage,
			VacancyName: chat.VacancyName, VacancyURL: chat.VacancyURL, CompanyName: chat.CompanyName,
			VacancyCompensation: chat.VacancyCompensation, ReplyOptions: options,
			ResumeHash: chat.ResumeHash, ResumeTitle: chat.ResumeTitle, ApplicantID: chat.ApplicantId,
			Candidate: autochatreply.Candidate{
				FirstName: chat.FirstName, LastName: chat.LastName, ResumeTitle: chat.ResumeTitle,
				Salary: chat.Salary, Skills: chat.Skills, Experience: chat.ResumeExperience,
				AlwaysEmphasize: chat.AlwaysEmphasize, AvoidClaiming: chat.AvoidClaiming,
				Context: chat.CandidateContext,
			},
			IsDiscard: chat.IsDiscard,
		})
	}
	return result, nil
}

func (s legacyAutoChatSource) ReadHistory(ctx context.Context, chatID, applicantID int64) (autochatorchestration.History, error) {
	data, err := s.responder.getChatDataThroughReadAdapterContext(ctx, chatID, applicantID)
	if err != nil {
		return autochatorchestration.History{}, err
	}
	history := autochatorchestration.History{WriteAllowed: data.ChatStates.WriteMessageState.Allowed, Messages: make([]autochatreply.HistoryMessage, 0, len(data.Chat.Messages.Items))}
	for _, message := range data.Chat.Messages.Items {
		messageID := ""
		if message.ID > 0 {
			messageID = strconv.FormatInt(message.ID, 10)
		}
		history.Messages = append(history.Messages, autochatreply.HistoryMessage{ID: messageID, Timestamp: message.CreationTime, Author: message.ParticipantDisplay.Name, Text: message.Text})
	}
	if len(data.Chat.Messages.Items) > 0 {
		last := data.Chat.Messages.Items[len(data.Chat.Messages.Items)-1]
		if last.ID > 0 && strings.TrimSpace(last.ParticipantID) != strconv.FormatInt(applicantID, 10) {
			history.TriggerMessageID = strconv.FormatInt(last.ID, 10)
		}
	}
	return history, nil
}

func (s legacyAutoChatSource) IgnoreChat(chatID int64) {
	if s.responder != nil {
		s.responder.ignoredChats = append(s.responder.ignoredChats, chatID)
	}
}

func (s legacyAutoChatSource) IgnoreTrigger(chatID int64, triggerID string) {
	if s.responder != nil {
		s.responder.ignoreChatTrigger(chatID, triggerID)
	}
}

type legacyAutoChatActions struct{ responder *HHAIResponder }

func (a legacyAutoChatActions) SendChatMessage(ctx context.Context, chatID int64, text string) (autochatorchestration.ActionResult, error) {
	if a.responder == nil || !a.responder.chatSendingAllowed() {
		return autochatorchestration.ActionResult{Outcome: autochatorchestration.ActionNoOp}, nil
	}
	key, err := generateUUIDv4()
	if err != nil {
		return autochatorchestration.ActionResult{Outcome: autochatorchestration.ActionNotSent, Error: err.Error()}, err
	}
	service, err := a.responder.newLegacyWriteService()
	if err != nil {
		return autochatorchestration.ActionResult{Outcome: autochatorchestration.ActionNotSent, Error: err.Error()}, err
	}
	result, err := service.SendChatMessage(ctxOrBackground(ctx), hhwritegateway.ChatMessageRequest{ConversationID: strconv.FormatInt(chatID, 10), Text: text, IdempotencyKey: key})
	return autoChatActionResult(result, err)
}

func (a legacyAutoChatActions) SendChatMessageForAttempt(ctx context.Context, request autochatorchestration.ActionRequest, text string) (autochatorchestration.ActionResult, error) {
	if a.responder == nil || !a.responder.chatSendingAllowed() {
		return autochatorchestration.ActionResult{Outcome: autochatorchestration.ActionNoOp}, nil
	}
	if strings.TrimSpace(request.RequestKey) == "" {
		return autochatorchestration.ActionResult{Outcome: autochatorchestration.ActionNotSent, Error: "durable auto-chat reply request key is missing"}, errors.New("durable auto-chat reply request key is missing")
	}
	service, err := a.responder.newLegacyWriteService()
	if err != nil {
		return autochatorchestration.ActionResult{Outcome: autochatorchestration.ActionNotSent, Error: err.Error()}, err
	}
	result, err := service.SendChatMessage(ctxOrBackground(ctx), hhwritegateway.ChatMessageRequest{ConversationID: request.ConversationID, Text: text, IdempotencyKey: request.RequestKey})
	return autoChatActionResult(result, err)
}

func (a legacyAutoChatActions) LeaveChat(ctx context.Context, chatID int64) (autochatorchestration.ActionResult, error) {
	if a.responder == nil || !a.responder.chatSendingAllowed() {
		return autochatorchestration.ActionResult{Outcome: autochatorchestration.ActionNoOp}, nil
	}
	service, err := a.responder.newLegacyWriteService()
	if err != nil {
		return autochatorchestration.ActionResult{Outcome: autochatorchestration.ActionNotSent, Error: err.Error()}, err
	}
	result, err := service.LeaveChat(ctxOrBackground(ctx), hhwritegateway.LeaveChatRequest{ConversationID: strconv.FormatInt(chatID, 10)})
	return autoChatActionResult(result, err)
}

func (a legacyAutoChatActions) LeaveChatForAttempt(ctx context.Context, request autochatorchestration.ActionRequest) (autochatorchestration.ActionResult, error) {
	if a.responder == nil || !a.responder.chatSendingAllowed() {
		return autochatorchestration.ActionResult{Outcome: autochatorchestration.ActionNoOp}, nil
	}
	service, err := a.responder.newLegacyWriteService()
	if err != nil {
		return autochatorchestration.ActionResult{Outcome: autochatorchestration.ActionNotSent, Error: err.Error()}, err
	}
	result, err := service.LeaveChat(ctxOrBackground(ctx), hhwritegateway.LeaveChatRequest{ConversationID: request.ConversationID})
	return autoChatActionResult(result, err)
}

func autoChatActionResult(result hhwritegateway.Result, err error) (autochatorchestration.ActionResult, error) {
	outcome := autochatorchestration.ActionNotSent
	switch result.Outcome {
	case hhwritegateway.OutcomeAccepted:
		outcome = autochatorchestration.ActionAccepted
	case hhwritegateway.OutcomeRejected:
		outcome = autochatorchestration.ActionRejected
	case hhwritegateway.OutcomeDeliveryUncertain:
		outcome = autochatorchestration.ActionDeliveryUncertain
	case hhwritegateway.OutcomeBlocked:
		outcome = autochatorchestration.ActionBlocked
	}
	if err != nil {
		return autochatorchestration.ActionResult{Outcome: outcome, ProviderID: result.ProviderID, ProviderStatus: result.ProviderStatus, Metadata: result.Metadata, Error: err.Error()}, hhWriteErrorFromPort(err)
	}
	return autochatorchestration.ActionResult{Outcome: outcome, ProviderID: result.ProviderID, ProviderStatus: result.ProviderStatus, Metadata: result.Metadata, Error: strings.TrimSpace(result.Error)}, nil
}

type legacyAutoChatAudit struct{ responder *HHAIResponder }

func (a legacyAutoChatAudit) Append(_ context.Context, event autochatorchestration.AuditEvent) error {
	if a.responder == nil {
		return errors.New("auto-chat audit responder is not configured")
	}
	switch event.Type {
	case "chat_reply_preview", "chat_reply":
		a.responder.writeEvent(ChatResult{Type: event.Type, Resume: event.Resume, ResumeTitle: event.ResumeTitle, ChatId: event.ChatID, AttemptID: event.AttemptID, ConversationID: event.ConversationID, TriggerMessageID: event.TriggerMessageID, EmployerMsg: event.EmployerMsg, Reply: event.Reply, ReviewReason: event.ReviewReason, SentAt: event.At})
	case "chat_reply_error":
		a.responder.writeEvent(ErrorResult{Type: event.Type, Context: map[string]any{"chat_id": event.ChatID, "resume": event.Resume, "resume_title": event.ResumeTitle, "attempt_id": event.AttemptID, "conversation_id": event.ConversationID, "trigger_message_id": event.TriggerMessageID, "state": event.State}, Error: event.Error, Time: event.At})
	}
	return nil
}

func (r *HHAIResponder) newAutoChatOrchestrationService() *autochatorchestration.Service {
	var completion llmport.CompletionProvider
	model := ""
	if r != nil && r.ai != nil {
		completion = r.ai
		model = r.ai.ModelName()
	}
	return autochatorchestration.NewService(autochatorchestration.Dependencies{
		Chats:         legacyAutoChatSource{responder: r},
		ReplyPreparer: autochatreply.NewService(autochatreply.Dependencies{Completion: completion}, autochatreply.Options{Model: model, MaxTokens: 512, Temperature: 0.5}),
		Actions:       legacyAutoChatActions{responder: r},
		Audit:         legacyAutoChatAudit{responder: r},
		Attempts:      r.autoChatAttempts,
		Notifications: r.reliabilityNotifications,
		Logger:        logger,
	}, autochatorchestration.Options{
		Mode: r.effectiveChatMode(), DryRun: r.dryRun, MaxPages: 10, MaxHistoryMessages: 20,
		CommunicationProfile: candidateCommunicationProfile, GitHubURL: r.githubURL,
		Contacts: r.contacts, ExtraPrompt: r.extraChatReplyPrompt, DurableAttempts: true, AttemptStoreInitErr: r.autoChatAttemptStoreInitErr, WriteEnabled: r.hhWriteEnabled && r.chatSendingAllowed(),
	})
}
