package autochatorchestration

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	autochatattempt "hh-ai-responder/internal/autochatattempt"
	"hh-ai-responder/internal/usecase/autochatreply"
	reliabilitynotifications "hh-ai-responder/internal/usecase/reliabilitynotifications"
)

type Service struct {
	deps Dependencies
	opts Options
}

func NewService(deps Dependencies, options Options) *Service {
	if options.MaxPages <= 0 {
		options.MaxPages = 10
	}
	if options.MaxHistoryMessages <= 0 {
		options.MaxHistoryMessages = 20
	}
	if options.Now == nil {
		options.Now = func() time.Time { return time.Now().UTC() }
	}
	return &Service{deps: deps, opts: options}
}

// Run executes exactly one current AutoRespondChats iteration. Scheduling,
// cadence, and the next run remain caller responsibilities.
func (s *Service) Run(ctx context.Context, _ Input) (Result, error) {
	var result Result
	if s == nil || s.deps.Chats == nil {
		return result, errors.New("auto-chat source is not configured")
	}
	if ctx == nil {
		return result, errors.New("auto-chat context is nil")
	}

	chats, err := s.deps.Chats.AwaitingChats(ctx, s.opts.MaxPages)
	if err != nil {
		return result, fmt.Errorf("load chats error: %v", err)
	}
	result.Seen = len(chats)
	handledTriggers := map[string]struct{}{}
	s.logDebug("total chats to reply: %d", len(chats))
	for _, chat := range chats {
		item := ItemResult{ChatID: chat.ID}
		if chat.IsDiscard {
			if s.opts.Mode != "auto" || s.opts.DryRun || (s.opts.DurableAttempts && !s.opts.WriteEnabled) {
				s.logInfo("REVIEW: would leave discarded chat %d", chat.ID)
				item.Outcome = ItemManualReview
				result.ManualReview++
				result.Items = append(result.Items, item)
				continue
			}
			if s.durableLive() {
				if blocked, gateErr := s.blockedBeforeAI(ctx, chat.ID, chat.TriggerMessageID); gateErr != nil {
					result.Errors++
					item.Outcome, item.Error = ItemFailed, gateErr.Error()
					result.Items = append(result.Items, item)
					continue
				} else if blocked {
					result.Skipped++
					item.Outcome = ItemSkipped
					result.Items = append(result.Items, item)
					continue
				}
			}
			triggerID := strings.TrimSpace(chat.TriggerMessageID)
			if s.durableLive() && triggerID == "" {
				result.Errors++
				item.Outcome, item.Error = ItemFailed, "auto-chat trigger message ID is unavailable; live leave is blocked"
				result.Items = append(result.Items, item)
				continue
			}
			key := autochatattempt.ConflictKey(fmt.Sprint(chat.ID), triggerID)
			if triggerID != "" {
				if _, exists := handledTriggers[key]; exists {
					result.Skipped++
					item.Outcome = ItemSkipped
					result.Items = append(result.Items, item)
					continue
				}
			}
			if s.deps.Actions == nil {
				result.Errors++
				item.Outcome, item.Error = ItemFailed, "auto-chat action executor is not configured"
				result.Items = append(result.Items, item)
				continue
			}
			attempt, reserveErr := s.reserve(ctx, chat.ID, triggerID, autochatattempt.ActionLeave)
			if reserveErr != nil {
				result.Errors++
				item.Outcome, item.Error = ItemFailed, reserveErr.Error()
				result.Items = append(result.Items, item)
				continue
			}
			if attempt.AttemptID != "" {
				handledTriggers[key] = struct{}{}
			}
			actionResult, actionErr := s.executeLeave(ctx, attempt, chat.ID)
			if actionErr == nil && actionResult.Outcome == ActionNoOp && attempt.AttemptID == "" {
				item.Outcome = ItemSkipped
				result.Skipped++
				result.Items = append(result.Items, item)
				continue
			}
			item.Outcome, item.Error = s.finishAction(ctx, attempt, actionResult, actionErr, ItemLeaveExecuted)
			if item.Outcome == ItemLeaveExecuted {
				result.Left++
			} else {
				result.Errors++
			}
			if item.Error != "" {
				s.logWarn("Can't leave discarded chat %d: %s", chat.ID, item.Error)
			}
			result.Items = append(result.Items, item)
			continue
		}

		history, historyErr := s.deps.Chats.ReadHistory(ctx, chat.ID, chat.ApplicantID)
		if historyErr != nil {
			s.logWarn("Can't load messages from chat #%d: %v", chat.ID, historyErr)
			result.Errors++
			item.Outcome, item.Error = ItemFailed, historyErr.Error()
			result.Items = append(result.Items, item)
			continue
		}
		// The legacy safety gate intentionally rejects the boundary case of 20
		// messages, and records it in the in-memory ignore list.
		if !history.WriteAllowed || len(history.Messages) >= s.opts.MaxHistoryMessages {
			s.logDebug("Ignore chat #%d", chat.ID)
			s.ignoreChat(chat.ID, "")
			result.Skipped++
			item.Outcome = ItemSkipped
			result.Items = append(result.Items, item)
			continue
		}
		result.Eligible++
		triggerID := strings.TrimSpace(history.TriggerMessageID)
		if triggerID == "" {
			triggerID = strings.TrimSpace(chat.TriggerMessageID)
		}
		key := autochatattempt.ConflictKey(fmt.Sprint(chat.ID), triggerID)
		if s.durableLive() {
			if blocked, gateErr := s.blockedBeforeAI(ctx, chat.ID, triggerID); gateErr != nil {
				result.Errors++
				item.Outcome, item.Error = ItemFailed, gateErr.Error()
				result.Items = append(result.Items, item)
				continue
			} else if blocked {
				result.Skipped++
				item.Outcome = ItemSkipped
				result.Items = append(result.Items, item)
				continue
			}
			if triggerID == "" {
				result.Errors++
				item.Outcome, item.Error = ItemFailed, "auto-chat trigger message ID is unavailable; live reply is blocked"
				result.Items = append(result.Items, item)
				continue
			}
			if _, exists := handledTriggers[key]; exists {
				result.Skipped++
				item.Outcome = ItemSkipped
				result.Items = append(result.Items, item)
				continue
			}
		}

		if s.deps.ReplyPreparer == nil {
			result.Errors++
			item.Outcome, item.Error = ItemFailed, "auto-chat reply preparer is not configured"
			result.Items = append(result.Items, item)
			continue
		}
		proposal, prepareErr := s.deps.ReplyPreparer.Prepare(ctx, autochatreply.Input{
			ChatID: chat.ID, ContactName: chat.ContactName, EmployerMessage: chat.EmployerMessage,
			VacancyName: chat.VacancyName, VacancyURL: chat.VacancyURL, CompanyName: chat.CompanyName,
			VacancyCompensation: chat.VacancyCompensation, Candidate: chat.Candidate,
			History: history.Messages, Buttons: append([]autochatreply.Button(nil), chat.ReplyOptions...),
			CommunicationProfile: s.opts.CommunicationProfile, GitHubURL: s.opts.GitHubURL,
			Contacts: s.opts.Contacts, ExtraPrompt: s.opts.ExtraPrompt, State: autochatreply.ChatStateActive,
		})
		if prepareErr != nil {
			result.Errors++
			item.Outcome, item.Error = ItemFailed, prepareErr.Error()
			result.Items = append(result.Items, item)
			continue
		}
		result.Generated++
		if proposal.Outcome == autochatreply.OutcomeNoReply || proposal.Outcome == autochatreply.OutcomeLeaveRecommended {
			item.Outcome = ItemNoReply
			result.Items = append(result.Items, item)
			continue
		}

		reviewReason := proposal.ReviewReason
		if reviewReason == "" && s.opts.Mode == "review" {
			reviewReason = "chat mode review"
		}
		if reviewReason == "" && s.opts.DryRun {
			reviewReason = "dry-run"
		}
		if reviewReason == "" && s.opts.DurableAttempts && !s.opts.WriteEnabled {
			reviewReason = "HH writes disabled"
		}
		s.logDebug("Reply prepared for chat #%d (review=%t)", chat.ID, reviewReason != "")
		if reviewReason != "" {
			s.logInfo("REVIEW: would reply in chat %d (%s): %s", chat.ID, reviewReason, proposal.Text)
			s.appendAudit(ctx, AuditEvent{Type: "chat_reply_preview", ChatID: chat.ID, ConversationID: fmt.Sprint(chat.ID), TriggerMessageID: triggerID, EmployerMsg: chat.EmployerMessage, Resume: chat.ResumeHash, ResumeTitle: chat.ResumeTitle, Reply: proposal.Text, ReviewReason: reviewReason, At: s.opts.Now()})
			result.ManualReview++
			item.Outcome = ItemManualReview
			result.Items = append(result.Items, item)
			continue
		}

		attempt, reserveErr := s.reserve(ctx, chat.ID, triggerID, autochatattempt.ActionReply)
		if reserveErr != nil {
			result.Errors++
			item.Outcome, item.Error = ItemFailed, reserveErr.Error()
			result.Items = append(result.Items, item)
			continue
		}
		if attempt.AttemptID != "" {
			handledTriggers[key] = struct{}{}
		}
		actionResult, actionErr := s.executeSend(ctx, attempt, chat.ID, proposal.Text)
		if actionErr == nil && (actionResult.Outcome == ActionAccepted || (actionResult.Outcome == ActionNoOp && attempt.AttemptID == "")) {
			if attempt.AttemptID != "" {
				if recordErr := s.recordOutcome(ctx, attempt, autochatattempt.StateAccepted, actionResult, nil); recordErr != nil {
					result.Errors++
					item.Outcome, item.Error = ItemDeliveryUncertain, recordErr.Error()
					s.appendReplyError(ctx, chat, attempt, autochatattempt.StateSending, proposal.Text, recordErr)
					s.projectOutcome(ctx, reliabilitynotifications.AutoChatOutcome{AttemptID: attempt.AttemptID, ConversationID: attempt.ConversationID, TriggerMessageID: attempt.TriggerMessageID, ActionType: string(attempt.ActionType), State: string(autochatattempt.StateSending), PersistenceUncertain: true})
					result.Items = append(result.Items, item)
					continue
				}
			}
			result.Replied++
			item.Outcome = ItemReplySent
			s.logInfo("Auto-replied in chat %d", chat.ID)
			s.appendAudit(ctx, AuditEvent{Type: "chat_reply", ChatID: chat.ID, AttemptID: attempt.AttemptID, ConversationID: fmt.Sprint(chat.ID), TriggerMessageID: triggerID, State: string(autochatattempt.StateAccepted), Resume: chat.ResumeHash, ResumeTitle: chat.ResumeTitle, EmployerMsg: chat.EmployerMessage, Reply: proposal.Text, At: s.opts.Now()})
			result.Items = append(result.Items, item)
			continue
		}
		if prepareErr == nil {
			prepareErr = errors.New(string(actionResult.Outcome))
		}
		state := actionState(actionResult)
		if attempt.AttemptID != "" {
			if recordErr := s.recordOutcome(ctx, attempt, state, actionResult, prepareErr); recordErr != nil {
				prepareErr = errors.Join(prepareErr, recordErr)
				state = autochatattempt.StateSending // conservative: residue remains blocking
			}
		}
		if state == autochatattempt.StateDeliveryUncertain || state == autochatattempt.StateSending {
			item.Outcome = ItemDeliveryUncertain
		} else {
			item.Outcome = ItemFailed
		}
		item.Error = actionError(actionResult, prepareErr)
		result.Errors++
		s.logError("Failed reply to chat #%d: %v", chat.ID, prepareErr)
		s.appendReplyError(ctx, chat, attempt, state, proposal.Text, prepareErr)
		if attempt.AttemptID != "" {
			s.projectOutcome(ctx, reliabilitynotifications.AutoChatOutcome{AttemptID: attempt.AttemptID, ConversationID: attempt.ConversationID, TriggerMessageID: attempt.TriggerMessageID, ActionType: string(attempt.ActionType), State: string(state), PersistenceUncertain: strings.Contains(prepareErr.Error(), "persist")})
		}
		s.ignoreChat(chat.ID, triggerID)
		result.Items = append(result.Items, item)
	}
	return result, nil
}

