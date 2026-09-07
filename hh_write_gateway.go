package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	HHWriteActionsFilename = "hh_write_actions.json"
	HHWriteEventsFilename  = "hh_write_events.json"
)

// HHWriteClient is deliberately smaller than HHReadClient. A client obtained
// by a sync/monitor component cannot satisfy this interface accidentally.
type HHWriteClient interface {
	SendConversationMessage(context.Context, string, string, string) (HHWriteTransportResult, error)
}

type HHWriteTransportResult struct {
	ExternalMessageID string            `json:"external_message_id,omitempty"`
	Timestamp         time.Time         `json:"timestamp,omitempty"`
	Metadata          map[string]string `json:"metadata,omitempty"`
}

// HHWriteError lets the transport distinguish a known rejected request from a
// response lost after HH may have accepted the message. Both cases are never
// retried automatically by the gateway.
type HHWriteActionType string

const (
	HHActionConversationReply HHWriteActionType = "conversation_reply"
	HHActionFollowUp          HHWriteActionType = "follow_up"
)

type HHWriteActionStatus string

const (
	HHWritePending           HHWriteActionStatus = "pending"
	HHWriteApproved          HHWriteActionStatus = "approved"
	HHWriteSending           HHWriteActionStatus = "sending"
	HHWriteSent              HHWriteActionStatus = "sent"
	HHWriteFailed            HHWriteActionStatus = "failed"
	HHWriteCancelled         HHWriteActionStatus = "cancelled"
	HHWriteStale             HHWriteActionStatus = "stale"
	HHWriteSentUnconfirmed   HHWriteActionStatus = "sent_unconfirmed"
	HHWriteDeliveryUncertain HHWriteActionStatus = "delivery_uncertain"
	HHWriteManualReview      HHWriteActionStatus = "manual_review"
	HHWriteDeliveryConfirmed HHWriteActionStatus = "delivery_confirmed"
)

const (
	HHPreflightReasonDeliveryAlreadyConfirmed = "DELIVERY_ALREADY_CONFIRMED"
	HHPreflightReasonSentUnconfirmed          = "SENT_UNCONFIRMED"
	HHPreflightReasonDeliveryUncertain        = "DELIVERY_UNCERTAIN"
	HHPreflightReasonFailedAfterTransport     = "FAILED_AFTER_TRANSPORT"
	HHPreflightReasonManualReview             = "MANUAL_REVIEW_REQUIRED"
	HHPreflightReasonActionNotApproved        = "ACTION_NOT_APPROVED"
)

type ApprovedHHAction struct {
	ID                        string                    `json:"id"`
	ActionType                HHWriteActionType         `json:"action_type"`
	ConversationID            string                    `json:"conversation_id"`
	ApplicationID             string                    `json:"application_id,omitempty"`
	DraftID                   string                    `json:"draft_id"`
	ReplyPurpose              string                    `json:"reply_purpose,omitempty"`
	ApprovedText              string                    `json:"approved_text"`
	ApprovedBy                string                    `json:"approved_by"`
	ApprovedAt                time.Time                 `json:"approved_at"`
	SourceMessageID           string                    `json:"source_message_id,omitempty"`
	ConversationVersion       string                    `json:"conversation_version"`
	CandidateKnowledgeVersion string                    `json:"candidate_knowledge_version,omitempty"`
	RelevantKnowledgeSnapshot RelevantKnowledgeSnapshot `json:"relevant_knowledge_snapshot,omitempty"`
	RelevantKnowledgeHash     string                    `json:"relevant_knowledge_hash,omitempty"`
	LastMessageID             string                    `json:"last_message_id,omitempty"`
	ContentHash               string                    `json:"content_hash"`
	SendNonce                 string                    `json:"send_nonce,omitempty"`
	NonceUsedAt               *time.Time                `json:"nonce_used_at,omitempty"`
	LastPreflightAt           *time.Time                `json:"last_preflight_at,omitempty"`
	LastPreflightOK           bool                      `json:"last_preflight_ok,omitempty"`
	LastPreflightReasonCode   string                    `json:"last_preflight_reason_code,omitempty"`
	LastPreflightReasons      []string                  `json:"last_preflight_reasons,omitempty"`
	Status                    HHWriteActionStatus       `json:"status"`
	CreatedAt                 time.Time                 `json:"created_at"`
	UpdatedAt                 time.Time                 `json:"updated_at"`
	SentAt                    *time.Time                `json:"sent_at,omitempty"`
	ExternalMessageID         string                    `json:"external_message_id,omitempty"`
	Error                     string                    `json:"error,omitempty"`
}

func (a ApprovedHHAction) validate() error {
	if strings.TrimSpace(a.ID) == "" || strings.TrimSpace(a.ConversationID) == "" || strings.TrimSpace(a.DraftID) == "" || strings.TrimSpace(a.ApprovedText) == "" || strings.TrimSpace(a.ContentHash) == "" || a.CreatedAt.IsZero() || a.UpdatedAt.Before(a.CreatedAt) {
		return errors.New("invalid approved HH action")
	}
	if a.ActionType != HHActionConversationReply && a.ActionType != HHActionFollowUp {
		return errors.New("invalid HH action type")
	}
	switch a.Status {
	case HHWritePending, HHWriteApproved, HHWriteSending, HHWriteSent, HHWriteSentUnconfirmed, HHWriteFailed, HHWriteCancelled, HHWriteStale, HHWriteDeliveryUncertain, HHWriteManualReview, HHWriteDeliveryConfirmed:
	default:
		return errors.New("invalid HH action status")
	}
	if len(a.ContentHash) != sha256.Size*2 {
		return errors.New("invalid HH action content hash")
	}
	if _, err := hex.DecodeString(a.ContentHash); err != nil {
		return errors.New("invalid HH action content hash")
	}
	return nil
}

type hhWriteActionFile struct {
	Version int                `json:"version"`
	Actions []ApprovedHHAction `json:"actions"`
}

type ApprovedHHActionStore struct {
	path    string
	actions []ApprovedHHAction
}

type HHWriteActionStore = ApprovedHHActionStore

func NewApprovedHHActionStore(path string) *ApprovedHHActionStore {
	return &ApprovedHHActionStore{path: path, actions: []ApprovedHHAction{}}
}
func NewHHWriteActionStore(path string) *ApprovedHHActionStore { return NewApprovedHHActionStore(path) }

func (s *ApprovedHHActionStore) Load() error {
	start := time.Now()
	defer perfRecord("store.ApprovedHHActionStore.Load", start, 1)
	if s == nil {
		return errors.New("HH action store is nil")
	}
	if strings.TrimSpace(s.path) == "" {
		s.actions = []ApprovedHHAction{}
		return nil
	}
	raw, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		s.actions = []ApprovedHHAction{}
		return nil
	}
	if err != nil || profileContainsSecret(raw) {
		return errors.New("cannot read HH action store")
	}
	var file hhWriteActionFile
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&file); err != nil || file.Version != 1 || file.Actions == nil {
		return errors.New("invalid HH action store")
	}
	var trailing any
	if err := dec.Decode(&trailing); err != io.EOF {
		return errors.New("invalid HH action store")
	}
	return s.setActions(file.Actions)
}

// Reload refreshes a long-lived Dashboard process from the durable action
// projection. The CLI and Dashboard are separate processes, so keeping the
// initial in-memory slice would hide approvals created after startup.
func (s *ApprovedHHActionStore) Reload() error {
	return s.Load()
}

func (s *ApprovedHHActionStore) setActions(values []ApprovedHHAction) error {
	seen := map[string]bool{}
	for _, value := range values {
		if err := value.validate(); err != nil {
			return err
		}
		if seen[value.ID] {
			return errors.New("duplicate HH action id")
		}
		seen[value.ID] = true
	}
	s.actions = append([]ApprovedHHAction{}, values...)
	return nil
}

func (s *ApprovedHHActionStore) Save() error {
	if s == nil {
		return errors.New("HH action store is nil")
	}
	if err := s.setActions(s.actions); err != nil {
		return err
	}
	if strings.TrimSpace(s.path) == "" {
		return nil
	}
	raw, err := json.MarshalIndent(hhWriteActionFile{Version: 1, Actions: s.actions}, "", "  ")
	if err != nil {
		return errors.New("cannot encode HH action store")
	}
	return atomicPrivateStoreWrite(s.path, raw, ".hh-write-actions-*.tmp")
}

func (s *ApprovedHHActionStore) Get(id string) (ApprovedHHAction, error) {
	if s != nil {
		for _, value := range s.actions {
			if value.ID == id {
				return value, nil
			}
		}
	}
	return ApprovedHHAction{}, errors.New("HH action not found")
}
func (s *ApprovedHHActionStore) List() []ApprovedHHAction {
	if s == nil {
		return []ApprovedHHAction{}
	}
	return append([]ApprovedHHAction{}, s.actions...)
}
func (s *ApprovedHHActionStore) put(value ApprovedHHAction) error {
	if err := value.validate(); err != nil {
		return err
	}
	for i := range s.actions {
		if s.actions[i].ID == value.ID {
			s.actions[i] = value
			return nil
		}
	}
	s.actions = append(s.actions, value)
	return nil
}

