package postgresstorage

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"hh-ai-responder/internal/vacancy"
	"hh-ai-responder/internal/vacancyreview"
)

// VacancyReviewRepository is the PostgreSQL implementation of P1.1's
// freshness and review-state foundation. It never talks to HH or an AI
// provider.
type VacancyReviewRepository struct {
	pool *pgxpool.Pool
	db   postgresDBTX
}

func NewVacancyReviewRepository(pool *pgxpool.Pool) *VacancyReviewRepository {
	var db postgresDBTX
	if pool != nil {
		db = pool
	}
	return &VacancyReviewRepository{pool: pool, db: db}
}

func (r *VacancyReviewRepository) requireDB() error {
	if r == nil || r.db == nil {
		return errors.New("vacancy review repository is not configured")
	}
	return nil
}

func (r *VacancyReviewRepository) GetFreshness(ctx context.Context, id int) (vacancy.Freshness, error) {
	if err := r.prepare(ctx); err != nil {
		return vacancy.Freshness{}, err
	}
	value, err := scanFreshness(r.db.QueryRow(ctx, `SELECT vacancy_id, first_seen_at, last_seen_at, first_seen_at_ns, last_seen_at_ns, source_fingerprint, previous_source_fingerprint, material_fingerprint, fingerprint_version, material_changed_at, material_changed_at_ns FROM vacancy_freshness WHERE vacancy_id=$1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		// A missing row is the documented unknown legacy state, not a missing
		// vacancy. Vacancy existence is checked by review writes.
		return vacancy.Freshness{VacancyID: id}, nil
	}
	return value, err
}

func (r *VacancyReviewRepository) ListFreshness(ctx context.Context, ids []int) (map[int]vacancy.Freshness, error) {
	result := make(map[int]vacancy.Freshness)
	if len(ids) == 0 {
		return result, nil
	}
	if err := r.prepare(ctx); err != nil {
		return nil, err
	}
	rows, err := r.db.Query(ctx, `SELECT vacancy_id, first_seen_at, last_seen_at, first_seen_at_ns, last_seen_at_ns, source_fingerprint, previous_source_fingerprint, material_fingerprint, fingerprint_version, material_changed_at, material_changed_at_ns FROM vacancy_freshness WHERE vacancy_id = ANY($1::int[])`, ids)
	if err != nil {
		return nil, fmt.Errorf("list vacancy freshness: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		value, err := scanFreshness(rows)
		if err != nil {
			return nil, err
		}
		result[value.VacancyID] = value
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate vacancy freshness: %w", err)
	}
	return result, nil
}

func (r *VacancyReviewRepository) GetReviewState(ctx context.Context, id int) (vacancyreview.ReviewState, error) {
	if err := r.prepare(ctx); err != nil {
		return vacancyreview.ReviewState{}, err
	}
	return scanReviewState(r.db.QueryRow(ctx, `SELECT vacancy_id, state, state_changed_at, state_changed_at_ns, decision_source_fingerprint, decision_material_fingerprint, decision_fingerprint_version, reason, updated_at, updated_at_ns FROM vacancy_review_states WHERE vacancy_id=$1`, id))
}

func (r *VacancyReviewRepository) ListReviewStates(ctx context.Context, ids []int) (map[int]vacancyreview.ReviewState, error) {
	result := make(map[int]vacancyreview.ReviewState)
	if len(ids) == 0 {
		return result, nil
	}
	if err := r.prepare(ctx); err != nil {
		return nil, err
	}
	rows, err := r.db.Query(ctx, `SELECT vacancy_id, state, state_changed_at, state_changed_at_ns, decision_source_fingerprint, decision_material_fingerprint, decision_fingerprint_version, reason, updated_at, updated_at_ns FROM vacancy_review_states WHERE vacancy_id = ANY($1::int[])`, ids)
	if err != nil {
		return nil, fmt.Errorf("list vacancy review states: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		value, err := scanReviewState(rows)
		if err != nil {
			return nil, err
		}
		result[value.VacancyID] = value
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate vacancy review states: %w", err)
	}
	return result, nil
}

func (r *VacancyReviewRepository) ListReviewEvents(ctx context.Context, id int) ([]vacancyreview.Event, error) {
	if err := r.prepare(ctx); err != nil {
		return nil, err
	}
	rows, err := r.db.Query(ctx, `SELECT id, vacancy_id, event_type, occurred_at, occurred_at_ns, source_fingerprint, material_fingerprint, fingerprint_version, reason, source FROM vacancy_review_events WHERE vacancy_id=$1 ORDER BY occurred_at, id`, id)
	if err != nil {
		return nil, fmt.Errorf("list vacancy review events: %w", err)
	}
	defer rows.Close()
	result := []vacancyreview.Event{}
	for rows.Next() {
		value, err := scanReviewEvent(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate vacancy review events: %w", err)
	}
	return result, nil
}

func (r *VacancyReviewRepository) RecordReviewAction(ctx context.Context, action vacancyreview.Action) error {
	ctx = postgresContext(ctx)
	if err := repositoryContextErr(ctx); err != nil {
		return err
	}
	if err := r.requireDB(); err != nil {
		return err
	}
	action.Source = strings.TrimSpace(action.Source)
	if err := vacancyreview.ValidateAction(action.State, action.Source, action.OccurredAt); err != nil {
		return err
	}
	tx, owned, err := r.beginMutation(ctx)
	if err != nil {
		return fmt.Errorf("begin vacancy review action: %w", err)
	}
	db := r.db
	if owned {
		db = tx
		defer func() { _ = tx.Rollback(ctx) }()
	}
	var exists int
	if err := db.QueryRow(ctx, `SELECT id FROM vacancies WHERE id=$1 FOR UPDATE`, action.VacancyID).Scan(&exists); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return vacancy.ErrVacancyNotFound
		}
		return fmt.Errorf("check vacancy for review action: %w", err)
	}
	freshness, err := scanFreshness(db.QueryRow(ctx, `SELECT vacancy_id, first_seen_at, last_seen_at, first_seen_at_ns, last_seen_at_ns, source_fingerprint, previous_source_fingerprint, material_fingerprint, fingerprint_version, material_changed_at, material_changed_at_ns FROM vacancy_freshness WHERE vacancy_id=$1 FOR UPDATE`, action.VacancyID))
	if errors.Is(err, pgx.ErrNoRows) {
		freshness = vacancy.Freshness{VacancyID: action.VacancyID}
	} else if err != nil {
		return err
	}
	current, currentErr := scanReviewState(db.QueryRow(ctx, `SELECT vacancy_id, state, state_changed_at, state_changed_at_ns, decision_source_fingerprint, decision_material_fingerprint, decision_fingerprint_version, reason, updated_at, updated_at_ns FROM vacancy_review_states WHERE vacancy_id=$1 FOR UPDATE`, action.VacancyID))
	var previous *vacancyreview.State
	if currentErr == nil {
		value := current.State
		previous = &value
	} else if !errors.Is(currentErr, pgx.ErrNoRows) && !errors.Is(currentErr, vacancyreview.ErrReviewStateNotFound) {
		return currentErr
	}
	if !vacancyreview.CanTransition(previous, action.State) {
		return fmt.Errorf("invalid vacancy review transition to %q", action.State)
	}
	if currentErr == nil && current.State == action.State && current.DecisionSourceFingerprint == freshness.SourceFingerprint && current.DecisionMaterialFingerprint == freshness.MaterialFingerprint && current.FingerprintVersion == freshness.FingerprintVersion {
		return commitReviewTx(ctx, tx, owned)
	}
	if _, err := db.Exec(ctx, `INSERT INTO vacancy_review_events (vacancy_id, event_type, occurred_at, occurred_at_ns, source_fingerprint, material_fingerprint, fingerprint_version, reason, source) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`, action.VacancyID, action.State, action.OccurredAt, action.OccurredAt.UnixNano(), freshness.SourceFingerprint, freshness.MaterialFingerprint, freshness.FingerprintVersion, strings.TrimSpace(action.Reason), action.Source); err != nil {
		return fmt.Errorf("append vacancy review event: %w", err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO vacancy_review_states (vacancy_id, state, state_changed_at, state_changed_at_ns, decision_source_fingerprint, decision_material_fingerprint, decision_fingerprint_version, reason, updated_at, updated_at_ns) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$3,$4) ON CONFLICT (vacancy_id) DO UPDATE SET state=EXCLUDED.state, state_changed_at=EXCLUDED.state_changed_at, state_changed_at_ns=EXCLUDED.state_changed_at_ns, decision_source_fingerprint=EXCLUDED.decision_source_fingerprint, decision_material_fingerprint=EXCLUDED.decision_material_fingerprint, decision_fingerprint_version=EXCLUDED.decision_fingerprint_version, reason=EXCLUDED.reason, updated_at=EXCLUDED.updated_at, updated_at_ns=EXCLUDED.updated_at_ns`, action.VacancyID, action.State, action.OccurredAt, action.OccurredAt.UnixNano(), freshness.SourceFingerprint, freshness.MaterialFingerprint, freshness.FingerprintVersion, strings.TrimSpace(action.Reason)); err != nil {
		return fmt.Errorf("update vacancy review state: %w", err)
	}
	return commitReviewTx(ctx, tx, owned)
}

func (r *VacancyReviewRepository) beginMutation(ctx context.Context) (pgx.Tx, bool, error) {
	if r.pool == nil {
		return nil, false, nil
	}
	tx, err := r.pool.Begin(ctx)
	return tx, true, err
}

func commitReviewTx(ctx context.Context, tx pgx.Tx, owned bool) error {
	if owned {
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit vacancy review action: %w", err)
		}
	}
	return nil
}

