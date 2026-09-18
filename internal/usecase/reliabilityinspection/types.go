package reliabilityinspection

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	applicationattempt "hh-ai-responder/internal/applicationattempt"
	autochatattempt "hh-ai-responder/internal/autochatattempt"
	applicationport "hh-ai-responder/internal/ports/applicationattempt"
	autochatport "hh-ai-responder/internal/ports/autochatattempt"
)

const (
	DefaultLimit = 20
	MaxLimit     = 100
)

var (
	ErrNotConfigured = errors.New("reliability inspection is not configured")
	ErrInvalidFilter = errors.New("invalid reliability inspection filter")
)

type ApplicationFilter struct {
	Limit          int
	NeedsAttention bool
	State          string
	VacancyID      *int
}

type AutoChatFilter struct {
	Limit          int
	NeedsAttention bool
	State          string
	ConversationID *string
	ActionType     string
}

type Classification string

const (
	ClassificationConfirmed  Classification = "CONFIRMED"
	ClassificationAccepted   Classification = "ACCEPTED"
	ClassificationUnresolved Classification = "UNRESOLVED"
	ClassificationReplayable Classification = "REPLAYABLE"
	ClassificationNotSent    Classification = "NOT_SENT"
	ClassificationOther      Classification = "OTHER"
)

type ApplicationAttemptReadModel struct {
	AttemptID              string                                      `json:"attempt_id"`
	VacancyID              int                                         `json:"vacancy_id"`
	ResumeID               string                                      `json:"resume_id"`
	State                  applicationattempt.State                    `json:"state"`
	Classification         Classification                              `json:"classification"`
	DisplayLabel           string                                      `json:"display_label"`
	Reason                 string                                      `json:"reason,omitempty"`
	CreatedAt              time.Time                                   `json:"created_at"`
	UpdatedAt              time.Time                                   `json:"updated_at"`
	ProviderStatus         int                                         `json:"provider_status,omitempty"`
	ErrorClass             string                                      `json:"error_class,omitempty"`
	ProviderApplicationID  string                                      `json:"provider_application_id,omitempty"`
	ProviderNegotiationID  string                                      `json:"provider_negotiation_id,omitempty"`
	ProviderConversationID string                                      `json:"provider_conversation_id,omitempty"`
	ProviderIdentities     []applicationattempt.ProviderIdentity       `json:"provider_identities,omitempty"`
	EvidenceKind           applicationattempt.EvidenceKind             `json:"evidence_kind,omitempty"`
	EvidenceSource         string                                      `json:"evidence_source,omitempty"`
	ConfirmationSource     string                                      `json:"confirmation_source,omitempty"`
	EvidenceStrength       applicationattempt.EvidenceStrength         `json:"evidence_strength,omitempty"`
	ReconciliationHistory  []applicationattempt.ReconciliationEvidence `json:"reconciliation_history,omitempty"`
	ObservedAt             *time.Time                                  `json:"observed_at,omitempty"`
	ProviderResponseAt     *time.Time                                  `json:"provider_response_at,omitempty"`
	CausalityNote          string                                      `json:"causality_note,omitempty"`
}

type AutoChatAttemptReadModel struct {
	AttemptID                 string                       `json:"attempt_id"`
	ConversationID            string                       `json:"conversation_id"`
	TriggerMessageID          string                       `json:"trigger_message_id"`
	ActionType                autochatattempt.ActionType   `json:"action_type"`
	State                     autochatattempt.State        `json:"state"`
	Classification            Classification               `json:"classification"`
	DisplayLabel              string                       `json:"display_label"`
	Reason                    string                       `json:"reason,omitempty"`
	CreatedAt                 time.Time                    `json:"created_at"`
	UpdatedAt                 time.Time                    `json:"updated_at"`
	ProviderRequestKey        string                       `json:"provider_request_key,omitempty"`
	ProviderOutgoingMessageID string                       `json:"provider_outgoing_message_id,omitempty"`
	ProviderStatus            int                          `json:"provider_status,omitempty"`
	ErrorClass                string                       `json:"error_class,omitempty"`
	EvidenceKind              autochatattempt.EvidenceKind `json:"evidence_kind,omitempty"`
	EvidenceSource            string                       `json:"evidence_source,omitempty"`
	EvidenceProviderMessageID string                       `json:"evidence_provider_message_id,omitempty"`
	ObservedAt                *time.Time                   `json:"observed_at,omitempty"`
	CausalityNote             string                       `json:"causality_note,omitempty"`
}

