package hhwritegateway

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"hh-ai-responder/internal/ports/hhwrite"
)

type Service struct {
	deps            Dependencies
	opts            Options
	mu              sync.Mutex
	actionMu        sync.Mutex
	attemptsThisRun int
}

func NewService(deps Dependencies, opts Options) *Service {
	if opts.Now == nil {
		opts.Now = func() time.Time { return time.Now().UTC() }
	}
	return &Service{deps: deps, opts: opts}
}

// Configure updates runtime policy supplied by composition while retaining
// the service's per-run attempt counter. This is useful for compatibility
// facades whose public options are intentionally mutable in tests and UI code.
func (s *Service) Configure(opts Options) {
	if s == nil {
		return
	}
	if opts.Now == nil {
		opts.Now = s.opts.Now
	}
	s.mu.Lock()
	s.opts.WriteEnabled, s.opts.DryRun = opts.WriteEnabled, opts.DryRun
	s.opts.MaxWritesPerRun, s.opts.MaxWritesPerDay = opts.MaxWritesPerRun, opts.MaxWritesPerDay
	s.opts.Now = opts.Now
	s.mu.Unlock()
}

func (s *Service) SendChatMessage(ctx context.Context, request ChatMessageRequest) (Result, error) {
	if s == nil {
		return Result{Operation: "chat_message"}, ErrCapabilityUnavailable
	}
	if strings.TrimSpace(request.ActionID) == "" {
		return s.invoke(ctx, "chat_message", func(ctx context.Context) (hhwrite.WriteResult, error) {
			if strings.TrimSpace(request.ConversationID) == "" || strings.TrimSpace(request.Text) == "" || strings.TrimSpace(request.IdempotencyKey) == "" {
				return hhwrite.WriteResult{Outcome: hhwrite.OutcomeNotSent}, errors.New("HH chat message request is invalid")
			}
			if s.deps.ChatMessageWriter == nil {
				return hhwrite.WriteResult{Outcome: hhwrite.OutcomeNotSent}, ErrCapabilityUnavailable
			}
			return s.deps.ChatMessageWriter.SendChatMessage(ctx, hhwrite.ChatMessageRequest{ConversationID: request.ConversationID, Text: request.Text, IdempotencyKey: request.IdempotencyKey})
		})
	}

	if s.deps.Actions == nil {
		return Result{ActionID: request.ActionID, Operation: "chat_message", Timestamp: s.now()}, ErrCapabilityUnavailable
	}
	action, err := s.deps.Actions.Load(ctx, request.ActionID)
	if err != nil {
		return Result{ActionID: request.ActionID, Operation: "chat_message", Timestamp: s.now(), Error: err.Error()}, fmt.Errorf("%w: %v", ErrActionNotFound, err)
	}
	result := Result{ActionID: action.ID, Operation: "chat_message", Status: action.Status, Timestamp: s.now()}
	if terminalAction(action.Status) {
		result.Outcome = terminalOutcome(action.Status)
		result.ProviderID = action.ProviderID
		result.Timestamp = deref(action.AttemptedAt, action.UpdatedAt, result.Timestamp)
		result.NeedsReconciliation = action.Status == ActionDeliveryUncertain || action.Status == ActionManualReview || action.Status == ActionSentUnconfirmed
		if result.Outcome == OutcomeAccepted {
			return result, nil
		}
		result.Error = "HH write action cannot be sent again"
		return result, ErrActionNotSendable
	}
	if action.Status != ActionApproved {
		result.Outcome, result.Error = OutcomeBlocked, "HH write action is not approved"
		return result, ErrActionNotSendable
	}
	if err := s.policy(); err != nil {
		result.Outcome = policyOutcome(err)
		result.Error = err.Error()
		return result, err
	}
	if strings.TrimSpace(action.Nonce) == "" {
		result.Error = ErrMissingNonce.Error()
		return result, ErrMissingNonce
	}
	if err := ctxErr(ctx); err != nil {
		result.Outcome, result.Error = OutcomeNotSent, err.Error()
		return result, err
	}
	// Serialize durable action reservations within one service instance. The
	// storage operation remains the authoritative cross-process guard, while
	// this lock also keeps per-run limits correct for concurrent callers.
	s.actionMu.Lock()
	defer s.actionMu.Unlock()
	if err := s.checkLimits(ctx); err != nil {
		result.Outcome, result.Error = OutcomeBlocked, err.Error()
		return result, err
	}

	now := s.now()
	reserved, err := s.deps.Actions.Reserve(ctx, action.ID, now)
	if err != nil {
		result.Error = err.Error()
		return result, fmt.Errorf("%w: %v", ErrPreTransportPersistence, err)
	}
	result.Status = reserved.Status
	if err := s.audit(ctx, AuditEvent{ActionID: action.ID, Operation: "chat_message", ConversationID: action.ConversationID, Type: "send_started", Result: string(ActionSending), CreatedAt: now}); err != nil {
		_ = s.deps.Actions.Record(ctx, action.ID, ActionRecord{Status: ActionManualReview, Timestamp: s.now(), Error: "write audit unavailable; HH send was not attempted"})
		result.Status, result.Outcome, result.Error = ActionManualReview, OutcomeBlocked, err.Error()
		return result, fmt.Errorf("%w: %v", ErrPreTransportPersistence, err)
	}
	s.countAttempt()
	result.TransportAttempted = true
	if s.deps.ChatMessageWriter == nil {
		writeResult := hhwrite.WriteResult{Outcome: hhwrite.OutcomeNotSent}
		return s.finishAction(ctx, reserved, "chat_message", writeResult, ErrCapabilityUnavailable, result)
	}
	writeResult, writeErr := s.deps.ChatMessageWriter.SendChatMessage(ctx, hhwrite.ChatMessageRequest{ConversationID: reserved.ConversationID, Text: reserved.Text, IdempotencyKey: reserved.Nonce})
	return s.finishAction(ctx, reserved, "chat_message", writeResult, writeErr, result)
}