func (r *VacancyReviewRepository) prepare(ctx context.Context) error {
	ctx = postgresContext(ctx)
	if err := repositoryContextErr(ctx); err != nil {
		return err
	}
	return r.requireDB()
}

type freshnessScanner interface{ Scan(...interface{}) error }

func scanFreshness(scanner freshnessScanner) (vacancy.Freshness, error) {
	var (
		value                                      vacancy.Freshness
		firstSeen, lastSeen, materialChanged       pgtype.Timestamptz
		firstSeenNS, lastSeenNS, materialChangedNS pgtype.Int8
		source, previous, material                 pgtype.Text
	)
	err := scanner.Scan(&value.VacancyID, &firstSeen, &lastSeen, &firstSeenNS, &lastSeenNS, &source, &previous, &material, &value.FingerprintVersion, &materialChanged, &materialChangedNS)
	if err != nil {
		return vacancy.Freshness{}, err
	}
	value.FirstSeenAt = postgresTimeExact(firstSeen, firstSeenNS)
	value.LastSeenAt = postgresTimeExact(lastSeen, lastSeenNS)
	value.MaterialChangedAt = postgresTimeExact(materialChanged, materialChangedNS)
	value.SourceFingerprint, value.PreviousSourceFingerprint, value.MaterialFingerprint = source.String, previous.String, material.String
	return value, nil
}

