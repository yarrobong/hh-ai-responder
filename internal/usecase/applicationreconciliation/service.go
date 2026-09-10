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
)

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
		ProviderApplicationID: provider.ApplicationID, ProviderNegotiationID: provider.NegotiationID,
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
	positiveCount := 0
	for _, value := range snapshot.Applications {
		if value.VacancyID == vacancyID && value.ResponseByApplicant && (strings.TrimSpace(value.ApplicationID) != "" || strings.TrimSpace(value.NegotiationID) != "") {
			positive = value
			positiveCount++
		}
	}
	if positiveCount > 1 {
		return domain.EvidenceConflicting, domain.EvidenceAbsent, "multiple conflicting provider response identities", ProviderResponse{}
	}
	preflightPositive := snapshot.PreflightAvailable && snapshot.Preflight.AlreadyRespondedKnown && snapshot.Preflight.AlreadyResponded
	preflightApplicable := snapshot.PreflightAvailable && snapshot.Preflight.AlreadyRespondedKnown && !snapshot.Preflight.AlreadyResponded
	if positiveCount == 1 && preflightApplicable {
		return domain.EvidenceConflicting, domain.EvidenceAbsent, "fresh preflight says applicable while negotiation evidence says response exists", ProviderResponse{}
	}
	// Explicit provider response identity has precedence over a generic
	// already-responded marker, but only when no contradictory explicit
	// applicable state is present.
	if positiveCount == 1 {
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
