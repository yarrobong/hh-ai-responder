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

	"hh-ai-responder/internal/application"
	"hh-ai-responder/internal/conversation"
	"hh-ai-responder/internal/ports"
	"hh-ai-responder/internal/vacancy"
)

var (
	ErrApplicationNotFound    = application.ErrApplicationNotFound
	ErrDuplicateExternalID    = application.ErrDuplicateExternalID
	ErrDuplicateApplicationID = application.ErrDuplicateApplicationID
	ErrRepositoryConflict     = errors.New("repository conflict")
)

const applicationColumns = `
	id, external_id, vacancy_id, conversation_id, company_name, vacancy_title,
	vacancy_url, source, status, raw_status, created_at, updated_at,
	created_at_ns, updated_at_ns, follow_up_state, notes, next_action,
	match_result, hh_metadata, partial, data_completeness, reconciliation_evidence`

const applicationInsert = `INSERT INTO applications (` + applicationColumns + `)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22)`

const applicationUpdate = `UPDATE applications SET
	external_id=$2, vacancy_id=$3, conversation_id=$4, company_name=$5,
	vacancy_title=$6, vacancy_url=$7, source=$8, status=$9, raw_status=$10,
	created_at=$11, updated_at=$12, created_at_ns=$13, updated_at_ns=$14,
	follow_up_state=$15, notes=$16, next_action=$17, match_result=$18,
	hh_metadata=$19, partial=$20, data_completeness=$21,
	reconciliation_evidence=$22 WHERE id=$1`

// ApplicationRepository is the PostgreSQL implementation of the normalized
// application persistence contract. It stores only scalar relation IDs and
// persisted value objects; it never loads another repository.
type ApplicationRepository struct {
	pool *pgxpool.Pool
	db   postgresDBTX
}

func NewApplicationRepository(pool *pgxpool.Pool) *ApplicationRepository {
	var db postgresDBTX
	if pool != nil {
		db = pool
	}
	return &ApplicationRepository{pool: pool, db: db}
}

// NewApplicationRepositoryForTx binds the adapter to a caller-owned
// transaction. The transaction is not exposed through the persistence ports.
func NewApplicationRepositoryForTx(tx pgx.Tx) *ApplicationRepository {
	return &ApplicationRepository{db: tx}
}

// Pool is a composition-only inspection hook used by the root reconciliation
// wiring. It is not part of the application persistence port.
func (r *ApplicationRepository) Pool() *pgxpool.Pool {
	if r == nil {
		return nil
	}
	return r.pool
}

func (r *ApplicationRepository) requireDB() error {
	if r == nil || r.db == nil {
		return errors.New("application repository is not configured")
	}
	return nil
}

func (r *ApplicationRepository) Get(ctx context.Context, id string) (application.JobApplication, error) {
	ctx = postgresContext(ctx)
	if err := repositoryContextErr(ctx); err != nil {
		return application.JobApplication{}, err
	}
	if err := r.requireDB(); err != nil {
		return application.JobApplication{}, err
	}
	return scanPostgresApplication(r.db.QueryRow(ctx, `SELECT `+applicationColumns+` FROM applications WHERE id=$1`, id))
}

func (r *ApplicationRepository) GetByExternalID(ctx context.Context, externalID string) (application.JobApplication, error) {
	ctx = postgresContext(ctx)
	if err := repositoryContextErr(ctx); err != nil {
		return application.JobApplication{}, err
	}
	if err := r.requireDB(); err != nil {
		return application.JobApplication{}, err
	}
	return scanPostgresApplication(r.db.QueryRow(ctx, `SELECT `+applicationColumns+` FROM applications WHERE external_id=$1 AND btrim(external_id)<>''`, externalID))
}