type HHWriteEvent struct {
	ActionID            string            `json:"action_id"`
	Type                string            `json:"type"`
	ConversationID      string            `json:"conversation_id"`
	ApplicationID       string            `json:"application_id,omitempty"`
	DraftID             string            `json:"draft_id,omitempty"`
	ApprovedBy          string            `json:"approved_by,omitempty"`
	ApprovedAt          time.Time         `json:"approved_at,omitempty"`
	SentAt              *time.Time        `json:"sent_at,omitempty"`
	ContentHash         string            `json:"content_hash,omitempty"`
	Result              string            `json:"result,omitempty"`
	ExternalMessageID   string            `json:"external_message_id,omitempty"`
	Error               string            `json:"error,omitempty"`
	HTTPStatus          int               `json:"http_status,omitempty"`
	Endpoint            string            `json:"endpoint,omitempty"`
	RequestMethod       string            `json:"request_method,omitempty"`
	RequestContentType  string            `json:"request_content_type,omitempty"`
	DestinationType     string            `json:"destination_type,omitempty"`
	DestinationID       string            `json:"destination_id,omitempty"`
	ResponseContentType string            `json:"response_content_type,omitempty"`
	ResponseBody        string            `json:"response_body,omitempty"`
	HHErrorFields       map[string]string `json:"hh_error_fields,omitempty"`
	CorrelationIDs      map[string]string `json:"correlation_ids,omitempty"`
	CreatedAt           time.Time         `json:"created_at"`
}

type hhWriteEventFile struct {
	Version int            `json:"version"`
	Events  []HHWriteEvent `json:"events"`
}
type HHWriteAuditStore struct {
	mu     sync.RWMutex
	path   string
	events []HHWriteEvent
}

func NewHHWriteAuditStore(path string) *HHWriteAuditStore {
	return &HHWriteAuditStore{path: path, events: []HHWriteEvent{}}
}
func (s *HHWriteAuditStore) Load() error {
	start := time.Now()
	defer perfRecord("store.HHWriteAuditStore.Load", start, 1)
	if s == nil {
		return errors.New("HH write audit store is nil")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadUnlocked()
}

// Reload refreshes a long-lived Dashboard process from the durable audit.
// Reads are intentionally explicit: the CLI and Dashboard are separate
// processes and the Dashboard must not report a stale in-memory projection.
func (s *HHWriteAuditStore) Reload() error {
	return s.Load()
}

func (s *HHWriteAuditStore) loadUnlocked() error {
	if strings.TrimSpace(s.path) == "" {
		s.events = []HHWriteEvent{}
		return nil
	}
	raw, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		s.events = []HHWriteEvent{}
		return nil
	}
	if err != nil || profileContainsSecret(raw) {
		return errors.New("cannot read HH write audit")
	}
	var file hhWriteEventFile
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&file); err != nil || file.Version != 1 || file.Events == nil {
		return errors.New("invalid HH write audit")
	}
	var trailing any
	if err := dec.Decode(&trailing); err != io.EOF {
		return errors.New("invalid HH write audit")
	}
	s.events = append([]HHWriteEvent{}, file.Events...)
	return nil
}
func (s *HHWriteAuditStore) Save() error {
	if s == nil {
		return errors.New("HH write audit store is nil")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.saveUnlocked()
}

func (s *HHWriteAuditStore) saveUnlocked() error {
	start := time.Now()
	defer perfRecord("store.HHWriteAuditStore.saveUnlocked", start, 1)
	if strings.TrimSpace(s.path) == "" {
		return nil
	}
	for _, event := range s.events {
		if profileContainsSecret([]byte(event.Error)) || profileContainsSecret([]byte(event.Result)) || profileContainsSecret([]byte(event.ResponseBody)) {
			return errors.New("HH write audit contains a forbidden secret marker")
		}
	}
	raw, err := json.MarshalIndent(hhWriteEventFile{Version: 1, Events: s.events}, "", "  ")
	if err != nil {
		return errors.New("cannot encode HH write audit")
	}
	return atomicPrivateStoreWrite(s.path, raw, ".hh-write-audit-*.tmp")
}
func (s *HHWriteAuditStore) Append(event HHWriteEvent) error {
	if s == nil {
		return errors.New("HH write audit store is nil")
	}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now().UTC()
	}
	if strings.TrimSpace(event.ActionID) == "" || strings.TrimSpace(event.ConversationID) == "" {
		return errors.New("HH write event requires action and conversation")
	}
	if profileContainsSecret([]byte(event.Error)) || profileContainsSecret([]byte(event.Result)) || profileContainsSecret([]byte(event.ResponseBody)) {
		return errors.New("HH write event contains a forbidden secret marker")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if strings.TrimSpace(s.path) == "" {
		s.events = append(s.events, event)
		return nil
	}
	// Reload while holding the cross-process store lock. Without this, a
	// long-lived web process could append using an old in-memory slice and
	// overwrite events written by another process (the CLI or another web
	// process).
	var current []HHWriteEvent
	if err := withStoreLock(s.path, func() error {
		loaded, err := readHHWriteEventsFile(s.path)
		if err != nil {
			return err
		}
		current = loaded
		current = append(current, event)
		raw, err := json.MarshalIndent(hhWriteEventFile{Version: 1, Events: current}, "", "  ")
		if err != nil {
			return errors.New("cannot encode HH write audit")
		}
		return atomicPrivateStoreWriteUnlocked(s.path, raw, ".hh-write-audit-*.tmp")
	}); err != nil {
		return err
	}
	s.events = append([]HHWriteEvent{}, current...)
	return nil
}
func (s *HHWriteAuditStore) List() []HHWriteEvent {
	if s == nil {
		return []HHWriteEvent{}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]HHWriteEvent{}, s.events...)
}

func readHHWriteEventsFile(path string) ([]HHWriteEvent, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return []HHWriteEvent{}, nil
	}
	if err != nil || profileContainsSecret(raw) {
		return nil, errors.New("cannot read HH write audit")
	}
	var file hhWriteEventFile
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&file); err != nil || file.Version != 1 || file.Events == nil {
		return nil, errors.New("invalid HH write audit")
	}
	var trailing any
	if err := dec.Decode(&trailing); err != io.EOF {
		return nil, errors.New("invalid HH write audit")
	}
	return append([]HHWriteEvent{}, file.Events...), nil
}

type HHConversationReadState struct {
	ExternalID       string                       `json:"external_id"`
	LastMessageID    string                       `json:"last_message_id,omitempty"`
	State            string                       `json:"state,omitempty"`
	ReplyRequirement ConversationReplyRequirement `json:"reply_requirement,omitempty"`
	MessageCount     int                          `json:"message_count"`
	Warnings         []string                     `json:"warnings,omitempty"`
	MessageIDs       []string                     `json:"message_ids,omitempty"`
}

type HHWritePreflight struct {
	Allowed              bool     `json:"allowed"`
	Status               string   `json:"status"`
	ReasonCode           string   `json:"reason_code,omitempty"`
	Reason               string   `json:"reason,omitempty"`
	Reasons              []string `json:"reasons,omitempty"`
	SendRequestPerformed bool     `json:"send_request_performed"`
}

type HHWriteResult struct {
	ActionID           string              `json:"action_id"`
	Success            bool                `json:"success"`
	TransportAttempted bool                `json:"transport_attempted"`
	WriteCapability    string              `json:"write_capability,omitempty"`
	ExternalMessageID  string              `json:"external_message_id,omitempty"`
	Timestamp          time.Time           `json:"timestamp"`
	Metadata           map[string]string   `json:"metadata,omitempty"`
	Error              string              `json:"error,omitempty"`
	Retryable          bool                `json:"retryable"`
	Status             HHWriteActionStatus `json:"status,omitempty"`
}

type HHWriteGatewayOptions struct {
	Enabled          bool
	DryRun           bool
	RequireFreshRead bool
	MaxWritesPerRun  int
	MaxWritesPerDay  int
	Client           HHWriteClient
	ReadClient       HHReadClient
	Conversations    *ConversationStore
	Applications     *ApplicationStore
	Drafts           *AIDraftStore
	Clarifications   *CandidateClarificationStore
	Observations     *PilotObservationStore
	Resolver         *CandidateContextResolver
	Orchestrator     *AIReplyOrchestrator
	FollowUpPolicy   FollowUpPolicy
	ChatURL          string
	Actions          *ApprovedHHActionStore
	Audit            *HHWriteAuditStore
	Lifecycle        *DashboardLifecycleStore
}

type HHWriteGateway struct {
	HHWriteGatewayOptions
	mu            sync.Mutex
	writesThisRun int
	startedAt     time.Time
}

// The variadic constructor accepts either HHWriteGatewayOptions or individual
// dependencies, which keeps small tests and future integrations concise.
func NewHHWriteGateway(values ...any) *HHWriteGateway {
	o := HHWriteGatewayOptions{}
	for _, value := range values {
		switch v := value.(type) {
		case HHWriteGatewayOptions:
			o = v
		case *HHWriteGatewayOptions:
			if v != nil {
				o = *v
			}
		case bool:
			o.Enabled = v
		case HHWriteClient:
			o.Client = v
		case HHReadClient:
			o.ReadClient = v
		case *ConversationStore:
			o.Conversations = v
		case *ApplicationStore:
			o.Applications = v
		case *AIDraftStore:
			o.Drafts = v
		case *CandidateClarificationStore:
			o.Clarifications = v
		case *PilotObservationStore:
			o.Observations = v
		case *CandidateContextResolver:
			o.Resolver = v
		case *AIReplyOrchestrator:
			o.Orchestrator = v
		case *ApprovedHHActionStore:
			o.Actions = v
		case *HHWriteAuditStore:
			o.Audit = v
		case FollowUpPolicy:
			o.FollowUpPolicy = v
		}
	}
	if o.Actions == nil {
		o.Actions = NewApprovedHHActionStore("")
	}
	if o.Audit == nil {
		o.Audit = NewHHWriteAuditStore("")
	}
	if o.FollowUpPolicy == (FollowUpPolicy{}) {
		o.FollowUpPolicy = DefaultFollowUpPolicy()
	}
	return &HHWriteGateway{HHWriteGatewayOptions: o, startedAt: time.Now().UTC()}
}

