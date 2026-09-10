package runtime

import (
	"context"
	"errors"
	"time"

	hhwriteport "hh-ai-responder/internal/ports/hhwrite"
	hhwritegateway "hh-ai-responder/internal/usecase/hhwritegateway"
)

// approvedHHActionGatewayStore is a composition adapter. The extracted
// gateway core sees only semantic action transitions; JSON/Postgres details
// remain outside the usecase package.
type approvedHHActionGatewayStore struct {
	store         *ApprovedHHActionStore
	conversations *ConversationStore
}

func (s approvedHHActionGatewayStore) Load(_ context.Context, id string) (hhwritegateway.Action, error) {
	if s.store == nil {
		return hhwritegateway.Action{}, errors.New("HH action store is unavailable")
	}
	action, err := s.store.Get(id)
	if err != nil {
		return hhwritegateway.Action{}, err
	}
	return s.actionValue(action)
}

func (s approvedHHActionGatewayStore) Reserve(_ context.Context, id string, at time.Time) (hhwritegateway.Action, error) {
	if s.store == nil {
		return hhwritegateway.Action{}, errors.New("HH action store is unavailable")
	}
	action, err := s.store.Get(id)
	if err != nil {
		return hhwritegateway.Action{}, err
	}
	if action.Status != HHWriteApproved {
		return hhwritegateway.Action{}, errors.New("HH write action is not approved")
	}
	if action.SendNonce == "" {
		return hhwritegateway.Action{}, errors.New("send nonce is missing; re-approval is required")
	}
	if action.NonceUsedAt != nil {
		return hhwritegateway.Action{}, errors.New("send nonce was already used")
	}
	action.Status, action.NonceUsedAt, action.UpdatedAt = HHWriteSending, coreTimePtr(at), at
	if err := s.store.put(action); err != nil {
		return hhwritegateway.Action{}, err
	}
	if err := s.store.Save(); err != nil {
		return hhwritegateway.Action{}, err
	}
	return s.actionValue(action)
}

func (s approvedHHActionGatewayStore) actionValue(action ApprovedHHAction) (hhwritegateway.Action, error) {
	destinationID := action.ConversationID
	if s.conversations != nil {
		if conversation, err := s.conversations.GetConversation(action.ConversationID); err == nil {
			destinationID = conversation.HHConversationID
		}
	}
	return hhwritegateway.Action{
		ID: action.ID, Operation: string(action.ActionType), ConversationID: destinationID,
		Text: action.ApprovedText, Nonce: action.SendNonce, Status: hhGatewayActionStatus(action.Status),
		NonceUsedAt: action.NonceUsedAt, ProviderID: action.ExternalMessageID, AttemptedAt: action.SentAt, UpdatedAt: action.UpdatedAt,
	}, nil
}

func (s approvedHHActionGatewayStore) Record(_ context.Context, id string, record hhwritegateway.ActionRecord) error {
	if s.store == nil {
		return errors.New("HH action store is unavailable")
	}
	action, err := s.store.Get(id)
	if err != nil {
		return err
	}
	if record.Status != "" {
		action.Status = hhWriteActionStatus(record.Status)
	}
	if record.ProviderID != "" {
		action.ExternalMessageID = record.ProviderID
	}
	if !record.Timestamp.IsZero() && (record.Status == hhwritegateway.ActionSent || record.Status == hhwritegateway.ActionSentUnconfirmed || record.Status == hhwritegateway.ActionDeliveryConfirmed) {
		action.SentAt = coreTimePtr(record.Timestamp)
	}
	action.Error, action.UpdatedAt = record.Error, time.Now().UTC()
	if err := s.store.put(action); err != nil {
		return err
	}
	return s.store.Save()
}

type hhGatewayAuditSink struct{ audit *HHWriteAuditStore }

func (s hhGatewayAuditSink) Append(_ context.Context, event hhwritegateway.AuditEvent) error {
	if s.audit == nil {
		return nil
	}
	return s.audit.Append(HHWriteEvent{
		ActionID: event.ActionID, Type: event.Type, ConversationID: event.ConversationID, Result: event.Result,
		ExternalMessageID: event.ProviderID, Error: event.Error, HTTPStatus: event.ProviderStatus,
		ResponseContentType: event.ResponseContentType, ResponseBody: event.ResponseBody,
		HHErrorFields: copyStringMap(event.ErrorFields), CorrelationIDs: copyStringMap(event.CorrelationIDs), CreatedAt: event.CreatedAt,
	})
}

func (s hhGatewayAuditSink) AttemptsSince(_ context.Context, since time.Time) (int, error) {
	if s.audit == nil {
		return 0, nil
	}
	count := 0
	for _, event := range s.audit.List() {
		if event.Type == "send_started" && !event.CreatedAt.Before(since) {
			count++
		}
	}
	return count, nil
}

type legacyChatGatewayWriter struct{ client HHWriteClient }