func scanReviewState(scanner freshnessScanner) (vacancyreview.ReviewState, error) {
	var (
		value                                           vacancyreview.ReviewState
		state, decisionSource, decisionMaterial, reason pgtype.Text
		changedAt, updatedAt                            pgtype.Timestamptz
		changedNS, updatedNS                            pgtype.Int8
	)
	err := scanner.Scan(&value.VacancyID, &state, &changedAt, &changedNS, &decisionSource, &decisionMaterial, &value.FingerprintVersion, &reason, &updatedAt, &updatedNS)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return vacancyreview.ReviewState{}, vacancyreview.ErrReviewStateNotFound
		}
		return vacancyreview.ReviewState{}, err
	}
	value.State = StateFromText(state.String)
	value.StateChangedAt = postgresTimeExact(changedAt, changedNS)
	value.DecisionSourceFingerprint, value.DecisionMaterialFingerprint = decisionSource.String, decisionMaterial.String
	value.Reason, value.UpdatedAt = reason.String, postgresTimeExact(updatedAt, updatedNS)
	return value, nil
}

func scanReviewEvent(scanner freshnessScanner) (vacancyreview.Event, error) {
	var (
		value                                                       vacancyreview.Event
		typ, source, sourceFingerprint, materialFingerprint, reason pgtype.Text
		occurredAt                                                  pgtype.Timestamptz
		occurredNS                                                  pgtype.Int8
	)
	err := scanner.Scan(&value.ID, &value.VacancyID, &typ, &occurredAt, &occurredNS, &sourceFingerprint, &materialFingerprint, &value.FingerprintVersion, &reason, &source)
	if err != nil {
		return vacancyreview.Event{}, err
	}
	value.Type, value.Source = StateFromText(typ.String), source.String
	value.OccurredAt = postgresTimeExact(occurredAt, occurredNS)
	value.SourceFingerprint, value.MaterialFingerprint, value.Reason = sourceFingerprint.String, materialFingerprint.String, reason.String
	return value, nil
}