func (s *Service) durableLive() bool {
	return s.opts.DurableAttempts && s.opts.Mode == "auto" && !s.opts.DryRun && s.opts.WriteEnabled
}

func (s *Service) blockedBeforeAI(ctx context.Context, chatID int64, triggerID string) (bool, error) {
	if strings.TrimSpace(triggerID) == "" {
		return false, errors.New("auto-chat trigger message ID is unavailable")
	}
	if s.opts.AttemptStoreInitErr != nil {
		s.projectStoreUnavailable(ctx, fmt.Sprint(chatID), s.opts.AttemptStoreInitErr)
		return false, fmt.Errorf("auto-chat attempt authority unavailable: %w", s.opts.AttemptStoreInitErr)
	}
	if s.deps.Attempts == nil {
		s.projectStoreUnavailable(ctx, fmt.Sprint(chatID), errors.New("auto-chat attempt authority is not configured"))
		return false, errors.New("auto-chat attempt authority is not configured")
	}
	attempt, err := s.deps.Attempts.FindBlockingForTrigger(ctx, fmt.Sprint(chatID), triggerID)
	if errors.Is(err, autochatattempt.ErrAttemptNotFound) {
		return false, nil
	}
	if err != nil {
		s.projectStoreUnavailable(ctx, fmt.Sprint(chatID), err)
		return false, fmt.Errorf("read auto-chat attempt authority: %w", err)
	}
	s.projectOutcome(ctx, reliabilitynotifications.AutoChatOutcome{AttemptID: attempt.AttemptID, ConversationID: attempt.ConversationID, TriggerMessageID: attempt.TriggerMessageID, ActionType: string(attempt.ActionType), State: string(attempt.State)})
	return true, nil
}