func (w legacyChatGatewayWriter) SendChatMessage(ctx context.Context, request hhwriteport.ChatMessageRequest) (hhwriteport.WriteResult, error) {
	if w.client == nil {
		return hhwriteport.WriteResult{Outcome: hhwriteport.OutcomeNotSent}, errors.New("HH writer is not configured")
	}
	result, err := w.client.SendConversationMessage(ctx, request.ConversationID, request.Text, request.IdempotencyKey)
	if result.Outcome == "" {
		if err != nil {
			result.Outcome = legacyOutcome(err)
		} else {
			result.Outcome = hhwriteport.OutcomeAccepted
		}
	}
	if err != nil {
		converted := legacyWriteError(err)
		var transportErr *hhwriteport.TransportError
		if errors.As(converted, &transportErr) && transportErr != nil {
			result.ProviderStatus = transportErr.Status
		}
		return hhwriteport.WriteResult{Outcome: result.Outcome, ProviderStatus: result.ProviderStatus, ProviderID: result.ExternalMessageID, Timestamp: result.Timestamp, Metadata: result.Metadata}, converted
	}
	return hhwriteport.WriteResult{Outcome: result.Outcome, ProviderStatus: result.ProviderStatus, ProviderID: result.ExternalMessageID, Timestamp: result.Timestamp, Metadata: result.Metadata}, nil
}

func legacyOutcome(err error) hhwriteport.Outcome {
	var source *HHWriteTransportError
	if errors.As(err, &source) && source != nil {
		if source.Outcome != "" {
			return source.Outcome
		}
		if source.DeliveryUncertain || source.Status == 0 || source.Status >= 500 || source.Status == 409 {
			return hhwriteport.OutcomeAmbiguous
		}
		return hhwriteport.OutcomeRejected
	}
	return hhwriteport.OutcomeAmbiguous
}

func legacyWriteError(err error) error {
	var source *HHWriteTransportError
	if !errors.As(err, &source) || source == nil {
		return err
	}
	outcome := legacyOutcome(err)
	category := hhwriteport.ErrorProvider
	switch source.Category {
	case HHWriteErrorRequestValidationFailed:
		category = hhwriteport.ErrorRequestValidation
	case HHWriteErrorAuthenticationFailed:
		category = hhwriteport.ErrorAuthentication
	case HHWriteErrorPermissionDenied:
		category = hhwriteport.ErrorPermission
	case HHWriteErrorRateLimited:
		category = hhwriteport.ErrorRateLimited
	case HHWriteErrorServerError:
		category = hhwriteport.ErrorServer
	case HHWriteErrorNetworkUncertain:
		category = hhwriteport.ErrorNetworkAmbiguous
	case HHWriteErrorDeliveryUncertain:
		category = hhwriteport.ErrorResponseAmbiguous
	}
	return &hhwriteport.TransportError{Category: category, Outcome: outcome, Status: source.Status, ResponseContentType: source.ResponseContentType, ResponseBody: source.ResponseBody, ErrorFields: copyStringMap(source.HHErrorFields), CorrelationIDs: copyStringMap(source.CorrelationIDs), Err: source.Err}
}

func hhGatewayActionStatus(status HHWriteActionStatus) hhwritegateway.ActionStatus {
	return hhwritegateway.ActionStatus(status)
}

func hhWriteActionStatus(status hhwritegateway.ActionStatus) HHWriteActionStatus {
	return HHWriteActionStatus(status)
}

func coreTimePtr(value time.Time) *time.Time {
	copy := value
	return &copy
}

func (g *HHWriteGateway) newCoreService() *hhwritegateway.Service {
	options := hhwritegateway.Options{
		WriteEnabled:    g.Enabled && g.Client != nil,
		DryRun:          g.DryRun,
		MaxWritesPerRun: g.MaxWritesPerRun,
		MaxWritesPerDay: g.MaxWritesPerDay,
	}
	if g.core != nil {
		g.core.Configure(options)
		return g.core
	}
	g.core = hhwritegateway.NewService(
		hhwritegateway.Dependencies{
			ChatMessageWriter: legacyChatGatewayWriter{client: g.Client},
			Actions:           approvedHHActionGatewayStore{store: g.Actions, conversations: g.Conversations},
			Audit:             hhGatewayAuditSink{audit: g.Audit},
		},
		options,
	)
	return g.core
}

// newLegacyWriteService preserves the old root method signatures while
// routing their explicit mutation calls through the same typed safety core.
// Those methods intentionally do not receive an approved application action:
// resume maintenance and job-search status have always used their own flags.
func (r *HHAIResponder) newLegacyWriteService() (*hhwritegateway.Service, error) {
	adapter, err := r.hhWriteAdapter()
	if err != nil {
		return nil, err
	}
	return hhwritegateway.NewService(hhwritegateway.Dependencies{
		ChatMessageWriter:     adapter,
		ChatLeaveWriter:       adapter,
		VacancyResponseWriter: adapter,
		ResumeWriter:          adapter,
		JobSearchStatusWriter: adapter,
	}, hhwritegateway.Options{WriteEnabled: r.hhWriteEnabled, DryRun: r.dryRun}), nil
}

func hhWriteResultFromGateway(value hhwritegateway.Result) HHWriteResult {
	writeCapability := ""
	errText := value.Error
	if value.Outcome == hhwritegateway.OutcomeDryRun {
		writeCapability = "BLOCKED_BY_DRY_RUN"
		errText = writeCapability
	} else if value.Outcome == hhwritegateway.OutcomeBlocked {
		writeCapability = "BLOCKED_BY_WRITE_DISABLED"
		errText = writeCapability
	}
	return HHWriteResult{
		ActionID: value.ActionID, Success: value.Outcome == hhwritegateway.OutcomeAccepted,
		TransportAttempted: value.TransportAttempted, ExternalMessageID: value.ProviderID,
		WriteCapability: writeCapability, Timestamp: value.Timestamp, Metadata: value.Metadata, Error: errText,
		Status: HHWriteActionStatus(value.Status), Retryable: false,
	}
}
