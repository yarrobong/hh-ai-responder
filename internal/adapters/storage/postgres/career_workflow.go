package postgresstorage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"hh-ai-responder/internal/careeragent"
	"hh-ai-responder/internal/ports"
)

// CareerWorkflowRepository stores redacted Career Agent runs, item outcomes,
// and reviewable application preparations. It never calls HH or an AI model.
type CareerWorkflowRepository struct {
	pool *pgxpool.Pool
	db   postgresDBTX
}

var _ ports.CareerWorkflowStore = (*CareerWorkflowRepository)(nil)

func NewCareerWorkflowRepository(pool *pgxpool.Pool) *CareerWorkflowRepository {
	if pool == nil {
		return &CareerWorkflowRepository{}
	}
	return &CareerWorkflowRepository{pool: pool, db: pool}
}

func NewCareerWorkflowRepositoryForTx(tx pgx.Tx) *CareerWorkflowRepository {
	return &CareerWorkflowRepository{db: tx}
}

func (r *CareerWorkflowRepository) requireDB() error {
	if r == nil || r.db == nil {
		return errors.New("postgres career workflow repository is not configured")
	}
	return nil
}

func (r *CareerWorkflowRepository) StartRun(ctx context.Context, run careeragent.AgentRun) error {
	if err := r.requireDB(); err != nil {
		return err
	}
	if err := run.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(run.RunType) == "" {
		run.RunType = "career_agent"
	}
	createdAt := run.CreatedAt
	if createdAt.IsZero() {
		createdAt = run.StartedAt
	}
	dailyResult, err := workflowNullableJSON(run.DailyResultJSON)
	if err != nil {
		return fmt.Errorf("encode daily Career Agent result: %w", err)
	}
	_, err = r.db.Exec(postgresContext(ctx), `
		INSERT INTO agent_runs
		(id, run_type, stage, status, started_at, result_code, summary, daily_result_json, error_code, error_summary, confidence, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8::jsonb,$9,$10,$11,$12)
		ON CONFLICT (id) DO NOTHING`,
		run.ID, run.RunType, run.Stage, run.Status, run.StartedAt, workflowNullableString(string(run.ResultCode)),
		workflowNullableString(run.Summary), dailyResult, workflowNullableString(run.ErrorCode), workflowNullableString(run.ErrorSummary), workflowNullableFloat(run.Confidence), createdAt)
	if err != nil {
		return fmt.Errorf("start career agent run: %w", err)
	}
	existing, err := r.GetRun(ctx, run.ID)
	if err != nil {
		return err
	}
	if existing.RunType != run.RunType || existing.Stage != run.Stage || !existing.StartedAt.Equal(run.StartedAt) {
		return ErrWorkflowConflict(run.ID)
	}
	return nil
}

func (r *CareerWorkflowRepository) AcquireRun(ctx context.Context, run careeragent.AgentRun) (bool, error) {
	if err := r.requireDB(); err != nil {
		return false, err
	}
	if run.Status != careeragent.AgentRunStatusRunning {
		return false, errors.New("career workflow acquire requires a running run")
	}
	if err := run.Validate(); err != nil {
		return false, err
	}
	dailyResult, err := workflowNullableJSON(run.DailyResultJSON)
	if err != nil {
		return false, fmt.Errorf("encode daily Career Agent result: %w", err)
	}
	command, err := r.db.Exec(postgresContext(ctx), `
		INSERT INTO agent_runs
		(id, run_type, stage, status, started_at, result_code, summary, daily_result_json, error_code, error_summary, confidence, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8::jsonb,$9,$10,$11,$12)
		ON CONFLICT (id) DO NOTHING`,
		run.ID, run.RunType, run.Stage, run.Status, run.StartedAt, workflowNullableString(string(run.ResultCode)), workflowNullableString(run.Summary), dailyResult, workflowNullableString(run.ErrorCode), workflowNullableString(run.ErrorSummary), workflowNullableFloat(run.Confidence), run.CreatedAt)
	if err != nil {
		return false, fmt.Errorf("acquire career agent run: %w", err)
	}
	if command.RowsAffected() == 1 {
		return true, nil
	}
	existing, err := r.GetRun(ctx, run.ID)
	if err != nil {
		return false, err
	}
	if existing.Stage != run.Stage || existing.RunType != run.RunType || !existing.StartedAt.Equal(run.StartedAt) {
		return false, ErrWorkflowConflict(run.ID)
	}
	return false, nil
}