func (g *HHWriteGateway) WriteEnabled() bool {
	return g != nil && g.Enabled && !g.DryRun && g.Client != nil
}

// PreflightEnabled is deliberately independent from write capability. A
// rehearsal must be able to perform the same read-only safety checks while
// HH_DRY_RUN=true or HH_WRITE_ENABLED=false; only Send requires capability.
func (g *HHWriteGateway) PreflightEnabled() bool {
	return g != nil
}

// PreflightIsFresh prevents a persisted preflight from unlocking Send after a
// process restart. The user must run the read-only preflight again.
func (g *HHWriteGateway) PreflightIsFresh(action ApprovedHHAction) bool {
	return g != nil && action.Status == HHWriteApproved && action.LastPreflightOK &&
		action.LastPreflightAt != nil && !action.LastPreflightAt.IsZero() && !action.LastPreflightAt.Before(g.startedAt)
}

func (g *HHWriteGateway) requestPreviewLocked(action ApprovedHHAction) (HHWriteRequestPreview, error) {
	if g == nil || g.Conversations == nil {
		return HHWriteRequestPreview{}, errors.New("HH write conversation store is unavailable")
	}
	c, err := g.Conversations.GetConversation(action.ConversationID)
	if err != nil {
		return HHWriteRequestPreview{}, err
	}
	chatURL := g.ChatURL
	if strings.TrimSpace(chatURL) == "" {
		chatURL = defaultHHChatURL
	}
	preview, _, err := buildHHWriteRequest(chatURL, c.HHConversationID, action.ApprovedText, action.SendNonce)
	return preview, err
}

// BuildRequestPreview is offline-only. It builds and validates the exact
// request shape without resolving cookies or invoking HHRequester.
func (g *HHWriteGateway) BuildRequestPreview(actionID string) (HHWriteRequestPreview, error) {
	if g == nil || g.Actions == nil {
		return HHWriteRequestPreview{}, errors.New("HH write action store is unavailable")
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	action, err := g.Actions.Get(actionID)
	if err != nil {
		return HHWriteRequestPreview{}, err
	}
	return g.requestPreviewLocked(action)
}

func contentHash(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

func conversationVersion(c EmployerConversation) string {
	raw, _ := json.Marshal(struct {
		ID, HHID string
		Status   ConversationStatus
		Messages []ConversationMessage
	}{c.ID, c.HHConversationID, c.Status, c.Messages})
	return contentHash(string(raw))
}

// candidateKnowledgeVersion fingerprints only the current employer-safe
// projection. Pending proposals, unknowns and private source metadata are not
// part of an approved action and cannot make a safe draft stale.
func currentCandidateKnowledgeVersion(resolver *CandidateContextResolver) string {
	if resolver == nil || (resolver.kb == nil && resolver.candidate == nil) {
		return ""
	}
	view, err := resolver.employerSafeView()
	if err != nil {
		return ""
	}
	raw, err := json.Marshal(view)
	if err != nil {
		return ""
	}
	return contentHash(string(raw))
}
func latestDeliveredMessageID(c EmployerConversation) string {
	values := deliveredMessages(c.Messages)
	if len(values) == 0 {
		return ""
	}
	return values[len(values)-1].ID
}

func latestEmployerMessageIDForAction(c EmployerConversation) string {
	values := deliveredMessages(c.Messages)
	for i := len(values) - 1; i >= 0; i-- {
		if values[i].Sender == ConversationSenderEmployer {
			return values[i].ID
		}
	}
	return ""
}

func hhActionReplyPurpose(actionType HHWriteActionType) string {
	switch actionType {
	case HHActionConversationReply:
		return "conversation_reply"
	case HHActionFollowUp:
		return "follow_up"
	default:
		return ""
	}
}

func hhActionIsActive(status HHWriteActionStatus) bool {
	return status == HHWritePending || status == HHWriteApproved || status == HHWriteSending
}
func (g *HHWriteGateway) actionForDraft(draft AIDraft) (HHWriteActionType, error) {
	switch draft.Type {
	case AIDraftEmployerReply:
		return HHActionConversationReply, nil
	case AIDraftFollowUp:
		return HHActionFollowUp, nil
	default:
		return "", errors.New("only conversation reply and follow-up drafts may be sent")
	}
}

func (g *HHWriteGateway) currentContext(c EmployerConversation) (ConversationContext, error) {
	if g.Resolver == nil {
		return ConversationContext{}, errors.New("candidate context resolver is unavailable")
	}
	return NewConversationContextBuilder(g.Conversations, g.Resolver).BuildForReply(c.ID)
}

func (g *HHWriteGateway) validateDraftText(text string, c EmployerConversation) error {
	if strings.TrimSpace(text) == "" {
		return errors.New("message is empty")
	}
	if len([]rune(text)) > 600 {
		return errors.New("message is too long")
	}
	ctx, err := g.currentContext(c)
	if err != nil {
		return err
	}
	// Salary remains in the explicit approval flow. A relocation answer is
	// allowed only when the resolver has a confirmed fact; an unknown or
	// inferred relocation claim remains blocked with the other high-risk
	// topics.
	if reason := classifyHighRiskChatMessage(text); reason != "" && reason != "salary negotiation" {
		if reason != "relocation" || !hasAnswerableResolvedFact(ctx.CandidateContext, "relocation") {
			return errors.New("message requires manual review")
		}
	}
	if g.Orchestrator != nil {
		return g.Orchestrator.ValidateAIDraft(text, ctx.CandidateContext, c.Summary.CandidateClaims)
	}
	return ValidateAIDraft(text, ctx.CandidateContext, c.Summary.CandidateClaims)
}

func hasAnswerableResolvedFact(context CandidateContext, topic string) bool {
	for _, fact := range context.ResolvedFacts {
		if fact.Topic == topic && fact.Status == ResolvedFactAnswerable && strings.TrimSpace(fact.Value) != "" {
			return true
		}
	}
	return false
}

func (g *HHWriteGateway) EditDraft(draftID, text string) (AIDraft, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.Drafts == nil || g.Conversations == nil {
		return AIDraft{}, errors.New("HH write draft dependencies are unavailable")
	}
	draft, err := g.Drafts.Get(draftID)
	if err != nil {
		return AIDraft{}, err
	}
	if draft.Type != AIDraftEmployerReply && draft.Type != AIDraftFollowUp {
		return AIDraft{}, errors.New("draft type cannot be sent to HH")
	}
	c, err := g.Conversations.GetConversation(draft.ConversationID)
	if err != nil {
		return AIDraft{}, err
	}
	if err := g.validateDraftText(text, c); err != nil {
		return AIDraft{}, err
	}
	if err := g.Drafts.updateText(draftID, text, AIDraftSourceUserEdited); err != nil {
		return AIDraft{}, err
	}
	for _, action := range g.Actions.List() {
		if action.DraftID == draftID && action.Status != HHWriteSent && action.Status != HHWriteCancelled {
			action.Status = HHWriteStale
			action.UpdatedAt = time.Now().UTC()
			action.Error = "draft edited after approval"
			_ = g.Actions.put(action)
		}
	}
	if err := g.Drafts.Save(); err != nil {
		return AIDraft{}, err
	}
	if err := g.Actions.Save(); err != nil {
		return AIDraft{}, err
	}
	return g.Drafts.Get(draftID)
}

// InvalidateConversationApprovals is called before a regenerated draft is
// persisted. An approval belongs to one exact draft, never to a conversation
// in the abstract.
func (g *HHWriteGateway) InvalidateConversationApprovals(conversationID string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.Actions == nil {
		return nil
	}
	for _, action := range g.Actions.List() {
		if action.ConversationID != conversationID || action.Status != HHWriteApproved {
			continue
		}
		action.Status, action.Error, action.UpdatedAt = HHWriteStale, "draft regenerated after approval", time.Now().UTC()
		if err := g.Actions.put(action); err != nil {
			return err
		}
		_ = g.Audit.Append(HHWriteEvent{ActionID: action.ID, Type: "stale", ConversationID: action.ConversationID, ApplicationID: action.ApplicationID, DraftID: action.DraftID, ContentHash: action.ContentHash, Result: "stale", Error: action.Error})
	}
	return g.Actions.Save()
}

func (g *HHWriteGateway) ApproveDraft(draftID, approvedBy string) (ApprovedHHAction, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.Drafts == nil || g.Conversations == nil {
		return ApprovedHHAction{}, errors.New("HH write approval dependencies are unavailable")
	}
	if strings.TrimSpace(approvedBy) == "" {
		return ApprovedHHAction{}, errors.New("approved_by is required")
	}
	draft, err := g.Drafts.Get(draftID)
	if err != nil {
		return ApprovedHHAction{}, err
	}
	if draft.Status != AIDraftGenerated && draft.Status != AIDraftApproved {
		return ApprovedHHAction{}, errors.New("only a current generated draft can be approved")
	}
	typ, err := g.actionForDraft(draft)
	if err != nil {
		return ApprovedHHAction{}, err
	}
	c, err := g.Conversations.GetConversation(draft.ConversationID)
	if err != nil {
		return ApprovedHHAction{}, err
	}
	if err := g.validateDraftText(draft.Text, c); err != nil {
		return ApprovedHHAction{}, err
	}
	lastMessageID := latestDeliveredMessageID(c)
	if typ == HHActionConversationReply {
		lastMessageID = latestEmployerMessageIDForAction(c)
	}
	replyPurpose := hhActionReplyPurpose(typ)
	context, err := g.currentContext(c)
	if err != nil {
		return ApprovedHHAction{}, err
	}
	for _, old := range g.Actions.List() {
		oldPurpose := old.ReplyPurpose
		if oldPurpose == "" {
			oldPurpose = hhActionReplyPurpose(old.ActionType)
		}
		if old.ConversationID == c.ID && oldPurpose == replyPurpose && old.LastMessageID == lastMessageID && hhActionIsActive(old.Status) && old.DraftID != draft.ID {
			return ApprovedHHAction{}, errors.New("conversation already has an active approval for the latest employer message")
		}
		if old.DraftID != draft.ID {
			continue
		}
		if old.Status == HHWriteApproved && old.RelevantKnowledgeHash != "" {
			if current, snapshotErr := relevantKnowledgeSnapshotForResolverWithSemantic(g.Resolver, context.CandidateContext, old.ApprovedText, draft.UsedFacts, context.RelevantExamples); snapshotErr == nil && RelevantKnowledgeHash(current) != old.RelevantKnowledgeHash {
				old.Status = HHWriteStale
				old.Error = "candidate knowledge relevant to this draft changed"
				old.UpdatedAt = time.Now().UTC()
				if err := g.Actions.put(old); err != nil {
					return ApprovedHHAction{}, err
				}
				_ = g.Audit.Append(HHWriteEvent{ActionID: old.ID, Type: "stale", ConversationID: old.ConversationID, ApplicationID: old.ApplicationID, DraftID: old.DraftID, ContentHash: old.ContentHash, Result: "stale", Error: old.Error})
				continue
			}
		}
		if old.Status == HHWriteApproved || old.Status == HHWriteSending {
			return ApprovedHHAction{}, errors.New("draft already has an active approval")
		}
	}
	now := time.Now().UTC()
	actionID, err := newKnowledgeID("hh-action")
	if err != nil {
		return ApprovedHHAction{}, err
	}
	nonce, err := newKnowledgeID("hh-send")
	if err != nil {
		return ApprovedHHAction{}, err
	}
	relevantSnapshot, err := relevantKnowledgeSnapshotForResolverWithSemantic(g.Resolver, context.CandidateContext, draft.Text, draft.UsedFacts, context.RelevantExamples)
	if err != nil {
		return ApprovedHHAction{}, err
	}
	action := ApprovedHHAction{ID: actionID, ActionType: typ, ConversationID: c.ID, ApplicationID: draft.ApplicationID, DraftID: draft.ID, ReplyPurpose: replyPurpose, ApprovedText: draft.Text, ApprovedBy: approvedBy, ApprovedAt: now, SourceMessageID: latestDeliveredMessageID(c), ConversationVersion: conversationVersion(c), CandidateKnowledgeVersion: currentCandidateKnowledgeVersion(g.Resolver), RelevantKnowledgeSnapshot: relevantSnapshot, RelevantKnowledgeHash: RelevantKnowledgeHash(relevantSnapshot), LastMessageID: lastMessageID, ContentHash: contentHash(draft.Text), SendNonce: nonce, Status: HHWriteApproved, CreatedAt: now, UpdatedAt: now}
	if err := g.Actions.put(action); err != nil {
		return ApprovedHHAction{}, err
	}
	draft.Status = AIDraftApproved
	if err := g.Drafts.SetStatus(draft.ID, AIDraftApproved); err != nil {
		return ApprovedHHAction{}, err
	}
	if err := g.Actions.Save(); err != nil {
		return ApprovedHHAction{}, err
	}
	if err := g.Drafts.Save(); err != nil {
		return ApprovedHHAction{}, err
	}
	_ = g.Audit.Append(HHWriteEvent{ActionID: action.ID, Type: "approved", ConversationID: action.ConversationID, ApplicationID: action.ApplicationID, DraftID: action.DraftID, ApprovedBy: action.ApprovedBy, ApprovedAt: action.ApprovedAt, ContentHash: action.ContentHash, Result: "approved"})
	return action, nil
}

func (g *HHWriteGateway) freshState(ctx context.Context, c EmployerConversation) (HHConversationReadState, error) {
	if reader, ok := g.ReadClient.(HHConversationPreflightReader); ok {
		return reader.ReadConversationState(ctx, c.HHConversationID)
	}
	if g.RequireFreshRead {
		return HHConversationReadState{}, errors.New("fresh HH conversation read is unavailable")
	}
	return HHConversationReadState{ExternalID: c.HHConversationID, LastMessageID: latestDeliveredMessageID(c), State: string(c.Status), MessageCount: len(deliveredMessages(c.Messages))}, nil
}

func containsWarning(values []string) bool {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return true
		}
	}
	return false
}

