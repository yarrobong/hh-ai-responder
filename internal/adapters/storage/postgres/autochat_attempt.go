package postgresstorage

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	domain "hh-ai-responder/internal/autochatattempt"
	autochatport "hh-ai-responder/internal/ports/autochatattempt"
)

const autoChatAttemptColumns = "attempt_id, conversation_id, trigger_message_id, action_type, state, created_at, updated_at, request_key, provider_outgoing_message_id, provider_status, error_class, reconciliation_kind, reconciliation_source, reconciliation_provider_message_id, reconciliation_observed_at, reconciliation_causality_note"

// AutoChatAttemptRepository is the PostgreSQL implementation of the same
// narrow trigger authority as the JSON adapter. The partial unique index in
// the migration is the cross-process reservation authority.
type AutoChatAttemptRepository struct {
	pool *pgxpool.Pool
}

func NewAutoChatAttemptRepository(pool *pgxpool.Pool) *AutoChatAttemptRepository {
	return &AutoChatAttemptRepository{pool: pool}
}

func (r *AutoChatAttemptRepository) FindBlockingForTrigger(ctx context.Context, conversationID, triggerMessageID string) (domain.Attempt, error) {
	ctx = postgresContext(ctx)
	if err := repositoryContextErr(ctx); err != nil {
		return domain.Attempt{}, err
	}
	if strings.TrimSpace(conversationID) == "" || strings.TrimSpace(triggerMessageID) == "" {
		return domain.Attempt{}, fmt.Errorf("%w: conversation and trigger IDs are required", domain.ErrInvalidAttempt)
	}
	if r == nil || r.pool == nil {
		return domain.Attempt{}, errors.New("auto-chat attempt repository is not configured")
	}
	return r.blockingForTrigger(ctx, conversationID, triggerMessageID)
}

func (r *AutoChatAttemptRepository) Reserve(ctx context.Context, value domain.Attempt) (autochatport.ReserveResult, error) {
	ctx = postgresContext(ctx)
	if err := repositoryContextErr(ctx); err != nil {
		return autochatport.ReserveResult{}, err
	}
	if err := value.Validate(); err != nil || value.State != domain.StateSending {
		return autochatport.ReserveResult{}, fmt.Errorf("reserve auto-chat attempt: %w", domain.ErrInvalidAttempt)
	}
	if r == nil || r.pool == nil {
		return autochatport.ReserveResult{}, errors.New("auto-chat attempt repository is not configured")
	}
	stored, err := scanAutoChatAttempt(r.pool.QueryRow(ctx, `INSERT INTO legacy_auto_chat_attempts (`+autoChatAttemptColumns+`)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		ON CONFLICT DO NOTHING
		RETURNING `+autoChatAttemptColumns,
		value.AttemptID, value.ConversationID, value.TriggerMessageID, string(value.ActionType), string(value.State), value.CreatedAt, value.UpdatedAt,
		value.RequestKey, value.ProviderOutgoingMessageID, value.ProviderStatus, value.ErrorClass, "", "", "", nil, ""))
	if err == nil {
		return autochatport.ReserveResult{Reserved: true, Attempt: stored}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return autochatport.ReserveResult{}, fmt.Errorf("reserve auto-chat attempt: %w", err)
	}
	existing, lookupErr := r.blockingForTrigger(ctx, value.ConversationID, value.TriggerMessageID)
	if lookupErr == nil {
		return autochatport.ReserveResult{Existing: &existing}, fmt.Errorf("%w: attempt %s", domain.ErrTriggerBlocked, existing.AttemptID)
	}
	if !errors.Is(lookupErr, domain.ErrAttemptNotFound) {
		return autochatport.ReserveResult{}, lookupErr
	}
	return autochatport.ReserveResult{}, errors.New("auto-chat attempt reservation conflict")
}