func (r *CareerWorkflowRepository) FinishRun(ctx context.Context, run careeragent.AgentRun) error {
	if err := r.requireDB(); err != nil {
		return err
	}
	if err := run.Validate(); err != nil {
		return err
	}
	if run.FinishedAt == nil {
		return errors.New("finished career agent run requires finished_at")
	}
	resultCode := run.ResultCode
	if resultCode == "" {
		resultCode = careeragent.AgentRunResultCode(run.Status)
	}
	result := strings.TrimSpace(run.Summary)
	if result == "" {
		result = strings.TrimSpace(run.Result)
	}
	errorSummary := strings.TrimSpace(run.ErrorSummary)
	if errorSummary == "" && len(run.Errors) > 0 {
		errorSummary = strings.TrimSpace(run.Errors[len(run.Errors)-1])
	}
	dailyResult, err := workflowNullableJSON(run.DailyResultJSON)
	if err != nil {
		return fmt.Errorf("encode daily Career Agent result: %w", err)
	}
	command, err := r.db.Exec(postgresContext(ctx), `
		UPDATE agent_runs SET
		status=$2, finished_at=$3, result_code=$4, summary=$5, daily_result_json=$6::jsonb,
		error_code=$7, error_summary=$8, confidence=$9
		WHERE id=$1 AND status='running'`,
		run.ID, run.Status, *run.FinishedAt, workflowNullableString(string(resultCode)), workflowNullableString(result),
		dailyResult, workflowNullableString(run.ErrorCode), workflowNullableString(errorSummary), workflowNullableFloat(run.Confidence))
	if err != nil {
		return fmt.Errorf("finish career agent run: %w", err)
	}
	if command.RowsAffected() == 0 {
		if _, getErr := r.GetRun(ctx, run.ID); errors.Is(getErr, careeragent.ErrAgentRunNotFound) {
			return getErr
		}
		return ErrWorkflowConflict(run.ID)
	}
	return nil
}

func (r *CareerWorkflowRepository) UpsertRunItem(ctx context.Context, item careeragent.AgentRunItem) error {
	if err := r.requireDB(); err != nil {
		return err
	}
	item.NormalizeTarget()
	if err := item.Validate(); err != nil {
		return err
	}
	evidence, err := workflowNullableJSON(item.Evidence)
	if err != nil {
		return fmt.Errorf("encode career agent run item evidence: %w", err)
	}
	err = r.withMutation(ctx, func(db postgresDBTX) error {
		_, execErr := db.Exec(postgresContext(ctx), `
		INSERT INTO agent_run_items
		(id, run_id, target_type, target_id, vacancy_id, application_id, conversation_id, stage, status, decision_code, confidence, evidence_json, error_code, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12::jsonb,$13,$14)
		ON CONFLICT (run_id, target_type, target_id) DO UPDATE SET
		target_type=EXCLUDED.target_type, target_id=EXCLUDED.target_id,
		vacancy_id=EXCLUDED.vacancy_id, application_id=EXCLUDED.application_id,
		conversation_id=EXCLUDED.conversation_id,
		stage=EXCLUDED.stage, status=EXCLUDED.status, decision_code=EXCLUDED.decision_code,
		confidence=EXCLUDED.confidence, evidence_json=EXCLUDED.evidence_json,
		error_code=EXCLUDED.error_code, created_at=EXCLUDED.created_at`,
			item.ID, item.RunID, item.TargetType, item.TargetID, nullableWorkflowInt(item.VacancyID), workflowNullableString(item.ApplicationID), workflowNullableString(item.ConversationID), item.Stage, item.Status, workflowNullableString(item.DecisionCode),
			workflowNullableFloat(item.Confidence), evidence, workflowNullableString(item.ErrorCode), item.CreatedAt)
		return execErr
	})
	if err != nil {
		return fmt.Errorf("upsert career agent run item: %w", err)
	}
	return nil
}