func blockedHHWritePreflight(code, reason string) HHWritePreflight {
	return HHWritePreflight{
		Allowed:              false,
		Status:               "BLOCKED",
		ReasonCode:           code,
		Reason:               reason,
		Reasons:              []string{reason},
		SendRequestPerformed: false,
	}
}

// terminalHHWritePreflight is deliberately local-only. Once transport has
// been attempted, preflight must not re-evaluate mutable candidate or
// conversation context: the action is no longer sendable and retrying it is
// unsafe. This also lets dashboard preflight skip its targeted HH read.
func terminalHHWritePreflight(action ApprovedHHAction) (HHWritePreflight, bool) {
	switch action.Status {
	case HHWriteDeliveryConfirmed:
		return blockedHHWritePreflight(HHPreflightReasonDeliveryAlreadyConfirmed, "This action was already delivered and cannot be sent again."), true
	case HHWriteSentUnconfirmed, HHWriteSent:
		return blockedHHWritePreflight(HHPreflightReasonSentUnconfirmed, "This action has a recorded transport result but delivery is not confirmed; automatic retry is forbidden."), true
	case HHWriteDeliveryUncertain:
		return blockedHHWritePreflight(HHPreflightReasonDeliveryUncertain, "Delivery is uncertain after the transport attempt; automatic retry is forbidden."), true
	case HHWriteFailed:
		return blockedHHWritePreflight(HHPreflightReasonFailedAfterTransport, "This action failed after a transport attempt; automatic retry is forbidden."), true
	case HHWriteManualReview:
		return blockedHHWritePreflight(HHPreflightReasonManualReview, "This action requires manual review after a transport decision; automatic retry is forbidden."), true
	case HHWriteApproved:
		return HHWritePreflight{}, false
	default:
		return blockedHHWritePreflight(HHPreflightReasonActionNotApproved, "This action is not approved and cannot be sent."), true
	}
}