func (r *AutoChatAttemptRepository) RecordOutcome(ctx context.Context, attemptID string, state domain.State, updatedAt time.Time, providerID string, providerStatus int, errorClass string) error {
	ctx = postgresContext(ctx)
	if err := repositoryContextErr(ctx); err != nil {
		return err
	}
	if strings.TrimSpace(attemptID) == "" {
		return domain.ErrAttemptNotFound
	}
	if r == nil || r.pool == nil {
		return errors.New("auto-chat attempt repository is not configured")
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin auto-chat attempt outcome: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	current, err := scanAutoChatAttempt(tx.QueryRow(ctx, `SELECT `+autoChatAttemptColumns+` FROM legacy_auto_chat_attempts WHERE attempt_id=$1 FOR UPDATE`, attemptID))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrAttemptNotFound
	}
	if err != nil {
		return fmt.Errorf("read auto-chat attempt outcome: %w", err)
	}
	updated, err := current.WithOutcome(state, updatedAt, providerID, providerStatus, errorClass)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE legacy_auto_chat_attempts SET state=$2, updated_at=$3, provider_outgoing_message_id=$4, provider_status=$5, error_class=$6 WHERE attempt_id=$1`, attemptID, string(updated.State), updated.UpdatedAt, updated.ProviderOutgoingMessageID, updated.ProviderStatus, updated.ErrorClass); err != nil {
		return fmt.Errorf("record auto-chat attempt outcome: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit auto-chat attempt outcome: %w", err)
	}
	return nil
}

func (r *AutoChatAttemptRepository) RecordReconciliation(ctx context.Context, attemptID string, evidence domain.ReconciliationEvidence, updatedAt time.Time) error {
	ctx = postgresContext(ctx)
	if err := repositoryContextErr(ctx); err != nil {
		return err
	}
	if strings.TrimSpace(attemptID) == "" {
		return domain.ErrAttemptNotFound
	}
	if r == nil || r.pool == nil {
		return errors.New("auto-chat attempt repository is not configured")
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin auto-chat attempt reconciliation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	current, err := scanAutoChatAttempt(tx.QueryRow(ctx, `SELECT `+autoChatAttemptColumns+` FROM legacy_auto_chat_attempts WHERE attempt_id=$1 FOR UPDATE`, attemptID))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrAttemptNotFound
	}
	if err != nil {
		return fmt.Errorf("read auto-chat attempt reconciliation: %w", err)
	}
	updated, err := current.WithReconciliation(evidence, updatedAt)
	if err != nil {
		return err
	}
	var kind, source, providerID, causality string
	var observedAt any
	if updated.Reconciliation != nil {
		kind, source, providerID, causality = string(updated.Reconciliation.Kind), updated.Reconciliation.Source, updated.Reconciliation.ProviderMessageID, updated.Reconciliation.CausalityNote
		observedAt = updated.Reconciliation.ObservedAt
	}
	if _, err := tx.Exec(ctx, `UPDATE legacy_auto_chat_attempts SET state=$2, updated_at=$3, reconciliation_kind=$4, reconciliation_source=$5, reconciliation_provider_message_id=$6, reconciliation_observed_at=$7, reconciliation_causality_note=$8 WHERE attempt_id=$1`, attemptID, string(updated.State), updated.UpdatedAt, kind, source, providerID, observedAt, causality); err != nil {
		return fmt.Errorf("record auto-chat attempt reconciliation: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit auto-chat attempt reconciliation: %w", err)
	}
	return nil
}

func (r *AutoChatAttemptRepository) GetByID(ctx context.Context, attemptID string) (domain.Attempt, error) {
	ctx = postgresContext(ctx)
	if err := repositoryContextErr(ctx); err != nil {
		return domain.Attempt{}, err
	}
	if strings.TrimSpace(attemptID) == "" {
		return domain.Attempt{}, domain.ErrAttemptNotFound
	}
	if r == nil || r.pool == nil {
		return domain.Attempt{}, errors.New("auto-chat attempt repository is not configured")
	}
	value, err := scanAutoChatAttempt(r.pool.QueryRow(ctx, `SELECT `+autoChatAttemptColumns+` FROM legacy_auto_chat_attempts WHERE attempt_id=$1`, attemptID))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Attempt{}, domain.ErrAttemptNotFound
	}
	if err != nil {
		return domain.Attempt{}, fmt.Errorf("get auto-chat attempt: %w", err)
	}
	return value, nil
}

func (r *AutoChatAttemptRepository) List(ctx context.Context, query autochatport.ReadQuery) ([]domain.Attempt, error) {
	ctx = postgresContext(ctx)
	if err := repositoryContextErr(ctx); err != nil {
		return nil, err
	}
	if query.Limit <= 0 || query.Limit > 100 {
		return nil, errors.New("auto-chat attempt read limit must be between 1 and 100")
	}
	if r == nil || r.pool == nil {
		return nil, errors.New("auto-chat attempt repository is not configured")
	}
	where := []string{"TRUE"}
	args := make([]any, 0, 4)
	if query.ConversationID != nil {
		args = append(args, *query.ConversationID)
		where = append(where, fmt.Sprintf("conversation_id=$%d", len(args)))
	}
	if query.ActionType != nil {
		args = append(args, string(*query.ActionType))
		where = append(where, fmt.Sprintf("action_type=$%d", len(args)))
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
	rows, err := r.pool.Query(ctx, `SELECT `+autoChatAttemptColumns+` FROM legacy_auto_chat_attempts WHERE `+strings.Join(where, " AND ")+` ORDER BY updated_at DESC, attempt_id ASC LIMIT $`+fmt.Sprint(len(args)), args...)
	if err != nil {
		return nil, fmt.Errorf("list auto-chat attempts: %w", err)
	}
	defer rows.Close()
	result := make([]domain.Attempt, 0, query.Limit)
	for rows.Next() {
		value, scanErr := scanAutoChatAttempt(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("scan auto-chat attempt: %w", scanErr)
		}
		result = append(result, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list auto-chat attempts: %w", err)
	}
	return result, nil
}

func (r *AutoChatAttemptRepository) blockingForTrigger(ctx context.Context, conversationID, triggerMessageID string) (domain.Attempt, error) {
	value, err := scanAutoChatAttempt(r.pool.QueryRow(ctx, `SELECT `+autoChatAttemptColumns+` FROM legacy_auto_chat_attempts WHERE conversation_id=$1 AND trigger_message_id=$2 AND state IN ('SENDING','ACCEPTED','DELIVERY_UNCERTAIN','TARGET_REPLY_CONFIRMED','TARGET_LEAVE_CONFIRMED') ORDER BY created_at, attempt_id LIMIT 1`, conversationID, triggerMessageID))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Attempt{}, domain.ErrAttemptNotFound
	}
	if err != nil {
		return domain.Attempt{}, fmt.Errorf("find blocking auto-chat attempt: %w", err)
	}
	return value, nil
}

type autoChatAttemptRow interface{ Scan(...any) error }

func scanAutoChatAttempt(row autoChatAttemptRow) (domain.Attempt, error) {
	var value domain.Attempt
	var actionType, state, kind, source, providerID, causality string
	var observedAt *time.Time
	if err := row.Scan(&value.AttemptID, &value.ConversationID, &value.TriggerMessageID, &actionType, &state, &value.CreatedAt, &value.UpdatedAt, &value.RequestKey, &value.ProviderOutgoingMessageID, &value.ProviderStatus, &value.ErrorClass, &kind, &source, &providerID, &observedAt, &causality); err != nil {
		return domain.Attempt{}, err
	}
	value.ActionType, value.State = domain.ActionType(actionType), domain.State(state)
	if kind != "" || source != "" || providerID != "" || observedAt != nil || causality != "" {
		value.Reconciliation = &domain.ReconciliationEvidence{Kind: domain.EvidenceKind(kind), Source: source, ProviderMessageID: providerID, CausalityNote: causality}
		if observedAt != nil {
			value.Reconciliation.ObservedAt = *observedAt
		}
	}
	if err := value.Validate(); err != nil {
		return domain.Attempt{}, err
	}
	return value, nil
}

var _ autochatport.Store = (*AutoChatAttemptRepository)(nil)
var _ autochatport.Reader = (*AutoChatAttemptRepository)(nil)
var _ autochatport.ReconciliationWriter = (*AutoChatAttemptRepository)(nil)