func (s *Service) LeaveChat(ctx context.Context, request LeaveChatRequest) (Result, error) {
	return s.invoke(ctx, "chat_leave", func(ctx context.Context) (hhwrite.WriteResult, error) {
		if strings.TrimSpace(request.ConversationID) == "" {
			return hhwrite.WriteResult{Outcome: hhwrite.OutcomeNotSent}, errors.New("HH chat leave request is invalid")
		}
		if s.deps.ChatLeaveWriter == nil {
			return hhwrite.WriteResult{Outcome: hhwrite.OutcomeNotSent}, ErrCapabilityUnavailable
		}
		return s.deps.ChatLeaveWriter.LeaveChat(ctx, request)
	})
}

func (s *Service) SubmitVacancyResponse(ctx context.Context, request VacancyResponseRequest) (Result, error) {
	return s.invoke(ctx, "vacancy_response", func(ctx context.Context) (hhwrite.WriteResult, error) {
		if s.deps.VacancyResponseWriter == nil {
			return hhwrite.WriteResult{Outcome: hhwrite.OutcomeNotSent}, ErrCapabilityUnavailable
		}
		return s.deps.VacancyResponseWriter.SubmitVacancyResponse(ctx, request)
	})
}

func (s *Service) TouchResume(ctx context.Context, request ResumeTouchRequest) (Result, error) {
	return s.invoke(ctx, "resume_touch", func(ctx context.Context) (hhwrite.WriteResult, error) {
		if s.deps.ResumeWriter == nil {
			return hhwrite.WriteResult{Outcome: hhwrite.OutcomeNotSent}, ErrCapabilityUnavailable
		}
		return s.deps.ResumeWriter.TouchResume(ctx, request)
	})
}

func (s *Service) SetJobSearchStatus(ctx context.Context, request JobSearchStatusRequest) (Result, error) {
	return s.invoke(ctx, "job_search_status", func(ctx context.Context) (hhwrite.WriteResult, error) {
		if s.deps.JobSearchStatusWriter == nil {
			return hhwrite.WriteResult{Outcome: hhwrite.OutcomeNotSent}, ErrCapabilityUnavailable
		}
		return s.deps.JobSearchStatusWriter.SetJobSearchStatus(ctx, request)
	})
}