func (s *Service) reserve(ctx context.Context, chatID int64, triggerID string, actionType autochatattempt.ActionType) (autochatattempt.Attempt, error) {
	if !s.durableLive() {
		return autochatattempt.Attempt{}, nil
	}
	if strings.TrimSpace(triggerID) == "" {
		return autochatattempt.Attempt{}, errors.New("auto-chat trigger message ID is unavailable; reservation is blocked")
	}
	if s.opts.AttemptStoreInitErr != nil {
		return autochatattempt.Attempt{}, fmt.Errorf("auto-chat attempt authority unavailable: %w", s.opts.AttemptStoreInitErr)
	}
	if s.deps.Attempts == nil {
		return autochatattempt.Attempt{}, errors.New("auto-chat attempt authority is not configured")
	}
	if s.deps.Actions == nil {
		return autochatattempt.Attempt{}, errors.New("auto-chat action executor is not configured")
	}
	if _, ok := s.deps.Actions.(ReservedChatActionExecutor); !ok {
		return autochatattempt.Attempt{}, errors.New("auto-chat action executor does not support durable attempts")
	}
	id, err := newAttemptID()
	if err != nil {
		return autochatattempt.Attempt{}, fmt.Errorf("generate auto-chat attempt ID: %w", err)
	}
	now := s.opts.Now().UTC()
	attempt := autochatattempt.Attempt{AttemptID: id, ConversationID: fmt.Sprint(chatID), TriggerMessageID: triggerID, ActionType: actionType, State: autochatattempt.StateSending, CreatedAt: now, UpdatedAt: now}
	if actionType == autochatattempt.ActionReply {
		attempt.RequestKey = id
	}
	reserved, err := s.deps.Attempts.Reserve(ctx, attempt)
	if err != nil {
		if reserved.Existing != nil {
			return autochatattempt.Attempt{}, fmt.Errorf("auto-chat trigger is already reserved by attempt %s", reserved.Existing.AttemptID)
		}
		s.projectStoreUnavailable(ctx, attempt.ConversationID, err)
		return autochatattempt.Attempt{}, fmt.Errorf("reserve auto-chat attempt: %w", err)
	}
	return reserved.Attempt, nil
}