func (g *HHWriteGateway) preflightLocked(ctx context.Context, action ApprovedHHAction) (HHWritePreflight, EmployerConversation, AIDraft) {
	start := time.Now()
	defer perfRecord("preflight.total", start, 1)
	if p, terminal := terminalHHWritePreflight(action); terminal {
		return p, EmployerConversation{}, AIDraft{}
	}
	p := HHWritePreflight{Allowed: false, Status: "BLOCKED", Reasons: []string{}, SendRequestPerformed: false}
	if g.Conversations == nil || g.Drafts == nil {
		p.Reasons = append(p.Reasons, "write stores are unavailable")
		p.Reason = p.Reasons[0]
		return p, EmployerConversation{}, AIDraft{}
	}
	c, err := g.Conversations.GetConversation(action.ConversationID)
	if err != nil {
		p.Reasons = append(p.Reasons, "conversation does not exist")
		p.Reason = p.Reasons[0]
		return p, EmployerConversation{}, AIDraft{}
	}
	draft, err := g.Drafts.Get(action.DraftID)
	if err != nil {
		p.Reasons = append(p.Reasons, "approved draft does not exist")
		p.Reason = p.Reasons[0]
		return p, c, AIDraft{}
	}
	if draft.Text != action.ApprovedText || contentHash(draft.Text) != action.ContentHash {
		p.Reasons = append(p.Reasons, "approved text hash does not match current draft")
	}
	if strings.TrimSpace(c.HHConversationID) == "" {
		p.Reasons = append(p.Reasons, "conversation has no strong HH external identifier")
	}
	if action.ConversationVersion != conversationVersion(c) || action.LastMessageID != latestDeliveredMessageID(c) {
		p.Reasons = append(p.Reasons, "conversation changed after approval")
	}
	if action.RelevantKnowledgeHash != "" {
		context, contextErr := g.currentContext(c)
		if contextErr != nil {
			p.Reasons = append(p.Reasons, "candidate context is unavailable")
		} else if current, snapshotErr := relevantKnowledgeSnapshotForResolverWithSemantic(g.Resolver, context.CandidateContext, action.ApprovedText, draft.UsedFacts, context.RelevantExamples); snapshotErr != nil {
			p.Reasons = append(p.Reasons, "candidate knowledge snapshot is unavailable")
		} else if RelevantKnowledgeHash(current) != action.RelevantKnowledgeHash {
			p.Reasons = append(p.Reasons, "candidate knowledge relevant to this draft changed")
		}
	} else if action.CandidateKnowledgeVersion != "" && action.CandidateKnowledgeVersion != currentCandidateKnowledgeVersion(g.Resolver) {
		// Legacy action files predate relevant snapshots. Keep the old check only
		// for those records; all newly approved actions use the scoped hash above.
		p.Reasons = append(p.Reasons, "candidate knowledge changed after approval")
	}
	freshStart := time.Now()
	fresh, freshErr := g.freshState(ctx, c)
	perfRecord("preflight.fresh_hh", freshStart, 1)
	if freshErr != nil {
		p.Reasons = append(p.Reasons, "fresh conversation preflight failed")
		p.Reason = p.Reasons[0]
		return p, c, draft
	}
	if fresh.ExternalID == "" || fresh.ExternalID != c.HHConversationID {
		p.Reasons = append(p.Reasons, "HH conversation destination is ambiguous")
	}
	if action.LastMessageID != fresh.LastMessageID {
		p.Reasons = append(p.Reasons, "new HH message arrived after approval")
	}
	if fresh.State == string(ConversationRejected) || fresh.State == string(ConversationClosed) {
		p.Reasons = append(p.Reasons, "HH conversation is closed or rejected")
	}
	if action.ActionType == HHActionConversationReply && fresh.State != "" && fresh.State != string(ConversationCandidateActionRequired) {
		p.Reasons = append(p.Reasons, "HH conversation no longer requires a candidate reply")
	}
	if action.ActionType == HHActionConversationReply && fresh.ReplyRequirement != "" && fresh.ReplyRequirement != ReplyRequired {
		p.Reasons = append(p.Reasons, "fresh HH conversation reply policy is not REPLY_REQUIRED")
	}
	if len(fresh.Warnings) > 0 {
		p.Reasons = append(p.Reasons, "fresh HH conversation has consistency warnings")
	}
	if fresh.MessageCount < len(deliveredMessages(c.Messages)) {
		p.Reasons = append(p.Reasons, "HH conversation history is incomplete")
	}
	var app JobApplication
	if action.ApplicationID != "" && g.Applications != nil {
		app, err = g.Applications.GetApplication(action.ApplicationID)
		if err != nil {
			p.Reasons = append(p.Reasons, "application does not exist")
		}
	}
	if app.Status == ApplicationRejected || app.Status == ApplicationArchived {
		p.Reasons = append(p.Reasons, "application is rejected or closed")
	}
	state := (ConversationStateResolver{}).Resolve(app, c, nil, g.hasPendingClarification(action), nil, time.Now().UTC())
	if state.Status == ConversationManualReview || containsWarning(state.Warnings) {
		p.Reasons = append(p.Reasons, "conversation has a critical or manual-review condition")
	}
	if action.ActionType == HHActionConversationReply && state.Status != ConversationCandidateActionRequired {
		p.Reasons = append(p.Reasons, "conversation no longer requires a candidate reply")
	}
	if action.ActionType == HHActionFollowUp {
		if g.Applications == nil || app.ID == "" {
			p.Reasons = append(p.Reasons, "follow-up requires a confirmed application relation")
		} else {
			in := FollowUpInput{Application: app, Conversation: c, Warnings: state.Warnings, PendingClarification: g.hasPendingClarification(action)}
			events, _ := g.Applications.GetApplicationTimeline(app.ID)
			if known := knownApplicationTime(app, events); known != nil {
				in.AppliedAt = known
			}
			if got := (FollowUpEngine{Policy: g.FollowUpPolicy}).Evaluate(in, time.Now().UTC()); got.Status != FollowUpEligible {
				p.Reasons = append(p.Reasons, "follow-up eligibility changed")
			}
		}
	}
	if g.Resolver != nil {
		if context, contextErr := g.currentContext(c); contextErr != nil {
			p.Reasons = append(p.Reasons, "conversation context is unavailable")
		} else if len(context.UnresolvedQuestions) > 0 || len(context.ConsistencyWarnings) > 0 {
			p.Reasons = append(p.Reasons, "clarification or consistency warning is unresolved")
		} else if err := g.validateDraftText(action.ApprovedText, c); err != nil {
			p.Reasons = append(p.Reasons, err.Error())
		} else if action.ActionType == HHActionConversationReply {
			if context.ReplyRequirement != ReplyRequired {
				p.Reasons = append(p.Reasons, "conversation reply policy is not REPLY_REQUIRED")
			}
			if strings.Contains(strings.ToLower(action.ApprovedText), "зарплат") || strings.Contains(strings.ToLower(action.ApprovedText), "руб") || strings.Contains(action.ApprovedText, "₽") {
				salaryAnswerable := false
				for _, fact := range context.CandidateContext.ResolvedFacts {
					if fact.Topic == "salary" && fact.Status == ResolvedFactAnswerable {
						salaryAnswerable = true
						break
					}
				}
				if !salaryAnswerable {
					p.Reasons = append(p.Reasons, "salary fact is not ANSWERABLE")
				}
			}
			linked := []JobApplication{}
			if g.Applications != nil {
				if values, listErr := g.Applications.ListApplications(); listErr == nil {
					for _, value := range values {
						if value.ConversationID == c.ID || value.HHMetadata["conversation_external_id"] == c.HHConversationID {
							linked = append(linked, value)
						}
					}
				}
			}
			eligibility := EvaluateReplyEligibility(EligibilityEvaluationInput{Conversation: c, Application: app, Applications: linked, PendingClarification: g.hasPendingClarification(action), Context: &context, Now: time.Now().UTC()})
			if eligibility.Classification != EligibilitySafe {
				p.Reasons = append(p.Reasons, "conversation is not SAFE_FOR_MANUAL_REPLY")
			}
		}
	} else {
		p.Reasons = append(p.Reasons, "candidate context resolver is unavailable")
	}
	p.Reasons = uniqueStrings(p.Reasons)
	p.Allowed = len(p.Reasons) == 0
	if p.Allowed {
		p.Status = "READY_TO_SEND"
	} else {
		p.Status = "BLOCKED"
		p.Reason = strings.Join(p.Reasons, "; ")
	}
	return p, c, draft
}

func (g *HHWriteGateway) hasPendingClarification(action ApprovedHHAction) bool {
	if g.Clarifications == nil {
		return false
	}
	values, _ := g.Clarifications.List()
	for _, value := range values {
		if value.Status == ClarificationPending && (value.ConversationID == action.ConversationID || value.ApplicationID != "" && value.ApplicationID == action.ApplicationID) {
			return true
		}
	}
	return false
}

func (g *HHWriteGateway) PreflightAction(ctx context.Context, actionID string) HHWritePreflight {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.Actions == nil {
		return blockedHHWritePreflight("ACTION_NOT_FOUND", "Action store unavailable")
	}
	if err := g.Actions.Reload(); err != nil {
		return blockedHHWritePreflight("DURABLE_STATE_UNAVAILABLE", "Cannot reload durable action state")
	}

	action, err := g.Actions.Get(actionID)
	if err != nil {
		return blockedHHWritePreflight("ACTION_NOT_FOUND", "The HH action was not found.")
	}
	p, _, _ := g.preflightLocked(ctx, action)
	now := time.Now().UTC()
	action.LastPreflightAt = &now
	action.LastPreflightOK = p.Allowed
	action.LastPreflightReasonCode = p.ReasonCode
	action.LastPreflightReasons = append([]string{}, p.Reasons...)
	if !p.Allowed && action.Status == HHWriteApproved {
		action.Status = HHWriteStale
		action.Error = strings.Join(p.Reasons, "; ")
		action.UpdatedAt = now
		_ = g.Audit.Append(HHWriteEvent{ActionID: action.ID, Type: "preflight_blocked", ConversationID: action.ConversationID, ApplicationID: action.ApplicationID, DraftID: action.DraftID, ContentHash: action.ContentHash, Result: "stale", Error: safeWriteError(errors.New(action.Error))})
	}
	if g.Actions.put(action) == nil {
		_ = g.Actions.Save()
	}
	if p.Allowed {
		g.recordLifecycle(action.ID, "preflight_passed", "fresh read-only preflight passed")
		_ = g.Audit.Append(HHWriteEvent{ActionID: action.ID, Type: "preflight_passed", ConversationID: action.ConversationID, ApplicationID: action.ApplicationID, DraftID: action.DraftID, ContentHash: action.ContentHash, Result: "preflight_passed"})
	}
	return p
}

