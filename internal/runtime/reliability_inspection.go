package runtime

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	applicationattempt "hh-ai-responder/internal/applicationattempt"
	autochatattempt "hh-ai-responder/internal/autochatattempt"
	applicationreconciliation "hh-ai-responder/internal/usecase/applicationreconciliation"
	autochatreconciliation "hh-ai-responder/internal/usecase/autochatreconciliation"
	reliabilityinspection "hh-ai-responder/internal/usecase/reliabilityinspection"
)

type reliabilityStoreInfo struct {
	Status  string `json:"status"`
	Backend string `json:"backend"`
}

type reliabilityListResponse[T any] struct {
	Items          []T                  `json:"items"`
	Store          reliabilityStoreInfo `json:"store"`
	NeedsAttention bool                 `json:"needs_attention"`
	BoundedLimit   int                  `json:"limit"`
}

type reliabilityDetailResponse[T any] struct {
	Attempt T                    `json:"attempt"`
	Store   reliabilityStoreInfo `json:"store"`
}

type reliabilityReconcileResponse struct {
	Workflow string `json:"workflow"`
	Result   any    `json:"result"`
}

// reliabilityAutoChatReconcileResult deliberately omits the durable attempt
// object. In particular, RequestKey is a local idempotency secret and must
// not cross the operator API boundary.
type reliabilityAutoChatReconcileResult struct {
	AttemptID                 string                                 `json:"attempt_id"`
	ConversationID            string                                 `json:"conversation_id"`
	TriggerMessageID          string                                 `json:"trigger_message_id"`
	ActionType                autochatattempt.ActionType             `json:"action_type"`
	ProviderOutgoingMessageID string                                 `json:"provider_outgoing_message_id,omitempty"`
	PreviousState             autochatattempt.State                  `json:"previous_state"`
	NewState                  autochatattempt.State                  `json:"new_state"`
	Status                    autochatreconciliation.Status          `json:"status"`
	Evidence                  autochatattempt.ReconciliationEvidence `json:"evidence"`
	ReadAttempted             bool                                   `json:"read_attempted"`
	Reason                    string                                 `json:"reason,omitempty"`
}

func (s *DashboardServer) readReliabilityAPI(w http.ResponseWriter, r *http.Request, p []string) {
	if s.Reliability == nil {
		dashboardError(w, http.StatusServiceUnavailable, "Reliability attempt store unavailable")
		return
	}
	info := reliabilityStoreInfo{Status: "AVAILABLE", Backend: s.ReliabilityBackend}
	switch p[1] {
	case "application-attempts":
		if len(p) == 3 {
			value, err := s.Reliability.GetApplication(r.Context(), p[2])
			if err != nil {
				s.writeReliabilityError(w, err, "Application attempt unavailable")
				return
			}
			dashboardJSON(w, http.StatusOK, reliabilityDetailResponse[reliabilityinspection.ApplicationAttemptReadModel]{Attempt: value, Store: info})
			return
		}
		filter, err := applicationReliabilityFilter(r)
		if err != nil {
			dashboardError(w, http.StatusBadRequest, err.Error())
			return
		}
		items, err := s.Reliability.ListApplications(r.Context(), filter)
		if err != nil {
			s.writeReliabilityError(w, err, "Application attempt store unavailable")
			return
		}
		dashboardJSON(w, http.StatusOK, reliabilityListResponse[reliabilityinspection.ApplicationAttemptReadModel]{Items: items, Store: info, NeedsAttention: filter.NeedsAttention, BoundedLimit: filter.Limit})
	case "autochat-attempts":
		if len(p) == 3 {
			value, err := s.Reliability.GetAutoChat(r.Context(), p[2])
			if err != nil {
				s.writeReliabilityError(w, err, "Auto-chat attempt unavailable")
				return
			}
			dashboardJSON(w, http.StatusOK, reliabilityDetailResponse[reliabilityinspection.AutoChatAttemptReadModel]{Attempt: value, Store: info})
			return
		}
		filter, err := autoChatReliabilityFilter(r)
		if err != nil {
			dashboardError(w, http.StatusBadRequest, err.Error())
			return
		}
		items, err := s.Reliability.ListAutoChats(r.Context(), filter)
		if err != nil {
			s.writeReliabilityError(w, err, "Auto-chat attempt store unavailable")
			return
		}
		dashboardJSON(w, http.StatusOK, reliabilityListResponse[reliabilityinspection.AutoChatAttemptReadModel]{Items: items, Store: info, NeedsAttention: filter.NeedsAttention, BoundedLimit: filter.Limit})
	}
}

