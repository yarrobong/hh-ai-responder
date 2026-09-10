package postgresstorage

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"hh-ai-responder/internal/conversation"
	"hh-ai-responder/internal/ports"
)

// ErrDuplicateConversationExternal is kept distinct from generic repository
// conflicts because callers use it to classify an existing HH conversation.
var ErrDuplicateConversationExternal = errors.New("duplicate conversation external id")

const conversationColumns = `
	id, external_id, vacancy_id, application_id, company_name, vacancy_title,
	vacancy_description, status, created_at, updated_at, created_at_ns,
	updated_at_ns, hh_updated_at, hh_updated_at_ns, last_employer_message_at,
	last_employer_message_at_ns, last_candidate_message_at,
	last_candidate_message_at_ns, summary, next_action, waiting_since,
	waiting_since_ns, last_activity_at, last_activity_at_ns, follow_up_state,
	raw_status, hh_metadata`

const conversationInsert = `INSERT INTO conversations (` + conversationColumns + `)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26,$27)`

const conversationUpdate = `UPDATE conversations SET
	external_id=$2, vacancy_id=$3, application_id=$4, company_name=$5,
	vacancy_title=$6, vacancy_description=$7, status=$8, created_at=$9,
	updated_at=$10, created_at_ns=$11, updated_at_ns=$12,
	hh_updated_at=$13, hh_updated_at_ns=$14, last_employer_message_at=$15,
	last_employer_message_at_ns=$16, last_candidate_message_at=$17,
	last_candidate_message_at_ns=$18, summary=$19, next_action=$20,
	waiting_since=$21, waiting_since_ns=$22, last_activity_at=$23,
	last_activity_at_ns=$24, follow_up_state=$25, raw_status=$26,
	hh_metadata=$27 WHERE id=$1`

// ConversationRepository is the PostgreSQL implementation of the normalized
// conversation store. It stores only conversation-domain values and scalar
// relation IDs; policy, HH mapping, AI, and Candidate knowledge stay outside.
type ConversationRepository struct {
	pool *pgxpool.Pool
	db   postgresDBTX
}

// Pool is a composition-only inspection hook used by the root reconciliation
// wiring. It is not part of the Conversation persistence port.
func (r *ConversationRepository) Pool() *pgxpool.Pool {
	if r == nil {
		return nil
	}
	return r.pool
}

func NewConversationRepository(pool *pgxpool.Pool) *ConversationRepository {
	var db postgresDBTX
	if pool != nil {
		db = pool
	}
	return &ConversationRepository{pool: pool, db: db}
}

// NewConversationRepositoryForTx binds the repository to the caller-owned
// transaction and never falls back to the pool.
func NewConversationRepositoryForTx(tx pgx.Tx) *ConversationRepository {
	return &ConversationRepository{db: tx}
}

func (r *ConversationRepository) requireDB() error {
	if r == nil || r.db == nil {
		return errors.New("conversation repository is not configured")
	}
	return nil
}

func (r *ConversationRepository) Get(ctx context.Context, id string) (conversation.EmployerConversation, error) {
	ctx = postgresContext(ctx)
	if err := repositoryContextErr(ctx); err != nil {
		return conversation.EmployerConversation{}, err
	}
	if err := r.requireDB(); err != nil {
		return conversation.EmployerConversation{}, err
	}
	return loadPostgresConversation(ctx, r.db, "id=$1", id)
}

func (r *ConversationRepository) GetByHHConversationID(ctx context.Context, id string) (conversation.EmployerConversation, error) {
	ctx = postgresContext(ctx)
	if err := repositoryContextErr(ctx); err != nil {
		return conversation.EmployerConversation{}, err
	}
	if err := r.requireDB(); err != nil {
		return conversation.EmployerConversation{}, err
	}
	return loadPostgresConversation(ctx, r.db, "external_id=$1 AND btrim(external_id)<>''", id)
}

func (r *ConversationRepository) GetByVacancyID(ctx context.Context, id int) ([]conversation.EmployerConversation, error) {
	return r.listConversations(ctx, "vacancy_id=$1", id)
}