type Service struct {
	applications applicationport.Reader
	autoChats    autochatport.Reader
}

func NewService(applications applicationport.Reader, autoChats autochatport.Reader) *Service {
	return &Service{applications: applications, autoChats: autoChats}
}

func (s *Service) ListApplications(ctx context.Context, filter ApplicationFilter) ([]ApplicationAttemptReadModel, error) {
	if s == nil || s.applications == nil {
		return nil, ErrNotConfigured
	}
	query, err := applicationQuery(filter)
	if err != nil {
		return nil, err
	}
	values, err := s.applications.List(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("application attempt store: %w", err)
	}
	result := make([]ApplicationAttemptReadModel, 0, len(values))
	for _, value := range values {
		result = append(result, applicationModel(value))
	}
	return result, nil
}

func (s *Service) GetApplication(ctx context.Context, attemptID string) (ApplicationAttemptReadModel, error) {
	if s == nil || s.applications == nil {
		return ApplicationAttemptReadModel{}, ErrNotConfigured
	}
	value, err := s.applications.GetByID(ctx, strings.TrimSpace(attemptID))
	if err != nil {
		return ApplicationAttemptReadModel{}, fmt.Errorf("application attempt store: %w", err)
	}
	return applicationModel(value), nil
}

func (s *Service) ListAutoChats(ctx context.Context, filter AutoChatFilter) ([]AutoChatAttemptReadModel, error) {
	if s == nil || s.autoChats == nil {
		return nil, ErrNotConfigured
	}
	query, err := autoChatQuery(filter)
	if err != nil {
		return nil, err
	}
	values, err := s.autoChats.List(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("auto-chat attempt store: %w", err)
	}
	result := make([]AutoChatAttemptReadModel, 0, len(values))
	for _, value := range values {
		result = append(result, autoChatModel(value))
	}
	return result, nil
}

func (s *Service) GetAutoChat(ctx context.Context, attemptID string) (AutoChatAttemptReadModel, error) {
	if s == nil || s.autoChats == nil {
		return AutoChatAttemptReadModel{}, ErrNotConfigured
	}
	value, err := s.autoChats.GetByID(ctx, strings.TrimSpace(attemptID))
	if err != nil {
		return AutoChatAttemptReadModel{}, fmt.Errorf("auto-chat attempt store: %w", err)
	}
	return autoChatModel(value), nil
}

func applicationQuery(filter ApplicationFilter) (applicationport.ReadQuery, error) {
	limit, err := boundedLimit(filter.Limit)
	if err != nil {
		return applicationport.ReadQuery{}, err
	}
	states, err := applicationStates(filter.State, filter.NeedsAttention)
	if err != nil {
		return applicationport.ReadQuery{}, err
	}
	return applicationport.ReadQuery{Limit: limit, States: states, VacancyID: filter.VacancyID}, nil
}

func autoChatQuery(filter AutoChatFilter) (autochatport.ReadQuery, error) {
	limit, err := boundedLimit(filter.Limit)
	if err != nil {
		return autochatport.ReadQuery{}, err
	}
	states, err := autoChatStates(filter.State, filter.NeedsAttention)
	if err != nil {
		return autochatport.ReadQuery{}, err
	}
	var action *autochatattempt.ActionType
	if strings.TrimSpace(filter.ActionType) != "" {
		value := autochatattempt.ActionType(strings.TrimSpace(filter.ActionType))
		if value != autochatattempt.ActionReply && value != autochatattempt.ActionLeave {
			return autochatport.ReadQuery{}, fmt.Errorf("%w: action type %q", ErrInvalidFilter, filter.ActionType)
		}
		action = &value
	}
	return autochatport.ReadQuery{Limit: limit, States: states, ConversationID: filter.ConversationID, ActionType: action}, nil
}