func (g *HHWriteGateway) Send(ctx context.Context, actionID string) (HHWriteResult, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	result := HHWriteResult{ActionID: actionID, Timestamp: time.Now().UTC()}
	if g.Actions == nil {
		return result, errors.New("action store unavailable")
	}
	if err := g.Actions.Reload(); err != nil {
		result.Error = "durable action state unavailable"
		return result, err
	}

	action, err := g.Actions.Get(actionID)
	if err != nil {
		result.Error = "action not found"
		g.recordLifecycle(actionID, "gateway_invoked", "action lookup failed")
		return result, err
	}
	g.recordLifecycle(actionID, "gateway_invoked", "HHWriteGateway.Send entered")
	if action.Status == HHWriteSent || action.Status == HHWriteSentUnconfirmed || action.Status == HHWriteDeliveryConfirmed {
		result.Success = true
		result.Status = action.Status
		result.ExternalMessageID = action.ExternalMessageID
		result.Timestamp = derefTime(action.SentAt, action.UpdatedAt)
		return result, nil
	}
	if action.Status == HHWriteDeliveryUncertain || action.Status == HHWriteManualReview {
		result.Status = action.Status
		result.Error = "delivery requires manual reconciliation"
		return result, errors.New(result.Error)
	}
	if action.Status != HHWriteApproved {
		result.Status = action.Status
		result.Error = "action is not sendable"
		return result, errors.New(result.Error)
	}
	// Dry-run deliberately exercises the same safety and request gates as a
	// live Send, but stops before capability is granted. A prior preflight is
	// trusted only for this process; after restart (or when no preflight was
	// recorded) the read-only safety validation runs here before the capability
	// check. This keeps the Dashboard rehearsal representative without making
	// a UI click consume a transport nonce.
	if g.DryRun {
		if !g.PreflightIsFresh(action) {
			p, _, _ := g.preflightLocked(ctx, action)
			now := time.Now().UTC()
			action.LastPreflightAt = &now
			action.LastPreflightOK = p.Allowed
			action.LastPreflightReasons = append([]string{}, p.Reasons...)
			if !p.Allowed {
				action.Status = HHWriteStale
				action.Error = strings.Join(p.Reasons, "; ")
				action.UpdatedAt = now
				_ = g.Actions.put(action)
				_ = g.Actions.Save()
				_ = g.Audit.Append(HHWriteEvent{ActionID: action.ID, Type: "preflight_blocked", ConversationID: action.ConversationID, ApplicationID: action.ApplicationID, DraftID: action.DraftID, ContentHash: action.ContentHash, Result: "stale", Error: safeWriteError(errors.New(action.Error))})
				result.Status = HHWriteStale
				result.Error = action.Error
				return result, errors.New("HH write preflight refused")
			}
			_ = g.Actions.put(action)
			_ = g.Actions.Save()
			g.recordLifecycle(actionID, "preflight_passed", "fresh read-only preflight passed")
			_ = g.Audit.Append(HHWriteEvent{ActionID: action.ID, Type: "preflight_passed", ConversationID: action.ConversationID, ApplicationID: action.ApplicationID, DraftID: action.DraftID, ContentHash: action.ContentHash, Result: "preflight_passed"})
		}
		if strings.TrimSpace(action.SendNonce) == "" {
			result.Status = action.Status
			result.Error = "MISSING_NONCE"
			return result, errors.New("send nonce is missing; re-approval is required")
		}
		preview, previewErr := g.requestPreviewLocked(action)
		if previewErr != nil {
			result.Status = HHWriteManualReview
			result.Error = "REQUEST_VALIDATION_FAILED"
			return result, fmt.Errorf("REQUEST_VALIDATION_FAILED: %w", previewErr)
		}
		if err := ValidateHHWriteRequest(preview); err != nil {
			result.Status = HHWriteManualReview
			result.Error = "REQUEST_VALIDATION_FAILED"
			return result, fmt.Errorf("REQUEST_VALIDATION_FAILED: %w", err)
		}
		result.Status = action.Status
		result.Error = "BLOCKED_BY_DRY_RUN"
		result.WriteCapability = "BLOCKED_BY_DRY_RUN"
		g.recordLifecycle(actionID, "capability_blocked", "HH_DRY_RUN=true")
		return result, errors.New("HH_DRY_RUN blocks HH writes")
	}
	if !g.Enabled {
		result.Status = HHWriteStale
		result.Error = "BLOCKED_BY_WRITE_DISABLED"
		result.WriteCapability = "BLOCKED_BY_WRITE_DISABLED"
		g.recordLifecycle(actionID, "capability_blocked", "HH_WRITE_ENABLED=false")
		return result, errors.New("HH writes are disabled")
	}
	if g.Client == nil {
		result.Status = action.Status
		result.Error = "HH writer is unavailable"
		result.WriteCapability = "BLOCKED_BY_WRITE_DISABLED"
		g.recordLifecycle(actionID, "capability_blocked", "HHWriteClient is not configured")
		return result, errors.New(result.Error)
	}
	p, c, _ := g.preflightLocked(ctx, action)
	now := time.Now().UTC()
	action.LastPreflightAt = &now
	action.LastPreflightOK = p.Allowed
	action.LastPreflightReasons = append([]string{}, p.Reasons...)
	if !p.Allowed {
		action.Status = HHWriteStale
		action.Error = strings.Join(p.Reasons, "; ")
		action.UpdatedAt = now
		_ = g.Actions.put(action)
		_ = g.Actions.Save()
		_ = g.Audit.Append(HHWriteEvent{ActionID: action.ID, Type: "preflight_blocked", ConversationID: action.ConversationID, ApplicationID: action.ApplicationID, DraftID: action.DraftID, ContentHash: action.ContentHash, Result: "stale", Error: safeWriteError(errors.New(action.Error))})
		result.Status = HHWriteStale
		result.Error = action.Error
		return result, errors.New("HH write preflight refused")
	}
	g.recordLifecycle(actionID, "preflight_passed", "fresh read-only preflight passed")
	if strings.TrimSpace(action.SendNonce) == "" {
		result.Status = action.Status
		result.Error = "MISSING_NONCE"
		g.recordLifecycle(actionID, "send_failed", "persisted send nonce is missing")
		return result, errors.New("send nonce is missing; re-approval is required")
	}
	if action.NonceUsedAt != nil {
		result.Status = HHWriteManualReview
		result.Error = "send nonce was already used"
		return result, errors.New(result.Error)
	}
	preview, previewErr := g.requestPreviewLocked(action)
	if previewErr != nil {
		action.Status = HHWriteManualReview
		action.Error = safeWriteError(previewErr)
		action.UpdatedAt = time.Now().UTC()
		_ = g.Actions.put(action)
		_ = g.Actions.Save()
		_ = g.Audit.Append(HHWriteEvent{ActionID: action.ID, Type: HHWriteErrorRequestValidationFailed, ConversationID: action.ConversationID, ApplicationID: action.ApplicationID, DraftID: action.DraftID, ContentHash: action.ContentHash, Result: HHWriteErrorRequestValidationFailed, Error: safeWriteError(previewErr)})
		result.Status, result.Error = HHWriteManualReview, "REQUEST_VALIDATION_FAILED"
		return result, fmt.Errorf("REQUEST_VALIDATION_FAILED: %w", previewErr)
	}
	if err := ValidateHHWriteRequest(preview); err != nil {
		action.Status = HHWriteManualReview
		action.Error = safeWriteError(err)
		action.UpdatedAt = time.Now().UTC()
		_ = g.Actions.put(action)
		_ = g.Actions.Save()
		_ = g.Audit.Append(HHWriteEvent{ActionID: action.ID, Type: HHWriteErrorRequestValidationFailed, ConversationID: action.ConversationID, ApplicationID: action.ApplicationID, DraftID: action.DraftID, ContentHash: action.ContentHash, Result: HHWriteErrorRequestValidationFailed, Error: safeWriteError(err)})
		result.Status, result.Error = HHWriteManualReview, "REQUEST_VALIDATION_FAILED"
		return result, fmt.Errorf("REQUEST_VALIDATION_FAILED: %w", err)
	}
	_ = g.Audit.Append(HHWriteEvent{ActionID: action.ID, Type: "preflight_passed", ConversationID: action.ConversationID, ApplicationID: action.ApplicationID, DraftID: action.DraftID, ContentHash: action.ContentHash, Result: "preflight_passed"})
	if err := g.writeLimitError(now); err != nil {
		result.Status = HHWriteApproved
		result.Error = err.Error()
		_ = g.Audit.Append(HHWriteEvent{ActionID: action.ID, Type: "limit_blocked", ConversationID: action.ConversationID, ApplicationID: action.ApplicationID, DraftID: action.DraftID, ContentHash: action.ContentHash, Result: "blocked", Error: err.Error()})
		return result, err
	}
	action.Status = HHWriteSending
	action.NonceUsedAt = &now
	action.UpdatedAt = now
	if err := g.Actions.put(action); err != nil {
		return result, err
	}
	if err := g.Actions.Save(); err != nil {
		return result, err
	}
	g.writesThisRun++
	if err := g.Audit.Append(HHWriteEvent{ActionID: action.ID, Type: "send_started", ConversationID: action.ConversationID, ApplicationID: action.ApplicationID, DraftID: action.DraftID, ApprovedBy: action.ApprovedBy, ApprovedAt: action.ApprovedAt, ContentHash: action.ContentHash, Result: "send_started"}); err != nil {
		action.Status = HHWriteManualReview
		action.Error = "write audit unavailable; HH send was not attempted"
		action.UpdatedAt = time.Now().UTC()
		_ = g.Actions.put(action)
		if saveErr := g.Actions.Save(); saveErr != nil {
			action.Error += "; action persistence failed"
		}
		result.Status = HHWriteManualReview
		result.Error = action.Error
		g.writesThisRun--
		return result, errors.New(action.Error)
	}
	result.TransportAttempted = true
	g.recordLifecycle(actionID, "send_started", "HHWriteClient.SendConversationMessage is being called")
	// The approved one-time nonce is the transport idempotency key. The action
	// identifier is an audit identity and has a different length/contract.
	transport, sendErr := g.Client.SendConversationMessage(ctx, c.HHConversationID, action.ApprovedText, action.SendNonce)
	if sendErr != nil {
		status := HHWriteFailed
		uncertain := false
		retryable := false
		var writeErr *HHWriteError
		if errors.As(sendErr, &writeErr) {
			uncertain, retryable = writeErr.DeliveryUncertain, writeErr.Retryable
		}
		if uncertain {
			status = HHWriteDeliveryUncertain
		}
		action.Status, action.Error, action.UpdatedAt = status, safeWriteError(sendErr), time.Now().UTC()
		_ = g.Actions.put(action)
		_ = g.Actions.Save()
		event := HHWriteEvent{ActionID: action.ID, Type: string(status), ConversationID: action.ConversationID, ApplicationID: action.ApplicationID, DraftID: action.DraftID, ApprovedBy: action.ApprovedBy, ApprovedAt: action.ApprovedAt, ContentHash: action.ContentHash, Result: string(status), Error: safeWriteError(sendErr)}
		populateTransportErrorEvent(&event, sendErr, preview)
		transportEvent := event
		transportEvent.Type = "transport_response"
		transportEvent.Result = event.Error
		transportAuditErr := g.Audit.Append(transportEvent)
		failedAuditErr := g.Audit.Append(event)
		actionSaveErr := g.Actions.Save()
		persistenceErr := errors.Join(actionSaveErr, transportAuditErr, failedAuditErr)
		result.Status, result.Error, result.Retryable = status, safeWriteError(sendErr), retryable
		if persistenceErr != nil {
			// The send already happened. The pre-transport action snapshot is
			// durable and its consumed nonce prevents a retry even if the
			// terminal projection could not be saved. Surface the persistence
			// failure instead of returning a misleading clean transport error.
			result.Status = HHWriteManualReview
			result.Error = safeWriteError(errors.Join(sendErr, fmt.Errorf("HH write failure audit persistence failed: %w", persistenceErr)))
			result.Retryable = false
			return result, errors.Join(sendErr, persistenceErr)
		}
		return result, sendErr
	}
	_ = g.Audit.Append(HHWriteEvent{ActionID: action.ID, Type: "transport_response", ConversationID: action.ConversationID, ApplicationID: action.ApplicationID, DraftID: action.DraftID, ContentHash: action.ContentHash, Result: "accepted", ExternalMessageID: transport.ExternalMessageID, Endpoint: preview.Endpoint, RequestMethod: preview.Method, RequestContentType: preview.ContentType, DestinationType: preview.DestinationType, DestinationID: preview.DestinationID, CorrelationIDs: copyStringMap(transport.Metadata)})
	sentAt := transport.Timestamp
	if sentAt.IsZero() {
		sentAt = time.Now().UTC()
	}
	message := ConversationMessage{ExternalID: transport.ExternalMessageID, Timestamp: sentAt, Sender: ConversationSenderCandidate, Text: action.ApprovedText, Source: ConversationSourceHHWrite, Direction: ConversationOutgoing}
	if _, err := g.Conversations.AppendMessage(c.ID, message); err != nil {
		// HH accepted the write, but the local projection is not trustworthy.
		// Preserve the no-retry invariant and make reconciliation explicit.
		manualReviewErr := errors.New("HH message sent but local conversation update failed; reconcile manually")
		action.Status = HHWriteManualReview
		action.Error = manualReviewErr.Error()
		action.UpdatedAt = time.Now().UTC()
		_ = g.Actions.put(action)
		_ = g.Actions.Save()
		_ = g.Audit.Append(HHWriteEvent{ActionID: action.ID, Type: string(HHWriteManualReview), ConversationID: action.ConversationID, ApplicationID: action.ApplicationID, DraftID: action.DraftID, ContentHash: action.ContentHash, Result: string(HHWriteManualReview), Error: manualReviewErr.Error(), ExternalMessageID: transport.ExternalMessageID})
		result.Status = HHWriteManualReview
		result.ExternalMessageID = transport.ExternalMessageID
		result.Error = manualReviewErr.Error()
		return result, manualReviewErr
	}
	updated, _ := g.Conversations.GetConversation(c.ID)
	state := (ConversationStateResolver{}).Resolve(func() JobApplication {
		if g.Applications != nil && action.ApplicationID != "" {
			a, _ := g.Applications.GetApplication(action.ApplicationID)
			return a
		}
		return JobApplication{}
	}(), updated, nil, false, nil, time.Now().UTC())
	conversationState := ConversationState{Status: state.Status, NextAction: updated.NextAction, WaitingSince: state.WaitingSince, FollowUpState: updated.FollowUpState}
	if action.ActionType == HHActionFollowUp {
		conversationState.FollowUpState = ConversationFollowUpSent
	}
	_ = g.Conversations.UpdateConversationState(c.ID, conversationState)
	if g.Conversations != nil {
		_ = g.Conversations.Save()
	}
	if g.Applications != nil {
		if action.ActionType == HHActionFollowUp && action.ApplicationID != "" {
			_ = g.Applications.SetFollowUpState(action.ApplicationID, ConversationFollowUpSent)
			_ = g.Applications.RecordEvent(action.ApplicationID, sentAt, ApplicationEventFollowUpSent, "follow-up sent through HH Write Gateway")
		}
		_ = g.Applications.Save()
	}
	action.Status, action.SentAt, action.ExternalMessageID, action.UpdatedAt = HHWriteSent, &sentAt, transport.ExternalMessageID, time.Now().UTC()
	_ = g.Actions.put(action)
	_ = g.Actions.Save()
	_ = g.Drafts.SetStatus(action.DraftID, AIDraftSent)
	_ = g.Drafts.Save()
	_ = g.Audit.Append(HHWriteEvent{ActionID: action.ID, Type: "sent", ConversationID: action.ConversationID, ApplicationID: action.ApplicationID, DraftID: action.DraftID, ApprovedBy: action.ApprovedBy, ApprovedAt: action.ApprovedAt, SentAt: &sentAt, ContentHash: action.ContentHash, Result: "sent", ExternalMessageID: transport.ExternalMessageID})
	result.Success, result.Status, result.ExternalMessageID, result.Timestamp, result.Metadata = true, HHWriteSent, transport.ExternalMessageID, sentAt, transport.Metadata
	// A successful HTTP response is not delivery confirmation. Perform only a
	// fresh read here; an absent/failed read becomes sent_unconfirmed and is
	// never retried automatically.
	confirmed := false
	if strings.TrimSpace(transport.ExternalMessageID) != "" {
		if fresh, readErr := g.freshState(ctx, c); readErr == nil {
			confirmed = fresh.LastMessageID == transport.ExternalMessageID
			if !confirmed {
				for _, id := range fresh.MessageIDs {
					if id == transport.ExternalMessageID {
						confirmed = true
						break
					}
				}
			}
		}
	}
	if confirmed {
		action.Status = HHWriteDeliveryConfirmed
		result.Status = HHWriteDeliveryConfirmed
		_ = g.Audit.Append(HHWriteEvent{ActionID: action.ID, Type: "delivery_confirmed", ConversationID: action.ConversationID, ApplicationID: action.ApplicationID, DraftID: action.DraftID, ContentHash: action.ContentHash, Result: string(HHWriteDeliveryConfirmed), ExternalMessageID: transport.ExternalMessageID})
	} else {
		action.Status = HHWriteSentUnconfirmed
		result.Status = HHWriteSentUnconfirmed
		_ = g.Audit.Append(HHWriteEvent{ActionID: action.ID, Type: string(HHWriteSentUnconfirmed), ConversationID: action.ConversationID, ApplicationID: action.ApplicationID, DraftID: action.DraftID, ContentHash: action.ContentHash, Result: string(HHWriteSentUnconfirmed), ExternalMessageID: transport.ExternalMessageID, Error: "read-only delivery confirmation unavailable"})
	}
	_ = g.Actions.put(action)
	_ = g.Actions.Save()
	g.recordPilotObservation(action, result.Status, state.Status, confirmed)
	return result, nil
}