func (s *Service) executeSend(ctx context.Context, attempt autochatattempt.Attempt, chatID int64, text string) (ActionResult, error) {
	if attempt.AttemptID != "" {
		return s.deps.Actions.(ReservedChatActionExecutor).SendChatMessageForAttempt(ctx, ActionRequest{AttemptID: attempt.AttemptID, ConversationID: attempt.ConversationID, RequestKey: attempt.RequestKey}, text)
	}
	return s.deps.Actions.SendChatMessage(ctx, chatID, text)
}

func (s *Service) executeLeave(ctx context.Context, attempt autochatattempt.Attempt, chatID int64) (ActionResult, error) {
	if attempt.AttemptID != "" {
		return s.deps.Actions.(ReservedChatActionExecutor).LeaveChatForAttempt(ctx, ActionRequest{AttemptID: attempt.AttemptID, ConversationID: attempt.ConversationID})
	}
	return s.deps.Actions.LeaveChat(ctx, chatID)
}

func (s *Service) recordOutcome(ctx context.Context, attempt autochatattempt.Attempt, state autochatattempt.State, result ActionResult, actionErr error) error {
	errorClass := ""
	if actionErr != nil {
		errorClass = actionErr.Error()
	} else if result.Error != "" {
		errorClass = result.Error
	}
	return s.deps.Attempts.RecordOutcome(ctx, attempt.AttemptID, state, s.opts.Now().UTC(), result.ProviderID, result.ProviderStatus, errorClass)
}