func (s *DashboardServer) reconcileReliabilityAPI(w http.ResponseWriter, r *http.Request, p []string) {
	if len(p) != 4 || p[3] != "reconcile" {
		dashboardError(w, http.StatusNotFound, "Not found")
		return
	}
	switch p[1] {
	case "application-attempts":
		if s.ApplicationReconciliation == nil {
			dashboardError(w, http.StatusServiceUnavailable, "Application reconciliation unavailable")
			return
		}
		result, err := s.ApplicationReconciliation.Reconcile(r.Context(), p[2])
		if err != nil {
			dashboardReconcileError(w, err, result.Status)
			return
		}
		dashboardJSON(w, http.StatusOK, reliabilityReconcileResponse{Workflow: "application", Result: result})
	case "autochat-attempts":
		if s.AutoChatReconciliation == nil {
			dashboardError(w, http.StatusServiceUnavailable, "Auto-chat reconciliation unavailable")
			return
		}
		result, err := s.AutoChatReconciliation.Reconcile(r.Context(), p[2])
		if err != nil {
			dashboardReconcileError(w, err, result.Status)
			return
		}
		dashboardJSON(w, http.StatusOK, reliabilityReconcileResponse{Workflow: "autochat", Result: reliabilityAutoChatReconcileResult{
			AttemptID: result.Attempt.AttemptID, ConversationID: result.Attempt.ConversationID, TriggerMessageID: result.Attempt.TriggerMessageID,
			ActionType: result.Attempt.ActionType, ProviderOutgoingMessageID: result.Attempt.ProviderOutgoingMessageID,
			PreviousState: result.PreviousState, NewState: result.NewState, Status: result.Status, Evidence: result.Evidence,
			ReadAttempted: result.ReadAttempted, Reason: result.Reason,
		}})
	default:
		dashboardError(w, http.StatusNotFound, "Not found")
	}
}

func (s *DashboardServer) dashboardControlledReconcile(w http.ResponseWriter, r *http.Request, actionID string) {
	if s.ControlledReconciliation == nil {
		dashboardError(w, http.StatusServiceUnavailable, "Controlled reconciliation unavailable")
		return
	}
	result, err := s.ControlledReconciliation.ReconcileDelivery(r.Context(), actionID)
	if err != nil {
		dashboardReconcileError(w, err, result.Status)
		return
	}
	dashboardJSON(w, http.StatusOK, reliabilityReconcileResponse{Workflow: "controlled_chat", Result: result})
}

func dashboardReconcileError(w http.ResponseWriter, err error, status interface{}) {
	code := http.StatusInternalServerError
	switch status {
	case applicationreconciliation.StatusUnavailable, autochatreconciliation.StatusUnavailable:
		code = http.StatusServiceUnavailable
	case applicationreconciliation.StatusNotApplicable, autochatreconciliation.StatusNotApplicable, autochatreconciliation.StatusUnsupported:
		code = http.StatusConflict
	case autochatreconciliation.StatusPersistence:
		code = http.StatusInternalServerError
	}
	if errors.Is(err, applicationattempt.ErrAttemptNotFound) || errors.Is(err, autochatattempt.ErrAttemptNotFound) {
		code = http.StatusNotFound
	}
	dashboardJSON(w, code, map[string]any{"error": err.Error(), "status": status})
}

func (s *DashboardServer) writeReliabilityError(w http.ResponseWriter, err error, unavailable string) {
	if errors.Is(err, reliabilityinspection.ErrInvalidFilter) {
		dashboardError(w, http.StatusBadRequest, err.Error())
		return
	}
	if errors.Is(err, applicationattempt.ErrAttemptNotFound) || errors.Is(err, autochatattempt.ErrAttemptNotFound) {
		dashboardError(w, http.StatusNotFound, "Attempt not found")
		return
	}
	dashboardError(w, http.StatusServiceUnavailable, unavailable)
}

func applicationReliabilityFilter(r *http.Request) (reliabilityinspection.ApplicationFilter, error) {
	q := r.URL.Query()
	limit, err := queryLimit(q.Get("limit"))
	if err != nil {
		return reliabilityinspection.ApplicationFilter{}, err
	}
	attention, err := queryBool(q.Get("needs_attention"), true)
	if err != nil {
		return reliabilityinspection.ApplicationFilter{}, err
	}
	var vacancyID *int
	if raw := strings.TrimSpace(q.Get("vacancy_id")); raw != "" {
		value, parseErr := strconv.Atoi(raw)
		if parseErr != nil || value <= 0 {
			return reliabilityinspection.ApplicationFilter{}, errors.New("invalid vacancy_id")
		}
		vacancyID = &value
	}
	return reliabilityinspection.ApplicationFilter{Limit: limit, NeedsAttention: attention, State: q.Get("state"), VacancyID: vacancyID}, nil
}

func autoChatReliabilityFilter(r *http.Request) (reliabilityinspection.AutoChatFilter, error) {
	q := r.URL.Query()
	limit, err := queryLimit(q.Get("limit"))
	if err != nil {
		return reliabilityinspection.AutoChatFilter{}, err
	}
	attention, err := queryBool(q.Get("needs_attention"), true)
	if err != nil {
		return reliabilityinspection.AutoChatFilter{}, err
	}
	var conversationID *string
	if value := strings.TrimSpace(q.Get("conversation_id")); value != "" {
		conversationID = &value
	}
	return reliabilityinspection.AutoChatFilter{Limit: limit, NeedsAttention: attention, State: q.Get("state"), ConversationID: conversationID, ActionType: q.Get("action_type")}, nil
}

func queryLimit(raw string) (int, error) {
	if strings.TrimSpace(raw) == "" {
		return 0, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 1 || value > reliabilityinspection.MaxLimit {
		return 0, errors.New("invalid limit")
	}
	return value, nil
}

func queryBool(raw string, fallback bool) (bool, error) {
	if strings.TrimSpace(raw) == "" {
		return fallback, nil
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		return false, errors.New("invalid needs_attention")
	}
	return value, nil
}