func (r *ApplicationRepository) List(ctx context.Context) ([]application.JobApplication, error) {
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
	result := []application.JobApplication{}
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

// ListEvents is a compatibility read used by migration planning and
// verification. The append-only ledger is authoritative.
func (r *ApplicationRepository) ListEvents(ctx context.Context) ([]application.Event, error) {
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
	result := []application.Event{}
	for rows.Next() {
		event, scanErr := scanPostgresApplicationEvent(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		result = append(result, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate application events: %w", err)
	}
	return result, nil
}

func (r *ApplicationRepository) Create(ctx context.Context, value application.JobApplication) (application.JobApplication, error) {
	ctx = postgresContext(ctx)
	if err := repositoryContextErr(ctx); err != nil {
		return application.JobApplication{}, err
	}
	if err := r.requireDB(); err != nil {
		return application.JobApplication{}, err
	}
	value, err := prepareApplicationCreate(value)
	if err != nil {
		return application.JobApplication{}, err
	}
	event, err := newApplicationEvent(value.ID, value.CreatedAt, application.EventCreated, "application created")
	if err != nil {
		return application.JobApplication{}, err
	}
	err = r.withMutation(ctx, func(db postgresDBTX) error {
		if _, err := db.Exec(ctx, applicationInsert, applicationArgs(value)...); err != nil {
			return mapPostgresApplicationError("create application", err)
		}
		return appendApplicationEvent(ctx, db, event)
	})
	if err != nil {
		return application.JobApplication{}, err
	}
	return cloneApplication(value)
}

// Import inserts a legacy application snapshot exactly as supplied. It does
// not synthesize an event; source history is imported separately.
func (r *ApplicationRepository) Import(ctx context.Context, value application.JobApplication) error {
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
	if _, err := r.db.Exec(ctx, applicationInsert, applicationArgs(value)...); err != nil {
		return mapPostgresApplicationError("import application", err)
	}
	return nil
}

// ImportEvent appends one supplied immutable event. It never updates an
// existing event row.
func (r *ApplicationRepository) ImportEvent(ctx context.Context, event application.Event) error {
	ctx = postgresContext(ctx)
	if err := repositoryContextErr(ctx); err != nil {
		return err
	}
	if err := r.requireDB(); err != nil {
		return err
	}
	return appendApplicationEvent(ctx, r.db, event)
}

func (r *ApplicationRepository) Update(ctx context.Context, value application.JobApplication) error {
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
		current, err := scanPostgresApplication(db.QueryRow(ctx, `SELECT `+applicationColumns+` FROM applications WHERE id=$1 FOR UPDATE`, value.ID))
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
		if err := value.Validate(); err != nil {
			return err
		}
		if _, err := db.Exec(ctx, applicationUpdate, applicationArgs(value)...); err != nil {
			return mapPostgresApplicationError("update application", err)
		}
		return nil
	})
}

func (r *ApplicationRepository) UpsertImported(ctx context.Context, value application.JobApplication) (application.JobApplication, bool, error) {
	ctx = postgresContext(ctx)
	if err := repositoryContextErr(ctx); err != nil {
		return application.JobApplication{}, false, err
	}
	if err := r.requireDB(); err != nil {
		return application.JobApplication{}, false, err
	}
	if strings.TrimSpace(value.ExternalID) == "" {
		return application.JobApplication{}, false, errors.New("imported application requires an external id")
	}
	value, err := prepareImportedApplication(value)
	if err != nil {
		return application.JobApplication{}, false, err
	}
	var result application.JobApplication
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
				result, err = cloneApplication(old)
				return err
			}
			if err := value.Validate(); err != nil {
				return err
			}
			if _, err := db.Exec(ctx, applicationUpdate, applicationArgs(value)...); err != nil {
				return mapPostgresApplicationError("update imported application", err)
			}
			result, err = cloneApplication(value)
			return err
		}
		if _, err := db.Exec(ctx, applicationInsert, applicationArgs(value)...); err != nil {
			return mapPostgresApplicationError("import application", err)
		}
		event, eventErr := newApplicationEvent(value.ID, value.CreatedAt, application.EventCreated, "application created")
		if eventErr != nil {
			return eventErr
		}
		if err := appendApplicationEvent(ctx, db, event); err != nil {
			return err
		}
		created = true
		result, err = cloneApplication(value)
		return err
	})
	return result, created, err
}

func (r *ApplicationRepository) AttachImportedConversation(ctx context.Context, externalID, conversationID string) error {
	return r.attachConversationByExternal(ctx, externalID, conversationID)
}