func (g *HHWriteGateway) recordLifecycle(actionID, eventType, reason string) {
	if g != nil && g.Lifecycle != nil {
		g.Lifecycle.Record(actionID, eventType, reason)
	}
}

func (g *HHWriteGateway) recordPilotObservation(action ApprovedHHAction, deliveryStatus HHWriteActionStatus, conversationStatus ConversationStatus, confirmed bool) {
	if g == nil || g.Observations == nil {
		return
	}
	observation := PilotObservation{
		ActionID: action.ID, DraftGenerated: true, PreflightPassed: true,
		DeliveryState: string(deliveryStatus), ReconciliationSuccess: confirmed,
		ConversationStateAfter: string(conversationStatus), Issues: []string{}, CreatedAt: time.Now().UTC(),
	}
	if draft, err := g.Drafts.Get(action.DraftID); err == nil {
		observation.UserEdited = draft.Source == AIDraftSourceUserEdited
	}
	if !confirmed {
		observation.Issues = append(observation.Issues, "delivery confirmation unavailable")
	}
	_ = g.Observations.Append(observation)
}

func (g *HHWriteGateway) SendAction(ctx context.Context, actionID string) (HHWriteResult, error) {
	return g.Send(ctx, actionID)
}

// ReconcileDelivery is read-only. It is the only safe next step after a
// response-loss/uncertain result; it never sends again.
func (g *HHWriteGateway) ReconcileDelivery(ctx context.Context, actionID string) (HHWriteResult, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	action, err := g.Actions.Get(actionID)
	result := HHWriteResult{ActionID: actionID, Timestamp: time.Now().UTC()}
	if err != nil {
		result.Error = "action not found"
		return result, err
	}
	if action.Status != HHWriteDeliveryUncertain && action.Status != HHWriteManualReview && action.Status != HHWriteSentUnconfirmed {
		result.Status = action.Status
		result.Success = action.Status == HHWriteSent || action.Status == HHWriteDeliveryConfirmed
		result.ExternalMessageID = action.ExternalMessageID
		return result, nil
	}
	reader, ok := g.ReadClient.(HHConversationPreflightReader)
	if !ok || strings.TrimSpace(action.ExternalMessageID) == "" {
		result.Status, result.Error = action.Status, "delivery reconciliation is unavailable"
		return result, errors.New(result.Error)
	}
	c, err := g.Conversations.GetConversation(action.ConversationID)
	if err != nil {
		result.Status, result.Error = action.Status, "conversation not found for reconciliation"
		return result, err
	}
	fresh, err := reader.ReadConversationState(ctx, c.HHConversationID)
	if err != nil {
		result.Status, result.Error = action.Status, "delivery reconciliation read failed"
		return result, err
	}
	found := fresh.LastMessageID == action.ExternalMessageID
	for _, id := range fresh.MessageIDs {
		found = found || id == action.ExternalMessageID
	}
	if !found {
		result.Status, result.Error = action.Status, "delivery not confirmed; automatic retry is forbidden"
		return result, errors.New(result.Error)
	}
	action.Status, action.Error, action.UpdatedAt = HHWriteDeliveryConfirmed, "", time.Now().UTC()
	if err := g.Actions.put(action); err != nil {
		return result, err
	}
	if err := g.Actions.Save(); err != nil {
		return result, err
	}
	_ = g.Audit.Append(HHWriteEvent{ActionID: action.ID, Type: "delivery_confirmed", ConversationID: action.ConversationID, ApplicationID: action.ApplicationID, DraftID: action.DraftID, ContentHash: action.ContentHash, Result: string(HHWriteDeliveryConfirmed), ExternalMessageID: action.ExternalMessageID})
	g.recordPilotObservation(action, HHWriteDeliveryConfirmed, c.Status, true)
	result.Success, result.Status, result.ExternalMessageID = true, HHWriteDeliveryConfirmed, action.ExternalMessageID
	return result, nil
}