func (r *CareerWorkflowRepository) UpsertPreparation(ctx context.Context, preparation careeragent.ApplicationPreparation) error {
	if err := r.requireDB(); err != nil {
		return err
	}
	if err := preparation.Validate(); err != nil {
		return err
	}
	storyIDs, err := workflowNullableJSON(preparation.StoryIDs)
	if err != nil {
		return fmt.Errorf("encode preparation story ids: %w", err)
	}
	knowledgeRequests, err := workflowNullableJSON(preparation.KnowledgeRequests)
	if err != nil {
		return fmt.Errorf("encode preparation knowledge requests: %w", err)
	}
	evidence, err := workflowNullableJSON(preparation.Evidence)
	if err != nil {
		return fmt.Errorf("encode preparation evidence: %w", err)
	}
	testDrafts, err := workflowNullableJSON(preparation.TestAnswerDrafts)
	if err != nil {
		return fmt.Errorf("encode preparation test drafts: %w", err)
	}
	err = r.withMutation(ctx, func(db postgresDBTX) error {
		_, execErr := db.Exec(postgresContext(ctx), `
		INSERT INTO application_preparations
		(id, vacancy_id, resume_id, resume_provider_id, resume_fingerprint, browser_resume_hash, candidate_id, candidate_version,
		 candidate_snapshot_hash, route_status, route_confidence, evidence_json, story_ids,
		 cover_letter, cover_letter_hash, test_answer_drafts_json, knowledge_requests_json,
		 input_fingerprint, status, stale_reason, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12::jsonb,$13::jsonb,$14,$15,$16::jsonb,$17::jsonb,$18,$19,$20,$21,$22)
		ON CONFLICT (vacancy_id, input_fingerprint) DO UPDATE SET
		resume_id=EXCLUDED.resume_id, resume_provider_id=EXCLUDED.resume_provider_id, resume_fingerprint=EXCLUDED.resume_fingerprint, browser_resume_hash=EXCLUDED.browser_resume_hash,
		candidate_id=EXCLUDED.candidate_id, candidate_version=EXCLUDED.candidate_version,
		candidate_snapshot_hash=EXCLUDED.candidate_snapshot_hash, route_status=EXCLUDED.route_status,
		route_confidence=EXCLUDED.route_confidence, evidence_json=EXCLUDED.evidence_json,
		story_ids=EXCLUDED.story_ids, cover_letter=EXCLUDED.cover_letter,
		cover_letter_hash=EXCLUDED.cover_letter_hash, test_answer_drafts_json=EXCLUDED.test_answer_drafts_json,
		knowledge_requests_json=EXCLUDED.knowledge_requests_json, status=EXCLUDED.status,
		stale_reason=EXCLUDED.stale_reason, created_at=EXCLUDED.created_at, updated_at=EXCLUDED.updated_at`,
			preparation.ID, preparation.VacancyID, workflowNullableString(preparation.ResumeID), workflowNullableString(preparation.ResumeProviderID), workflowNullableString(preparation.ResumeFingerprint), workflowNullableString(preparation.BrowserResumeHash),
			preparation.CandidateID, preparation.CandidateVersion, preparation.CandidateSnapshotHash, preparation.RouteStatus,
			workflowNullableString(preparation.RouteConfidence), evidence, storyIDs,
			workflowNullableString(preparation.CoverLetter), workflowNullableString(preparation.CoverLetterHash), testDrafts,
			knowledgeRequests, preparation.InputFingerprint, preparation.Status, workflowNullableString(preparation.StaleReason), preparation.CreatedAt, preparation.UpdatedAt)
		return execErr
	})
	if err != nil {
		return fmt.Errorf("upsert application preparation: %w", err)
	}
	return nil
}

func (r *CareerWorkflowRepository) GetRun(ctx context.Context, id string) (careeragent.AgentRun, error) {
	if err := r.requireDB(); err != nil {
		return careeragent.AgentRun{}, err
	}
	return scanCareerAgentRun(r.db.QueryRow(postgresContext(ctx), `
		SELECT id, run_type, stage, status, started_at, finished_at, result_code, summary,
		       daily_result_json, error_code, error_summary, confidence, created_at
		FROM agent_runs WHERE id=$1`, id))
}

