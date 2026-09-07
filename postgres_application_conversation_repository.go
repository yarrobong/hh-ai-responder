package main

import (
	"context"
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
)

// postgresDBTX is implemented by both pgxpool.Pool and pgx.Tx. Keeping the
// repository on this narrow interface makes the transaction boundary real:
// repositories created for a transaction cannot accidentally use the pool.
type postgresDBTX interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

var (
	ErrDuplicateConversationExternal = errors.New("duplicate conversation external id")
	ErrRepositoryConflict            = errors.New("repository conflict")
)

const applicationColumns = `
	id, external_id, vacancy_id, conversation_id, company_name, vacancy_title,
	vacancy_url, source, status, raw_status, created_at, updated_at,
	created_at_ns, updated_at_ns, follow_up_state, notes, next_action,
	match_result, hh_metadata, partial, data_completeness, reconciliation_evidence`

const conversationColumns = `
	id, external_id, vacancy_id, application_id, company_name, vacancy_title,
	vacancy_description, status, created_at, updated_at, created_at_ns,
	updated_at_ns, hh_updated_at, hh_updated_at_ns, last_employer_message_at,
	last_employer_message_at_ns, last_candidate_message_at,
	last_candidate_message_at_ns, summary, next_action, waiting_since,
	waiting_since_ns, last_activity_at, last_activity_at_ns, follow_up_state,
	raw_status, hh_metadata`

const applicationInsert = `INSERT INTO applications (` + applicationColumns + `)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22)`

const applicationUpdate = `UPDATE applications SET
	external_id=$2, vacancy_id=$3, conversation_id=$4, company_name=$5,
	vacancy_title=$6, vacancy_url=$7, source=$8, status=$9, raw_status=$10,
	created_at=$11, updated_at=$12, created_at_ns=$13, updated_at_ns=$14,
	follow_up_state=$15, notes=$16, next_action=$17, match_result=$18,
	hh_metadata=$19, partial=$20, data_completeness=$21,
	reconciliation_evidence=$22 WHERE id=$1`

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

type PostgresApplicationRepository struct {
	pool *pgxpool.Pool
	db   postgresDBTX
}

func NewPostgresApplicationRepository(pool *pgxpool.Pool) *PostgresApplicationRepository {
	var db postgresDBTX
	if pool != nil {
		db = pool
	}
	return &PostgresApplicationRepository{pool: pool, db: db}
}

func newPostgresApplicationRepositoryTx(db postgresDBTX) *PostgresApplicationRepository {
	return &PostgresApplicationRepository{db: db}
}

func (r *PostgresApplicationRepository) requireDB() error {
	if r == nil || r.db == nil {
		return errors.New("application repository is not configured")
	}
	return nil
}

func (r *PostgresApplicationRepository) Get(ctx context.Context, id string) (JobApplication, error) {
	ctx = postgresContext(ctx)
	if err := repositoryContextErr(ctx); err != nil {
		return JobApplication{}, err
	}
	if err := r.requireDB(); err != nil {
		return JobApplication{}, err
	}
	return scanPostgresApplication(r.db.QueryRow(ctx, `SELECT `+applicationColumns+` FROM applications WHERE id=$1`, id))
}

func (r *PostgresApplicationRepository) GetByExternalID(ctx context.Context, id string) (JobApplication, error) {
	ctx = postgresContext(ctx)
	if err := repositoryContextErr(ctx); err != nil {
		return JobApplication{}, err
	}
	if err := r.requireDB(); err != nil {
		return JobApplication{}, err
	}
	return scanPostgresApplication(r.db.QueryRow(ctx, `SELECT `+applicationColumns+` FROM applications WHERE external_id=$1 AND btrim(external_id)<>''`, id))
}

