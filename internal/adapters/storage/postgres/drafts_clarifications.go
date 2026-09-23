package postgresstorage

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"hh-ai-responder/internal/ports"
	"hh-ai-responder/internal/usecase/aidraft"
	"hh-ai-responder/internal/usecase/candidateacquisition"
)

var _ aidraft.Backend = (*AIDraftRepository)(nil)
var _ ports.CandidateClarificationBackend = (*CandidateClarificationRepository)(nil)

type AIDraftRepository struct {
	db postgresDBTX
}

func NewAIDraftRepository(pool *pgxpool.Pool) *AIDraftRepository {
	if pool == nil {
		return &AIDraftRepository{}
	}
	return &AIDraftRepository{db: pool}
}

func NewAIDraftRepositoryForTx(tx pgx.Tx) *AIDraftRepository {
	return &AIDraftRepository{db: tx}
}

func (r *AIDraftRepository) requireDB() error {
	if r == nil || r.db == nil {
		return errors.New("postgres AI draft repository is not configured")
	}
	return nil
}

func (r *AIDraftRepository) Load(ctx context.Context) ([]aidraft.Draft, error) {
	return r.List(ctx)
}

func (r *AIDraftRepository) Save(ctx context.Context, values []aidraft.Draft) error {
	if err := r.requireDB(); err != nil {
		return err
	}
	for _, value := range values {
		if _, err := r.upsert(ctx, value); err != nil {
			return err
		}
	}
	return nil
}

func (r *AIDraftRepository) Create(ctx context.Context, value aidraft.Draft) (aidraft.Draft, error) {
	value = normalizeDraft(value)
	if err := value.Validate(); err != nil {
		return aidraft.Draft{}, err
	}
	if err := r.requireDB(); err != nil {
		return aidraft.Draft{}, err
	}
	usedFacts, err := json.Marshal(value.UsedFacts)
	if err != nil {
		return aidraft.Draft{}, fmt.Errorf("encode AI draft facts: %w", err)
	}
	_, err = r.db.Exec(postgresContext(ctx), `
		INSERT INTO ai_drafts
		(id,input_fingerprint,prompt_version,employer_message_hash,relevant_knowledge_hash,type,application_id,conversation_id,input_message_id,text,original_text,edited_text,source,status,model,created_at,updated_at,decision_reason,used_facts)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19)`,
		value.ID, value.InputFingerprint, value.PromptVersion, value.EmployerMessageHash, value.RelevantKnowledgeHash,
		string(value.Type), value.ApplicationID, value.ConversationID, value.InputMessageID, value.Text, value.OriginalText,
		value.EditedText, string(value.Source), string(value.Status), value.Model, value.CreatedAt, value.UpdatedAt,
		value.DecisionReason, usedFacts)
	if err != nil {
		return aidraft.Draft{}, fmt.Errorf("create AI draft: %w", err)
	}
	return value, nil
}

func (r *AIDraftRepository) UpsertByInputFingerprint(ctx context.Context, value aidraft.Draft) (aidraft.Draft, bool, error) {
	value = normalizeDraft(value)
	if strings.TrimSpace(value.InputFingerprint) == "" {
		created, err := r.Create(ctx, value)
		return created, err == nil, err
	}
	if err := r.requireDB(); err != nil {
		return aidraft.Draft{}, false, err
	}
	existing, err := scanAIDraft(r.db.QueryRow(postgresContext(ctx), aiDraftSelect+` WHERE input_fingerprint=$1`, value.InputFingerprint))
	if err == nil {
		return existing, false, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return aidraft.Draft{}, false, fmt.Errorf("find AI draft by fingerprint: %w", err)
	}
	usedFacts, marshalErr := json.Marshal(value.UsedFacts)
	if marshalErr != nil {
		return aidraft.Draft{}, false, fmt.Errorf("encode AI draft facts: %w", marshalErr)
	}
	_, err = r.db.Exec(postgresContext(ctx), `
		INSERT INTO ai_drafts
		(id,input_fingerprint,prompt_version,employer_message_hash,relevant_knowledge_hash,type,application_id,conversation_id,input_message_id,text,original_text,edited_text,source,status,model,created_at,updated_at,decision_reason,used_facts)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19)
		ON CONFLICT (input_fingerprint) WHERE input_fingerprint <> '' DO NOTHING`,
		value.ID, value.InputFingerprint, value.PromptVersion, value.EmployerMessageHash, value.RelevantKnowledgeHash,
		string(value.Type), value.ApplicationID, value.ConversationID, value.InputMessageID, value.Text, value.OriginalText,
		value.EditedText, string(value.Source), string(value.Status), value.Model, value.CreatedAt, value.UpdatedAt,
		value.DecisionReason, usedFacts)
	if err != nil {
		return aidraft.Draft{}, false, fmt.Errorf("upsert AI draft: %w", err)
	}
	existing, err = scanAIDraft(r.db.QueryRow(postgresContext(ctx), aiDraftSelect+` WHERE input_fingerprint=$1`, value.InputFingerprint))
	if err != nil {
		return aidraft.Draft{}, false, fmt.Errorf("read upserted AI draft: %w", err)
	}
	return existing, existing.ID == value.ID, nil
}

