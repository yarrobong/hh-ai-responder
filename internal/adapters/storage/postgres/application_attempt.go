package postgresstorage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	domain "hh-ai-responder/internal/applicationattempt"
	attemptport "hh-ai-responder/internal/ports/applicationattempt"
)

const applicationAttemptColumns = "attempt_id, vacancy_id, resume_id, state, created_at, updated_at, provider_status, error_class, reconciliation_kind, reconciliation_strength, reconciliation_source, provider_application_id, provider_negotiation_id, provider_conversation_id, reconciliation_confirmation_source, reconciliation_provider_identities, provider_response_at, reconciliation_observed_at, reconciliation_history"

type ApplicationAttemptRepository struct {
	pool *pgxpool.Pool
}

func NewApplicationAttemptRepository(pool *pgxpool.Pool) *ApplicationAttemptRepository {
	return &ApplicationAttemptRepository{pool: pool}
}

func (r *ApplicationAttemptRepository) Reserve(ctx context.Context, value domain.Attempt) (attemptport.ReserveResult, error) {
	ctx = postgresContext(ctx)
	if err := repositoryContextErr(ctx); err != nil {
		return attemptport.ReserveResult{}, err
	}
	if err := value.Validate(); err != nil || value.State != domain.StateSending {
		return attemptport.ReserveResult{}, fmt.Errorf("reserve application attempt: %w", domain.ErrInvalidAttempt)
	}
	if r == nil || r.pool == nil {
		return attemptport.ReserveResult{}, errors.New("application attempt repository is not configured")
	}
	stored, err := scanAttempt(r.pool.QueryRow(ctx, `INSERT INTO automatic_application_attempts (`+applicationAttemptColumns+`)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19)
		ON CONFLICT DO NOTHING
		RETURNING `+applicationAttemptColumns,
		value.AttemptID, value.VacancyID, value.ResumeID, string(value.State), value.CreatedAt, value.UpdatedAt, value.ProviderStatus, value.ErrorClass,
		"", "", "", "", "", "", "", []byte("[]"), nil, nil, []byte("[]"),
	))
	if err == nil {
		// The initial reservation has no reconciliation evidence.
		return attemptport.ReserveResult{Reserved: true, Attempt: stored}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return attemptport.ReserveResult{}, fmt.Errorf("reserve application attempt: %w", err)
	}
	existing, lookupErr := r.blockingForVacancy(ctx, value.VacancyID)
	if lookupErr == nil {
		return attemptport.ReserveResult{Existing: &existing}, fmt.Errorf("%w: attempt %s", domain.ErrTargetBlocked, existing.AttemptID)
	}
	if !errors.Is(lookupErr, domain.ErrAttemptNotFound) {
		return attemptport.ReserveResult{}, lookupErr
	}
	return attemptport.ReserveResult{}, errors.New("application attempt reservation conflict")
}