func (r *CareerWorkflowRepository) ListRuns(ctx context.Context, query careeragent.RunQuery) ([]careeragent.AgentRun, error) {
	if err := r.requireDB(); err != nil {
		return nil, err
	}
	limit := workflowLimit(query.Limit)
	statement := `SELECT id, run_type, stage, status, started_at, finished_at, result_code, summary,
		daily_result_json, error_code, error_summary, confidence, created_at FROM agent_runs`
	args := make([]any, 0, 3)
	if query.Status != nil {
		args = append(args, *query.Status)
		statement += " WHERE status=$1"
	}
	statement += fmt.Sprintf(" ORDER BY started_at DESC, id DESC LIMIT $%d", len(args)+1)
	args = append(args, limit)
	rows, err := r.db.Query(postgresContext(ctx), statement, args...)
	if err != nil {
		return nil, fmt.Errorf("list career agent runs: %w", err)
	}
	defer rows.Close()
	result := make([]careeragent.AgentRun, 0)
	for rows.Next() {
		run, scanErr := scanCareerAgentRun(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		result = append(result, run)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate career agent runs: %w", err)
	}
	return result, nil
}

func (r *CareerWorkflowRepository) ListRunItems(ctx context.Context, runID string, limit int) ([]careeragent.AgentRunItem, error) {
	if err := r.requireDB(); err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 1000 {
		limit = 1000
	}
	statement := `SELECT id, run_id, target_type, target_id, vacancy_id, application_id, conversation_id, stage, status, decision_code, confidence, evidence_json, error_code, created_at FROM agent_run_items`
	args := []any{}
	if strings.TrimSpace(runID) != "" {
		statement += " WHERE run_id=$1"
		args = append(args, runID)
	}
	statement += fmt.Sprintf(" ORDER BY created_at DESC, target_type, target_id LIMIT $%d", len(args)+1)
	args = append(args, limit)
	rows, err := r.db.Query(postgresContext(ctx), statement, args...)
	if err != nil {
		return nil, fmt.Errorf("list career agent run items: %w", err)
	}
	defer rows.Close()
	result := []careeragent.AgentRunItem{}
	for rows.Next() {
		var item careeragent.AgentRunItem
		var vacancyID pgtype.Int4
		var applicationID, conversationID, decisionCode, errorCode pgtype.Text
		var confidence pgtype.Float8
		var evidence []byte
		if err := rows.Scan(&item.ID, &item.RunID, &item.TargetType, &item.TargetID, &vacancyID, &applicationID, &conversationID, &item.Stage, &item.Status, &decisionCode, &confidence, &evidence, &errorCode, &item.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan career agent run item: %w", err)
		}
		if vacancyID.Valid {
			item.VacancyID = int(vacancyID.Int32)
		}
		item.ApplicationID, item.ConversationID, item.DecisionCode, item.ErrorCode = workflowNullableText(applicationID), workflowNullableText(conversationID), workflowNullableText(decisionCode), workflowNullableText(errorCode)
		if confidence.Valid {
			value := confidence.Float64
			item.Confidence = &value
		}
		item.Evidence = workflowCloneJSON(evidence)
		if err := item.Validate(); err != nil {
			return nil, fmt.Errorf("validate stored career agent run item: %w", err)
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate career agent run items: %w", err)
	}
	return result, nil
}

func (r *CareerWorkflowRepository) GetPreparation(ctx context.Context, vacancyID int) (careeragent.ApplicationPreparation, error) {
	if err := r.requireDB(); err != nil {
		return careeragent.ApplicationPreparation{}, err
	}
	return scanApplicationPreparation(r.db.QueryRow(postgresContext(ctx), preparationSelect+` WHERE vacancy_id=$1 ORDER BY updated_at DESC, id DESC LIMIT 1`, vacancyID))
}

func (r *CareerWorkflowRepository) ListPreparations(ctx context.Context, query careeragent.PreparationQuery) ([]careeragent.ApplicationPreparation, error) {
	if err := r.requireDB(); err != nil {
		return nil, err
	}
	limit := workflowLimit(query.Limit)
	statement := preparationSelect
	args := []any{}
	conditions := []string{}
	if query.VacancyID != nil {
		args = append(args, *query.VacancyID)
		conditions = append(conditions, fmt.Sprintf("vacancy_id=$%d", len(args)))
	}
	if len(query.Statuses) > 0 {
		values := make([]string, 0, len(query.Statuses))
		for _, status := range query.Statuses {
			if strings.TrimSpace(string(status)) == "" {
				continue
			}
			values = append(values, string(status))
		}
		if len(values) > 0 {
			args = append(args, values)
			conditions = append(conditions, fmt.Sprintf("status = ANY($%d::text[])", len(args)))
		}
	}
	if len(conditions) > 0 {
		statement += " WHERE " + strings.Join(conditions, " AND ")
	}
	statement += fmt.Sprintf(" ORDER BY updated_at DESC, id DESC LIMIT $%d", len(args)+1)
	args = append(args, limit)
	rows, err := r.db.Query(postgresContext(ctx), statement, args...)
	if err != nil {
		return nil, fmt.Errorf("list application preparations: %w", err)
	}
	defer rows.Close()
	result := make([]careeragent.ApplicationPreparation, 0)
	for rows.Next() {
		preparation, scanErr := scanApplicationPreparation(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		result = append(result, preparation)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate application preparations: %w", err)
	}
	return result, nil
}

func (r *CareerWorkflowRepository) RecoverInterruptedRuns(ctx context.Context, finishedAt time.Time) error {
	if err := r.requireDB(); err != nil {
		return err
	}
	if finishedAt.IsZero() {
		return errors.New("career agent recovery timestamp is required")
	}
	_, err := r.db.Exec(postgresContext(ctx), `
		UPDATE agent_runs SET status='failed', finished_at=$1, result_code='interrupted',
		 summary='interrupted run', error_code='INTERRUPTED', error_summary='run interrupted before completion'
		WHERE status='running' AND started_at <= $1`, finishedAt.UTC())
	if err != nil {
		return fmt.Errorf("recover interrupted career agent runs: %w", err)
	}
	return nil
}

func (r *CareerWorkflowRepository) withMutation(ctx context.Context, fn func(postgresDBTX) error) error {
	if r.pool == nil {
		return fn(r.db)
	}
	tx, err := r.pool.Begin(postgresContext(ctx))
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(postgresContext(ctx)) }()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(postgresContext(ctx))
}

const preparationSelect = `SELECT id, vacancy_id, resume_id, resume_provider_id, resume_fingerprint, browser_resume_hash, candidate_id, candidate_version,
       candidate_snapshot_hash, route_status, route_confidence, evidence_json, story_ids,
       cover_letter, cover_letter_hash, test_answer_drafts_json, knowledge_requests_json,
       input_fingerprint, status, stale_reason, created_at, updated_at
       FROM application_preparations`

type careerAgentRunScanner interface {
	Scan(...any) error
}

func scanCareerAgentRun(scanner careerAgentRunScanner) (careeragent.AgentRun, error) {
	var (
		run                                          careeragent.AgentRun
		runType, stage, status                       string
		finishedAt                                   pgtype.Timestamptz
		resultCode, summary, errorCode, errorSummary pgtype.Text
		dailyResult                                  []byte
		confidence                                   pgtype.Float8
		startedAt, createdAt                         time.Time
	)
	if err := scanner.Scan(&run.ID, &runType, &stage, &status, &startedAt, &finishedAt, &resultCode, &summary,
		&dailyResult, &errorCode, &errorSummary, &confidence, &createdAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return careeragent.AgentRun{}, careeragent.ErrAgentRunNotFound
		}
		return careeragent.AgentRun{}, fmt.Errorf("scan career agent run: %w", err)
	}
	run.RunType, run.Stage, run.Status, run.StartedAt, run.CreatedAt = runType, careeragent.AgentRunStage(stage), careeragent.AgentRunStatus(status), startedAt, createdAt
	if finishedAt.Valid {
		value := finishedAt.Time.UTC()
		run.FinishedAt = &value
	}
	if resultCode.Valid {
		run.ResultCode = careeragent.AgentRunResultCode(resultCode.String)
	}
	if summary.Valid {
		run.Result, run.Summary = summary.String, summary.String
	}
	run.DailyResultJSON = workflowCloneJSON(dailyResult)
	if errorCode.Valid {
		run.ErrorCode = errorCode.String
	}
	if errorSummary.Valid && strings.TrimSpace(errorSummary.String) != "" {
		run.ErrorSummary = errorSummary.String
		run.Errors = []string{errorSummary.String}
	} else {
		run.Errors = []string{}
	}
	if confidence.Valid {
		value := confidence.Float64
		run.Confidence = &value
	}
	if err := run.Validate(); err != nil {
		return careeragent.AgentRun{}, fmt.Errorf("validate stored career agent run: %w", err)
	}
	return run, nil
}

func scanApplicationPreparation(scanner careerAgentRunScanner) (careeragent.ApplicationPreparation, error) {
	var (
		preparation                                                                                                     careeragent.ApplicationPreparation
		resumeID, resumeProviderID, resumeFingerprint, browserResumeHash, routeConfidence, coverLetter, coverLetterHash pgtype.Text
		staleReason                                                                                                     pgtype.Text
		evidence, storyIDs, testDrafts, knowledgeRequests                                                               []byte
		routeStatus, status                                                                                             string
	)
	if err := scanner.Scan(&preparation.ID, &preparation.VacancyID, &resumeID, &resumeProviderID, &resumeFingerprint, &browserResumeHash, &preparation.CandidateID,
		&preparation.CandidateVersion, &preparation.CandidateSnapshotHash, &routeStatus, &routeConfidence, &evidence,
		&storyIDs, &coverLetter, &coverLetterHash, &testDrafts, &knowledgeRequests, &preparation.InputFingerprint,
		&status, &staleReason, &preparation.CreatedAt, &preparation.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return careeragent.ApplicationPreparation{}, careeragent.ErrPreparationNotFound
		}
		return careeragent.ApplicationPreparation{}, fmt.Errorf("scan application preparation: %w", err)
	}
	preparation.ResumeID, preparation.ResumeProviderID, preparation.ResumeFingerprint, preparation.BrowserResumeHash = workflowNullableText(resumeID), workflowNullableText(resumeProviderID), workflowNullableText(resumeFingerprint), workflowNullableText(browserResumeHash)
	preparation.RouteStatus, preparation.RouteConfidence = careeragent.ResumeRouteStatus(routeStatus), workflowNullableText(routeConfidence)
	preparation.Evidence, preparation.TestAnswerDrafts = workflowCloneJSON(evidence), workflowCloneJSON(testDrafts)
	preparation.CoverLetter, preparation.CoverLetterHash, preparation.StaleReason = workflowNullableText(coverLetter), workflowNullableText(coverLetterHash), workflowNullableText(staleReason)
	if err := workflowDecodeJSON(storyIDs, &preparation.StoryIDs); err != nil {
		return careeragent.ApplicationPreparation{}, fmt.Errorf("decode preparation story ids: %w", err)
	}
	if err := workflowDecodeJSON(knowledgeRequests, &preparation.KnowledgeRequests); err != nil {
		return careeragent.ApplicationPreparation{}, fmt.Errorf("decode preparation knowledge requests: %w", err)
	}
	preparation.Status = careeragent.PreparationStatus(status)
	if err := preparation.Validate(); err != nil {
		return careeragent.ApplicationPreparation{}, fmt.Errorf("validate stored application preparation: %w", err)
	}
	return preparation, nil
}

func workflowNullableString(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func nullableWorkflowInt(value int) any {
	if value <= 0 {
		return nil
	}
	return value
}

func workflowNullableFloat(value *float64) any {
	if value == nil {
		return nil
	}
	return *value
}

func workflowNullableJSON(value any) ([]byte, error) {
	if value == nil {
		return nil, nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	if string(raw) == "null" || string(raw) == "[]" {
		return nil, nil
	}
	return raw, nil
}

func workflowNullableText(value pgtype.Text) string {
	if !value.Valid {
		return ""
	}
	return value.String
}

func workflowCloneJSON(raw []byte) json.RawMessage {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	return append(json.RawMessage(nil), raw...)
}

func workflowDecodeJSON(raw []byte, target any) error {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	return json.Unmarshal(raw, target)
}

func workflowLimit(limit int) int {
	if limit <= 0 || limit > 100 {
		return 100
	}
	return limit
}

func ErrWorkflowConflict(id string) error {
	return fmt.Errorf("%w: %s", careeragent.ErrWorkflowConflict, strings.TrimSpace(id))
}