func (s *Service) finishAction(ctx context.Context, attempt autochatattempt.Attempt, result ActionResult, actionErr error, success ItemOutcome) (ItemOutcome, string) {
	state := actionState(result)
	if attempt.AttemptID != "" {
		if recordErr := s.recordOutcome(ctx, attempt, state, result, actionErr); recordErr != nil {
			s.projectOutcome(ctx, reliabilitynotifications.AutoChatOutcome{AttemptID: attempt.AttemptID, ConversationID: attempt.ConversationID, TriggerMessageID: attempt.TriggerMessageID, ActionType: string(attempt.ActionType), State: string(autochatattempt.StateSending), PersistenceUncertain: true})
			return ItemDeliveryUncertain, errors.Join(actionErr, recordErr).Error()
		}
		s.projectOutcome(ctx, reliabilitynotifications.AutoChatOutcome{AttemptID: attempt.AttemptID, ConversationID: attempt.ConversationID, TriggerMessageID: attempt.TriggerMessageID, ActionType: string(attempt.ActionType), State: string(state)})
	}
	if actionErr == nil && result.Outcome == ActionAccepted {
		return success, ""
	}
	return ItemFailed, actionError(result, actionErr)
}

func (s *Service) projectOutcome(ctx context.Context, event reliabilitynotifications.AutoChatOutcome) {
	if s != nil && s.deps.Notifications != nil {
		_ = s.deps.Notifications.ProjectAutoChatOutcome(ctx, event)
	}
}

func (s *Service) projectStoreUnavailable(ctx context.Context, conversationID string, _ error) {
	if s != nil && s.deps.Notifications != nil {
		_ = s.deps.Notifications.ProjectAutoChatStoreUnavailable(ctx, reliabilitynotifications.AutoChatStoreHealth{ConversationID: conversationID})
	}
}

func actionState(result ActionResult) autochatattempt.State {
	switch result.Outcome {
	case ActionAccepted:
		return autochatattempt.StateAccepted
	case ActionRejected:
		return autochatattempt.StateRejected
	case ActionDeliveryUncertain:
		return autochatattempt.StateDeliveryUncertain
	default:
		return autochatattempt.StateNotSent
	}
}

func newAttemptID() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}

func actionError(result ActionResult, err error) string {
	if err != nil {
		return err.Error()
	}
	if result.Error != "" {
		return result.Error
	}
	return string(result.Outcome)
}

func (s *Service) appendReplyError(ctx context.Context, chat Chat, attempt autochatattempt.Attempt, state autochatattempt.State, reply string, err error) {
	triggerID := attempt.TriggerMessageID
	if triggerID == "" {
		triggerID = chat.TriggerMessageID
	}
	s.appendAudit(ctx, AuditEvent{Type: "chat_reply_error", ChatID: chat.ID, AttemptID: attempt.AttemptID, ConversationID: fmt.Sprint(chat.ID), TriggerMessageID: triggerID, State: string(state), Resume: chat.ResumeHash, ResumeTitle: chat.ResumeTitle, EmployerMsg: chat.EmployerMessage, Reply: reply, Error: err.Error(), At: s.opts.Now()})
}

func (s *Service) ignoreChat(chatID int64, triggerID string) {
	if triggerAware, ok := s.deps.Chats.(interface{ IgnoreTrigger(int64, string) }); ok && strings.TrimSpace(triggerID) != "" {
		triggerAware.IgnoreTrigger(chatID, triggerID)
		return
	}
	s.deps.Chats.IgnoreChat(chatID)
}

func (s *Service) appendAudit(ctx context.Context, event AuditEvent) {
	if s.deps.Audit != nil {
		_ = s.deps.Audit.Append(ctx, event)
	}
}

func (s *Service) logDebug(format string, args ...any) {
	if s.deps.Logger != nil {
		s.deps.Logger.Debug(format, args...)
	}
}

func (s *Service) logInfo(format string, args ...any) {
	if s.deps.Logger != nil {
		s.deps.Logger.Info(format, args...)
	}
}

func (s *Service) logWarn(format string, args ...any) {
	if s.deps.Logger != nil {
		s.deps.Logger.Warn(format, args...)
	}
}

func (s *Service) logError(format string, args ...any) {
	if s.deps.Logger != nil {
		s.deps.Logger.Error(format, args...)
	}
}