func (s *Service) invoke(ctx context.Context, operation string, call func(context.Context) (hhwrite.WriteResult, error)) (Result, error) {
	result := Result{Operation: operation, Timestamp: s.now()}
	if err := s.policy(); err != nil {
		result.Outcome, result.Error = policyOutcome(err), err.Error()
		return result, err
	}
	if err := ctxErr(ctx); err != nil {
		result.Outcome, result.Error = OutcomeNotSent, err.Error()
		return result, err
	}
	if err := s.checkLimits(ctx); err != nil {
		result.Outcome, result.Error = OutcomeBlocked, err.Error()
		return result, err
	}
	s.countAttempt()
	result.TransportAttempted = true
	writeResult, err := call(ctx)
	result = mapResult(result, writeResult)
	result.Outcome = gatewayOutcome(writeResult.Outcome)
	result.NeedsReconciliation = result.Outcome == OutcomeDeliveryUncertain
	result.Error = errorText(err)
	return result, err
}

func (s *Service) finishAction(ctx context.Context, action Action, operation string, writeResult hhwrite.WriteResult, writeErr error, result Result) (Result, error) {
	result = mapResult(result, writeResult)
	if s.deps.ChatMessageWriter == nil {
		writeErr = ErrCapabilityUnavailable
		writeResult.Outcome = hhwrite.OutcomeNotSent
	}
	status := statusForOutcome(writeResult.Outcome)
	if writeResult.Outcome == "" {
		if writeErr != nil {
			writeResult.Outcome = hhwrite.OutcomeAmbiguous
			status = ActionDeliveryUncertain
			result = mapResult(result, writeResult)
		} else {
			writeResult.Outcome = hhwrite.OutcomeAccepted
			status = ActionSent
			result = mapResult(result, writeResult)
		}
	}
	recordErr := s.deps.Actions.Record(ctx, action.ID, ActionRecord{Outcome: writeResult.Outcome, Status: status, ProviderID: writeResult.ProviderID, Timestamp: timestamp(writeResult.Timestamp, s.now()), Error: errorText(writeErr)})
	evidence := transportEvidence(writeErr)
	auditErr := s.audit(ctx, AuditEvent{ActionID: action.ID, Operation: operation, ConversationID: action.ConversationID, Type: "transport_response", Result: string(writeResult.Outcome), ProviderID: writeResult.ProviderID, ProviderStatus: writeResult.ProviderStatus, Metadata: writeResult.Metadata, ResponseContentType: evidence.contentType, ResponseBody: evidence.body, ErrorFields: evidence.fields, CorrelationIDs: evidence.correlationIDs, Error: errorText(writeErr), CreatedAt: s.now()})
	if writeResult.Outcome == hhwrite.OutcomeAmbiguous {
		result.Outcome, result.Status, result.NeedsReconciliation = OutcomeDeliveryUncertain, ActionDeliveryUncertain, true
	} else if writeResult.Outcome == hhwrite.OutcomeAccepted {
		result.Outcome, result.Status = OutcomeAccepted, ActionSent
	} else if writeResult.Outcome == hhwrite.OutcomeRejected {
		result.Outcome, result.Status = OutcomeRejected, ActionFailed
	} else {
		result.Outcome, result.Status = OutcomeNotSent, ActionFailed
	}
	terminalType := string(result.Status)
	if writeResult.Outcome == hhwrite.OutcomeAccepted {
		// The root composition layer records the later local projection and
		// delivery-confirmation events. "accepted" is only the transport fact.
		terminalType = "accepted"
	}
	terminalAuditErr := s.audit(ctx, AuditEvent{ActionID: action.ID, Operation: operation, ConversationID: action.ConversationID, Type: terminalType, Result: string(result.Outcome), ProviderID: writeResult.ProviderID, Error: errorText(writeErr), CreatedAt: s.now()})
	if joined := errors.Join(recordErr, auditErr, terminalAuditErr); joined != nil {
		result.Outcome, result.Status, result.NeedsReconciliation = OutcomePersistenceUncertain, ActionManualReview, true
		result.Error = joined.Error()
		return result, errors.Join(writeErr, ErrPostTransportPersistence, joined)
	}
	result.Error = errorText(writeErr)
	return result, writeErr
}

func (s *Service) policy() error {
	if s == nil {
		return ErrCapabilityUnavailable
	}
	if s.opts.DryRun {
		return ErrDryRun
	}
	if !s.opts.WriteEnabled {
		return ErrWriteDisabled
	}
	return nil
}

