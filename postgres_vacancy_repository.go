package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

const vacancyColumns = `
	id, external_id, name, title, description, requirements, skills,
	salary, salary_currency, location, work_format, employment_type, source,
	published_at, hh_updated_at, hh_metadata, created_at, updated_at,
	published_at_ns, hh_updated_at_ns, created_at_ns, updated_at_ns,
	work_schedule, work_experience, links, total_responses_count,
	area_name, company_id, company_name, company_site_url, compensation,
	creation_time, last_change_time, user_labels, response_letter_required,
	user_test_present, archived, response_url, total_responses_count_known,
	match_result, application_recommendation, data_completeness,
	reconciliation_evidence`

const vacancySelect = `SELECT ` + vacancyColumns + ` FROM vacancies`

const vacancyInsert = `
	INSERT INTO vacancies (` + vacancyColumns + `)
	VALUES (
		$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13,
		$14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24, $25,
		$26, $27, $28, $29, $30, $31, $32, $33, $34, $35, $36, $37,
		$38, $39, $40, $41, $42, $43
	)`

const vacancyUpdate = `
	UPDATE vacancies SET
		external_id = $2, name = $3, title = $4, description = $5,
		requirements = $6, skills = $7, salary = $8, salary_currency = $9,
		location = $10, work_format = $11, employment_type = $12, source = $13,
		published_at = $14, hh_updated_at = $15, hh_metadata = $16,
		created_at = $17, updated_at = $18, published_at_ns = $19,
		hh_updated_at_ns = $20, created_at_ns = $21, updated_at_ns = $22,
		work_schedule = $23, work_experience = $24, links = $25,
		total_responses_count = $26, area_name = $27, company_id = $28,
		company_name = $29, company_site_url = $30, compensation = $31,
		creation_time = $32, last_change_time = $33, user_labels = $34,
		response_letter_required = $35, user_test_present = $36,
		archived = $37, response_url = $38,
		total_responses_count_known = $39, match_result = $40,
		application_recommendation = $41, data_completeness = $42,
		reconciliation_evidence = $43
	WHERE id = $1`

// PostgresVacancyRepository is the PostgreSQL implementation of the existing
// VacancyRepository contract. It deliberately stores domain IDs supplied by
// callers and has no HH or AI side effects.
type PostgresVacancyRepository struct {
	pool *pgxpool.Pool
	db   postgresDBTX
}

func NewPostgresVacancyRepository(pool *pgxpool.Pool) *PostgresVacancyRepository {
	var db postgresDBTX
	if pool != nil {
		db = pool
	}
	return &PostgresVacancyRepository{pool: pool, db: db}
}

func newPostgresVacancyRepositoryTx(tx postgresDBTX) *PostgresVacancyRepository {
	return &PostgresVacancyRepository{db: tx}
}

func (r *PostgresVacancyRepository) Get(ctx context.Context, id int) (Vacancy, error) {
	ctx = postgresContext(ctx)
	if err := repositoryContextErr(ctx); err != nil {
		return Vacancy{}, err
	}
	if err := r.requirePool(); err != nil {
		return Vacancy{}, err
	}
	return scanPostgresVacancy(r.db.QueryRow(ctx, vacancySelect+" WHERE id = $1", id))
}

func (r *PostgresVacancyRepository) GetByExternalID(ctx context.Context, externalID string) (Vacancy, error) {
	ctx = postgresContext(ctx)
	if err := repositoryContextErr(ctx); err != nil {
		return Vacancy{}, err
	}
	if err := r.requirePool(); err != nil {
		return Vacancy{}, err
	}
	return scanPostgresVacancy(r.db.QueryRow(ctx, vacancySelect+" WHERE external_id = $1 ORDER BY id LIMIT 1", externalID))
}

func (r *PostgresVacancyRepository) List(ctx context.Context, query VacancyQuery) ([]Vacancy, error) {
	ctx = postgresContext(ctx)
	if err := repositoryContextErr(ctx); err != nil {
		return nil, err
	}
	if err := r.requirePool(); err != nil {
		return nil, err
	}

	sql := vacancySelect + " WHERE TRUE"
	args := make([]any, 0, 2)
	if query.ID != nil {
		args = append(args, *query.ID)
		sql += fmt.Sprintf(" AND id = $%d", len(args))
	}
	if strings.TrimSpace(query.ExternalID) != "" {
		args = append(args, query.ExternalID)
		sql += fmt.Sprintf(" AND external_id = $%d", len(args))
	}
	sql += " ORDER BY id"

	rows, err := r.db.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("list vacancies: %w", err)
	}
	defer rows.Close()
	values := make([]Vacancy, 0)
	for rows.Next() {
		value, err := scanPostgresVacancy(rows)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate vacancies: %w", err)
	}
	return values, nil
}