func (r *PostgresApplicationRepository) List(ctx context.Context) ([]JobApplication, error) {
	ctx = postgresContext(ctx)
	if err := repositoryContextErr(ctx); err != nil {
		return nil, err
	}
	if err := r.requireDB(); err != nil {
		return nil, err
	}
	rows, err := r.db.Query(ctx, `SELECT `+applicationColumns+` FROM applications ORDER BY created_at, id`)
	if err != nil {
		return nil, fmt.Errorf("list applications: %w", err)
	}
	defer rows.Close()
	result := []JobApplication{}
	for rows.Next() {
		value, scanErr := scanPostgresApplication(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		result = append(result, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate applications: %w", err)
	}
	return result, nil
}

// ListEvents returns the append-only event ledger without requiring an
// application id. It is used by migration planning and verification only.
func (r *PostgresApplicationRepository) ListEvents(ctx context.Context) ([]ApplicationEvent, error) {
	ctx = postgresContext(ctx)
	if err := repositoryContextErr(ctx); err != nil {
		return nil, err
	}
	if err := r.requireDB(); err != nil {
		return nil, err
	}
	rows, err := r.db.Query(ctx, `SELECT id, application_id, created_at, created_at_ns, event_type, payload FROM application_events ORDER BY created_at, sequence`)
	if err != nil {
		return nil, fmt.Errorf("list application events: %w", err)
	}
	defer rows.Close()
	result := []ApplicationEvent{}
	for rows.Next() {
		var event ApplicationEvent
		var at pgtype.Timestamptz
		var ns pgtype.Int8
		var payload []byte
		if err := rows.Scan(&event.ID, &event.ApplicationID, &at, &ns, &event.Type, &payload); err != nil {
			return nil, fmt.Errorf("scan application event: %w", err)
		}
		event.Timestamp = postgresTimeExact(at, ns)
		var body struct {
			Description string `json:"description"`
		}
		if err := decodeNullableJSON(payload, &body); err != nil {
			return nil, fmt.Errorf("decode application event: %w", err)
		}
		event.Description = body.Description
		result = append(result, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate application events: %w", err)
	}
	return result, nil
}

func (r *PostgresApplicationRepository) Create(ctx context.Context, value JobApplication) (JobApplication, error) {
	ctx = postgresContext(ctx)
	if err := repositoryContextErr(ctx); err != nil {
		return JobApplication{}, err
	}
	if err := r.requireDB(); err != nil {
		return JobApplication{}, err
	}
	value, err := prepareApplicationCreate(value)
	if err != nil {
		return JobApplication{}, err
	}
	event, err := newApplicationEvent(value.ID, value.CreatedAt, ApplicationEventCreated, "application created")
	if err != nil {
		return JobApplication{}, err
	}
	err = r.withMutation(ctx, func(db postgresDBTX) error {
		if _, err := db.Exec(ctx, applicationInsert, applicationArgs(value)...); err != nil {
			return mapPostgresApplicationError("create application", err)
		}
		return appendApplicationEvent(ctx, db, event)
	})
	if err != nil {
		return JobApplication{}, err
	}
	return cloneKnowledge(value)
}

// Import inserts a legacy application snapshot exactly as supplied. Unlike
// Create it does not synthesize the application-created event; the source
// event ledger is imported separately and in its original order.
func (r *PostgresApplicationRepository) Import(ctx context.Context, value JobApplication) error {
	ctx = postgresContext(ctx)
	if err := repositoryContextErr(ctx); err != nil {
		return err
	}
	if err := r.requireDB(); err != nil {
		return err
	}
	if err := value.validate(); err != nil {
		return err
	}
	if _, err := r.db.Exec(ctx, applicationInsert, applicationArgs(value)...); err != nil {
		return mapPostgresApplicationError("import application", err)
	}
	return nil
}

// ImportEvent appends one already validated immutable event. Duplicate and
// conflicting identities are deliberately left to the planner; this method
// never updates an existing event.
func (r *PostgresApplicationRepository) ImportEvent(ctx context.Context, event ApplicationEvent) error {
	ctx = postgresContext(ctx)
	if err := repositoryContextErr(ctx); err != nil {
		return err
	}
	if err := r.requireDB(); err != nil {
		return err
	}
	return appendApplicationEvent(ctx, r.db, event)
}

func (r *PostgresApplicationRepository) Update(ctx context.Context, value JobApplication) error {
	ctx = postgresContext(ctx)
	if err := repositoryContextErr(ctx); err != nil {
		return err
	}
	if err := r.requireDB(); err != nil {
		return err
	}
	if strings.TrimSpace(value.ID) == "" {
		return errors.New("application update requires an id")
	}
	return r.withMutation(ctx, func(db postgresDBTX) error {
		var current JobApplication
		var err error
		current, err = scanPostgresApplication(db.QueryRow(ctx, `SELECT `+applicationColumns+` FROM applications WHERE id=$1 FOR UPDATE`, value.ID))
		if errors.Is(err, ErrApplicationNotFound) {
			return err
		}
		if err != nil {
			return err
		}
		if value.CreatedAt.IsZero() {
			value.CreatedAt = current.CreatedAt
		}
		if value.UpdatedAt.IsZero() || value.UpdatedAt.Before(value.CreatedAt) {
			value.UpdatedAt = current.UpdatedAt
			if value.UpdatedAt.Before(value.CreatedAt) {
				value.UpdatedAt = value.CreatedAt
			}
		}
		if err := value.validate(); err != nil {
			return err
		}
		if _, err := db.Exec(ctx, applicationUpdate, applicationArgs(value)...); err != nil {
			return mapPostgresApplicationError("update application", err)
		}
		return nil
	})
}

func (r *PostgresApplicationRepository) UpsertImported(ctx context.Context, value JobApplication) (JobApplication, bool, error) {
	ctx = postgresContext(ctx)
	if err := repositoryContextErr(ctx); err != nil {
		return JobApplication{}, false, err
	}
	if err := r.requireDB(); err != nil {
		return JobApplication{}, false, err
	}
	if strings.TrimSpace(value.ExternalID) == "" {
		return JobApplication{}, false, errors.New("imported application requires an external id")
	}
	value, err := prepareImportedApplication(value)
	if err != nil {
		return JobApplication{}, false, err
	}
	var result JobApplication
	created := false
	err = r.withMutation(ctx, func(db postgresDBTX) error {
		old, oldErr := scanPostgresApplication(db.QueryRow(ctx, `SELECT `+applicationColumns+` FROM applications WHERE external_id=$1 FOR UPDATE`, value.ExternalID))
		if oldErr != nil && !errors.Is(oldErr, ErrApplicationNotFound) {
			return oldErr
		}
		if oldErr == nil {
			value.ID, value.CreatedAt = old.ID, old.CreatedAt
			if value.UpdatedAt.IsZero() || value.UpdatedAt.Before(old.UpdatedAt) {
				value.UpdatedAt = old.UpdatedAt
			}
			if applicationEquivalent(old, value) {
				result, err = cloneKnowledge(old)
				return err
			}
			if err := value.validate(); err != nil {
				return err
			}
			if _, err := db.Exec(ctx, applicationUpdate, applicationArgs(value)...); err != nil {
				return mapPostgresApplicationError("update imported application", err)
			}
			result, err = cloneKnowledge(value)
			return err
		}
		if _, err := db.Exec(ctx, applicationInsert, applicationArgs(value)...); err != nil {
			return mapPostgresApplicationError("import application", err)
		}
		event, eventErr := newApplicationEvent(value.ID, value.CreatedAt, ApplicationEventCreated, "application created")
		if eventErr != nil {
			return eventErr
		}
		if err := appendApplicationEvent(ctx, db, event); err != nil {
			return err
		}
		created = true
		result, err = cloneKnowledge(value)
		return err
	})
	return result, created, err
}

func (r *PostgresApplicationRepository) AttachImportedConversation(ctx context.Context, externalID, conversationID string) error {
	return r.attachConversationByExternal(ctx, externalID, conversationID)
}

func (r *PostgresApplicationRepository) attachConversationByExternal(ctx context.Context, externalID, conversationID string) error {
	ctx = postgresContext(ctx)
	if err := repositoryContextErr(ctx); err != nil {
		return err
	}
	if err := r.requireDB(); err != nil {
		return err
	}
	return r.withMutation(ctx, func(db postgresDBTX) error {
		var id string
		if err := db.QueryRow(ctx, `SELECT id FROM applications WHERE external_id=$1 FOR UPDATE`, externalID).Scan(&id); err != nil {
			return mapPostgresNotFound("find application", err, ErrApplicationNotFound)
		}
		return attachConversationSQL(ctx, db, id, conversationID)
	})
}

func (r *PostgresApplicationRepository) AttachConversation(ctx context.Context, applicationID, conversationID string) error {
	ctx = postgresContext(ctx)
	if err := repositoryContextErr(ctx); err != nil {
		return err
	}
	if err := r.requireDB(); err != nil {
		return err
	}
	return r.withMutation(ctx, func(db postgresDBTX) error { return attachConversationSQL(ctx, db, applicationID, conversationID) })
}

func attachConversationSQL(ctx context.Context, db postgresDBTX, applicationID, conversationID string) error {
	var applicationVacancy, conversationVacancy int
	var currentConversationID string
	if err := db.QueryRow(ctx, `SELECT vacancy_id,conversation_id FROM applications WHERE id=$1 FOR UPDATE`, applicationID).Scan(&applicationVacancy, &currentConversationID); err != nil {
		return mapPostgresNotFound("find application", err, ErrApplicationNotFound)
	}
	if err := db.QueryRow(ctx, `SELECT vacancy_id FROM conversations WHERE id=$1 FOR UPDATE`, conversationID).Scan(&conversationVacancy); err != nil {
		return mapPostgresNotFound("find conversation", err, ErrConversationNotFound)
	}
	if applicationVacancy != conversationVacancy {
		return errors.New("conversation vacancy does not match application vacancy")
	}
	var linkedApplication *string
	if err := db.QueryRow(ctx, `SELECT application_id FROM conversations WHERE id=$1 FOR UPDATE`, conversationID).Scan(&linkedApplication); err != nil {
		return mapPostgresNotFound("find conversation", err, ErrConversationNotFound)
	}
	if linkedApplication != nil && *linkedApplication != applicationID {
		return fmt.Errorf("%w: conversation already belongs to another application", ErrRepositoryConflict)
	}
	at := time.Now().UTC()
	if currentConversationID != conversationID {
		if _, err := db.Exec(ctx, `UPDATE applications SET conversation_id=$2, updated_at=$3, updated_at_ns=$4 WHERE id=$1`, applicationID, conversationID, at, at.UnixNano()); err != nil {
			return mapPostgresApplicationError("attach application conversation", err)
		}
	}
	if _, err := db.Exec(ctx, `UPDATE conversations SET application_id=$2 WHERE id=$1`, conversationID, applicationID); err != nil {
		return mapPostgresConversationError("attach conversation application", err)
	}
	return nil
}

func (r *PostgresApplicationRepository) UpdateStatus(ctx context.Context, id string, status ApplicationStatus) error {
	ctx = postgresContext(ctx)
	if err := repositoryContextErr(ctx); err != nil {
		return err
	}
	if err := r.requireDB(); err != nil {
		return err
	}
	if !applicationStatuses[status] {
		return errors.New("invalid application status")
	}
	return r.withMutation(ctx, func(db postgresDBTX) error {
		current, err := scanPostgresApplication(db.QueryRow(ctx, `SELECT `+applicationColumns+` FROM applications WHERE id=$1 FOR UPDATE`, id))
		if err != nil {
			return err
		}
		if current.Status == status {
			return nil
		}
		updated := current
		updated.Status, updated.UpdatedAt = status, time.Now().UTC()
		if updated.UpdatedAt.Before(updated.CreatedAt) {
			updated.UpdatedAt = updated.CreatedAt
		}
		if _, err := db.Exec(ctx, applicationUpdate, applicationArgs(updated)...); err != nil {
			return mapPostgresApplicationError("update application status", err)
		}
		event, err := newApplicationEvent(id, updated.UpdatedAt, applicationEventTypeForStatus(status), fmt.Sprintf("status changed from %q to %q", current.Status, status))
		if err != nil {
			return err
		}
		return appendApplicationEvent(ctx, db, event)
	})
}

func (r *PostgresApplicationRepository) SetFollowUpState(ctx context.Context, id string, state ConversationFollowUpState) error {
	ctx = postgresContext(ctx)
	if err := repositoryContextErr(ctx); err != nil {
		return err
	}
	if err := r.requireDB(); err != nil {
		return err
	}
	if state == "" {
		return errors.New("follow-up state is required")
	}
	return r.withMutation(ctx, func(db postgresDBTX) error { return updateApplicationField(ctx, db, id, `follow_up_state`, state) })
}

func (r *PostgresApplicationRepository) SaveMatchResult(ctx context.Context, id string, result MatchResult) error {
	ctx = postgresContext(ctx)
	if err := repositoryContextErr(ctx); err != nil {
		return err
	}
	if err := r.requireDB(); err != nil {
		return err
	}
	if err := result.validate(); err != nil {
		return err
	}
	result.normalize()
	return r.withMutation(ctx, func(db postgresDBTX) error {
		old, err := scanPostgresApplication(db.QueryRow(ctx, `SELECT `+applicationColumns+` FROM applications WHERE id=$1 FOR UPDATE`, id))
		if err != nil {
			return err
		}
		if old.MatchResult != nil && reflect.DeepEqual(*old.MatchResult, result) {
			return nil
		}
		updated := old
		updated.MatchResult = &result
		updated.UpdatedAt = time.Now().UTC()
		if updated.UpdatedAt.Before(updated.CreatedAt) {
			updated.UpdatedAt = updated.CreatedAt
		}
		if _, err := db.Exec(ctx, applicationUpdate, applicationArgs(updated)...); err != nil {
			return mapPostgresApplicationError("save application match", err)
		}
		event, err := newApplicationEvent(id, updated.UpdatedAt, ApplicationEventMatched, "vacancy match result saved")
		if err != nil {
			return err
		}
		return appendApplicationEvent(ctx, db, event)
	})
}

func (r *PostgresApplicationRepository) AppendEvent(ctx context.Context, id string, at time.Time, eventType ApplicationEventType, description string) error {
	ctx = postgresContext(ctx)
	if err := repositoryContextErr(ctx); err != nil {
		return err
	}
	if err := r.requireDB(); err != nil {
		return err
	}
	event, err := newApplicationEvent(id, at, eventType, description)
	if err != nil {
		return err
	}
	return r.withMutation(ctx, func(db postgresDBTX) error {
		if err := ensureApplication(ctx, db, id); err != nil {
			return err
		}
		return appendApplicationEvent(ctx, db, event)
	})
}

func (r *PostgresApplicationRepository) Timeline(ctx context.Context, id string) ([]ApplicationEvent, error) {
	ctx = postgresContext(ctx)
	if err := repositoryContextErr(ctx); err != nil {
		return nil, err
	}
	if err := r.requireDB(); err != nil {
		return nil, err
	}
	if err := ensureApplication(ctx, r.db, id); err != nil {
		return nil, err
	}
	rows, err := r.db.Query(ctx, `SELECT id, application_id, created_at, created_at_ns, event_type, payload FROM application_events WHERE application_id=$1 ORDER BY created_at, sequence`, id)
	if err != nil {
		return nil, fmt.Errorf("list application events: %w", err)
	}
	defer rows.Close()
	result := []ApplicationEvent{}
	for rows.Next() {
		var event ApplicationEvent
		var at pgtype.Timestamptz
		var ns pgtype.Int8
		var payload []byte
		if err := rows.Scan(&event.ID, &event.ApplicationID, &at, &ns, &event.Type, &payload); err != nil {
			return nil, fmt.Errorf("scan application event: %w", err)
		}
		event.Timestamp = postgresTimeExact(at, ns)
		var body struct {
			Description string `json:"description"`
		}
		if err := decodeNullableJSON(payload, &body); err != nil {
			return nil, fmt.Errorf("decode application event: %w", err)
		}
		event.Description = body.Description
		result = append(result, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate application events: %w", err)
	}
	return result, nil
}

func (r *PostgresApplicationRepository) Save(ctx context.Context) error {
	ctx = postgresContext(ctx)
	if err := repositoryContextErr(ctx); err != nil {
		return err
	}
	return r.requireDB()
}

func (r *PostgresApplicationRepository) withMutation(ctx context.Context, fn func(postgresDBTX) error) error {
	if r.pool == nil {
		return fn(r.db)
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin postgres application transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit postgres application transaction: %w", err)
	}
	return nil
}

type PostgresConversationRepository struct {
	pool *pgxpool.Pool
	db   postgresDBTX
}

func NewPostgresConversationRepository(pool *pgxpool.Pool) *PostgresConversationRepository {
	var db postgresDBTX
	if pool != nil {
		db = pool
	}
	return &PostgresConversationRepository{pool: pool, db: db}
}
func newPostgresConversationRepositoryTx(db postgresDBTX) *PostgresConversationRepository {
	return &PostgresConversationRepository{db: db}
}
func (r *PostgresConversationRepository) requireDB() error {
	if r == nil || r.db == nil {
		return errors.New("conversation repository is not configured")
	}
	return nil
}

func (r *PostgresConversationRepository) Get(ctx context.Context, id string) (EmployerConversation, error) {
	ctx = postgresContext(ctx)
	if err := repositoryContextErr(ctx); err != nil {
		return EmployerConversation{}, err
	}
	if err := r.requireDB(); err != nil {
		return EmployerConversation{}, err
	}
	return r.loadConversation(ctx, r.db, `id=$1`, id)
}
func (r *PostgresConversationRepository) GetByHHConversationID(ctx context.Context, id string) (EmployerConversation, error) {
	ctx = postgresContext(ctx)
	if err := repositoryContextErr(ctx); err != nil {
		return EmployerConversation{}, err
	}
	if err := r.requireDB(); err != nil {
		return EmployerConversation{}, err
	}
	return r.loadConversation(ctx, r.db, `external_id=$1 AND btrim(external_id)<>''`, id)
}
func (r *PostgresConversationRepository) GetByVacancyID(ctx context.Context, id int) ([]EmployerConversation, error) {
	return r.listConversations(ctx, `vacancy_id=$1`, id)
}
func (r *PostgresConversationRepository) List(ctx context.Context) ([]EmployerConversation, error) {
	return r.listConversations(ctx, `TRUE`)
}
func (r *PostgresConversationRepository) listConversations(ctx context.Context, predicate string, arg ...any) ([]EmployerConversation, error) {
	ctx = postgresContext(ctx)
	if err := repositoryContextErr(ctx); err != nil {
		return nil, err
	}
	if err := r.requireDB(); err != nil {
		return nil, err
	}
	rows, err := r.db.Query(ctx, `SELECT `+conversationColumns+` FROM conversations WHERE `+predicate+` ORDER BY created_at,id`, arg...)
	if err != nil {
		return nil, fmt.Errorf("list conversations: %w", err)
	}
	defer rows.Close()
	result := []EmployerConversation{}
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
		messages, msgErr := r.timeline(ctx, r.db, result[i].ID)
		if msgErr != nil {
			return nil, msgErr
		}
		result[i].Messages = messages
	}
	return result, nil
}

func (r *PostgresConversationRepository) Upsert(ctx context.Context, value EmployerConversation) (EmployerConversation, error) {
	ctx = postgresContext(ctx)
	if err := repositoryContextErr(ctx); err != nil {
		return EmployerConversation{}, err
	}
	if err := r.requireDB(); err != nil {
		return EmployerConversation{}, err
	}
	var err error
	returnValue := EmployerConversation{}
	incomingCreatedAt := value.CreatedAt
	err = r.withMutation(ctx, func(db postgresDBTX) error {
		if value.ID == "" && value.HHConversationID != "" {
			old, findErr := loadPostgresConversation(ctx, db, `external_id=$1`, value.HHConversationID)
			if findErr == nil {
				value.ID = old.ID
			} else if !errors.Is(findErr, ErrConversationNotFound) {
				return findErr
			}
		}
		if value.ID == "" {
			value.ID, err = newKnowledgeID("conversation")
			if err != nil {
				return err
			}
		}
		if value.CreatedAt.IsZero() {
			value.CreatedAt = time.Now().UTC()
		}
		if value.Status == "" {
			value.Status = ConversationApplied
		}
		if value.FollowUpState == "" {
			value.FollowUpState = ConversationFollowUpNone
		}
		if value.Messages == nil {
			value.Messages = []ConversationMessage{}
		}
		value.UpdatedAt = time.Now().UTC()
		if value.UpdatedAt.Before(value.CreatedAt) {
			value.UpdatedAt = value.CreatedAt
		}
		value.refreshActivity()
		if value.LastActivityAt != nil && value.UpdatedAt.Before(*value.LastActivityAt) {
			value.UpdatedAt = *value.LastActivityAt
		}
		old, oldErr := EmployerConversation{}, error(nil)
		if value.ID != "" {
			old, oldErr = loadPostgresConversation(ctx, db, `id=$1`, value.ID)
		}
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
		}
		if err := value.validate(); err != nil {
			return err
		}
		args, argsErr := conversationArgs(value)
		if argsErr != nil {
			return argsErr
		}
		if oldErr == nil {
			if _, err := db.Exec(ctx, conversationUpdate, args...); err != nil {
				return mapPostgresConversationError("update conversation", err)
			}
		} else {
			if !errors.Is(oldErr, ErrConversationNotFound) {
				return oldErr
			}
			if _, err := db.Exec(ctx, conversationInsert, args...); err != nil {
				return mapPostgresConversationError("create conversation", err)
			}
		}
		for _, message := range value.Messages {
			if _, err := appendConversationMessage(ctx, db, value.ID, message); err != nil {
				return err
			}
		}
		returnValue, err = loadPostgresConversation(ctx, db, `id=$1`, value.ID)
		return err
	})
	return returnValue, err
}

// Import inserts a legacy conversation parent without messages. Messages are
// imported separately so append-only identity checks remain explicit and no
// repository-generated timestamps are introduced.
func (r *PostgresConversationRepository) Import(ctx context.Context, value EmployerConversation) error {
	ctx = postgresContext(ctx)
	if err := repositoryContextErr(ctx); err != nil {
		return err
	}
	if err := r.requireDB(); err != nil {
		return err
	}
	if err := value.validate(); err != nil {
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

// ImportMessage appends one legacy message while retaining the repository's
// existing identical-duplicate/conflicting-duplicate semantics.
func (r *PostgresConversationRepository) ImportMessage(ctx context.Context, conversationID string, value ConversationMessage) error {
	ctx = postgresContext(ctx)
	if err := repositoryContextErr(ctx); err != nil {
		return err
	}
	if err := r.requireDB(); err != nil {
		return err
	}
	if _, err := loadPostgresConversation(ctx, r.db, `id=$1`, conversationID); err != nil {
		return err
	}
	_, err := appendConversationMessage(ctx, r.db, conversationID, value)
	return err
}

func (r *PostgresConversationRepository) AppendMessage(ctx context.Context, id string, value ConversationMessage) (ConversationMessage, error) {
	ctx = postgresContext(ctx)
	if err := repositoryContextErr(ctx); err != nil {
		return ConversationMessage{}, err
	}
	if err := r.requireDB(); err != nil {
		return ConversationMessage{}, err
	}
	if value.ID == "" {
		var err error
		value.ID, err = newKnowledgeID("message")
		if err != nil {
			return ConversationMessage{}, err
		}
	}
	if err := value.validate(); err != nil {
		return ConversationMessage{}, err
	}
	result := value
	err := r.withMutation(ctx, func(db postgresDBTX) error {
		if _, err := loadPostgresConversation(ctx, db, `id=$1`, id); err != nil {
			return err
		}
		var err error
		result, err = appendConversationMessage(ctx, db, id, value)
		return err
	})
	return result, err
}

func appendConversationMessage(ctx context.Context, db postgresDBTX, conversationID string, value ConversationMessage) (ConversationMessage, error) {
	if value.ID == "" {
		var err error
		value.ID, err = newKnowledgeID("message")
		if err != nil {
			return ConversationMessage{}, err
		}
	}
	if err := value.validate(); err != nil {
		return ConversationMessage{}, err
	}
	var existing ConversationMessage
	row := db.QueryRow(ctx, `SELECT id, external_id, timestamp, timestamp_ns, sender, direction, source, text, system_event, content_unavailable, metadata FROM conversation_messages WHERE conversation_id=$1 AND ((id=$2) OR (source=$3 AND btrim($4)<>'' AND external_id=$4)) LIMIT 1`, conversationID, value.ID, value.Source, value.ExternalID)
	var at pgtype.Timestamptz
	var ns pgtype.Int8
	var metadata []byte
	err := row.Scan(&existing.ID, &existing.ExternalID, &at, &ns, &existing.Sender, &existing.Direction, &existing.Source, &existing.Text, &existing.HHSystemEvent, &existing.ContentUnavailable, &metadata)
	if err == nil {
		existing.Timestamp = postgresTimeExact(at, ns)
		if err := decodeNullableJSON(metadata, &existing.Metadata); err != nil {
			return ConversationMessage{}, fmt.Errorf("decode conversation message metadata: %w", err)
		}
		if !sameConversationMessage(existing, value) {
			return ConversationMessage{}, errors.New("message identity conflicts with original content")
		}
		return existing, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return ConversationMessage{}, fmt.Errorf("find conversation message: %w", err)
	}
	metadata, err = nullableJSON(value.Metadata)
	if err != nil {
		return ConversationMessage{}, fmt.Errorf("encode conversation message metadata: %w", err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO conversation_messages (id,conversation_id,external_id,timestamp,timestamp_ns,sender,direction,source,text,system_event,content_unavailable,metadata) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`, value.ID, conversationID, value.ExternalID, value.Timestamp, value.Timestamp.UnixNano(), value.Sender, value.Direction, value.Source, value.Text, value.HHSystemEvent, value.ContentUnavailable, metadata); err != nil {
		return ConversationMessage{}, mapPostgresConversationError("append conversation message", err)
	}
	return value, nil
}

func (r *PostgresConversationRepository) UpdateState(ctx context.Context, id string, state ConversationState) error {
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
		c, err := loadPostgresConversation(ctx, db, `id=$1`, id)
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
func (r *PostgresConversationRepository) UpdateSummary(ctx context.Context, id string, summary ConversationSummary) error {
	ctx = postgresContext(ctx)
	if err := repositoryContextErr(ctx); err != nil {
		return err
	}
	if err := r.requireDB(); err != nil {
		return err
	}
	return r.withMutation(ctx, func(db postgresDBTX) error {
		c, err := loadPostgresConversation(ctx, db, `id=$1`, id)
		if err != nil {
			return err
		}
		c.Summary = summary
		c.UpdatedAt = time.Now().UTC()
		return r.upsertOn(ctx, db, c)
	})
}
func (r *PostgresConversationRepository) AppendCandidateClaim(ctx context.Context, id string, claim CandidateConversationClaim) error {
	ctx = postgresContext(ctx)
	if err := repositoryContextErr(ctx); err != nil {
		return err
	}
	if err := r.requireDB(); err != nil {
		return err
	}
	return r.withMutation(ctx, func(db postgresDBTX) error {
		c, err := loadPostgresConversation(ctx, db, `id=$1`, id)
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
func (r *PostgresConversationRepository) Timeline(ctx context.Context, id string) ([]ConversationMessage, error) {
	ctx = postgresContext(ctx)
	if err := repositoryContextErr(ctx); err != nil {
		return nil, err
	}
	if err := r.requireDB(); err != nil {
		return nil, err
	}
	if _, err := loadPostgresConversation(ctx, r.db, `id=$1`, id); err != nil {
		return nil, err
	}
	return r.timeline(ctx, r.db, id)
}
func (r *PostgresConversationRepository) timeline(ctx context.Context, db postgresDBTX, id string) ([]ConversationMessage, error) {
	rows, err := db.Query(ctx, `SELECT id,external_id,timestamp,timestamp_ns,sender,direction,source,text,system_event,content_unavailable,metadata FROM conversation_messages WHERE conversation_id=$1 ORDER BY timestamp,sequence`, id)
	if err != nil {
		return nil, fmt.Errorf("list conversation messages: %w", err)
	}
	defer rows.Close()
	result := []ConversationMessage{}
	for rows.Next() {
		var m ConversationMessage
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
func (r *PostgresConversationRepository) Save(ctx context.Context) error {
	ctx = postgresContext(ctx)
	if err := repositoryContextErr(ctx); err != nil {
		return err
	}
	return r.requireDB()
}
func (r *PostgresConversationRepository) upsertOn(ctx context.Context, db postgresDBTX, c EmployerConversation) error {
	args, err := conversationArgs(c)
	if err != nil {
		return err
	}
	if _, err = db.Exec(ctx, conversationUpdate, args...); err != nil {
		return mapPostgresConversationError("update conversation", err)
	}
	return nil
}
func (r *PostgresConversationRepository) loadConversation(ctx context.Context, db postgresDBTX, predicate string, arg any) (EmployerConversation, error) {
	return loadPostgresConversation(ctx, db, predicate, arg)
}
func (r *PostgresConversationRepository) withMutation(ctx context.Context, fn func(postgresDBTX) error) error {
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

func prepareApplicationCreate(value JobApplication) (JobApplication, error) {
	if value.ID == "" {
		var err error
		value.ID, err = newKnowledgeID("application")
		if err != nil {
			return JobApplication{}, err
		}
	}
	if value.Source == "" {
		value.Source = ApplicationSourceManual
	}
	if value.Status == "" {
		value.Status = ApplicationDiscovered
	}
	now := time.Now().UTC()
	if value.CreatedAt.IsZero() {
		value.CreatedAt = now
	}
	if value.UpdatedAt.IsZero() {
		value.UpdatedAt = value.CreatedAt
	}
	if value.MatchResult != nil {
		copyResult, err := cloneKnowledge(*value.MatchResult)
		if err != nil {
			return JobApplication{}, err
		}
		copyResult.normalize()
		value.MatchResult = &copyResult
	}
	if err := value.validate(); err != nil {
		return JobApplication{}, err
	}
	return value, nil
}
func prepareImportedApplication(value JobApplication) (JobApplication, error) {
	if value.Source == "" {
		value.Source = ApplicationSourceHH
	}
	if value.Status == "" {
		value.Status = ApplicationUnknown
	}
	if value.CreatedAt.IsZero() {
		value.CreatedAt = time.Now().UTC()
	}
	if value.UpdatedAt.IsZero() || value.UpdatedAt.Before(value.CreatedAt) {
		value.UpdatedAt = value.CreatedAt
	}
	if value.HHMetadata == nil {
		value.HHMetadata = map[string]string{}
	}
	if value.ID == "" {
		var err error
		value.ID, err = newKnowledgeID("application")
		if err != nil {
			return JobApplication{}, err
		}
	}
	if value.MatchResult != nil {
		value.MatchResult.normalize()
	}
	if err := value.validate(); err != nil {
		return JobApplication{}, err
	}
	return value, nil
}
func applicationArgs(v JobApplication) []any {
	match, _ := nullableJSON(v.MatchResult)
	meta, _ := nullableJSON(v.HHMetadata)
	evidence, _ := nullableJSON(v.ReconciliationEvidence)
	return []any{v.ID, v.ExternalID, v.VacancyID, v.ConversationID, v.CompanyName, v.VacancyTitle, v.VacancyURL, v.Source, v.Status, v.RawStatus, v.CreatedAt, v.UpdatedAt, v.CreatedAt.UnixNano(), v.UpdatedAt.UnixNano(), v.FollowUpState, v.Notes, v.NextAction, match, meta, v.Partial, v.DataCompleteness, evidence}
}
func scanPostgresApplication(scanner postgresVacancyScanner) (JobApplication, error) {
	var v JobApplication
	var created, updated pgtype.Timestamptz
	var cns, uns pgtype.Int8
	var match, meta, evidence []byte
	err := scanner.Scan(&v.ID, &v.ExternalID, &v.VacancyID, &v.ConversationID, &v.CompanyName, &v.VacancyTitle, &v.VacancyURL, &v.Source, &v.Status, &v.RawStatus, &created, &updated, &cns, &uns, &v.FollowUpState, &v.Notes, &v.NextAction, &match, &meta, &v.Partial, &v.DataCompleteness, &evidence)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return JobApplication{}, ErrApplicationNotFound
		}
		return JobApplication{}, fmt.Errorf("scan application: %w", err)
	}
	v.CreatedAt = postgresTimeExact(created, cns)
	v.UpdatedAt = postgresTimeExact(updated, uns)
	for _, field := range []struct {
		raw    []byte
		target any
		name   string
	}{{match, &v.MatchResult, "match result"}, {meta, &v.HHMetadata, "HH metadata"}, {evidence, &v.ReconciliationEvidence, "reconciliation evidence"}} {
		if err := decodeNullableJSON(field.raw, field.target); err != nil {
			return JobApplication{}, fmt.Errorf("decode application %s: %w", field.name, err)
		}
	}
	return cloneKnowledge(v)
}

func conversationArgs(v EmployerConversation) ([]any, error) {
	summary, err := nullableJSON(v.Summary)
	if err != nil {
		return nil, err
	}
	meta, err := nullableJSON(v.HHMetadata)
	if err != nil {
		return nil, err
	}
	return []any{v.ID, v.HHConversationID, v.VacancyID, nullableString(v.ApplicationID), v.CompanyName, v.VacancyTitle, v.VacancyDescription, v.Status, v.CreatedAt, v.UpdatedAt, v.CreatedAt.UnixNano(), v.UpdatedAt.UnixNano(), nullableTime(v.HHUpdatedAt), nullableUnixNano(v.HHUpdatedAt), nullableConversationPtr(v.LastEmployerMessageAt), nullableConversationPtrNanos(v.LastEmployerMessageAt), nullableConversationPtr(v.LastCandidateMessageAt), nullableConversationPtrNanos(v.LastCandidateMessageAt), summary, v.NextAction, nullableConversationPtr(v.WaitingSince), nullableConversationPtrNanos(v.WaitingSince), nullableConversationPtr(v.LastActivityAt), nullableConversationPtrNanos(v.LastActivityAt), v.FollowUpState, v.RawStatus, meta}, nil
}
func nullableString(v string) any {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	return v
}
func nullableConversationPtr(v *time.Time) any {
	if v == nil || v.IsZero() {
		return nil
	}
	return *v
}
func nullableConversationPtrNanos(v *time.Time) any {
	if v == nil || v.IsZero() {
		return nil
	}
	return v.UnixNano()
}
func prepareConversation(v EmployerConversation) (EmployerConversation, error) {
	if v.ID == "" {
		var err error
		v.ID, err = newKnowledgeID("conversation")
		if err != nil {
			return EmployerConversation{}, err
		}
	}
	if v.CreatedAt.IsZero() {
		v.CreatedAt = time.Now().UTC()
	}
	if v.Status == "" {
		v.Status = ConversationApplied
	}
	if v.FollowUpState == "" {
		v.FollowUpState = ConversationFollowUpNone
	}
	if v.Messages == nil {
		v.Messages = []ConversationMessage{}
	}
	v.UpdatedAt = time.Now().UTC()
	if v.UpdatedAt.Before(v.CreatedAt) {
		v.UpdatedAt = v.CreatedAt
	}
	v.refreshActivity()
	if v.LastActivityAt != nil && v.UpdatedAt.Before(*v.LastActivityAt) {
		v.UpdatedAt = *v.LastActivityAt
	}
	return v, nil
}
func validateConversationReplacement(old, next EmployerConversation) error {
	if !old.CreatedAt.Equal(next.CreatedAt) || (old.VacancyID != 0 && old.VacancyID != next.VacancyID) || (old.HHConversationID != "" && old.HHConversationID != next.HHConversationID) {
		return errors.New("cannot reassign an existing conversation")
	}
	for _, oldMessage := range old.Messages {
		found := false
		for _, incoming := range next.Messages {
			if oldMessage.ID == incoming.ID {
				found = true
				if !sameConversationMessage(oldMessage, incoming) {
					return errors.New("cannot rewrite original messages")
				}
				break
			}
		}
		if !found {
			return errors.New("cannot remove original messages")
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
func mergeConversationTimestamps(old, next EmployerConversation) EmployerConversation {
	if next.CreatedAt.IsZero() {
		next.CreatedAt = old.CreatedAt
	}
	if next.UpdatedAt.Before(old.UpdatedAt) {
		next.UpdatedAt = old.UpdatedAt
	}
	return next
}
func loadPostgresConversation(ctx context.Context, db postgresDBTX, predicate string, arg any) (EmployerConversation, error) {
	var c EmployerConversation
	var created, updated, hhUpdated pgtype.Timestamptz
	var cns, uns, hns pgtype.Int8
	var le, lc, la, waiting pgtype.Timestamptz
	var lens, lcns, lans, wns pgtype.Int8
	var applicationID *string
	var summary, meta []byte
	row := db.QueryRow(ctx, `SELECT `+conversationColumns+` FROM conversations WHERE `+predicate+` LIMIT 1`, arg)
	err := row.Scan(&c.ID, &c.HHConversationID, &c.VacancyID, &applicationID, &c.CompanyName, &c.VacancyTitle, &c.VacancyDescription, &c.Status, &created, &updated, &cns, &uns, &hhUpdated, &hns, &le, &lens, &lc, &lcns, &summary, &c.NextAction, &waiting, &wns, &la, &lans, &c.FollowUpState, &c.RawStatus, &meta)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return EmployerConversation{}, ErrConversationNotFound
		}
		return EmployerConversation{}, fmt.Errorf("scan conversation: %w", err)
	}
	if applicationID != nil {
		c.ApplicationID = *applicationID
	}
	c.CreatedAt = postgresTimeExact(created, cns)
	c.UpdatedAt = postgresTimeExact(updated, uns)
	c.HHUpdatedAt = postgresTimeExact(hhUpdated, hns)
	c.LastEmployerMessageAt = postgresConversationTimePtr(le, lens)
	c.LastCandidateMessageAt = postgresConversationTimePtr(lc, lcns)
	c.WaitingSince = postgresConversationTimePtr(waiting, wns)
	c.LastActivityAt = postgresConversationTimePtr(la, lans)
	if err := decodeNullableJSON(summary, &c.Summary); err != nil {
		return EmployerConversation{}, fmt.Errorf("decode conversation summary: %w", err)
	}
	if err := decodeNullableJSON(meta, &c.HHMetadata); err != nil {
		return EmployerConversation{}, fmt.Errorf("decode conversation metadata: %w", err)
	}
	messages, err := loadConversationMessages(ctx, db, c.ID)
	if err != nil {
		return EmployerConversation{}, err
	}
	c.Messages = messages
	return cloneKnowledge(c)
}
func (r *PostgresConversationRepository) loadConversationWithMessages(ctx context.Context, db postgresDBTX, predicate string, arg any) (EmployerConversation, error) {
	return loadPostgresConversation(ctx, db, predicate, arg)
}
func postgresConversationTimePtr(v pgtype.Timestamptz, ns pgtype.Int8) *time.Time {
	if !v.Valid && !ns.Valid {
		return nil
	}
	t := postgresTimeExact(v, ns)
	return &t
}
func scanPostgresConversation(scanner postgresVacancyScanner) (EmployerConversation, error) {
	return scanPostgresConversationWithDB(scanner, nil)
}
func scanPostgresConversationWithDB(scanner postgresVacancyScanner, _ postgresDBTX) (EmployerConversation, error) {
	var c EmployerConversation
	var created, updated, hhUpdated pgtype.Timestamptz
	var cns, uns, hns pgtype.Int8
	var le, lc, la, waiting pgtype.Timestamptz
	var lens, lcns, lans, wns pgtype.Int8
	var applicationID *string
	var summary, meta []byte
	err := scanner.Scan(&c.ID, &c.HHConversationID, &c.VacancyID, &applicationID, &c.CompanyName, &c.VacancyTitle, &c.VacancyDescription, &c.Status, &created, &updated, &cns, &uns, &hhUpdated, &hns, &le, &lens, &lc, &lcns, &summary, &c.NextAction, &waiting, &wns, &la, &lans, &c.FollowUpState, &c.RawStatus, &meta)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return EmployerConversation{}, ErrConversationNotFound
		}
		return EmployerConversation{}, fmt.Errorf("scan conversation: %w", err)
	}
	if applicationID != nil {
		c.ApplicationID = *applicationID
	}
	c.CreatedAt = postgresTimeExact(created, cns)
	c.UpdatedAt = postgresTimeExact(updated, uns)
	c.HHUpdatedAt = postgresTimeExact(hhUpdated, hns)
	c.LastEmployerMessageAt = postgresConversationTimePtr(le, lens)
	c.LastCandidateMessageAt = postgresConversationTimePtr(lc, lcns)
	c.WaitingSince = postgresConversationTimePtr(waiting, wns)
	c.LastActivityAt = postgresConversationTimePtr(la, lans)
	if err := decodeNullableJSON(summary, &c.Summary); err != nil {
		return EmployerConversation{}, err
	}
	if err := decodeNullableJSON(meta, &c.HHMetadata); err != nil {
		return EmployerConversation{}, err
	}
	return c, nil
}
func loadConversationMessages(ctx context.Context, db postgresDBTX, id string) ([]ConversationMessage, error) {
	rows, err := db.Query(ctx, `SELECT id,external_id,timestamp,timestamp_ns,sender,direction,source,text,system_event,content_unavailable,metadata FROM conversation_messages WHERE conversation_id=$1 ORDER BY timestamp,sequence`, id)
	if err != nil {
		return nil, fmt.Errorf("list conversation messages: %w", err)
	}
	defer rows.Close()
	result := []ConversationMessage{}
	for rows.Next() {
		var m ConversationMessage
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
	return result, rows.Err()
}
func ensureApplication(ctx context.Context, db postgresDBTX, id string) error {
	var found string
	if err := db.QueryRow(ctx, `SELECT id FROM applications WHERE id=$1`, id).Scan(&found); err != nil {
		return mapPostgresNotFound("find application", err, ErrApplicationNotFound)
	}
	return nil
}
func appendApplicationEvent(ctx context.Context, db postgresDBTX, event ApplicationEvent) error {
	if err := event.validate(); err != nil {
		return err
	}
	payload, _ := json.Marshal(map[string]string{"description": event.Description})
	if _, err := db.Exec(ctx, `INSERT INTO application_events (id,application_id,event_type,created_at,created_at_ns,payload) VALUES ($1,$2,$3,$4,$5,$6)`, event.ID, event.ApplicationID, event.Type, event.Timestamp, event.Timestamp.UnixNano(), payload); err != nil {
		return mapPostgresApplicationError("append application event", err)
	}
	return nil
}
func updateApplicationField(ctx context.Context, db postgresDBTX, id, field string, value any) error {
	if field != "follow_up_state" {
		return errors.New("unsupported application field")
	}
	var created pgtype.Timestamptz
	var cns pgtype.Int8
	if err := db.QueryRow(ctx, `SELECT created_at,created_at_ns FROM applications WHERE id=$1 FOR UPDATE`, id).Scan(&created, &cns); err != nil {
		return mapPostgresNotFound("find application", err, ErrApplicationNotFound)
	}
	at := time.Now().UTC()
	if at.Before(postgresTimeExact(created, cns)) {
		at = postgresTimeExact(created, cns)
	}
	if _, err := db.Exec(ctx, `UPDATE applications SET follow_up_state=$2,updated_at=$3,updated_at_ns=$4 WHERE id=$1`, id, value, at, at.UnixNano()); err != nil {
		return mapPostgresApplicationError("update application field", err)
	}
	return nil
}

func mapPostgresNotFound(action string, err error, sentinel error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return sentinel
	}
	return fmt.Errorf("%s: %w", action, err)
}
func mapPostgresApplicationError(action string, err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505":
			if pgErr.ConstraintName == "applications_external_id_unique" {
				return fmt.Errorf("%s: %w", action, ErrDuplicateExternalID)
			}
			return fmt.Errorf("%s: %w", action, ErrRepositoryConflict)
		case "23503":
			if pgErr.ConstraintName == "application_events_application_fk" {
				return fmt.Errorf("%s: %w", action, ErrApplicationNotFound)
			}
			if pgErr.ConstraintName == "applications_vacancy_fk" {
				return fmt.Errorf("%s: %w", action, ErrVacancyNotFound)
			}
			return fmt.Errorf("%s: %w", action, ErrRepositoryConflict)
		}
	}
	return fmt.Errorf("%s: %w", action, err)
}
func mapPostgresConversationError(action string, err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505":
			if pgErr.ConstraintName == "conversations_external_id_unique" {
				return fmt.Errorf("%s: %w", action, ErrDuplicateConversationExternal)
			}
			if pgErr.ConstraintName == "conversation_messages_external_id_unique" {
				return fmt.Errorf("%s: %w", action, ErrRepositoryConflict)
			}
			return fmt.Errorf("%s: %w", action, ErrRepositoryConflict)
		case "23503":
			switch pgErr.ConstraintName {
			case "conversations_vacancy_fk":
				return fmt.Errorf("%s: %w", action, ErrVacancyNotFound)
			case "conversations_application_fk":
				return fmt.Errorf("%s: %w", action, ErrApplicationNotFound)
			case "conversation_messages_conversation_fk":
				return fmt.Errorf("%s: %w", action, ErrConversationNotFound)
			}
			return fmt.Errorf("%s: %w", action, ErrRepositoryConflict)
		}
	}
	return fmt.Errorf("%s: %w", action, err)
}

var _ ApplicationRepository = (*PostgresApplicationRepository)(nil)
var _ ConversationRepository = (*PostgresConversationRepository)(nil)

// Keep the transaction API intentionally small and concrete. JSON stores do
// not implement it because staged multi-file writes are not atomic.
type CareerTx interface {
	Vacancies() VacancyRepository
	Applications() ApplicationRepository
	Conversations() ConversationRepository
}

// CareerRepositoriesTx is the descriptive compatibility name for callers
// that prefer the transaction boundary to be explicit in the type name.
type CareerRepositoriesTx = CareerTx
type PostgresCareerStore struct{ pool *pgxpool.Pool }

func NewPostgresCareerStore(pool *pgxpool.Pool) *PostgresCareerStore {
	return &PostgresCareerStore{pool: pool}
}
func (s *PostgresCareerStore) WithTx(ctx context.Context, fn func(CareerTx) error) error {
	ctx = postgresContext(ctx)
	if s == nil || s.pool == nil {
		return errors.New("postgres career store is not configured")
	}
	if fn == nil {
		return errors.New("postgres transaction callback is required")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin postgres career transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	repos := postgresCareerTx{vacancies: newPostgresVacancyRepositoryTx(tx), applications: newPostgresApplicationRepositoryTx(tx), conversations: newPostgresConversationRepositoryTx(tx)}
	if err := fn(repos); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit postgres career transaction: %w", err)
	}
	return nil
}

type postgresCareerTx struct {
	vacancies     *PostgresVacancyRepository
	applications  *PostgresApplicationRepository
	conversations *PostgresConversationRepository
}

func (t postgresCareerTx) Vacancies() VacancyRepository          { return t.vacancies }
func (t postgresCareerTx) Applications() ApplicationRepository   { return t.applications }
func (t postgresCareerTx) Conversations() ConversationRepository { return t.conversations }