func StateFromText(value string) vacancyreview.State {
	return vacancyreview.State(strings.TrimSpace(value))
}

var _ vacancyreview.Store = (*VacancyReviewRepository)(nil)

// ObserveVacancy persists one successful provider observation and its
// freshness metadata in the same transaction as the vacancy upsert. Existing
// local matching and reconciliation values remain untouched.
func (r *VacancyRepository) ObserveVacancy(ctx context.Context, value Vacancy, observedAt time.Time) (vacancy.ObservationResult, error) {
	ctx = postgresContext(ctx)
	if err := repositoryContextErr(ctx); err != nil {
		return vacancy.ObservationResult{}, err
	}
	if err := r.requirePool(); err != nil {
		return vacancy.ObservationResult{}, err
	}
	if observedAt.IsZero() {
		return vacancy.ObservationResult{}, errors.New("vacancy observation timestamp is required")
	}
	observedAt = observedAt.UTC()
	value.ExternalID = strings.TrimSpace(value.ExternalID)
	if err := vacancy.Validate(value); err != nil {
		return vacancy.ObservationResult{}, err
	}
	pair := vacancy.Fingerprints(value)
	if strings.TrimSpace(value.ExternalID) == "" {
		return vacancy.ObservationResult{}, errors.New("vacancy observation requires an external id")
	}
	tx, owned, err := r.beginMutation(ctx)
	if err != nil {
		return vacancy.ObservationResult{}, fmt.Errorf("begin vacancy observation: %w", err)
	}
	db := r.db
	if owned {
		db = tx
		defer func() { _ = tx.Rollback(ctx) }()
	}
	old, getErr := scanPostgresVacancy(db.QueryRow(ctx, vacancySelect+" WHERE external_id=$1 ORDER BY id LIMIT 1 FOR UPDATE", value.ExternalID))
	result := vacancy.ObservationResult{}
	if errors.Is(getErr, pgx.ErrNoRows) || errors.Is(getErr, vacancy.ErrVacancyNotFound) {
		value = vacancyForCreate(value)
		args, err := vacancyArgs(value)
		if err != nil {
			return vacancy.ObservationResult{}, err
		}
		if value.ID == 0 {
			if _, err := db.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('hh-ai-responder.vacancy-id'))`); err != nil {
				return vacancy.ObservationResult{}, fmt.Errorf("lock vacancy id allocation: %w", err)
			}
			if err := db.QueryRow(ctx, `SELECT COALESCE(MAX(id), 0) + 1 FROM vacancies`).Scan(&value.ID); err != nil {
				return vacancy.ObservationResult{}, fmt.Errorf("allocate vacancy id: %w", err)
			}
			args[0] = value.ID
		}
		if _, err := db.Exec(ctx, vacancyInsert, args...); err != nil {
			return vacancy.ObservationResult{}, mapPostgresVacancyError("observe vacancy", err)
		}
		if err := insertFreshness(ctx, db, value.ID, observedAt, pair); err != nil {
			return vacancy.ObservationResult{}, err
		}
		result.Created = true
	} else if getErr != nil {
		return vacancy.ObservationResult{}, getErr
	} else {
		value = mergeObservedVacancy(old, value)
		pair = vacancy.Fingerprints(value)
		value.ID, value.CreatedAt = old.ID, old.CreatedAt
		if value.UpdatedAt.IsZero() || value.UpdatedAt.Before(value.CreatedAt) {
			value.UpdatedAt = old.UpdatedAt
		}
		value.MatchResult = old.MatchResult
		value.ApplicationRecommendation = old.ApplicationRecommendation
		value.ReconciliationEvidence = old.ReconciliationEvidence
		oldPair := vacancy.Fingerprints(old)
		if oldPair.SourceFingerprint != pair.SourceFingerprint {
			args, err := vacancyArgs(value)
			if err != nil {
				return vacancy.ObservationResult{}, err
			}
			if _, err := db.Exec(ctx, vacancyUpdate, args...); err != nil {
				return vacancy.ObservationResult{}, mapPostgresVacancyError("observe vacancy", err)
			}
			result.Updated = true
		}
		oldFreshness, err := scanFreshness(db.QueryRow(ctx, `SELECT vacancy_id, first_seen_at, last_seen_at, first_seen_at_ns, last_seen_at_ns, source_fingerprint, previous_source_fingerprint, material_fingerprint, fingerprint_version, material_changed_at, material_changed_at_ns FROM vacancy_freshness WHERE vacancy_id=$1 FOR UPDATE`, old.ID))
		if errors.Is(err, pgx.ErrNoRows) {
			if err := insertFreshness(ctx, db, old.ID, observedAt, pair); err != nil {
				return vacancy.ObservationResult{}, err
			}
		} else if err != nil {
			return vacancy.ObservationResult{}, err
		} else {
			legacyPair := vacancy.FingerprintPair{}
			legacySupported := false
			if oldFreshness.FingerprintVersion == vacancy.LegacyFingerprintVersion {
				// Compare the upgraded observation under the old contract before
				// accepting the v2 baseline. This distinguishes a real change in
				// fields that v1 already understood from a hash change caused only
				// by adding professional_roles to the input.
				legacyPair, legacySupported = vacancy.FingerprintsForVersion(value, oldFreshness.FingerprintVersion)
			}
			result.SourceChanged, result.MaterialChanged = vacancy.ObservationFingerprintChanges(oldFreshness, pair, legacyPair, legacySupported)
			versionCompatible := oldFreshness.FingerprintVersion == vacancy.FingerprintVersion
			previous := oldFreshness.PreviousSourceFingerprint
			if result.SourceChanged {
				previous = oldFreshness.SourceFingerprint
			} else if !versionCompatible {
				// A new algorithm version is not evidence of a provider change.
				previous = ""
			}
			materialChangedAt, materialChangedAtNS := oldFreshness.MaterialChangedAt, nullableUnixNano(oldFreshness.MaterialChangedAt)
			if result.MaterialChanged {
				materialChangedAt, materialChangedAtNS = observedAt, observedAt.UnixNano()
			}
			if _, err := db.Exec(ctx, `UPDATE vacancy_freshness SET last_seen_at=$2, last_seen_at_ns=$3, previous_source_fingerprint=$4, source_fingerprint=$5, material_fingerprint=$6, fingerprint_version=$7, material_changed_at=$8, material_changed_at_ns=$9 WHERE vacancy_id=$1`, old.ID, observedAt, observedAt.UnixNano(), previous, pair.SourceFingerprint, pair.MaterialFingerprint, vacancy.FingerprintVersion, nullableTime(materialChangedAt), materialChangedAtNS); err != nil {
				return vacancy.ObservationResult{}, fmt.Errorf("update vacancy freshness: %w", err)
			}
		}
	}
	if owned {
		if err := tx.Commit(ctx); err != nil {
			return vacancy.ObservationResult{}, fmt.Errorf("commit vacancy observation: %w", err)
		}
	}
	return result, nil
}

// mergeObservedVacancy protects detail-backed fields from a later partial
// search projection. The adapter deliberately has no knowledge of HH HTTP;
// non-empty incoming provider values win, while omitted normalized values
// retain the last known rich value.
func mergeObservedVacancy(old, incoming Vacancy) Vacancy {
	merged := incoming
	first := func(current, previous string) string {
		if strings.TrimSpace(current) != "" {
			return current
		}
		return previous
	}
	merged.Name, merged.Title = first(incoming.Name, old.Name), first(incoming.Title, old.Title)
	merged.Description, merged.Salary = first(incoming.Description, old.Description), first(incoming.Salary, old.Salary)
	merged.SalaryCurrency, merged.Location = first(incoming.SalaryCurrency, old.SalaryCurrency), first(incoming.Location, old.Location)
	merged.WorkFormat, merged.EmploymentType = first(incoming.WorkFormat, old.WorkFormat), first(incoming.EmploymentType, old.EmploymentType)
	merged.WorkSchedule, merged.WorkExperience = first(incoming.WorkSchedule, old.WorkSchedule), first(incoming.WorkExperience, old.WorkExperience)
	merged.Source, merged.ResponseURL = first(incoming.Source, old.Source), first(incoming.ResponseURL, old.ResponseURL)
	merged.CreationTime, merged.LastChangeTime.Value = first(incoming.CreationTime, old.CreationTime), first(incoming.LastChangeTime.Value, old.LastChangeTime.Value)
	merged.Area.Name = first(incoming.Area.Name, old.Area.Name)
	merged.Company.ID, merged.Company.Name, merged.Company.CompanySiteURL = incoming.Company.ID, first(incoming.Company.Name, old.Company.Name), first(incoming.Company.CompanySiteURL, old.Company.CompanySiteURL)
	if merged.Company.ID == 0 {
		merged.Company.ID = old.Company.ID
	}
	if incoming.PublishedAt.IsZero() {
		merged.PublishedAt = old.PublishedAt
	}
	if incoming.HHUpdatedAt.IsZero() {
		merged.HHUpdatedAt = old.HHUpdatedAt
	}
	if len(incoming.Requirements) == 0 {
		merged.Requirements = append([]string(nil), old.Requirements...)
	}
	if len(incoming.Skills) == 0 {
		merged.Skills = append([]string(nil), old.Skills...)
	}
	if len(incoming.ProfessionalRoles) == 0 {
		merged.ProfessionalRoles = append([]string(nil), old.ProfessionalRoles...)
	}
	if len(incoming.Links) == 0 {
		merged.Links = cloneStringMap(old.Links)
	} else {
		merged.Links = cloneStringMap(old.Links)
		for key, value := range incoming.Links {
			if strings.TrimSpace(value) != "" {
				merged.Links[key] = value
			}
		}
	}
	if incoming.Compensation.From == nil && incoming.Compensation.To == nil && incoming.Compensation.Currency == "" && incoming.Compensation.Gross == nil {
		merged.Compensation = old.Compensation
	}
	merged.HHMetadata = cloneStringMap(old.HHMetadata)
	for key, value := range incoming.HHMetadata {
		if strings.TrimSpace(value) != "" {
			merged.HHMetadata[key] = value
		}
	}
	if !incoming.ResponseLetterRequired {
		merged.ResponseLetterRequired = old.ResponseLetterRequired
	}
	if !incoming.UserTestPresent {
		merged.UserTestPresent = old.UserTestPresent
	}
	if strings.TrimSpace(merged.Description) != "" && (len(merged.Requirements) > 0 || len(merged.Skills) > 0) && strings.TrimSpace(first(merged.Location, merged.Area.Name)) != "" {
		merged.DataCompleteness = vacancy.DataCompletenessFull
	} else if strings.TrimSpace(merged.Description) != "" || len(merged.Requirements) > 0 || len(merged.Skills) > 0 || strings.TrimSpace(first(merged.Location, merged.Area.Name)) != "" {
		merged.DataCompleteness = vacancy.DataCompletenessPartial
	} else {
		merged.DataCompleteness = vacancy.DataCompletenessMinimal
	}
	return merged
}

func cloneStringMap(value map[string]string) map[string]string {
	if value == nil {
		return map[string]string{}
	}
	result := make(map[string]string, len(value))
	for key, item := range value {
		result[key] = item
	}
	return result
}

func insertFreshness(ctx context.Context, db postgresDBTX, id int, observedAt time.Time, pair vacancy.FingerprintPair) error {
	if _, err := db.Exec(ctx, `INSERT INTO vacancy_freshness (vacancy_id, first_seen_at, last_seen_at, first_seen_at_ns, last_seen_at_ns, source_fingerprint, previous_source_fingerprint, material_fingerprint, fingerprint_version) VALUES ($1,$2,$2,$3,$3,$4,'',$5,$6)`, id, observedAt, observedAt.UnixNano(), pair.SourceFingerprint, pair.MaterialFingerprint, vacancy.FingerprintVersion); err != nil {
		return fmt.Errorf("insert vacancy freshness: %w", err)
	}
	return nil
}