func (r *AIDraftRepository) Get(ctx context.Context, id string) (aidraft.Draft, error) {
	if err := r.requireDB(); err != nil {
		return aidraft.Draft{}, err
	}
	value, err := scanAIDraft(r.db.QueryRow(postgresContext(ctx), aiDraftSelect+` WHERE id=$1`, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return aidraft.Draft{}, errors.New("AI draft not found")
		}
		return aidraft.Draft{}, fmt.Errorf("get AI draft: %w", err)
	}
	return value, nil
}

func (r *AIDraftRepository) List(ctx context.Context) ([]aidraft.Draft, error) {
	if err := r.requireDB(); err != nil {
		return nil, err
	}
	rows, err := r.db.Query(postgresContext(ctx), aiDraftSelect+` ORDER BY updated_at DESC, id`)
	if err != nil {
		return nil, fmt.Errorf("list AI drafts: %w", err)
	}
	defer rows.Close()
	values := []aidraft.Draft{}
	for rows.Next() {
		value, err := scanAIDraft(rows)
		if err != nil {
			return nil, fmt.Errorf("scan AI draft: %w", err)
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate AI drafts: %w", err)
	}
	return values, nil
}

func (r *AIDraftRepository) SetStatus(ctx context.Context, id string, status aidraft.Status) error {
	if status != aidraft.StatusGenerated && status != aidraft.StatusApproved && status != aidraft.StatusRejected && status != aidraft.StatusSuperseded && status != aidraft.StatusSent {
		return errors.New("invalid AI draft status")
	}
	if err := r.requireDB(); err != nil {
		return err
	}
	command, err := r.db.Exec(postgresContext(ctx), `UPDATE ai_drafts SET status=$2, updated_at=$3 WHERE id=$1`, id, string(status), time.Now().UTC())
	if err != nil {
		return fmt.Errorf("set AI draft status: %w", err)
	}
	if command.RowsAffected() == 0 {
		return errors.New("AI draft not found")
	}
	return nil
}

func (r *AIDraftRepository) UpdateText(ctx context.Context, id, text string, source aidraft.Source) error {
	if strings.TrimSpace(text) == "" || len([]rune(text)) > 600 || (source != aidraft.SourceAI && source != aidraft.SourceUserEdited) {
		return errors.New("invalid AI draft text or source")
	}
	if err := r.requireDB(); err != nil {
		return err
	}
	command, err := r.db.Exec(postgresContext(ctx), `
		UPDATE ai_drafts SET
		original_text=CASE WHEN original_text='' THEN text ELSE original_text END,
		edited_text=CASE WHEN $3='user_edited' THEN $2 ELSE edited_text END,
		text=$2, source=$3, status='generated', updated_at=$4
		WHERE id=$1`, id, text, string(source), time.Now().UTC())
	if err != nil {
		return fmt.Errorf("update AI draft text: %w", err)
	}
	if command.RowsAffected() == 0 {
		return errors.New("AI draft not found")
	}
	return nil
}

func (r *AIDraftRepository) upsert(ctx context.Context, value aidraft.Draft) (aidraft.Draft, error) {
	value = normalizeDraft(value)
	if err := value.Validate(); err != nil {
		return aidraft.Draft{}, err
	}
	usedFacts, err := json.Marshal(value.UsedFacts)
	if err != nil {
		return aidraft.Draft{}, err
	}
	_, err = r.db.Exec(postgresContext(ctx), `
		INSERT INTO ai_drafts
		(id,input_fingerprint,prompt_version,employer_message_hash,relevant_knowledge_hash,type,application_id,conversation_id,input_message_id,text,original_text,edited_text,source,status,model,created_at,updated_at,decision_reason,used_facts)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19)
		ON CONFLICT (id) DO UPDATE SET input_fingerprint=EXCLUDED.input_fingerprint,prompt_version=EXCLUDED.prompt_version,employer_message_hash=EXCLUDED.employer_message_hash,relevant_knowledge_hash=EXCLUDED.relevant_knowledge_hash,type=EXCLUDED.type,application_id=EXCLUDED.application_id,conversation_id=EXCLUDED.conversation_id,input_message_id=EXCLUDED.input_message_id,text=EXCLUDED.text,original_text=EXCLUDED.original_text,edited_text=EXCLUDED.edited_text,source=EXCLUDED.source,status=EXCLUDED.status,model=EXCLUDED.model,created_at=EXCLUDED.created_at,updated_at=EXCLUDED.updated_at,decision_reason=EXCLUDED.decision_reason,used_facts=EXCLUDED.used_facts`,
		value.ID, value.InputFingerprint, value.PromptVersion, value.EmployerMessageHash, value.RelevantKnowledgeHash,
		string(value.Type), value.ApplicationID, value.ConversationID, value.InputMessageID, value.Text, value.OriginalText,
		value.EditedText, string(value.Source), string(value.Status), value.Model, value.CreatedAt, value.UpdatedAt,
		value.DecisionReason, usedFacts)
	if err != nil {
		return aidraft.Draft{}, fmt.Errorf("save AI draft: %w", err)
	}
	return value, nil
}

const aiDraftSelect = `SELECT id,input_fingerprint,prompt_version,employer_message_hash,relevant_knowledge_hash,type,application_id,conversation_id,input_message_id,text,original_text,edited_text,source,status,model,created_at,updated_at,decision_reason,used_facts FROM ai_drafts`

type postgresScanner interface {
	Scan(...any) error
}

func scanAIDraft(scanner postgresScanner) (aidraft.Draft, error) {
	var value aidraft.Draft
	var draftType, source, status string
	var usedFacts []byte
	if err := scanner.Scan(&value.ID, &value.InputFingerprint, &value.PromptVersion, &value.EmployerMessageHash, &value.RelevantKnowledgeHash, &draftType, &value.ApplicationID, &value.ConversationID, &value.InputMessageID, &value.Text, &value.OriginalText, &value.EditedText, &source, &status, &value.Model, &value.CreatedAt, &value.UpdatedAt, &value.DecisionReason, &usedFacts); err != nil {
		return aidraft.Draft{}, err
	}
	value.Type, value.Source, value.Status = aidraft.Type(draftType), aidraft.Source(source), aidraft.Status(status)
	if len(usedFacts) == 0 {
		value.UsedFacts = []string{}
	} else if err := json.Unmarshal(usedFacts, &value.UsedFacts); err != nil {
		return aidraft.Draft{}, fmt.Errorf("decode AI draft facts: %w", err)
	}
	return value, nil
}

func normalizeDraft(value aidraft.Draft) aidraft.Draft {
	if strings.TrimSpace(value.ID) == "" {
		var raw [16]byte
		if _, err := rand.Read(raw[:]); err == nil {
			value.ID = fmt.Sprintf("ai_draft-%x", raw)
		}
	}
	now := time.Now().UTC()
	if value.CreatedAt.IsZero() {
		value.CreatedAt = now
	}
	if value.UpdatedAt.IsZero() {
		value.UpdatedAt = value.CreatedAt
	}
	if value.Status == "" {
		value.Status = aidraft.StatusGenerated
	}
	if value.Source == "" {
		value.Source = aidraft.SourceAI
	}
	if value.OriginalText == "" {
		value.OriginalText = value.Text
	}
	if value.UsedFacts == nil {
		value.UsedFacts = []string{}
	}
	return value
}

type CandidateClarificationRepository struct {
	db postgresDBTX
}

func NewCandidateClarificationRepository(pool *pgxpool.Pool) *CandidateClarificationRepository {
	if pool == nil {
		return &CandidateClarificationRepository{}
	}
	return &CandidateClarificationRepository{db: pool}
}

func NewCandidateClarificationRepositoryForTx(tx pgx.Tx) *CandidateClarificationRepository {
	return &CandidateClarificationRepository{db: tx}
}

func (r *CandidateClarificationRepository) requireClarificationDB() error {
	if r == nil || r.db == nil {
		return errors.New("postgres clarification repository is not configured")
	}
	return nil
}

func (r *CandidateClarificationRepository) Load(ctx context.Context) ([]candidateacquisition.CandidateClarificationRequest, error) {
	return r.List(ctx)
}

func (r *CandidateClarificationRepository) Save(ctx context.Context, values []candidateacquisition.CandidateClarificationRequest) error {
	if err := r.requireClarificationDB(); err != nil {
		return err
	}
	for _, value := range values {
		if _, err := r.replace(ctx, value); err != nil {
			return err
		}
	}
	return nil
}

func (r *CandidateClarificationRepository) Create(ctx context.Context, value candidateacquisition.CandidateClarificationRequest) (candidateacquisition.CandidateClarificationRequest, error) {
	value = normalizeClarification(value)
	if err := validateClarificationRepository(value); err != nil {
		return candidateacquisition.CandidateClarificationRequest{}, err
	}
	if err := r.requireClarificationDB(); err != nil {
		return candidateacquisition.CandidateClarificationRequest{}, err
	}
	payload, err := json.Marshal(value)
	if err != nil {
		return candidateacquisition.CandidateClarificationRequest{}, fmt.Errorf("encode clarification: %w", err)
	}
	_, err = r.db.Exec(postgresContext(ctx), `INSERT INTO candidate_clarifications (id,identity,conversation_id,application_id,vacancy_id,employer_message_id,gap_key,status,created_at,updated_at,payload) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, value.ID, candidateacquisition.ClarificationIdentity(value), value.ConversationID, value.ApplicationID, value.VacancyID, value.EmployerMessageID, value.GapKey, string(value.Status), value.CreatedAt, value.CreatedAt, payload)
	if err != nil {
		return candidateacquisition.CandidateClarificationRequest{}, fmt.Errorf("create clarification: %w", err)
	}
	return value, nil
}

func (r *CandidateClarificationRepository) UpsertByIdentity(ctx context.Context, value candidateacquisition.CandidateClarificationRequest) (candidateacquisition.CandidateClarificationRequest, bool, error) {
	value = normalizeClarification(value)
	if err := validateClarificationRepository(value); err != nil {
		return candidateacquisition.CandidateClarificationRequest{}, false, err
	}
	if err := r.requireClarificationDB(); err != nil {
		return candidateacquisition.CandidateClarificationRequest{}, false, err
	}
	identity := candidateacquisition.ClarificationIdentity(value)
	existing, err := r.getByIdentity(ctx, identity)
	if err == nil {
		return existing, false, nil
	}
	if !errors.Is(err, errClarificationNotFound) {
		return candidateacquisition.CandidateClarificationRequest{}, false, err
	}
	payload, marshalErr := json.Marshal(value)
	if marshalErr != nil {
		return candidateacquisition.CandidateClarificationRequest{}, false, marshalErr
	}
	_, err = r.db.Exec(postgresContext(ctx), `INSERT INTO candidate_clarifications (id,identity,conversation_id,application_id,vacancy_id,employer_message_id,gap_key,status,created_at,updated_at,payload) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) ON CONFLICT (identity) DO NOTHING`, value.ID, identity, value.ConversationID, value.ApplicationID, value.VacancyID, value.EmployerMessageID, value.GapKey, string(value.Status), value.CreatedAt, value.CreatedAt, payload)
	if err != nil {
		return candidateacquisition.CandidateClarificationRequest{}, false, fmt.Errorf("upsert clarification: %w", err)
	}
	existing, err = r.getByIdentity(ctx, identity)
	if err != nil {
		return candidateacquisition.CandidateClarificationRequest{}, false, err
	}
	return existing, existing.ID == value.ID, nil
}

func (r *CandidateClarificationRepository) Get(ctx context.Context, id string) (candidateacquisition.CandidateClarificationRequest, error) {
	if err := r.requireClarificationDB(); err != nil {
		return candidateacquisition.CandidateClarificationRequest{}, err
	}
	var value candidateacquisition.CandidateClarificationRequest
	var payload []byte
	err := r.db.QueryRow(postgresContext(ctx), `SELECT payload FROM candidate_clarifications WHERE id=$1`, id).Scan(&payload)
	if errors.Is(err, pgx.ErrNoRows) {
		return candidateacquisition.CandidateClarificationRequest{}, errClarificationNotFound
	}
	if err != nil {
		return candidateacquisition.CandidateClarificationRequest{}, fmt.Errorf("get clarification: %w", err)
	}
	if err := json.Unmarshal(payload, &value); err != nil {
		return candidateacquisition.CandidateClarificationRequest{}, fmt.Errorf("decode clarification: %w", err)
	}
	return value, nil
}

func (r *CandidateClarificationRepository) List(ctx context.Context) ([]candidateacquisition.CandidateClarificationRequest, error) {
	if err := r.requireClarificationDB(); err != nil {
		return nil, err
	}
	rows, err := r.db.Query(postgresContext(ctx), `SELECT payload FROM candidate_clarifications ORDER BY updated_at DESC, id`)
	if err != nil {
		return nil, fmt.Errorf("list clarifications: %w", err)
	}
	defer rows.Close()
	values := []candidateacquisition.CandidateClarificationRequest{}
	for rows.Next() {
		var payload []byte
		if err := rows.Scan(&payload); err != nil {
			return nil, err
		}
		var value candidateacquisition.CandidateClarificationRequest
		if err := json.Unmarshal(payload, &value); err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

var errClarificationNotFound = errors.New("clarification not found")

func (r *CandidateClarificationRepository) getByIdentity(ctx context.Context, identity string) (candidateacquisition.CandidateClarificationRequest, error) {
	var payload []byte
	if err := r.db.QueryRow(postgresContext(ctx), `SELECT payload FROM candidate_clarifications WHERE identity=$1`, identity).Scan(&payload); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return candidateacquisition.CandidateClarificationRequest{}, errClarificationNotFound
		}
		return candidateacquisition.CandidateClarificationRequest{}, fmt.Errorf("get clarification identity: %w", err)
	}
	var value candidateacquisition.CandidateClarificationRequest
	if err := json.Unmarshal(payload, &value); err != nil {
		return candidateacquisition.CandidateClarificationRequest{}, err
	}
	return value, nil
}

func (r *CandidateClarificationRepository) SetStatus(ctx context.Context, id string, status candidateacquisition.CandidateClarificationStatus) error {
	value, err := r.Get(ctx, id)
	if err != nil {
		return err
	}
	if status != candidateacquisition.ClarificationPending && status != candidateacquisition.ClarificationAnswered && status != candidateacquisition.ClarificationDismissed && status != candidateacquisition.ClarificationResolvedExistingKnowledge {
		return errors.New("invalid clarification status")
	}
	value.Status = status
	if status == candidateacquisition.ClarificationPending {
		value.ResolvedAt = nil
	} else {
		now := time.Now().UTC()
		value.ResolvedAt = &now
	}
	return r.Replace(ctx, value)
}

func (r *CandidateClarificationRepository) RecordAnswer(ctx context.Context, id string, answer candidateacquisition.CandidateAnswer) error {
	if strings.TrimSpace(answer.Raw) == "" {
		return errors.New("candidate answer is empty")
	}
	value, err := r.Get(ctx, id)
	if err != nil {
		return err
	}
	value.Answer = &answer
	value.Status = candidateacquisition.ClarificationAnswered
	value.ReadyForRegeneration = false
	now := time.Now().UTC()
	value.ResolvedAt = &now
	return r.Replace(ctx, value)
}

func (r *CandidateClarificationRepository) RecordAnswerEvidence(ctx context.Context, id string, answer candidateacquisition.CandidateAnswer) error {
	if strings.TrimSpace(answer.Raw) == "" {
		return errors.New("candidate answer is empty")
	}
	value, err := r.Get(ctx, id)
	if err != nil {
		return err
	}
	value.Answer = &answer
	return r.Replace(ctx, value)
}

func (r *CandidateClarificationRepository) SetProposalIDs(ctx context.Context, id string, ids []string) error {
	value, err := r.Get(ctx, id)
	if err != nil {
		return err
	}
	value.ProposalIDs = append([]string{}, ids...)
	if len(ids) > 0 {
		value.ProposalID = ids[0]
	}
	return r.Replace(ctx, value)
}

func (r *CandidateClarificationRepository) MarkResolved(ctx context.Context, id string, status candidateacquisition.CandidateClarificationStatus, reason string) error {
	if status != candidateacquisition.ClarificationResolvedExistingKnowledge && status != candidateacquisition.ClarificationDismissed {
		return errors.New("invalid resolved clarification status")
	}
	value, err := r.Get(ctx, id)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	value.Status, value.ResolvedAt, value.ResolutionReason, value.ReadyForRegeneration = status, &now, reason, status == candidateacquisition.ClarificationResolvedExistingKnowledge
	return r.Replace(ctx, value)
}

func (r *CandidateClarificationRepository) Reopen(ctx context.Context, id, reason string) error {
	value, err := r.Get(ctx, id)
	if err != nil {
		return err
	}
	value.Status, value.ResolvedAt, value.ResolutionReason, value.ReadyForRegeneration = candidateacquisition.ClarificationPending, nil, strings.TrimSpace(reason), true
	return r.Replace(ctx, value)
}

func (r *CandidateClarificationRepository) Replace(ctx context.Context, value candidateacquisition.CandidateClarificationRequest) error {
	_, err := r.replace(ctx, value)
	return err
}

func (r *CandidateClarificationRepository) replace(ctx context.Context, value candidateacquisition.CandidateClarificationRequest) (candidateacquisition.CandidateClarificationRequest, error) {
	if err := r.requireClarificationDB(); err != nil {
		return candidateacquisition.CandidateClarificationRequest{}, err
	}
	if err := validateClarificationRepository(value); err != nil {
		return candidateacquisition.CandidateClarificationRequest{}, err
	}
	payload, err := json.Marshal(value)
	if err != nil {
		return candidateacquisition.CandidateClarificationRequest{}, err
	}
	command, err := r.db.Exec(postgresContext(ctx), `UPDATE candidate_clarifications SET identity=$2,conversation_id=$3,application_id=$4,vacancy_id=$5,employer_message_id=$6,gap_key=$7,status=$8,updated_at=$9,payload=$10 WHERE id=$1`, value.ID, candidateacquisition.ClarificationIdentity(value), value.ConversationID, value.ApplicationID, value.VacancyID, value.EmployerMessageID, value.GapKey, string(value.Status), time.Now().UTC(), payload)
	if err != nil {
		return candidateacquisition.CandidateClarificationRequest{}, fmt.Errorf("replace clarification: %w", err)
	}
	if command.RowsAffected() == 0 {
		return candidateacquisition.CandidateClarificationRequest{}, errClarificationNotFound
	}
	return value, nil
}

func normalizeClarification(value candidateacquisition.CandidateClarificationRequest) candidateacquisition.CandidateClarificationRequest {
	if strings.TrimSpace(value.ID) == "" {
		var raw [16]byte
		if _, err := rand.Read(raw[:]); err == nil {
			value.ID = fmt.Sprintf("clarification-%x", raw)
		}
	}
	if value.CreatedAt.IsZero() {
		value.CreatedAt = time.Now().UTC()
	}
	if value.Status == "" {
		value.Status = candidateacquisition.ClarificationPending
	}
	return value
}

func validateClarificationRepository(value candidateacquisition.CandidateClarificationRequest) error {
	if strings.TrimSpace(value.ID) == "" || strings.TrimSpace(value.Question) == "" || strings.TrimSpace(value.Topic) == "" || strings.TrimSpace(value.Reason) == "" || value.CreatedAt.IsZero() {
		return errors.New("invalid clarification request")
	}
	switch value.Status {
	case candidateacquisition.ClarificationPending, candidateacquisition.ClarificationAnswered, candidateacquisition.ClarificationDismissed, candidateacquisition.ClarificationResolvedExistingKnowledge:
	default:
		return errors.New("invalid clarification status")
	}
	if value.ResolvedAt != nil && value.ResolvedAt.IsZero() {
		return errors.New("invalid clarification resolved_at")
	}
	if value.Answer != nil && strings.TrimSpace(value.Answer.Raw) == "" {
		return errors.New("clarification answer raw value is required")
	}
	return nil
}