func (r *ApplicationRepository) attachConversationByExternal(ctx context.Context, externalID, conversationID string) error {
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

func (r *ApplicationRepository) AttachConversation(ctx context.Context, applicationID, conversationID string) error {
	ctx = postgresContext(ctx)
	if err := repositoryContextErr(ctx); err != nil {
		return err
	}
	if err := r.requireDB(); err != nil {
		return err
	}
	return r.withMutation(ctx, func(db postgresDBTX) error {
		return attachConversationSQL(ctx, db, applicationID, conversationID)
	})
}

// attachConversationSQL preserves the existing bidirectional SQL-side link
// update. It stores relation IDs directly and does not invoke a repository.
func attachConversationSQL(ctx context.Context, db postgresDBTX, applicationID, conversationID string) error {
	var applicationVacancy, conversationVacancy int
	var currentConversationID *string
	if err := db.QueryRow(ctx, `SELECT vacancy_id,conversation_id FROM applications WHERE id=$1 FOR UPDATE`, applicationID).Scan(&applicationVacancy, &currentConversationID); err != nil {
		return mapPostgresNotFound("find application", err, ErrApplicationNotFound)
	}
	if err := db.QueryRow(ctx, `SELECT vacancy_id FROM conversations WHERE id=$1 FOR UPDATE`, conversationID).Scan(&conversationVacancy); err != nil {
		return mapPostgresNotFound("find conversation", err, conversation.ErrConversationNotFound)
	}
	if applicationVacancy != conversationVacancy {
		return errors.New("conversation vacancy does not match application vacancy")
	}
	var linkedApplication *string
	if err := db.QueryRow(ctx, `SELECT application_id FROM conversations WHERE id=$1 FOR UPDATE`, conversationID).Scan(&linkedApplication); err != nil {
		return mapPostgresNotFound("find conversation", err, conversation.ErrConversationNotFound)
	}
	if linkedApplication != nil && *linkedApplication != applicationID {
		return fmt.Errorf("%w: conversation already belongs to another application", ErrRepositoryConflict)
	}
	at := time.Now().UTC()
	if currentConversationID == nil || *currentConversationID != conversationID {
		if _, err := db.Exec(ctx, `UPDATE applications SET conversation_id=$2, updated_at=$3, updated_at_ns=$4 WHERE id=$1`, applicationID, conversationID, at, at.UnixNano()); err != nil {
			return mapPostgresApplicationError("attach application conversation", err)
		}
	}
	if _, err := db.Exec(ctx, `UPDATE conversations SET application_id=$2 WHERE id=$1`, conversationID, applicationID); err != nil {
		return mapPostgresApplicationRelationError("attach conversation application", err)
	}
	return nil
}

func (r *ApplicationRepository) UpdateStatus(ctx context.Context, id string, status application.Status) error {
	ctx = postgresContext(ctx)
	if err := repositoryContextErr(ctx); err != nil {
		return err
	}
	if err := r.requireDB(); err != nil {
		return err
	}
	if !application.IsValidStatus(status) {
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
		event, err := newApplicationEvent(id, updated.UpdatedAt, application.EventTypeForStatus(status), fmt.Sprintf("status changed from %q to %q", current.Status, status))
		if err != nil {
			return err
		}
		return appendApplicationEvent(ctx, db, event)
	})
}

func (r *ApplicationRepository) SetFollowUpState(ctx context.Context, id string, state conversation.FollowUpState) error {
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
	return r.withMutation(ctx, func(db postgresDBTX) error {
		return updateApplicationField(ctx, db, id, "follow_up_state", state)
	})
}

func (r *ApplicationRepository) SaveMatchResult(ctx context.Context, id string, result vacancy.MatchResult) error {
	ctx = postgresContext(ctx)
	if err := repositoryContextErr(ctx); err != nil {
		return err
	}
	if err := r.requireDB(); err != nil {
		return err
	}
	if err := result.Validate(); err != nil {
		return err
	}
	result.Normalize()
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
		event, err := newApplicationEvent(id, updated.UpdatedAt, application.EventMatched, "vacancy match result saved")
		if err != nil {
			return err
		}
		return appendApplicationEvent(ctx, db, event)
	})
}

