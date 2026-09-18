package applicationreconciliation

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	domain "hh-ai-responder/internal/applicationattempt"
	reliabilitynotifications "hh-ai-responder/internal/usecase/reliabilitynotifications"
)

var (
	ErrNotConfigured          = errors.New("application reconciliation is not configured")
	ErrEvidenceUnavailable    = errors.New("application reconciliation evidence is unavailable")
	ErrPersistenceUncertain   = errors.New("application reconciliation persistence is uncertain")
	ErrAttemptNotReconcilable = errors.New("application attempt is not reconcilable")
	ErrInvalidPassLimit       = errors.New("application reconciliation pass limit must be positive")
)

const DefaultMaxReconciliationPasses = 3

type Dependencies struct {
	Attempts      AttemptStore
	Reader        EvidenceReader
	Now           func() time.Time
	Notifications reliabilitynotifications.Sink
}

type Service struct {
	deps Dependencies
}

func NewService(deps Dependencies) *Service {
	if deps.Now == nil {
		deps.Now = func() time.Time { return time.Now().UTC() }
	}
	return &Service{deps: deps}
}

func (s *Service) Reconcile(ctx context.Context, attemptID string) (Result, error) {
	if ctx == nil {
		return Result{}, context.Canceled
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if s == nil || s.deps.Attempts == nil || s.deps.Reader == nil {
		return Result{}, ErrNotConfigured
	}
	attempt, err := s.deps.Attempts.Get(ctx, strings.TrimSpace(attemptID))
	if err != nil {
		return Result{}, err
	}
	return s.reconcileAttempt(ctx, attempt)
}

// ConfirmManual records an explicit operator/provider verification as terminal
// evidence. It performs no provider read and has no writer capability; the
// only mutation is the local durable attempt record.
func (s *Service) ConfirmManual(ctx context.Context, attemptID string, confirmation ManualConfirmation) (Result, error) {
	if ctx == nil {
		return Result{}, context.Canceled
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if s == nil || s.deps.Attempts == nil {
		return Result{}, ErrNotConfigured
	}
	if strings.TrimSpace(confirmation.ProviderNegotiationID) == "" || strings.TrimSpace(confirmation.ProviderConversationID) == "" {
		return Result{}, errors.New("manual provider verification requires negotiation and conversation identities")
	}
	attempt, err := s.deps.Attempts.Get(ctx, strings.TrimSpace(attemptID))
	if err != nil {
		return Result{}, err
	}
	if !reconcilable(attempt.State) {
		return Result{Attempt: attempt, PreviousState: attempt.State, Status: StatusNotApplicable, Reason: "attempt is already terminal or not an unresolved automatic attempt"}, ErrAttemptNotReconcilable
	}
	now := s.deps.Now().UTC()
	evidence := domain.ReconciliationEvidence{
		Kind:                   domain.EvidenceConfirmedResponseExists,
		Strength:               domain.EvidenceStrong,
		Source:                 domain.EvidenceSourceManualProviderVerification,
		ConfirmationSource:     domain.EvidenceSourceManualProviderVerification,
		ProviderNegotiationID:  strings.TrimSpace(confirmation.ProviderNegotiationID),
		ProviderConversationID: strings.TrimSpace(confirmation.ProviderConversationID),
		ObservedAt:             now,
		ProviderIdentities: []domain.ProviderIdentity{
			{Type: "negotiation", Value: strings.TrimSpace(confirmation.ProviderNegotiationID), Source: domain.EvidenceSourceManualProviderVerification, VacancyID: attempt.VacancyID},
			{Type: "conversation", Value: strings.TrimSpace(confirmation.ProviderConversationID), Source: domain.EvidenceSourceManualProviderVerification, VacancyID: attempt.VacancyID},
		},
	}
	if err := s.deps.Attempts.RecordReconciliation(ctx, attempt.AttemptID, evidence, now); err != nil {
		return Result{Attempt: attempt, PreviousState: attempt.State, Status: StatusConfirmed, Evidence: evidence, Reason: "manual confirmation could not be persisted"}, fmt.Errorf("%w: %v", ErrPersistenceUncertain, err)
	}
	updated, getErr := s.deps.Attempts.Get(ctx, attempt.AttemptID)
	if getErr != nil {
		return Result{Attempt: attempt, PreviousState: attempt.State, Status: StatusConfirmed, Evidence: evidence, Reason: "manual confirmation persisted but reload was uncertain"}, fmt.Errorf("%w: %v", ErrPersistenceUncertain, getErr)
	}
	result := Result{Status: StatusConfirmed, Attempt: updated, PreviousState: attempt.State, Evidence: evidence, Reason: "operator manually verified the HH application and exact cover letter"}
	s.project(ctx, reliabilitynotifications.ApplicationReconciliation{AttemptID: attempt.AttemptID, VacancyID: attempt.VacancyID, Status: string(StatusConfirmed)})
	return result, nil
}

// ReconcileBounded performs GET-only reconciliation passes and stops as soon
// as provider evidence confirms the target. Unresolved or conflicting reads
// never release the blocking attempt and never authorize a retry.
func (s *Service) ReconcileBounded(ctx context.Context, attemptID string, maxPasses int) (Result, error) {
	if maxPasses <= 0 {
		return Result{}, ErrInvalidPassLimit
	}
	var last Result
	var lastErr error
	for pass := 0; pass < maxPasses; pass++ {
		result, err := s.Reconcile(ctx, attemptID)
		last, lastErr = result, err
		if result.Status == StatusConfirmed || ctx.Err() != nil {
			return result, err
		}
	}
	return last, lastErr
}

// ReconcileVacancy is the explicit operator/runtime entry point for an
// existing blocking target. It never creates or releases an attempt.
func (s *Service) ReconcileVacancy(ctx context.Context, vacancyID int) (Result, error) {
	if ctx == nil {
		return Result{}, context.Canceled
	}
	if s == nil || s.deps.Attempts == nil || s.deps.Reader == nil {
		return Result{}, ErrNotConfigured
	}
	attempt, err := s.deps.Attempts.FindBlocking(ctx, vacancyID)
	if err != nil {
		return Result{Status: StatusNotApplicable}, err
	}
	return s.reconcileAttempt(ctx, attempt)
}

func (s *Service) reconcileAttempt(ctx context.Context, attempt domain.Attempt) (Result, error) {
	result := Result{Attempt: attempt, PreviousState: attempt.State}
	if !reconcilable(attempt.State) {
		result.Status = StatusNotApplicable
		result.Reason = "attempt is already terminal or not an unresolved automatic attempt"
		return result, ErrAttemptNotReconcilable
	}
	snapshot, readErr := s.deps.Reader.ReadVacancyResponseEvidence(ctx, Target{VacancyID: attempt.VacancyID, ResumeID: attempt.ResumeID})
	result.ReadAttempted = true
	kind, strength, source, provider := classify(snapshot, attempt.VacancyID)
	evidence := domain.ReconciliationEvidence{
		Kind: kind, Strength: strength, Source: source, ObservedAt: observedAt(snapshot, s.deps.Now),
		ProviderApplicationID: provider.ApplicationID, ProviderNegotiationID: provider.NegotiationID, ProviderConversationID: provider.ConversationID,
		ProviderIdentities: providerIdentities(provider, attempt.VacancyID),
		ProviderResponseAt: provider.ProviderResponseAt,
	}
	result.Evidence = evidence
	result.Status = statusFor(kind)
	result.Reason = reasonFor(kind, snapshot, readErr)
	if readErr != nil && kind != domain.EvidenceConfirmedResponseExists && attempt.State != domain.StateTargetResponseConfirmed {
		result.Status = StatusUnavailable
		result.Evidence.Kind = domain.EvidenceUnavailable
		result.Evidence.Strength = domain.EvidenceAbsent
		if result.Reason == "" {
			result.Reason = readErr.Error()
		}
	}
	if attempt.State == domain.StateTargetResponseConfirmed && kind != domain.EvidenceConfirmedResponseExists {
		// A later weak or unavailable read cannot downgrade a prior positive
		// observation or make the target replayable.
		result.Status = StatusConfirmed
		result.Reason = "target response was already confirmed; later evidence cannot downgrade the blocking state"
	}
	if err := s.deps.Attempts.RecordReconciliation(ctx, attempt.AttemptID, result.Evidence, s.deps.Now()); err != nil {
		s.project(ctx, reliabilitynotifications.ApplicationReconciliation{AttemptID: attempt.AttemptID, VacancyID: attempt.VacancyID, Status: "PERSISTENCE_ERROR"})
		return result, fmt.Errorf("%w: %v", ErrPersistenceUncertain, err)
	}
	updated, getErr := s.deps.Attempts.Get(ctx, attempt.AttemptID)
	if getErr == nil {
		result.Attempt = updated
	}
	if result.Status == StatusUnavailable {
		return result, fmt.Errorf("%w: %v", ErrEvidenceUnavailable, firstNonEmpty(result.Reason, "provider evidence unavailable"))
	}
	s.project(ctx, reliabilitynotifications.ApplicationReconciliation{AttemptID: attempt.AttemptID, VacancyID: attempt.VacancyID, Status: string(result.Status)})
	return result, nil
}

func (s *Service) project(ctx context.Context, event reliabilitynotifications.ApplicationReconciliation) {
	if s != nil && s.deps.Notifications != nil {
		_ = s.deps.Notifications.ProjectApplicationReconciliation(ctx, event)
	}
}

func classify(snapshot EvidenceSnapshot, vacancyID int) (domain.EvidenceKind, domain.EvidenceStrength, string, ProviderResponse) {
	if snapshot.VacancyID != 0 && snapshot.VacancyID != vacancyID {
		return domain.EvidenceConflicting, domain.EvidenceAbsent, "target vacancy identity conflict", ProviderResponse{}
	}
	var positive ProviderResponse
	positiveResponses := make([]ProviderResponse, 0, len(snapshot.Applications))
	seenResponses := map[string]struct{}{}
	for _, value := range snapshot.Applications {
		if value.VacancyID != vacancyID || !value.ResponseByApplicant || !hasProviderIdentity(value) {
			continue
		}
		key := providerResponseKey(value)
		if _, exists := seenResponses[key]; exists {
			continue
		}
		seenResponses[key] = struct{}{}
		positiveResponses = append(positiveResponses, value)
	}
	identities := mergeProviderIdentities(positiveResponses, vacancyID)
	positive = aggregateProviderResponse(positiveResponses, identities)
	positiveCount := len(positiveResponses)
	if hasSameTypeIdentityConflict(identities) {
		return domain.EvidenceConflicting, domain.EvidenceAbsent, "multiple conflicting provider identities of the same type", positive
	}
	preflightPositive := snapshot.PreflightAvailable && snapshot.Preflight.AlreadyRespondedKnown && snapshot.Preflight.AlreadyResponded
	preflightApplicable := snapshot.PreflightAvailable && snapshot.Preflight.AlreadyRespondedKnown && !snapshot.Preflight.AlreadyResponded
	if positiveCount > 0 && preflightApplicable {
		return domain.EvidenceConflicting, domain.EvidenceAbsent, "fresh preflight says applicable while negotiation evidence says response exists", positive
	}
	// Explicit provider response identity has precedence over a generic
	// already-responded marker, but only when no contradictory explicit
	// applicable state is present.
	if positiveCount > 0 {
		return domain.EvidenceConfirmedResponseExists, domain.EvidenceStrong, "application/negotiation response-by-applicant identity", positive
	}
	if preflightPositive {
		return domain.EvidenceConfirmedResponseExists, domain.EvidenceStrong, "fresh vacancy preflight already-responded marker", ProviderResponse{}
	}
	if snapshot.PreflightAvailable && snapshot.ApplicationsAvailable {
		return domain.EvidenceInsufficient, domain.EvidenceAbsent, "fresh reads contain no sufficient positive response evidence", ProviderResponse{}
	}
	return domain.EvidenceUnavailable, domain.EvidenceAbsent, "one or more fresh evidence sources are unavailable", ProviderResponse{}
}

func hasProviderIdentity(value ProviderResponse) bool {
	return strings.TrimSpace(value.ApplicationID) != "" || strings.TrimSpace(value.NegotiationID) != "" || strings.TrimSpace(value.ConversationID) != ""
}

func providerResponseKey(value ProviderResponse) string {
	identities := providerIdentities(value, value.VacancyID)
	parts := make([]string, 0, len(identities))
	for _, identity := range identities {
		parts = append(parts, identity.Type+"\x00"+identity.Value)
	}
	return strings.Join(parts, "\x01")
}

func providerIdentities(value ProviderResponse, vacancyID int) []domain.ProviderIdentity {
	if len(value.Identities) > 0 {
		return append([]domain.ProviderIdentity(nil), value.Identities...)
	}
	identities := make([]domain.ProviderIdentity, 0, 3)
	appendIdentity := func(identityType, identityValue string) {
		identityValue = strings.TrimSpace(identityValue)
		if identityValue == "" {
			return
		}
		identities = append(identities, domain.ProviderIdentity{Type: identityType, Value: identityValue, Source: firstNonEmpty(value.Source, "unknown"), VacancyID: vacancyID})
	}
	appendIdentity("application", value.ApplicationID)
	appendIdentity("negotiation", value.NegotiationID)
	appendIdentity("conversation", value.ConversationID)
	return identities
}

func mergeProviderIdentities(values []ProviderResponse, vacancyID int) []domain.ProviderIdentity {
	result := make([]domain.ProviderIdentity, 0)
	seen := map[string]struct{}{}
	for _, value := range values {
		for _, identity := range providerIdentities(value, vacancyID) {
			key := identity.Type + "\x00" + identity.Value + "\x00" + identity.Source
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			result = append(result, identity)
		}
	}
	return result
}

func aggregateProviderResponse(values []ProviderResponse, identities []domain.ProviderIdentity) ProviderResponse {
	if len(values) == 0 {
		return ProviderResponse{Identities: nil}
	}
	result := values[0]
	result.Identities = append([]domain.ProviderIdentity(nil), identities...)
	for _, identity := range identities {
		switch identity.Type {
		case "application":
			if result.ApplicationID == "" {
				result.ApplicationID = identity.Value
			}
		case "negotiation":
			if result.NegotiationID == "" {
				result.NegotiationID = identity.Value
			}
		case "conversation":
			if result.ConversationID == "" {
				result.ConversationID = identity.Value
			}
		}
	}
	return result
}

func hasSameTypeIdentityConflict(identities []domain.ProviderIdentity) bool {
	seen := map[string]string{}
	for _, identity := range identities {
		if previous, exists := seen[identity.Type]; exists && previous != identity.Value {
			return true
		}
		seen[identity.Type] = identity.Value
	}
	return false
}

func reconcilable(state domain.State) bool {
	return state == domain.StateSending || state == domain.StateDeliveryUncertain || state == domain.StateAccepted || state == domain.StateTargetResponseConfirmed
}

func statusFor(kind domain.EvidenceKind) Status {
	switch kind {
	case domain.EvidenceConfirmedResponseExists:
		return StatusConfirmed
	case domain.EvidenceConflicting:
		return StatusConflicting
	case domain.EvidenceUnavailable:
		return StatusUnavailable
	default:
		return StatusInsufficient
	}
}

func observedAt(snapshot EvidenceSnapshot, now func() time.Time) time.Time {
	if !snapshot.ObservedAt.IsZero() {
		return snapshot.ObservedAt.UTC()
	}
	return now().UTC()
}

func reasonFor(kind domain.EvidenceKind, snapshot EvidenceSnapshot, readErr error) string {
	if kind == domain.EvidenceConfirmedResponseExists {
		return "target vacancy response is confirmed by fresh provider evidence; exact local attempt causality is not established"
	}
	if kind == domain.EvidenceConflicting {
		return "same-vacancy negotiation evidence conflicts with fresh preflight applicability; delivery remains unresolved"
	}
	if readErr != nil {
		return readErr.Error()
	}
	if snapshot.PreflightError != "" {
		return snapshot.PreflightError
	}
	if snapshot.ApplicationsError != "" {
		return snapshot.ApplicationsError
	}
	return "absence of provider evidence is not proof of non-delivery"
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