func (r *PostgresVacancyRepository) Create(ctx context.Context, value Vacancy) (Vacancy, error) {
	ctx = postgresContext(ctx)
	if err := repositoryContextErr(ctx); err != nil {
		return Vacancy{}, err
	}
	if err := r.requirePool(); err != nil {
		return Vacancy{}, err
	}
	// JSON creates an ID when the caller omits it. Keep that compatibility
	// behavior without replacing an explicitly supplied domain ID.
	value = vacancyForCreate(value)
	if err := validateVacancy(value); err != nil {
		return Vacancy{}, err
	}
	args, err := vacancyArgs(value)
	if err != nil {
		return Vacancy{}, err
	}

	tx, owned, err := r.beginMutation(ctx)
	if err != nil {
		return Vacancy{}, fmt.Errorf("begin create vacancy: %w", err)
	}
	db := r.db
	if owned {
		db = tx
	}
	if owned {
		defer func() { _ = tx.Rollback(ctx) }()
	}
	if value.ID == 0 {
		// Serialize only the compatibility ID allocation. Domain IDs supplied
		// by callers never pass through this path.
		if _, err := db.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('hh-ai-responder.vacancy-id'))`); err != nil {
			return Vacancy{}, fmt.Errorf("lock vacancy id allocation: %w", err)
		}
		if err := db.QueryRow(ctx, `SELECT COALESCE(MAX(id), 0) + 1 FROM vacancies`).Scan(&value.ID); err != nil {
			return Vacancy{}, fmt.Errorf("allocate vacancy id: %w", err)
		}
		args[0] = value.ID
	}
	if _, err := db.Exec(ctx, vacancyInsert, args...); err != nil {
		return Vacancy{}, mapPostgresVacancyError("create vacancy", err)
	}
	if owned {
		if err := tx.Commit(ctx); err != nil {
			return Vacancy{}, fmt.Errorf("commit create vacancy: %w", err)
		}
	}
	return cloneKnowledge(value)
}

// Import inserts an already validated legacy snapshot without applying the
// normal create-time defaults. It is intentionally insert-only: the migration
// planner has already classified the row and must never use this method to
// overwrite an existing vacancy.
func (r *PostgresVacancyRepository) Import(ctx context.Context, value Vacancy) error {
	ctx = postgresContext(ctx)
	if err := repositoryContextErr(ctx); err != nil {
		return err
	}
	if err := r.requirePool(); err != nil {
		return err
	}
	if err := validateVacancy(value); err != nil {
		return err
	}
	args, err := vacancyArgs(value)
	if err != nil {
		return err
	}
	if _, err := r.db.Exec(ctx, vacancyInsert, args...); err != nil {
		return mapPostgresVacancyError("import vacancy", err)
	}
	return nil
}

func (r *PostgresVacancyRepository) Update(ctx context.Context, value Vacancy) error {
	ctx = postgresContext(ctx)
	if err := repositoryContextErr(ctx); err != nil {
		return err
	}
	if err := r.requirePool(); err != nil {
		return err
	}
	if value.ID == 0 {
		return errors.New("vacancy update requires an id")
	}

	tx, owned, err := r.beginMutation(ctx)
	if err != nil {
		return fmt.Errorf("begin update vacancy: %w", err)
	}
	db := r.db
	if owned {
		db = tx
	}
	if owned {
		defer func() { _ = tx.Rollback(ctx) }()
	}
	var existingCreatedAt pgtype.Timestamptz
	var existingCreatedAtNanos pgtype.Int8
	if err := db.QueryRow(ctx, `SELECT created_at, created_at_ns FROM vacancies WHERE id = $1 FOR UPDATE`, value.ID).Scan(&existingCreatedAt, &existingCreatedAtNanos); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrVacancyNotFound
		}
		return fmt.Errorf("read vacancy before update: %w", err)
	}
	if value.CreatedAt.IsZero() {
		value.CreatedAt = postgresTimeExact(existingCreatedAt, existingCreatedAtNanos)
	}
	if value.UpdatedAt.IsZero() {
		value.UpdatedAt = time.Now().UTC()
	}
	if value.UpdatedAt.Before(value.CreatedAt) {
		value.UpdatedAt = value.CreatedAt
	}
	if err := validateVacancy(value); err != nil {
		return err
	}
	args, err := vacancyArgs(value)
	if err != nil {
		return err
	}
	if _, err := db.Exec(ctx, vacancyUpdate, args...); err != nil {
		return mapPostgresVacancyError("update vacancy", err)
	}
	if owned {
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit update vacancy: %w", err)
		}
	}
	return nil
}

// SQL mutations are committed by Create/Update. Save remains a compatibility
// no-op so callers can use either backend through the same interface.
func (r *PostgresVacancyRepository) Save(ctx context.Context) error {
	ctx = postgresContext(ctx)
	if err := repositoryContextErr(ctx); err != nil {
		return err
	}
	return r.requirePool()
}

func (r *PostgresVacancyRepository) requirePool() error {
	if r == nil || r.db == nil {
		return errors.New("vacancy repository is not configured")
	}
	return nil
}

func (r *PostgresVacancyRepository) beginMutation(ctx context.Context) (pgx.Tx, bool, error) {
	if r == nil || r.db == nil {
		return nil, false, errors.New("vacancy repository is not configured")
	}
	if r.pool == nil {
		return nil, false, nil
	}
	tx, err := r.pool.Begin(ctx)
	return tx, true, err
}

func vacancyForCreate(value Vacancy) Vacancy {
	if value.CreatedAt.IsZero() {
		value.CreatedAt = time.Now().UTC()
	}
	if value.UpdatedAt.IsZero() {
		value.UpdatedAt = value.CreatedAt
	}
	return value
}

func vacancyArgs(value Vacancy) ([]any, error) {
	requirements, err := nullableJSON(value.Requirements)
	if err != nil {
		return nil, fmt.Errorf("encode vacancy requirements: %w", err)
	}
	skills, err := nullableJSON(value.Skills)
	if err != nil {
		return nil, fmt.Errorf("encode vacancy skills: %w", err)
	}
	hhMetadata, err := nullableJSON(value.HHMetadata)
	if err != nil {
		return nil, fmt.Errorf("encode vacancy HH metadata: %w", err)
	}
	links, err := nullableJSON(value.Links)
	if err != nil {
		return nil, fmt.Errorf("encode vacancy links: %w", err)
	}
	compensation, err := nullableJSON(value.Compensation)
	if err != nil {
		return nil, fmt.Errorf("encode vacancy compensation: %w", err)
	}
	lastChangeTime, err := nullableJSON(value.LastChangeTime)
	if err != nil {
		return nil, fmt.Errorf("encode vacancy last change time: %w", err)
	}
	userLabels, err := nullableJSON(value.UserLabels)
	if err != nil {
		return nil, fmt.Errorf("encode vacancy user labels: %w", err)
	}
	matchResult, err := nullableJSON(value.MatchResult)
	if err != nil {
		return nil, fmt.Errorf("encode vacancy match result: %w", err)
	}
	recommendation, err := nullableJSON(value.ApplicationRecommendation)
	if err != nil {
		return nil, fmt.Errorf("encode vacancy recommendation: %w", err)
	}
	evidence, err := nullableJSON(value.ReconciliationEvidence)
	if err != nil {
		return nil, fmt.Errorf("encode vacancy reconciliation evidence: %w", err)
	}
	return []any{
		value.ID, value.ExternalID, value.Name, value.Title, value.Description,
		requirements, skills, value.Salary, value.SalaryCurrency, value.Location,
		value.WorkFormat, value.EmploymentType, value.Source,
		nullableTime(value.PublishedAt), nullableTime(value.HHUpdatedAt), hhMetadata,
		nullableTime(value.CreatedAt), nullableTime(value.UpdatedAt),
		nullableUnixNano(value.PublishedAt), nullableUnixNano(value.HHUpdatedAt),
		nullableUnixNano(value.CreatedAt), nullableUnixNano(value.UpdatedAt),
		value.WorkSchedule, value.WorkExperience, links, value.TotalResponsesCount,
		value.Area.Name, value.Company.ID, value.Company.Name, value.Company.CompanySiteURL,
		compensation, value.CreationTime, lastChangeTime, userLabels,
		value.ResponseLetterRequired, value.UserTestPresent, value.Archived, value.ResponseURL,
		value.TotalResponsesCountKnown, matchResult, recommendation, value.DataCompleteness,
		evidence,
	}, nil
}

func nullableJSON(value any) ([]byte, error) {
	if value == nil {
		return nil, nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	if string(raw) == "null" {
		return nil, nil
	}
	return raw, nil
}

func nullableTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value
}

func nullableUnixNano(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value.UnixNano()
}

type postgresVacancyScanner interface {
	Scan(dest ...any) error
}

func scanPostgresVacancy(scanner postgresVacancyScanner) (Vacancy, error) {
	var (
		value                        Vacancy
		requirements, skills         []byte
		hhMetadata, links            []byte
		compensation, lastChangeTime []byte
		userLabels, matchResult      []byte
		recommendation, evidence     []byte
		publishedAt, hhUpdatedAt     pgtype.Timestamptz
		createdAt, updatedAt         pgtype.Timestamptz
		publishedAtNS, hhUpdatedAtNS pgtype.Int8
		createdAtNS, updatedAtNS     pgtype.Int8
	)
	err := scanner.Scan(
		&value.ID, &value.ExternalID, &value.Name, &value.Title, &value.Description,
		&requirements, &skills, &value.Salary, &value.SalaryCurrency, &value.Location,
		&value.WorkFormat, &value.EmploymentType, &value.Source,
		&publishedAt, &hhUpdatedAt, &hhMetadata, &createdAt, &updatedAt,
		&publishedAtNS, &hhUpdatedAtNS, &createdAtNS, &updatedAtNS,
		&value.WorkSchedule, &value.WorkExperience, &links, &value.TotalResponsesCount,
		&value.Area.Name, &value.Company.ID, &value.Company.Name, &value.Company.CompanySiteURL,
		&compensation, &value.CreationTime, &lastChangeTime, &userLabels,
		&value.ResponseLetterRequired, &value.UserTestPresent, &value.Archived, &value.ResponseURL,
		&value.TotalResponsesCountKnown, &matchResult, &recommendation, &value.DataCompleteness,
		&evidence,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Vacancy{}, ErrVacancyNotFound
		}
		return Vacancy{}, fmt.Errorf("scan vacancy: %w", err)
	}
	value.PublishedAt = postgresTimeExact(publishedAt, publishedAtNS)
	value.HHUpdatedAt = postgresTimeExact(hhUpdatedAt, hhUpdatedAtNS)
	value.CreatedAt = postgresTimeExact(createdAt, createdAtNS)
	value.UpdatedAt = postgresTimeExact(updatedAt, updatedAtNS)
	for _, field := range []struct {
		raw    []byte
		target any
		name   string
	}{
		{requirements, &value.Requirements, "requirements"},
		{skills, &value.Skills, "skills"},
		{hhMetadata, &value.HHMetadata, "HH metadata"},
		{links, &value.Links, "links"},
		{compensation, &value.Compensation, "compensation"},
		{lastChangeTime, &value.LastChangeTime, "last change time"},
		{userLabels, &value.UserLabels, "user labels"},
		{matchResult, &value.MatchResult, "match result"},
		{recommendation, &value.ApplicationRecommendation, "recommendation"},
		{evidence, &value.ReconciliationEvidence, "reconciliation evidence"},
	} {
		if err := decodeNullableJSON(field.raw, field.target); err != nil {
			return Vacancy{}, fmt.Errorf("decode vacancy %s: %w", field.name, err)
		}
	}
	return cloneKnowledge(value)
}

func decodeNullableJSON(raw []byte, target any) error {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	return json.Unmarshal(raw, target)
}

func postgresTime(value pgtype.Timestamptz) time.Time {
	if !value.Valid {
		return time.Time{}
	}
	return value.Time
}

func postgresTimeExact(value pgtype.Timestamptz, nanos pgtype.Int8) time.Time {
	if nanos.Valid {
		return time.Unix(0, nanos.Int64).UTC()
	}
	return postgresTime(value)
}

func mapPostgresVacancyError(action string, err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		switch pgErr.ConstraintName {
		case "vacancies_pkey":
			return fmt.Errorf("%s: %w", action, ErrDuplicateVacancyID)
		case "vacancies_external_id_unique":
			return fmt.Errorf("%s: %w", action, ErrDuplicateVacancyExternal)
		}
	}
	return fmt.Errorf("%s: %w", action, err)
}

var _ VacancyRepository = (*PostgresVacancyRepository)(nil)