func boundedLimit(value int) (int, error) {
	if value == 0 {
		return DefaultLimit, nil
	}
	if value < 1 || value > MaxLimit {
		return 0, fmt.Errorf("%w: limit must be between 1 and %d", ErrInvalidFilter, MaxLimit)
	}
	return value, nil
}

func applicationStates(raw string, needsAttention bool) ([]applicationattempt.State, error) {
	if strings.TrimSpace(raw) != "" {
		state := applicationattempt.State(strings.TrimSpace(raw))
		if !validApplicationState(state) {
			return nil, fmt.Errorf("%w: application state %q", ErrInvalidFilter, raw)
		}
		return []applicationattempt.State{state}, nil
	}
	if needsAttention {
		return []applicationattempt.State{applicationattempt.StateSending, applicationattempt.StateDeliveryUncertain, applicationattempt.StateAccepted}, nil
	}
	return nil, nil
}

func autoChatStates(raw string, needsAttention bool) ([]autochatattempt.State, error) {
	if strings.TrimSpace(raw) != "" {
		state := autochatattempt.State(strings.TrimSpace(raw))
		if !validAutoChatState(state) {
			return nil, fmt.Errorf("%w: auto-chat state %q", ErrInvalidFilter, raw)
		}
		return []autochatattempt.State{state}, nil
	}
	if needsAttention {
		return []autochatattempt.State{autochatattempt.StateSending, autochatattempt.StateDeliveryUncertain, autochatattempt.StateAccepted}, nil
	}
	return nil, nil
}

func validApplicationState(state applicationattempt.State) bool {
	switch state {
	case applicationattempt.StateSending, applicationattempt.StateAccepted, applicationattempt.StateRejected, applicationattempt.StateNotSent, applicationattempt.StateDeliveryUncertain, applicationattempt.StateTargetResponseConfirmed:
		return true
	default:
		return false
	}
}

func validAutoChatState(state autochatattempt.State) bool {
	switch state {
	case autochatattempt.StateSending, autochatattempt.StateAccepted, autochatattempt.StateRejected, autochatattempt.StateNotSent, autochatattempt.StateDeliveryUncertain, autochatattempt.StateTargetReplyConfirmed, autochatattempt.StateTargetLeaveConfirmed:
		return true
	default:
		return false
	}
}

func applicationModel(value applicationattempt.Attempt) ApplicationAttemptReadModel {
	classification, label := classifyApplication(value.State)
	model := ApplicationAttemptReadModel{AttemptID: value.AttemptID, VacancyID: value.VacancyID, ResumeID: value.ResumeID, State: value.State, Classification: classification, DisplayLabel: label, Reason: firstNonEmpty(value.ErrorClass, label), CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt, ProviderStatus: value.ProviderStatus, ErrorClass: value.ErrorClass}
	if value.Reconciliation != nil {
		model.ProviderApplicationID = value.Reconciliation.ProviderApplicationID
		model.ProviderNegotiationID = value.Reconciliation.ProviderNegotiationID
		model.ProviderConversationID = value.Reconciliation.ProviderConversationID
		model.ProviderIdentities = value.Reconciliation.ProviderIdentities
		model.EvidenceKind = value.Reconciliation.Kind
		model.EvidenceSource = value.Reconciliation.Source
		model.ConfirmationSource = value.Reconciliation.ConfirmationSource
		model.EvidenceStrength = value.Reconciliation.Strength
		model.ReconciliationHistory = value.Reconciliation.History
		if !value.Reconciliation.ObservedAt.IsZero() {
			observed := value.Reconciliation.ObservedAt
			model.ObservedAt = &observed
		}
		model.ProviderResponseAt = value.Reconciliation.ProviderResponseAt
	}
	if value.State == applicationattempt.StateTargetResponseConfirmed {
		model.CausalityNote = "Отклик подтверждён на HH; принадлежность именно этой попытке не установлена автоматически."
	}
	return model
}