func (r *ApplicationRepository) AppendEvent(ctx context.Context, id string, at time.Time, eventType application.EventType, description string) error {
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

func (r *ApplicationRepository) Timeline(ctx context.Context, id string) ([]application.Event, error) {
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
	result := []application.Event{}
	for rows.Next() {
		event, scanErr := scanPostgresApplicationEvent(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		result = append(result, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate application events: %w", err)
	}
	return result, nil
}

// PostgreSQL mutations are immediate. Save remains a context/configuration
// check for parity with the port and does not add an in-memory buffer.
func (r *ApplicationRepository) Save(ctx context.Context) error {
	ctx = postgresContext(ctx)
	if err := repositoryContextErr(ctx); err != nil {
		return err
	}
	return r.requireDB()
}

func (r *ApplicationRepository) withMutation(ctx context.Context, fn func(postgresDBTX) error) error {
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

func prepareApplicationCreate(value application.JobApplication) (application.JobApplication, error) {
	if value.ID == "" {
		var err error
		value.ID, err = newApplicationID("application")
		if err != nil {
			return application.JobApplication{}, err
		}
	}
	if value.Source == "" {
		value.Source = application.SourceManual
	}
	if value.Status == "" {
		value.Status = application.StatusDiscovered
	}
	now := time.Now().UTC()
	if value.CreatedAt.IsZero() {
		value.CreatedAt = now
	}
	if value.UpdatedAt.IsZero() {
		value.UpdatedAt = value.CreatedAt
	}
	if value.MatchResult != nil {
		raw, err := json.Marshal(*value.MatchResult)
		if err != nil {
			return application.JobApplication{}, err
		}
		var copyResult vacancy.MatchResult
		if err := json.Unmarshal(raw, &copyResult); err != nil {
			return application.JobApplication{}, err
		}
		copyResult.Normalize()
		value.MatchResult = &copyResult
	}
	if err := value.Validate(); err != nil {
		return application.JobApplication{}, err
	}
	return value, nil
}

func prepareImportedApplication(value application.JobApplication) (application.JobApplication, error) {
	if value.Source == "" {
		value.Source = application.SourceHH
	}
	if value.Status == "" {
		value.Status = application.StatusUnknown
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
		value.ID, err = newApplicationID("application")
		if err != nil {
			return application.JobApplication{}, err
		}
	}
	if value.MatchResult != nil {
		value.MatchResult.Normalize()
	}
	if err := value.Validate(); err != nil {
		return application.JobApplication{}, err
	}
	return value, nil
}

func applicationArgs(value application.JobApplication) []interface{} {
	match, _ := nullableJSON(value.MatchResult)
	meta, _ := nullableJSON(value.HHMetadata)
	evidence, _ := nullableJSON(value.ReconciliationEvidence)
	return []interface{}{value.ID, value.ExternalID, value.VacancyID, value.ConversationID, value.CompanyName, value.VacancyTitle, value.VacancyURL, value.Source, value.Status, value.RawStatus, value.CreatedAt, value.UpdatedAt, value.CreatedAt.UnixNano(), value.UpdatedAt.UnixNano(), value.FollowUpState, value.Notes, value.NextAction, match, meta, value.Partial, value.DataCompleteness, evidence}
}

type applicationRowScanner interface {
	Scan(dest ...interface{}) error
}

func scanPostgresApplication(scanner applicationRowScanner) (application.JobApplication, error) {
	var value application.JobApplication
	var conversationID *string
	var created, updated pgtype.Timestamptz
	var createdNS, updatedNS pgtype.Int8
	var match, meta, evidence []byte
	err := scanner.Scan(&value.ID, &value.ExternalID, &value.VacancyID, &conversationID, &value.CompanyName, &value.VacancyTitle, &value.VacancyURL, &value.Source, &value.Status, &value.RawStatus, &created, &updated, &createdNS, &updatedNS, &value.FollowUpState, &value.Notes, &value.NextAction, &match, &meta, &value.Partial, &value.DataCompleteness, &evidence)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return application.JobApplication{}, ErrApplicationNotFound
		}
		return application.JobApplication{}, fmt.Errorf("scan application: %w", err)
	}
	if conversationID != nil {
		value.ConversationID = *conversationID
	}
	value.CreatedAt = postgresTimeExact(created, createdNS)
	value.UpdatedAt = postgresTimeExact(updated, updatedNS)
	for _, field := range []struct {
		raw    []byte
		target interface{}
		name   string
	}{{match, &value.MatchResult, "match result"}, {meta, &value.HHMetadata, "HH metadata"}, {evidence, &value.ReconciliationEvidence, "reconciliation evidence"}} {
		if err := decodeNullableJSON(field.raw, field.target); err != nil {
			return application.JobApplication{}, fmt.Errorf("decode application %s: %w", field.name, err)
		}
	}
	return cloneApplication(value)
}

func scanPostgresApplicationEvent(scanner applicationRowScanner) (application.Event, error) {
	var event application.Event
	var at pgtype.Timestamptz
	var ns pgtype.Int8
	var payload []byte
	if err := scanner.Scan(&event.ID, &event.ApplicationID, &at, &ns, &event.Type, &payload); err != nil {
		return application.Event{}, fmt.Errorf("scan application event: %w", err)
	}
	event.Timestamp = postgresTimeExact(at, ns)
	var body struct {
		Description string `json:"description"`
	}
	if err := decodeNullableJSON(payload, &body); err != nil {
		return application.Event{}, fmt.Errorf("decode application event: %w", err)
	}
	event.Description = body.Description
	return event, nil
}

func ensureApplication(ctx context.Context, db postgresDBTX, id string) error {
	var found string
	if err := db.QueryRow(ctx, `SELECT id FROM applications WHERE id=$1`, id).Scan(&found); err != nil {
		return mapPostgresNotFound("find application", err, ErrApplicationNotFound)
	}
	return nil
}

func appendApplicationEvent(ctx context.Context, db postgresDBTX, event application.Event) error {
	if err := event.Validate(); err != nil {
		return err
	}
	payload, err := json.Marshal(map[string]string{"description": event.Description})
	if err != nil {
		return fmt.Errorf("encode application event: %w", err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO application_events (id,application_id,event_type,created_at,created_at_ns,payload) VALUES ($1,$2,$3,$4,$5,$6)`, event.ID, event.ApplicationID, event.Type, event.Timestamp, event.Timestamp.UnixNano(), payload); err != nil {
		return mapPostgresApplicationError("append application event", err)
	}
	return nil
}

func updateApplicationField(ctx context.Context, db postgresDBTX, id, field string, value interface{}) error {
	if field != "follow_up_state" {
		return errors.New("unsupported application field")
	}
	var created pgtype.Timestamptz
	var createdNS pgtype.Int8
	if err := db.QueryRow(ctx, `SELECT created_at,created_at_ns FROM applications WHERE id=$1 FOR UPDATE`, id).Scan(&created, &createdNS); err != nil {
		return mapPostgresNotFound("find application", err, ErrApplicationNotFound)
	}
	at := time.Now().UTC()
	if at.Before(postgresTimeExact(created, createdNS)) {
		at = postgresTimeExact(created, createdNS)
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
			switch pgErr.ConstraintName {
			case "application_events_application_fk":
				return fmt.Errorf("%s: %w", action, ErrApplicationNotFound)
			case "applications_vacancy_fk":
				return fmt.Errorf("%s: %w", action, vacancy.ErrVacancyNotFound)
			case "conversations_application_fk":
				return fmt.Errorf("%s: %w", action, ErrApplicationNotFound)
			}
			return fmt.Errorf("%s: %w", action, ErrRepositoryConflict)
		}
	}
	return fmt.Errorf("%s: %w", action, err)
}

func mapPostgresApplicationRelationError(action string, err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23503":
			if pgErr.ConstraintName == "conversations_application_fk" {
				return fmt.Errorf("%s: %w", action, ErrApplicationNotFound)
			}
		}
	}
	return fmt.Errorf("%s: %w", action, err)
}

func newApplicationEvent(applicationID string, timestamp time.Time, eventType application.EventType, description string) (application.Event, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return application.Event{}, fmt.Errorf("generate application event id: %w", err)
	}
	event := application.Event{ID: fmt.Sprintf("application-event-%x", value), ApplicationID: applicationID, Timestamp: timestamp, Type: eventType, Description: description}
	if err := event.Validate(); err != nil {
		return application.Event{}, err
	}
	return event, nil
}

func newApplicationID(kind string) (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", fmt.Errorf("generate application id: %w", err)
	}
	return fmt.Sprintf("%s-%x", kind, value), nil
}

func cloneApplication(value application.JobApplication) (application.JobApplication, error) {
	var copy application.JobApplication
	raw, err := json.Marshal(value)
	if err != nil {
		return copy, err
	}
	if err := json.Unmarshal(raw, &copy); err != nil {
		return copy, err
	}
	return copy, nil
}

func applicationEquivalent(a, b application.JobApplication) bool {
	return a.FollowUpState == b.FollowUpState && a.ID == b.ID && a.VacancyID == b.VacancyID && a.ExternalID == b.ExternalID &&
		a.CompanyName == b.CompanyName && a.VacancyTitle == b.VacancyTitle && a.VacancyURL == b.VacancyURL &&
		a.Source == b.Source && a.CreatedAt.Equal(b.CreatedAt) && a.UpdatedAt.Equal(b.UpdatedAt) && a.Status == b.Status &&
		reflect.DeepEqual(a.MatchResult, b.MatchResult) && a.ConversationID == b.ConversationID &&
		a.Notes == b.Notes && a.NextAction == b.NextAction && a.RawStatus == b.RawStatus &&
		reflect.DeepEqual(a.HHMetadata, b.HHMetadata) && a.Partial == b.Partial && a.DataCompleteness == b.DataCompleteness &&
		reflect.DeepEqual(a.ReconciliationEvidence, b.ReconciliationEvidence)
}

var _ ports.ApplicationReader = (*ApplicationRepository)(nil)
var _ ports.ApplicationWriter = (*ApplicationRepository)(nil)
var _ ports.ApplicationStore = (*ApplicationRepository)(nil)