func (g *HHWriteGateway) Cancel(actionID string) (ApprovedHHAction, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	action, err := g.Actions.Get(actionID)
	if err != nil {
		return ApprovedHHAction{}, err
	}
	if action.Status != HHWriteApproved && action.Status != HHWritePending && action.Status != HHWriteStale {
		return ApprovedHHAction{}, errors.New("only an approved action can be cancelled")
	}
	action.Status, action.UpdatedAt = HHWriteCancelled, time.Now().UTC()
	if err := g.Actions.put(action); err != nil {
		return ApprovedHHAction{}, err
	}
	if err := g.Actions.Save(); err != nil {
		return ApprovedHHAction{}, err
	}
	_ = g.Audit.Append(HHWriteEvent{ActionID: action.ID, Type: "cancelled", ConversationID: action.ConversationID, ApplicationID: action.ApplicationID, DraftID: action.DraftID, ApprovedBy: action.ApprovedBy, ApprovedAt: action.ApprovedAt, ContentHash: action.ContentHash, Result: "cancelled"})
	return action, nil
}

func derefTime(value *time.Time, fallback time.Time) time.Time {
	if value != nil {
		return *value
	}
	return fallback
}
func safeWriteError(err error) string {
	if err == nil {
		return ""
	}
	if profileContainsSecret([]byte(err.Error())) {
		return "HH write failed"
	}
	return err.Error()
}

// HHAIResponderWriteClient is the only adapter from the legacy HTTP transport
// into HHWriteClient. It never exposes application, resume, status, test or
// leave-chat operations.
type HHAIResponderWriteClient struct{ responder *HHAIResponder }

func NewHHAIResponderWriteClient(responder *HHAIResponder) *HHAIResponderWriteClient {
	return &HHAIResponderWriteClient{responder: responder}
}
func (c *HHAIResponderWriteClient) SendConversationMessage(ctx context.Context, conversationID, text, idempotencyKey string) (HHWriteTransportResult, error) {
	if c == nil || c.responder == nil {
		return HHWriteTransportResult{}, errors.New("HH writer is not configured")
	}
	if err := ctx.Err(); err != nil {
		return HHWriteTransportResult{}, err
	}
	if c.responder.dryRun {
		return HHWriteTransportResult{}, errors.New("HH_DRY_RUN blocks HH writes")
	}
	chatID, err := strconv.ParseInt(strings.TrimSpace(conversationID), 10, 64)
	if err != nil || chatID <= 0 {
		return HHWriteTransportResult{}, errors.New("HH conversation external id is not numeric")
	}
	return c.responder.sendConversationMessage(chatID, text, idempotencyKey)
}

func (r *HHAIResponder) sendConversationMessage(chatID int64, text, idempotencyKey string) (HHWriteTransportResult, error) {
	preview, body, err := r.buildHHWriteRequest(chatID, text, idempotencyKey)
	if err != nil {
		if transportErr, ok := err.(*HHWriteTransportError); ok {
			return HHWriteTransportResult{}, transportErr
		}
		return HHWriteTransportResult{}, &HHWriteTransportError{Category: HHWriteErrorRequestValidationFailed, Err: err}
	}
	req, err := r.newHHWriteHTTPRequest(*preview, body)
	if err != nil {
		return HHWriteTransportResult{}, &HHWriteTransportError{Category: HHWriteErrorRequestValidationFailed, Err: err}
	}
	resp, err := r.requester.Do(req)
	if err != nil {
		return HHWriteTransportResult{}, &HHWriteTransportError{Category: HHWriteErrorNetworkUncertain, Err: err, DeliveryUncertain: true}
	}
	if resp.Status < 200 || resp.Status >= 300 {
		category := classifyHHWriteHTTPStatus(resp.Status)
		return HHWriteTransportResult{}, &HHWriteTransportError{
			Category:            category,
			Status:              resp.Status,
			ResponseContentType: resp.ContentType,
			ResponseBody:        sanitizeHHResponseBody(resp.Body, resp.ContentType),
			HHErrorFields:       sanitizedHHErrorFields(resp.Body, resp.ContentType),
			CorrelationIDs:      copyStringMap(resp.CorrelationIDs),
			Err:                 fmt.Errorf("HH returned status %d", resp.Status),
			Retryable:           resp.Status >= 500,
		}
	}
	var result map[string]any
	if err := json.Unmarshal(resp.Body, &result); err != nil {
		return HHWriteTransportResult{}, &HHWriteTransportError{Category: HHWriteErrorDeliveryUncertain, Status: resp.Status, ResponseContentType: resp.ContentType, ResponseBody: sanitizeHHResponseBody(resp.Body, resp.ContentType), CorrelationIDs: copyStringMap(resp.CorrelationIDs), Err: errors.New("invalid HH write response"), DeliveryUncertain: true}
	}
	if _, ok := result["error"]; ok {
		return HHWriteTransportResult{}, &HHWriteTransportError{Category: HHWriteErrorBadRequest, Status: resp.Status, ResponseContentType: resp.ContentType, ResponseBody: sanitizeHHResponseBody(resp.Body, resp.ContentType), HHErrorFields: sanitizedHHErrorFields(resp.Body, resp.ContentType), CorrelationIDs: copyStringMap(resp.CorrelationIDs), Err: errors.New("HH write rejected")}
	}
	externalID := firstMapString(result, "messageId", "message_id", "id")
	if strings.TrimSpace(externalID) == "" {
		return HHWriteTransportResult{}, &HHWriteTransportError{Category: HHWriteErrorDeliveryUncertain, Status: resp.Status, ResponseContentType: resp.ContentType, ResponseBody: sanitizeHHResponseBody(resp.Body, resp.ContentType), CorrelationIDs: copyStringMap(resp.CorrelationIDs), Err: errors.New("HH write response has no external message id"), DeliveryUncertain: true}
	}
	return HHWriteTransportResult{ExternalMessageID: externalID, Timestamp: time.Now().UTC(), Metadata: copyStringMap(resp.CorrelationIDs)}, nil
}

func classifyHHWriteHTTPStatus(status int) string {
	switch {
	case status == http.StatusUnauthorized:
		return HHWriteErrorAuthenticationFailed
	case status == http.StatusForbidden:
		return HHWriteErrorPermissionDenied
	case status == http.StatusTooManyRequests:
		return HHWriteErrorRateLimited
	case status >= 500:
		return HHWriteErrorServerError
	default:
		return HHWriteErrorBadRequest
	}
}

func firstMapString(value map[string]any, keys ...string) string {
	for _, key := range keys {
		if raw, ok := value[key]; ok {
			if s, ok := raw.(string); ok {
				return s
			}
			if f, ok := raw.(float64); ok {
				return strconv.FormatInt(int64(f), 10)
			}
		}
	}
	return ""
}