func autoChatModel(value autochatattempt.Attempt) AutoChatAttemptReadModel {
	classification, label := classifyAutoChat(value.State)
	model := AutoChatAttemptReadModel{AttemptID: value.AttemptID, ConversationID: value.ConversationID, TriggerMessageID: value.TriggerMessageID, ActionType: value.ActionType, State: value.State, Classification: classification, DisplayLabel: label, Reason: firstNonEmpty(value.ErrorClass, label), CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt, ProviderRequestKey: maskCorrelation(value.RequestKey), ProviderOutgoingMessageID: value.ProviderOutgoingMessageID, ProviderStatus: value.ProviderStatus, ErrorClass: value.ErrorClass}
	if value.Reconciliation != nil {
		model.EvidenceKind = value.Reconciliation.Kind
		model.EvidenceSource = value.Reconciliation.Source
		model.EvidenceProviderMessageID = value.Reconciliation.ProviderMessageID
		if !value.Reconciliation.ObservedAt.IsZero() {
			observed := value.Reconciliation.ObservedAt
			model.ObservedAt = &observed
		}
		if value.Reconciliation.CausalityNote != "" {
			model.CausalityNote = value.Reconciliation.CausalityNote
		}
	}
	if value.State == autochatattempt.StateTargetReplyConfirmed || value.State == autochatattempt.StateTargetLeaveConfirmed {
		if model.CausalityNote == "" {
			model.CausalityNote = "Целевое действие подтверждено на HH; причинность старой попытки не утверждается без точной идентичности исходящего сообщения."
		}
	}
	return model
}

func classifyApplication(state applicationattempt.State) (Classification, string) {
	switch state {
	case applicationattempt.StateSending:
		return ClassificationUnresolved, "Ожидает проверки: исход отправки неизвестен"
	case applicationattempt.StateAccepted:
		return ClassificationAccepted, "HH принял запрос; подтверждение не получено"
	case applicationattempt.StateRejected:
		return ClassificationReplayable, "Запрос отклонён HH"
	case applicationattempt.StateNotSent:
		return ClassificationNotSent, "Не отправлено: запрос к HH не выполнялся"
	case applicationattempt.StateDeliveryUncertain:
		return ClassificationUnresolved, "Возможно отправлено; повторная отправка запрещена"
	case applicationattempt.StateTargetResponseConfirmed:
		return ClassificationConfirmed, "Отклик подтверждён на HH"
	default:
		return ClassificationOther, "Состояние попытки неизвестно"
	}
}

func classifyAutoChat(state autochatattempt.State) (Classification, string) {
	switch state {
	case autochatattempt.StateSending:
		return ClassificationUnresolved, "Ожидает проверки: исход отправки неизвестен"
	case autochatattempt.StateAccepted:
		return ClassificationAccepted, "HH принял запрос; подтверждение не получено"
	case autochatattempt.StateRejected:
		return ClassificationReplayable, "Запрос отклонён HH"
	case autochatattempt.StateNotSent:
		return ClassificationNotSent, "Не отправлено: запрос к HH не выполнялся"
	case autochatattempt.StateDeliveryUncertain:
		return ClassificationUnresolved, "Возможно отправлено; повторная отправка запрещена"
	case autochatattempt.StateTargetReplyConfirmed:
		return ClassificationConfirmed, "Ответ в диалоге подтверждён на HH"
	case autochatattempt.StateTargetLeaveConfirmed:
		return ClassificationConfirmed, "Выход из диалога подтверждён на HH"
	default:
		return ClassificationOther, "Состояние попытки неизвестно"
	}
}

func maskCorrelation(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if len(value) <= 8 {
		return "••••"
	}
	return value[:4] + "…" + value[len(value)-4:]
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