func (s *Service) checkLimits(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.opts.MaxWritesPerRun > 0 && s.attemptsThisRun >= s.opts.MaxWritesPerRun {
		return errors.New("HH write run limit reached")
	}
	if s.opts.MaxWritesPerDay > 0 {
		if counter, ok := s.deps.Audit.(AttemptCounter); ok {
			count, err := counter.AttemptsSince(ctx, s.now().UTC().Truncate(24*time.Hour))
			if err != nil {
				return fmt.Errorf("HH write daily limit unavailable: %w", err)
			}
			if count >= s.opts.MaxWritesPerDay {
				return errors.New("HH write daily limit reached")
			}
		}
	}
	return nil
}

func (s *Service) countAttempt() { s.mu.Lock(); s.attemptsThisRun++; s.mu.Unlock() }
func (s *Service) now() time.Time {
	if s == nil || s.opts.Now == nil {
		return time.Now().UTC()
	}
	return s.opts.Now().UTC()
}
func (s *Service) audit(ctx context.Context, event AuditEvent) error {
	if s.deps.Audit == nil {
		return nil
	}
	return s.deps.Audit.Append(ctx, event)
}
func ctxErr(ctx context.Context) error {
	if ctx == nil {
		return errors.New("HH write context is nil")
	}
	return ctx.Err()
}
func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
func timestamp(value, fallback time.Time) time.Time {
	if value.IsZero() {
		return fallback
	}
	return value
}
func deref(value *time.Time, fallback, final time.Time) time.Time {
	if value != nil && !value.IsZero() {
		return *value
	}
	if !fallback.IsZero() {
		return fallback
	}
	return final
}

func policyOutcome(err error) GatewayOutcome {
	if errors.Is(err, ErrDryRun) {
		return OutcomeDryRun
	}
	return OutcomeBlocked
}

func terminalAction(status ActionStatus) bool {
	return status == ActionSent || status == ActionSentUnconfirmed || status == ActionDeliveryConfirmed || status == ActionDeliveryUncertain || status == ActionManualReview || status == ActionFailed
}

func terminalOutcome(status ActionStatus) GatewayOutcome {
	if status == ActionSent || status == ActionSentUnconfirmed || status == ActionDeliveryConfirmed {
		return OutcomeAccepted
	}
	if status == ActionDeliveryUncertain || status == ActionManualReview {
		return OutcomeDeliveryUncertain
	}
	return OutcomeRejected
}

func statusForOutcome(outcome hhwrite.Outcome) ActionStatus {
	switch outcome {
	case hhwrite.OutcomeAccepted:
		return ActionSent
	case hhwrite.OutcomeAmbiguous:
		return ActionDeliveryUncertain
	default:
		return ActionFailed
	}
}

func gatewayOutcome(outcome hhwrite.Outcome) GatewayOutcome {
	switch outcome {
	case hhwrite.OutcomeAccepted:
		return OutcomeAccepted
	case hhwrite.OutcomeRejected:
		return OutcomeRejected
	case hhwrite.OutcomeAmbiguous:
		return OutcomeDeliveryUncertain
	default:
		return OutcomeNotSent
	}
}

func mapResult(result Result, writeResult hhwrite.WriteResult) Result {
	result.ProviderID, result.ProviderStatus, result.Metadata = writeResult.ProviderID, writeResult.ProviderStatus, cloneMap(writeResult.Metadata)
	result.Timestamp = timestamp(writeResult.Timestamp, result.Timestamp)
	return result
}

func cloneMap(value map[string]string) map[string]string {
	if value == nil {
		return nil
	}
	result := make(map[string]string, len(value))
	for key, item := range value {
		result[key] = item
	}
	return result
}

type transportEvidenceValue struct {
	contentType, body      string
	fields, correlationIDs map[string]string
}

func transportEvidence(err error) transportEvidenceValue {
	var source *hhwrite.TransportError
	if !errors.As(err, &source) || source == nil {
		return transportEvidenceValue{}
	}
	return transportEvidenceValue{contentType: source.ResponseContentType, body: source.ResponseBody, fields: cloneMap(source.ErrorFields), correlationIDs: cloneMap(source.CorrelationIDs)}
}