func (r *ApplicationAttemptRepository) RecordOutcome(ctx context.Context, attemptID string, state domain.State, updatedAt time.Time, providerStatus int, errorClass string) error {
	ctx = postgresContext(ctx)
	if err := repositoryContextErr(ctx); err != nil {
		return err
	}
	if strings.TrimSpace(attemptID) == "" {
		return domain.ErrAttemptNotFound
	}
	if r == nil || r.pool == nil {
		return errors.New("application attempt repository is not configured")
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin application attempt outcome: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var current domain.Attempt
	current, err = scanAttempt(tx.QueryRow(ctx, `SELECT `+applicationAttemptColumns+` FROM automatic_application_attempts WHERE attempt_id=$1 FOR UPDATE`))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrAttemptNotFound
	}
	if err != nil {
		return fmt.Errorf("read application attempt outcome: %w", err)
	}
	updated, err := current.WithOutcome(state, updatedAt, providerStatus, errorClass)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE automatic_application_attempts SET state=$2, updated_at=$3, provider_status=$4, error_class=$5 WHERE attempt_id=$1`, attemptID, string(updated.State), updated.UpdatedAt, updated.ProviderStatus, updated.ErrorClass); err != nil {
		return fmt.Errorf("record application attempt outcome: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit application attempt outcome: %w", err)
	}
	return nil
}

func (r *ApplicationAttemptRepository) FindBlocking(ctx context.Context, vacancyID int) (domain.Attempt, error) {
	ctx = postgresContext(ctx)
	if err := repositoryContextErr(ctx); err != nil {
		return domain.Attempt{}, err
	}
	if vacancyID <= 0 {
		return domain.Attempt{}, domain.ErrAttemptNotFound
	}
	if r == nil || r.pool == nil {
		return domain.Attempt{}, errors.New("application attempt repository is not configured")
	}
	return findBlockingAttempt(r.pool.QueryRow(ctx, `SELECT `+applicationAttemptColumns+` FROM automatic_application_attempts WHERE vacancy_id=$1 AND state IN ('SENDING','ACCEPTED','DELIVERY_UNCERTAIN','TARGET_RESPONSE_CONFIRMED') ORDER BY created_at, attempt_id LIMIT 1`, vacancyID))
}

func (r *ApplicationAttemptRepository) RecordReconciliation(ctx context.Context, attemptID string, evidence domain.ReconciliationEvidence, now time.Time) error {
	ctx = postgresContext(ctx)
	if err := repositoryContextErr(ctx); err != nil {
		return err
	}
	if strings.TrimSpace(attemptID) == "" {
		return domain.ErrAttemptNotFound
	}
	if r == nil || r.pool == nil {
		return errors.New("application attempt repository is not configured")
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin application attempt reconciliation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	current, err := scanAttempt(tx.QueryRow(ctx, `SELECT `+applicationAttemptColumns+` FROM automatic_application_attempts WHERE attempt_id=$1 FOR UPDATE`, attemptID))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrAttemptNotFound
	}
	if err != nil {
		return fmt.Errorf("read application attempt reconciliation: %w", err)
	}
	updated, err := current.WithReconciliation(evidence, now)
	if err != nil {
		return err
	}
	var providerResponseAt any
	var observedAt any
	history, marshalErr := json.Marshal(nilSafeHistory(updated.Reconciliation))
	if marshalErr != nil {
		return fmt.Errorf("marshal application attempt reconciliation history: %w", marshalErr)
	}
	identities, marshalErr := json.Marshal(nilSafeIdentities(updated.Reconciliation))
	if marshalErr != nil {
		return fmt.Errorf("marshal application attempt provider identities: %w", marshalErr)
	}
	if updated.Reconciliation != nil {
		providerResponseAt = updated.Reconciliation.ProviderResponseAt
		observedAt = updated.Reconciliation.ObservedAt
	}
	_, err = tx.Exec(ctx, `UPDATE automatic_application_attempts SET state=$2, updated_at=$3, reconciliation_kind=$4, reconciliation_strength=$5, reconciliation_source=$6, provider_application_id=$7, provider_negotiation_id=$8, provider_conversation_id=$9, reconciliation_confirmation_source=$10, reconciliation_provider_identities=$11, provider_response_at=$12, reconciliation_observed_at=$13, reconciliation_history=$14 WHERE attempt_id=$1`,
		attemptID, string(updated.State), updated.UpdatedAt,
		valueString(updated.Reconciliation, func(v *domain.ReconciliationEvidence) string { return string(v.Kind) }),
		valueString(updated.Reconciliation, func(v *domain.ReconciliationEvidence) string { return string(v.Strength) }),
		valueString(updated.Reconciliation, func(v *domain.ReconciliationEvidence) string { return v.Source }),
		valueString(updated.Reconciliation, func(v *domain.ReconciliationEvidence) string { return v.ProviderApplicationID }),
		valueString(updated.Reconciliation, func(v *domain.ReconciliationEvidence) string { return v.ProviderNegotiationID }), valueString(updated.Reconciliation, func(v *domain.ReconciliationEvidence) string { return v.ProviderConversationID }), valueString(updated.Reconciliation, func(v *domain.ReconciliationEvidence) string { return v.ConfirmationSource }), identities, providerResponseAt, observedAt, history)
	if err != nil {
		return fmt.Errorf("record application attempt reconciliation: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit application attempt reconciliation: %w", err)
	}
	return nil
}

func (r *ApplicationAttemptRepository) Get(ctx context.Context, attemptID string) (domain.Attempt, error) {
	ctx = postgresContext(ctx)
	if err := repositoryContextErr(ctx); err != nil {
		return domain.Attempt{}, err
	}
	if r == nil || r.pool == nil {
		return domain.Attempt{}, errors.New("application attempt repository is not configured")
	}
	value, err := scanAttempt(r.pool.QueryRow(ctx, `SELECT `+applicationAttemptColumns+` FROM automatic_application_attempts WHERE attempt_id=$1`, attemptID))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Attempt{}, domain.ErrAttemptNotFound
	}
	if err != nil {
		return domain.Attempt{}, fmt.Errorf("get application attempt: %w", err)
	}
	return value, nil
}

func (r *ApplicationAttemptRepository) GetByID(ctx context.Context, attemptID string) (domain.Attempt, error) {
	return r.Get(ctx, attemptID)
}

func (r *ApplicationAttemptRepository) List(ctx context.Context, query attemptport.ReadQuery) ([]domain.Attempt, error) {
	ctx = postgresContext(ctx)
	if err := repositoryContextErr(ctx); err != nil {
		return nil, err
	}
	if query.Limit <= 0 || query.Limit > 100 {
		return nil, errors.New("application attempt read limit must be between 1 and 100")
	}
	if r == nil || r.pool == nil {
		return nil, errors.New("application attempt repository is not configured")
	}
	where := []string{"TRUE"}
	args := make([]any, 0, 3)
	if query.VacancyID != nil {
		args = append(args, *query.VacancyID)
		where = append(where, fmt.Sprintf("vacancy_id=$%d", len(args)))
	}
	if len(query.States) > 0 {
		states := make([]string, 0, len(query.States))
		for _, state := range query.States {
			states = append(states, string(state))
		}
		args = append(args, states)
		where = append(where, fmt.Sprintf("state = ANY($%d)", len(args)))
	}
	args = append(args, query.Limit)
	rows, err := r.pool.Query(ctx, `SELECT `+applicationAttemptColumns+` FROM automatic_application_attempts WHERE `+strings.Join(where, " AND ")+` ORDER BY updated_at DESC, attempt_id ASC LIMIT $`+fmt.Sprint(len(args)), args...)
	if err != nil {
		return nil, fmt.Errorf("list application attempts: %w", err)
	}
	defer rows.Close()
	result := make([]domain.Attempt, 0, query.Limit)
	for rows.Next() {
		value, scanErr := scanAttempt(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("scan application attempt: %w", scanErr)
		}
		result = append(result, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list application attempts: %w", err)
	}
	return result, nil
}

func (r *ApplicationAttemptRepository) blockingForVacancy(ctx context.Context, vacancyID int) (domain.Attempt, error) {
	return findBlockingAttempt(r.pool.QueryRow(ctx, `SELECT `+applicationAttemptColumns+` FROM automatic_application_attempts WHERE vacancy_id=$1 AND state IN ('SENDING','ACCEPTED','DELIVERY_UNCERTAIN','TARGET_RESPONSE_CONFIRMED') ORDER BY created_at, attempt_id LIMIT 1`, vacancyID))
}

// findBlockingAttempt applies the port contract for the expected-absence
// blocking lookup. A PostgreSQL no-row result is not store unavailability;
// only driver/database/scan errors other than pgx.ErrNoRows fail closed.
func findBlockingAttempt(row attemptRow) (domain.Attempt, error) {
	value, err := scanAttempt(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Attempt{}, domain.ErrAttemptNotFound
	}
	if err != nil {
		return domain.Attempt{}, fmt.Errorf("find blocking application attempt: %w", err)
	}
	return value, nil
}

type attemptRow interface{ Scan(...any) error }

func scanAttempt(row attemptRow) (domain.Attempt, error) {
	var value domain.Attempt
	var state string
	var kind, strength, source, providerApplicationID, providerNegotiationID, providerConversationID, confirmationSource *string
	var providerResponseAt, observedAt *time.Time
	var identitiesRaw, historyRaw []byte
	err := row.Scan(&value.AttemptID, &value.VacancyID, &value.ResumeID, &state, &value.CreatedAt, &value.UpdatedAt, &value.ProviderStatus, &value.ErrorClass,
		&kind, &strength, &source, &providerApplicationID, &providerNegotiationID, &providerConversationID, &confirmationSource, &identitiesRaw, &providerResponseAt, &observedAt, &historyRaw)
	if err != nil {
		return domain.Attempt{}, err
	}
	value.State = domain.State(state)
	var history []domain.ReconciliationEvidence
	if len(historyRaw) > 0 {
		if err := json.Unmarshal(historyRaw, &history); err != nil {
			return domain.Attempt{}, fmt.Errorf("decode application attempt reconciliation history: %w", err)
		}
	}
	var identities []domain.ProviderIdentity
	if len(identitiesRaw) > 0 {
		if err := json.Unmarshal(identitiesRaw, &identities); err != nil {
			return domain.Attempt{}, fmt.Errorf("decode application attempt provider identities: %w", err)
		}
	}
	if kind != nil || strength != nil || source != nil || providerApplicationID != nil || providerNegotiationID != nil || providerConversationID != nil || confirmationSource != nil || len(identities) > 0 || observedAt != nil {
		value.Reconciliation = &domain.ReconciliationEvidence{Kind: domain.EvidenceKind(stringValue(kind)), Strength: domain.EvidenceStrength(stringValue(strength)), Source: stringValue(source), ConfirmationSource: stringValue(confirmationSource), ProviderApplicationID: stringValue(providerApplicationID), ProviderNegotiationID: stringValue(providerNegotiationID), ProviderConversationID: stringValue(providerConversationID), ProviderIdentities: identities, History: history, ProviderResponseAt: providerResponseAt}
		if observedAt != nil {
			value.Reconciliation.ObservedAt = *observedAt
		}
	}
	return value, nil
}

func nilSafeIdentities(value *domain.ReconciliationEvidence) []domain.ProviderIdentity {
	if value == nil || value.ProviderIdentities == nil {
		return []domain.ProviderIdentity{}
	}
	return value.ProviderIdentities
}

func nilSafeHistory(value *domain.ReconciliationEvidence) []domain.ReconciliationEvidence {
	if value == nil || value.History == nil {
		return []domain.ReconciliationEvidence{}
	}
	return value.History
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func valueString(value *domain.ReconciliationEvidence, getter func(*domain.ReconciliationEvidence) string) any {
	if value == nil {
		return ""
	}

	return getter(value)
}

var _ attemptport.Store = (*ApplicationAttemptRepository)(nil)
var _ attemptport.BlockingReader = (*ApplicationAttemptRepository)(nil)
var _ attemptport.Reader = (*ApplicationAttemptRepository)(nil)