func (r *ConversationRepository) List(ctx context.Context) ([]conversation.EmployerConversation, error) {
	return r.listConversations(ctx, "TRUE")
}

func (r *ConversationRepository) listConversations(ctx context.Context, predicate string, args ...interface{}) ([]conversation.EmployerConversation, error) {
	ctx = postgresContext(ctx)
	if err := repositoryContextErr(ctx); err != nil {
		return nil, err
	}
	if err := r.requireDB(); err != nil {
		return nil, err
	}
	rows, err := r.db.Query(ctx, `SELECT `+conversationColumns+` FROM conversations WHERE `+predicate+` ORDER BY created_at,id`, args...)
	if err != nil {
		return nil, fmt.Errorf("list conversations: %w", err)
	}
	defer rows.Close()
	result := make([]conversation.EmployerConversation, 0)
	for rows.Next() {
		value, scanErr := scanPostgresConversation(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		result = append(result, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate conversations: %w", err)
	}
	for i := range result {
		messages, msgErr := loadConversationMessages(ctx, r.db, result[i].ID)
		if msgErr != nil {
			return nil, msgErr
		}
		result[i].Messages = messages
	}
	return result, nil
}

func (r *ConversationRepository) Upsert(ctx context.Context, value conversation.EmployerConversation) (conversation.EmployerConversation, error) {
	ctx = postgresContext(ctx)
	if err := repositoryContextErr(ctx); err != nil {
		return conversation.EmployerConversation{}, err
	}
	if err := r.requireDB(); err != nil {
		return conversation.EmployerConversation{}, err
	}
	incomingCreatedAt := value.CreatedAt
	var result conversation.EmployerConversation
	err := r.withMutation(ctx, func(db postgresDBTX) error {
		if value.ID == "" && value.HHConversationID != "" {
			old, findErr := loadPostgresConversation(ctx, db, "external_id=$1", value.HHConversationID)
			if findErr == nil {
				value.ID = old.ID
			} else if !errors.Is(findErr, conversation.ErrConversationNotFound) {
				return findErr
			}
		}
		if value.ID == "" {
			var err error
			value.ID, err = newConversationScopedID("conversation")
			if err != nil {
				return err
			}
		}
		if value.CreatedAt.IsZero() {
			value.CreatedAt = time.Now().UTC()
		}
		if value.Status == "" {
			value.Status = conversation.StatusApplied
		}
		if value.FollowUpState == "" {
			value.FollowUpState = conversation.FollowUpNone
		}
		if value.Messages == nil {
			value.Messages = []conversation.Message{}
		}
		value.UpdatedAt = time.Now().UTC()
		if value.UpdatedAt.Before(value.CreatedAt) {
			value.UpdatedAt = value.CreatedAt
		}
		value.RefreshActivity()
		if value.LastActivityAt != nil && value.UpdatedAt.Before(*value.LastActivityAt) {
			value.UpdatedAt = *value.LastActivityAt
		}

		old, oldErr := conversation.EmployerConversation{}, error(nil)
		old, oldErr = loadPostgresConversation(ctx, db, "id=$1", value.ID)
		if oldErr == nil {
			if incomingCreatedAt.IsZero() {
				value.CreatedAt = old.CreatedAt
			}
			if value.ApplicationID == "" {
				value.ApplicationID = old.ApplicationID
			} else if old.ApplicationID != "" && value.ApplicationID != old.ApplicationID {
				return fmt.Errorf("%w: cannot reassign an existing conversation", ErrRepositoryConflict)
			}
			if err := validateConversationReplacement(old, value); err != nil {
				return err
			}
			value = mergeConversationTimestamps(old, value)
		} else if !errors.Is(oldErr, conversation.ErrConversationNotFound) {
			return oldErr
		}
		if err := value.Validate(); err != nil {
			return err
		}
		args, err := conversationArgs(value)
		if err != nil {
			return err
		}
		if oldErr == nil {
			if _, err := db.Exec(ctx, conversationUpdate, args...); err != nil {
				return mapPostgresConversationError("update conversation", err)
			}
		} else if _, err := db.Exec(ctx, conversationInsert, args...); err != nil {
			return mapPostgresConversationError("create conversation", err)
		}
		for _, message := range value.Messages {
			if _, err := appendConversationMessage(ctx, db, value.ID, message); err != nil {
				return err
			}
		}
		result, err = loadPostgresConversation(ctx, db, "id=$1", value.ID)
		return err
	})
	return result, err
}

// Import inserts a legacy parent snapshot without messages. Message rows are
// imported separately so append-only identity checks remain explicit.
func (r *ConversationRepository) Import(ctx context.Context, value conversation.EmployerConversation) error {
	ctx = postgresContext(ctx)
	if err := repositoryContextErr(ctx); err != nil {
		return err
	}
	if err := r.requireDB(); err != nil {
		return err
	}
	if err := value.Validate(); err != nil {
		return err
	}
	value.Messages = nil
	args, err := conversationArgs(value)
	if err != nil {
		return err
	}
	if _, err := r.db.Exec(ctx, conversationInsert, args...); err != nil {
		return mapPostgresConversationError("import conversation", err)
	}
	return nil
}

// ImportMessage retains the migration path's durable duplicate/conflict
// behavior while keeping message persistence in this adapter.
func (r *ConversationRepository) ImportMessage(ctx context.Context, conversationID string, value conversation.Message) error {
	ctx = postgresContext(ctx)
	if err := repositoryContextErr(ctx); err != nil {
		return err
	}
	if err := r.requireDB(); err != nil {
		return err
	}
	if _, err := loadPostgresConversation(ctx, r.db, "id=$1", conversationID); err != nil {
		return err
	}
	_, err := appendConversationMessage(ctx, r.db, conversationID, value)
	return err
}

func (r *ConversationRepository) AppendMessage(ctx context.Context, id string, value conversation.Message) (conversation.Message, error) {
	ctx = postgresContext(ctx)
	if err := repositoryContextErr(ctx); err != nil {
		return conversation.Message{}, err
	}
	if err := r.requireDB(); err != nil {
		return conversation.Message{}, err
	}
	if value.ID == "" {
		var err error
		value.ID, err = newConversationScopedID("message")
		if err != nil {
			return conversation.Message{}, err
		}
	}
	if err := value.Validate(); err != nil {
		return conversation.Message{}, err
	}
	var result conversation.Message
	err := r.withMutation(ctx, func(db postgresDBTX) error {
		if _, err := loadPostgresConversation(ctx, db, "id=$1", id); err != nil {
			return err
		}
		var err error
		result, err = appendConversationMessage(ctx, db, id, value)
		return err
	})
	return result, err
}

func appendConversationMessage(ctx context.Context, db postgresDBTX, conversationID string, value conversation.Message) (conversation.Message, error) {
	if value.ID == "" {
		var err error
		value.ID, err = newConversationScopedID("message")
		if err != nil {
			return conversation.Message{}, err
		}
	}
	if err := value.Validate(); err != nil {
		return conversation.Message{}, err
	}
	var existing conversation.Message
	row := db.QueryRow(ctx, `SELECT id, external_id, timestamp, timestamp_ns, sender, direction, source, text, system_event, content_unavailable, metadata FROM conversation_messages WHERE conversation_id=$1 AND ((id=$2) OR (source=$3 AND btrim($4)<>'' AND external_id=$4)) LIMIT 1`, conversationID, value.ID, value.Source, value.ExternalID)
	var at pgtype.Timestamptz
	var ns pgtype.Int8
	var metadata []byte
	err := row.Scan(&existing.ID, &existing.ExternalID, &at, &ns, &existing.Sender, &existing.Direction, &existing.Source, &existing.Text, &existing.HHSystemEvent, &existing.ContentUnavailable, &metadata)
	if err == nil {
		existing.Timestamp = postgresTimeExact(at, ns)
		if err := decodeNullableJSON(metadata, &existing.Metadata); err != nil {
			return conversation.Message{}, fmt.Errorf("decode conversation message metadata: %w", err)
		}
		if !conversation.SameMessage(existing, value) {
			return conversation.Message{}, errors.New("message identity conflicts with original content")
		}
		return existing, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return conversation.Message{}, fmt.Errorf("find conversation message: %w", err)
	}
	metadata, err = nullableJSON(value.Metadata)
	if err != nil {
		return conversation.Message{}, fmt.Errorf("encode conversation message metadata: %w", err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO conversation_messages (id,conversation_id,external_id,timestamp,timestamp_ns,sender,direction,source,text,system_event,content_unavailable,metadata) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`, value.ID, conversationID, value.ExternalID, value.Timestamp, value.Timestamp.UnixNano(), value.Sender, value.Direction, value.Source, value.Text, value.HHSystemEvent, value.ContentUnavailable, metadata); err != nil {
		return conversation.Message{}, mapPostgresConversationError("append conversation message", err)
	}
	return value, nil
}

func (r *ConversationRepository) UpdateState(ctx context.Context, id string, state conversation.State) error {
	if state.Status == "" {
		return errors.New("state update requires an explicit status")
	}
	ctx = postgresContext(ctx)
	if err := repositoryContextErr(ctx); err != nil {
		return err
	}
	if err := r.requireDB(); err != nil {
		return err
	}
	return r.withMutation(ctx, func(db postgresDBTX) error {
		c, err := loadPostgresConversation(ctx, db, "id=$1", id)
		if err != nil {
			return err
		}
		c.Status, c.NextAction, c.WaitingSince, c.FollowUpState = state.Status, state.NextAction, state.WaitingSince, state.FollowUpState
		c.UpdatedAt = time.Now().UTC()
		if c.UpdatedAt.Before(c.CreatedAt) {
			c.UpdatedAt = c.CreatedAt
		}
		return r.upsertOn(ctx, db, c)
	})
}

func (r *ConversationRepository) UpdateSummary(ctx context.Context, id string, summary conversation.Summary) error {
	ctx = postgresContext(ctx)
	if err := repositoryContextErr(ctx); err != nil {
		return err
	}
	if err := r.requireDB(); err != nil {
		return err
	}
	return r.withMutation(ctx, func(db postgresDBTX) error {
		c, err := loadPostgresConversation(ctx, db, "id=$1", id)
		if err != nil {
			return err
		}
		c.Summary = summary
		c.UpdatedAt = time.Now().UTC()
		return r.upsertOn(ctx, db, c)
	})
}

func (r *ConversationRepository) AppendCandidateClaim(ctx context.Context, id string, claim conversation.Claim) error {
	ctx = postgresContext(ctx)
	if err := repositoryContextErr(ctx); err != nil {
		return err
	}
	if err := r.requireDB(); err != nil {
		return err
	}
	return r.withMutation(ctx, func(db postgresDBTX) error {
		c, err := loadPostgresConversation(ctx, db, "id=$1", id)
		if err != nil {
			return err
		}
		for _, old := range c.Summary.CandidateClaims {
			if reflect.DeepEqual(old, claim) {
				return nil
			}
		}
		c.Summary.CandidateClaims = append(c.Summary.CandidateClaims, claim)
		c.UpdatedAt = time.Now().UTC()
		return r.upsertOn(ctx, db, c)
	})
}

func (r *ConversationRepository) Timeline(ctx context.Context, id string) ([]conversation.Message, error) {
	ctx = postgresContext(ctx)
	if err := repositoryContextErr(ctx); err != nil {
		return nil, err
	}
	if err := r.requireDB(); err != nil {
		return nil, err
	}
	if _, err := loadPostgresConversation(ctx, r.db, "id=$1", id); err != nil {
		return nil, err
	}
	return loadConversationMessages(ctx, r.db, id)
}

// Save is a durability no-op for PostgreSQL: every mutation is committed
// immediately, while retaining the port's context/configuration check.
func (r *ConversationRepository) Save(ctx context.Context) error {
	ctx = postgresContext(ctx)
	if err := repositoryContextErr(ctx); err != nil {
		return err
	}
	return r.requireDB()
}

func (r *ConversationRepository) upsertOn(ctx context.Context, db postgresDBTX, c conversation.EmployerConversation) error {
	args, err := conversationArgs(c)
	if err != nil {
		return err
	}
	if _, err := db.Exec(ctx, conversationUpdate, args...); err != nil {
		return mapPostgresConversationError("update conversation", err)
	}
	return nil
}

func (r *ConversationRepository) withMutation(ctx context.Context, fn func(postgresDBTX) error) error {
	if r.pool == nil {
		return fn(r.db)
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin postgres conversation transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit postgres conversation transaction: %w", err)
	}
	return nil
}

func conversationArgs(v conversation.EmployerConversation) ([]interface{}, error) {
	summary, err := nullableJSON(v.Summary)
	if err != nil {
		return nil, err
	}
	meta, err := nullableJSON(v.HHMetadata)
	if err != nil {
		return nil, err
	}
	return []interface{}{v.ID, v.HHConversationID, v.VacancyID, nullableString(v.ApplicationID), v.CompanyName, v.VacancyTitle, v.VacancyDescription, v.Status, v.CreatedAt, v.UpdatedAt, v.CreatedAt.UnixNano(), v.UpdatedAt.UnixNano(), nullableTime(v.HHUpdatedAt), nullableUnixNano(v.HHUpdatedAt), nullableConversationPtr(v.LastEmployerMessageAt), nullableConversationPtrNanos(v.LastEmployerMessageAt), nullableConversationPtr(v.LastCandidateMessageAt), nullableConversationPtrNanos(v.LastCandidateMessageAt), summary, v.NextAction, nullableConversationPtr(v.WaitingSince), nullableConversationPtrNanos(v.WaitingSince), nullableConversationPtr(v.LastActivityAt), nullableConversationPtrNanos(v.LastActivityAt), v.FollowUpState, v.RawStatus, meta}, nil
}

func nullableString(value string) interface{} {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func nullableConversationPtr(value *time.Time) interface{} {
	if value == nil || value.IsZero() {
		return nil
	}
	return *value
}

func nullableConversationPtrNanos(value *time.Time) interface{} {
	if value == nil || value.IsZero() {
		return nil
	}
	return value.UnixNano()
}

func validateConversationReplacement(old, next conversation.EmployerConversation) error {
	if !old.CreatedAt.Equal(next.CreatedAt) || (old.VacancyID != 0 && old.VacancyID != next.VacancyID) ||
		(old.HHConversationID != "" && old.HHConversationID != next.HHConversationID) {
		return errors.New("cannot reassign an existing conversation")
	}
	if len(next.Messages) < len(old.Messages) {
		return errors.New("cannot remove original messages")
	}
	for i, oldMessage := range old.Messages {
		incoming := next.Messages[i]
		if oldMessage.ID != incoming.ID || !conversation.SameMessage(oldMessage, incoming) {
			return errors.New("cannot rewrite original messages")
		}
	}
	for _, claim := range old.Summary.CandidateClaims {
		found := false
		for _, incoming := range next.Summary.CandidateClaims {
			if reflect.DeepEqual(claim, incoming) {
				found = true
				break
			}
		}
		if !found {
			return errors.New("cannot remove or rewrite recorded claims")
		}
	}
	return nil
}

func mergeConversationTimestamps(old, next conversation.EmployerConversation) conversation.EmployerConversation {
	if next.CreatedAt.IsZero() {
		next.CreatedAt = old.CreatedAt
	}
	if next.UpdatedAt.Before(old.UpdatedAt) {
		next.UpdatedAt = old.UpdatedAt
	}
	return next
}

type postgresRowScanner interface {
	Scan(dest ...interface{}) error
}

func loadPostgresConversation(ctx context.Context, db postgresDBTX, predicate string, arg interface{}) (conversation.EmployerConversation, error) {
	var c conversation.EmployerConversation
	var created, updated, hhUpdated pgtype.Timestamptz
	var cns, uns, hns pgtype.Int8
	var lastEmployer, lastCandidate, lastActivity, waiting pgtype.Timestamptz
	var lastEmployerNS, lastCandidateNS, lastActivityNS, waitingNS pgtype.Int8
	var applicationID *string
	var summary, meta []byte
	row := db.QueryRow(ctx, `SELECT `+conversationColumns+` FROM conversations WHERE `+predicate+` LIMIT 1`, arg)
	err := row.Scan(&c.ID, &c.HHConversationID, &c.VacancyID, &applicationID, &c.CompanyName, &c.VacancyTitle, &c.VacancyDescription, &c.Status, &created, &updated, &cns, &uns, &hhUpdated, &hns, &lastEmployer, &lastEmployerNS, &lastCandidate, &lastCandidateNS, &summary, &c.NextAction, &waiting, &waitingNS, &lastActivity, &lastActivityNS, &c.FollowUpState, &c.RawStatus, &meta)
	if errors.Is(err, pgx.ErrNoRows) {
		return conversation.EmployerConversation{}, conversation.ErrConversationNotFound
	}
	if err != nil {
		return conversation.EmployerConversation{}, fmt.Errorf("scan conversation: %w", err)
	}
	c.CreatedAt = postgresTimeExact(created, cns)
	c.UpdatedAt = postgresTimeExact(updated, uns)
	c.HHUpdatedAt = postgresTimeExact(hhUpdated, hns)
	c.LastEmployerMessageAt = postgresConversationTimePtr(lastEmployer, lastEmployerNS)
	c.LastCandidateMessageAt = postgresConversationTimePtr(lastCandidate, lastCandidateNS)
	c.WaitingSince = postgresConversationTimePtr(waiting, waitingNS)
	c.LastActivityAt = postgresConversationTimePtr(lastActivity, lastActivityNS)
	if applicationID != nil {
		c.ApplicationID = *applicationID
	}
	if err := decodeNullableJSON(summary, &c.Summary); err != nil {
		return conversation.EmployerConversation{}, fmt.Errorf("decode conversation summary: %w", err)
	}
	if err := decodeNullableJSON(meta, &c.HHMetadata); err != nil {
		return conversation.EmployerConversation{}, fmt.Errorf("decode conversation metadata: %w", err)
	}
	messages, err := loadConversationMessages(ctx, db, c.ID)
	if err != nil {
		return conversation.EmployerConversation{}, err
	}
	c.Messages = messages
	return cloneConversation(c)
}

func scanPostgresConversation(scanner postgresRowScanner) (conversation.EmployerConversation, error) {
	var c conversation.EmployerConversation
	var created, updated, hhUpdated pgtype.Timestamptz
	var cns, uns, hns pgtype.Int8
	var lastEmployer, lastCandidate, lastActivity, waiting pgtype.Timestamptz
	var lastEmployerNS, lastCandidateNS, lastActivityNS, waitingNS pgtype.Int8
	var applicationID *string
	var summary, meta []byte
	err := scanner.Scan(&c.ID, &c.HHConversationID, &c.VacancyID, &applicationID, &c.CompanyName, &c.VacancyTitle, &c.VacancyDescription, &c.Status, &created, &updated, &cns, &uns, &hhUpdated, &hns, &lastEmployer, &lastEmployerNS, &lastCandidate, &lastCandidateNS, &summary, &c.NextAction, &waiting, &waitingNS, &lastActivity, &lastActivityNS, &c.FollowUpState, &c.RawStatus, &meta)
	if errors.Is(err, pgx.ErrNoRows) {
		return conversation.EmployerConversation{}, conversation.ErrConversationNotFound
	}
	if err != nil {
		return conversation.EmployerConversation{}, fmt.Errorf("scan conversation: %w", err)
	}
	c.CreatedAt = postgresTimeExact(created, cns)
	c.UpdatedAt = postgresTimeExact(updated, uns)
	c.HHUpdatedAt = postgresTimeExact(hhUpdated, hns)
	c.LastEmployerMessageAt = postgresConversationTimePtr(lastEmployer, lastEmployerNS)
	c.LastCandidateMessageAt = postgresConversationTimePtr(lastCandidate, lastCandidateNS)
	c.WaitingSince = postgresConversationTimePtr(waiting, waitingNS)
	c.LastActivityAt = postgresConversationTimePtr(lastActivity, lastActivityNS)
	if applicationID != nil {
		c.ApplicationID = *applicationID
	}
	if err := decodeNullableJSON(summary, &c.Summary); err != nil {
		return conversation.EmployerConversation{}, err
	}
	if err := decodeNullableJSON(meta, &c.HHMetadata); err != nil {
		return conversation.EmployerConversation{}, err
	}
	return c, nil
}

func loadConversationMessages(ctx context.Context, db postgresDBTX, id string) ([]conversation.Message, error) {
	rows, err := db.Query(ctx, `SELECT id,external_id,timestamp,timestamp_ns,sender,direction,source,text,system_event,content_unavailable,metadata FROM conversation_messages WHERE conversation_id=$1 ORDER BY timestamp,sequence`, id)
	if err != nil {
		return nil, fmt.Errorf("list conversation messages: %w", err)
	}
	defer rows.Close()
	result := make([]conversation.Message, 0)
	for rows.Next() {
		var m conversation.Message
		var at pgtype.Timestamptz
		var ns pgtype.Int8
		var metadata []byte
		if err := rows.Scan(&m.ID, &m.ExternalID, &at, &ns, &m.Sender, &m.Direction, &m.Source, &m.Text, &m.HHSystemEvent, &m.ContentUnavailable, &metadata); err != nil {
			return nil, fmt.Errorf("scan conversation message: %w", err)
		}
		m.Timestamp = postgresTimeExact(at, ns)
		if err := decodeNullableJSON(metadata, &m.Metadata); err != nil {
			return nil, fmt.Errorf("decode conversation message metadata: %w", err)
		}
		result = append(result, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate conversation messages: %w", err)
	}
	return result, nil
}

func postgresConversationTimePtr(value pgtype.Timestamptz, nanos pgtype.Int8) *time.Time {
	if !value.Valid && !nanos.Valid {
		return nil
	}
	t := postgresTimeExact(value, nanos)
	return &t
}

func cloneConversation(value conversation.EmployerConversation) (conversation.EmployerConversation, error) {
	var copy conversation.EmployerConversation
	raw, err := json.Marshal(value)
	if err != nil {
		return copy, err
	}
	if err := json.Unmarshal(raw, &copy); err != nil {
		return copy, err
	}
	return copy, nil
}

func newConversationScopedID(kind string) (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", fmt.Errorf("generate knowledge id: %w", err)
	}
	return fmt.Sprintf("%s-%x", kind, value), nil
}

func mapPostgresConversationError(action string, err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505":
			switch pgErr.ConstraintName {
			case "conversations_external_id_unique":
				return fmt.Errorf("%s: %w", action, ErrDuplicateConversationExternal)
			case "conversation_messages_external_id_unique":
				return fmt.Errorf("%s: %w", action, ErrRepositoryConflict)
			default:
				return fmt.Errorf("%s: %w", action, ErrRepositoryConflict)
			}
		case "23503":
			switch pgErr.ConstraintName {
			case "conversations_vacancy_fk":
				return fmt.Errorf("%s: %w", action, ErrVacancyNotFound)
			case "conversation_messages_conversation_fk":
				return fmt.Errorf("%s: %w", action, conversation.ErrConversationNotFound)
			case "conversations_application_fk":
				// Application identity is outside this adapter's allowed domain
				// dependencies; retain the established safe conflict class.
				return fmt.Errorf("%s: %w", action, ErrRepositoryConflict)
			default:
				return fmt.Errorf("%s: %w", action, ErrRepositoryConflict)
			}
		}
	}
	return fmt.Errorf("%s: %w", action, err)
}

var _ ports.ConversationReader = (*ConversationRepository)(nil)
var _ ports.ConversationWriter = (*ConversationRepository)(nil)
var _ ports.ConversationStore = (*ConversationRepository)(nil)
