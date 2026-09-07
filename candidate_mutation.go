package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// CandidateMutationService is the single application boundary for candidate
// knowledge writes. Callers submit typed commands; they do not load and save a
// whole Candidate and therefore cannot accidentally turn an ordinary update
// into a lost-update race.
type CandidateMutationService struct {
	backend      string
	reader       CandidateRepository
	store        *PostgresCandidateStore
	jsonProfile  string
	verifyGitHub func(source KnowledgeSourceRecord, entityType string, value json.RawMessage) error
	semantic     *CandidateSemanticIndexService
}

type CandidateKnowledgeMutationWriter interface {
	UpdateUnknown(CandidateUnknown, KnowledgeUpdate) (KnowledgeUpdateResult, error)
}

type UpdateSkillCommand struct {
	Actor  KnowledgeActor
	Value  CandidateSkillDetailed
	Update KnowledgeUpdate
}

type UpdateProjectCommand struct {
	Actor  KnowledgeActor
	Value  CandidateProject
	Update KnowledgeUpdate
}

type UpdateAchievementCommand struct {
	Actor  KnowledgeActor
	Value  CandidateAchievement
	Update KnowledgeUpdate
}

type AskCandidateUnknownCommand struct {
	Actor  KnowledgeActor
	Value  CandidateUnknown
	Update KnowledgeUpdate
}

type ResolveCandidateUnknownCommand struct {
	Actor     KnowledgeActor
	UnknownID string
	Answer    string
	Confirmed bool
	Evidence  string
}

type ImportHHResumeCommand struct {
	Actor      KnowledgeActor
	Resume     ResumeItem
	Facts      ResumeFacts
	ObservedAt time.Time
}

type ResolveKnowledgeProposalCommand struct {
	Actor      KnowledgeActor
	ProposalID string
}

type CandidateMutationResult struct {
	EntityID             string
	ProposalID           string
	QuestionID           string
	SemanticIndexWarning string `json:"semantic_index_warning,omitempty"`
}

func NewCandidateMutationService(backend string, reader CandidateRepository, store *PostgresCandidateStore, profilePath string) *CandidateMutationService {
	return &CandidateMutationService{backend: backend, reader: reader, store: store, jsonProfile: profilePath}
}

func (s *CandidateMutationService) SetGitHubVerifier(verify func(source KnowledgeSourceRecord, entityType string, value json.RawMessage) error) {
	if s != nil {
		s.verifyGitHub = verify
	}
}

// SetSemanticIndexer installs an optional post-commit hook. The hook runs only
// after the canonical PostgreSQL transaction has committed; an embedding
// outage therefore leaves the mutation committed and the semantic index stale.
func (s *CandidateMutationService) SetSemanticIndexer(indexer *CandidateSemanticIndexService) {
	if s != nil {
		s.semantic = indexer
	}
}

func (s *CandidateMutationService) UpdateSkill(ctx context.Context, command UpdateSkillCommand) (CandidateMutationResult, error) {
	if command.Value.Level == "" {
		command.Value.Level = SkillLevelUnknown
	}
	if s == nil || s.backend == storageBackendJSON {
		kb, err := s.loadJSON()
		if err != nil {
			return CandidateMutationResult{}, err
		}
		var verifier func(KnowledgeSourceRecord, string, json.RawMessage) error
		if s != nil {
			verifier = s.verifyGitHub
		}
		result, err := NewCandidateKnowledgeUpdater(kb, KnowledgeUpdaterOptions{Actor: command.Actor, VerifyGitHub: verifier}).UpdateSkill(command.Value, command.Update)
		if err != nil {
			return CandidateMutationResult{}, err
		}
		if err := kb.Save(); err != nil {
			return CandidateMutationResult{}, err
		}
		return CandidateMutationResult{EntityID: result.EntityID, ProposalID: result.ProposalID, QuestionID: result.QuestionID}, nil
	}
	return s.mutatePostgres(ctx, command.Actor, func(candidate *Candidate) (CandidateMutationResult, error) {
		return mutateCanonicalSkill(candidate, command.Value, command.Update, command.Actor, s.verifyGitHub)
	})
}

func (s *CandidateMutationService) UpdateProject(ctx context.Context, command UpdateProjectCommand) (CandidateMutationResult, error) {
	if s == nil || s.backend == storageBackendJSON {
		kb, err := s.loadJSON()
		if err != nil {
			return CandidateMutationResult{}, err
		}
		var verifier func(KnowledgeSourceRecord, string, json.RawMessage) error
		if s != nil {
			verifier = s.verifyGitHub
		}
		result, err := NewCandidateKnowledgeUpdater(kb, KnowledgeUpdaterOptions{Actor: command.Actor, VerifyGitHub: verifier}).UpdateProject(command.Value, command.Update)
		if err != nil {
			return CandidateMutationResult{}, err
		}
		if err = kb.Save(); err != nil {
			return CandidateMutationResult{}, err
		}
		return CandidateMutationResult{EntityID: result.EntityID, ProposalID: result.ProposalID, QuestionID: result.QuestionID}, nil
	}
	return s.mutatePostgres(ctx, command.Actor, func(candidate *Candidate) (CandidateMutationResult, error) {
		return mutateCanonicalProject(candidate, command.Value, command.Update, command.Actor, s.verifyGitHub)
	})
}

func (s *CandidateMutationService) UpdateAchievement(ctx context.Context, command UpdateAchievementCommand) (CandidateMutationResult, error) {
	if s == nil || s.backend == storageBackendJSON {
		kb, err := s.loadJSON()
		if err != nil {
			return CandidateMutationResult{}, err
		}
		var verifier func(KnowledgeSourceRecord, string, json.RawMessage) error
		if s != nil {
			verifier = s.verifyGitHub
		}
		result, err := NewCandidateKnowledgeUpdater(kb, KnowledgeUpdaterOptions{Actor: command.Actor, VerifyGitHub: verifier}).UpdateAchievement(command.Value, command.Update)
		if err != nil {
			return CandidateMutationResult{}, err
		}
		if err = kb.Save(); err != nil {
			return CandidateMutationResult{}, err
		}
		return CandidateMutationResult{EntityID: result.EntityID, ProposalID: result.ProposalID, QuestionID: result.QuestionID}, nil
	}
	return s.mutatePostgres(ctx, command.Actor, func(candidate *Candidate) (CandidateMutationResult, error) {
		return mutateCanonicalAchievement(candidate, command.Value, command.Update, command.Actor, s.verifyGitHub)
	})
}

// AskUnknown deliberately never confirms a fact. Candidate answers are kept
// as a separate knowledge state until a user confirmation command resolves it.
func (s *CandidateMutationService) AskUnknown(ctx context.Context, command AskCandidateUnknownCommand) (CandidateMutationResult, error) {
	if s == nil || s.backend == storageBackendJSON {
		kb, err := s.loadJSON()
		if err != nil {
			return CandidateMutationResult{}, err
		}
		var verifier func(KnowledgeSourceRecord, string, json.RawMessage) error
		if s != nil {
			verifier = s.verifyGitHub
		}
		result, err := NewCandidateKnowledgeUpdater(kb, KnowledgeUpdaterOptions{Actor: command.Actor, VerifyGitHub: verifier}).UpdateUnknown(command.Value, command.Update)
		if err != nil {
			return CandidateMutationResult{}, err
		}
		if err = kb.Save(); err != nil {
			return CandidateMutationResult{}, err
		}
		return CandidateMutationResult{EntityID: result.EntityID, ProposalID: result.ProposalID, QuestionID: result.QuestionID}, nil
	}
	return s.mutatePostgres(ctx, command.Actor, func(candidate *Candidate) (CandidateMutationResult, error) {
		return mutateCanonicalUnknown(candidate, command.Value, command.Update, command.Actor)
	})
}

// UpdateUnknown is the small adapter used by the AI clarification pipeline.
// It intentionally preserves the existing contract: an answer creates or
// updates an unknown/proposal and never confirms employer-safe knowledge.
func (s *CandidateMutationService) UpdateUnknown(value CandidateUnknown, update KnowledgeUpdate) (KnowledgeUpdateResult, error) {
	result, err := s.AskUnknown(context.Background(), AskCandidateUnknownCommand{Actor: KnowledgeActorAI, Value: value, Update: update})
	return KnowledgeUpdateResult{EntityID: result.EntityID, ProposalID: result.ProposalID, QuestionID: result.QuestionID}, err
}

// ImportHHResumeFacts is an explicit import operation. It records HH data as
// verified external evidence and never promotes it to user-confirmed truth.
// Startup intentionally does not call this method.
func (s *CandidateMutationService) ImportHHResumeFacts(ctx context.Context, command ImportHHResumeCommand) error {
	if command.Actor != KnowledgeActorImporter {
		return errors.New("HH resume import requires the trusted importer actor")
	}
	if s == nil || s.backend == storageBackendJSON {
		return errors.New("HH resume import is not available through the PostgreSQL mutation service in JSON mode")
	}
	observed := command.ObservedAt
	if observed.IsZero() {
		observed = time.Now().UTC()
	}
	_, err := s.mutatePostgres(ctx, command.Actor, func(candidate *Candidate) (CandidateMutationResult, error) {
		oldFacts := candidate.ResumeFacts
		facts := command.Facts
		candidate.ResumeFacts = &facts
		if strings.TrimSpace(command.Resume.Area) != "" && candidate.Identity.LocationMetadata.TruthStatus != TruthStatusConfirmed {
			candidate.Identity.Location = command.Resume.Area
			candidate.Identity.LocationMetadata = verifiedKnowledgeMetadata(KnowledgeSourceHHResume, "structured HH resume area", observed)
		}
		if command.Facts.TotalExperienceMonthsKnown && candidate.Profile.TotalExperienceMonths.ProfileFact.Source != CandidateSourceUserConfirmed {
			candidate.Profile.TotalExperienceMonths.Value = command.Facts.TotalExperienceMonths
			candidate.Profile.TotalExperienceMonths.ProfileFact = ProfileFact{Source: CandidateSourceHHResume, Confirmed: true, ConfirmedAt: observed, Evidence: []string{"structured HH resume total experience"}}
		}
		for _, name := range parseResumeSkillNames(command.Resume.Skills) {
			if _, err := mutateCanonicalSkill(candidate, CandidateSkillDetailed{Name: name, Level: SkillLevelUnknown}, KnowledgeUpdate{Source: KnowledgeSourceRecord{Type: KnowledgeSourceHHResume, Evidence: []string{"HH resume skills"}, ObservedAt: &observed}, Reason: "explicit HH resume import"}, KnowledgeActorImporter, nil); err != nil {
				return CandidateMutationResult{}, err
			}
		}
		if err := appendCandidateEvent(candidate, "import", "resume", firstNonEmpty(command.Resume.Hash, "hh-resume"), oldFacts, candidate.ResumeFacts, KnowledgeSourceHHResume, string(command.Actor)); err != nil {
			return CandidateMutationResult{}, err
		}
		return CandidateMutationResult{}, nil
	})
	return err
}

func (s *CandidateMutationService) ConfirmProposal(ctx context.Context, command ResolveKnowledgeProposalCommand) error {
	return s.resolveProposal(ctx, command, true)
}

func (s *CandidateMutationService) RejectProposal(ctx context.Context, command ResolveKnowledgeProposalCommand) error {
	return s.resolveProposal(ctx, command, false)
}

func (s *CandidateMutationService) ResolveUnknown(ctx context.Context, command ResolveCandidateUnknownCommand) error {
	if command.Actor != KnowledgeActorUser {
		return errors.New("only an explicit user action may resolve a candidate unknown")
	}
	if strings.TrimSpace(command.UnknownID) == "" || strings.TrimSpace(command.Answer) == "" {
		return errors.New("unknown id and explicit answer are required")
	}
	if s == nil || s.backend == storageBackendJSON {
		return errors.New("explicit unknown resolution through the legacy profile command is unsupported; use the existing profile questionnaire")
	}
	_, err := s.mutatePostgres(ctx, command.Actor, func(candidate *Candidate) (CandidateMutationResult, error) {
		for i := range candidate.Unknowns {
			unknown := &candidate.Unknowns[i]
			if unknown.ID != command.UnknownID {
				continue
			}
			if unknown.Status != CandidateUnknownNeedsConfirmation {
				return CandidateMutationResult{}, errors.New("candidate unknown is already resolved")
			}
			now := time.Now().UTC()
			old := *unknown
			unknown.Hypothesis = command.Answer
			unknown.Status = CandidateUnknownConfirmed
			unknown.TruthStatus = TruthStatusConfirmed
			unknown.UpdatedAt = now
			unknown.ConfirmedAt = &now
			unknown.Sources = []KnowledgeSourceRecord{{Type: KnowledgeSourceUserConfirmed, Evidence: []string{firstNonEmpty(command.Evidence, "explicit candidate answer")}, ObservedAt: &now}}
			unknown.Evidence = []string{command.Answer}
			if err := unknown.validate(); err != nil {
				return CandidateMutationResult{}, err
			}
			polarity := CanonicalClaimPositive
			if strings.HasPrefix(strings.ToLower(strings.TrimSpace(command.Answer)), "нет") || strings.HasPrefix(strings.ToLower(strings.TrimSpace(command.Answer)), "no") {
				polarity = CanonicalClaimNegative
			}
			claimID, err := appendCanonicalClaim(candidate, "unknown", unknown.ID, "answer", command.Answer, polarity, unknown.KnowledgeMetadata, true)
			if err != nil {
				return CandidateMutationResult{}, err
			}
			if err := appendCandidateEvent(candidate, "resolve", "unknown", unknown.ID, old, *unknown, KnowledgeSourceUserConfirmed, string(KnowledgeActorUser)); err != nil {
				return CandidateMutationResult{}, err
			}
			return CandidateMutationResult{EntityID: unknown.ID, ProposalID: claimID}, nil
		}
		return CandidateMutationResult{}, errors.New("candidate unknown not found")
	})
	return err
}

func (s *CandidateMutationService) resolveProposal(ctx context.Context, command ResolveKnowledgeProposalCommand, confirm bool) error {
	if command.Actor != KnowledgeActorUser {
		return errors.New("only an explicit user action may confirm or reject knowledge")
	}
	if s == nil || s.backend == storageBackendJSON {
		kb, err := s.loadJSON()
		if err != nil {
			return err
		}
		var verifier func(KnowledgeSourceRecord, string, json.RawMessage) error
		if s != nil {
			verifier = s.verifyGitHub
		}
		updater := NewCandidateKnowledgeUpdater(kb, KnowledgeUpdaterOptions{Actor: command.Actor, VerifyGitHub: verifier})
		if confirm {
			err = updater.ConfirmKnowledge(command.ProposalID)
		} else {
			err = updater.RejectKnowledge(command.ProposalID)
		}
		if err != nil {
			return err
		}
		return kb.Save()
	}
	_, err := s.mutatePostgres(ctx, command.Actor, func(candidate *Candidate) (CandidateMutationResult, error) {
		return resolveCanonicalProposal(candidate, command.ProposalID, confirm)
	})
	return err
}

func (s *CandidateMutationService) loadJSON() (*CandidateKnowledgeBase, error) {
	if s == nil || strings.TrimSpace(s.jsonProfile) == "" {
		return nil, errors.New("candidate JSON mutation service is not configured")
	}
	kb := NewCandidateKnowledgeBase(s.jsonProfile)
	if err := kb.Load(); err != nil {
		return nil, err
	}
	return kb, nil
}

func (s *CandidateMutationService) mutatePostgres(ctx context.Context, actor KnowledgeActor, fn func(*Candidate) (CandidateMutationResult, error)) (CandidateMutationResult, error) {
	if s == nil || s.store == nil {
		return CandidateMutationResult{}, errors.New("postgres candidate mutation service is not configured")
	}
	var result CandidateMutationResult
	err := s.store.WithTx(ctx, func(tx CandidateTx) error {
		current, err := tx.CurrentCandidate(ctx)
		if err != nil {
			return err
		}
		expected := current.Version
		result, err = fn(&current)
		if err != nil {
			return err
		}
		current.Version = expected + 1
		current.UpdatedAt = time.Now().UTC()
		sortCanonicalCandidate(&current)
		if err := tx.PersistCandidateIfVersion(ctx, current, expected); err != nil {
			return err
		}
		_ = actor // actor is recorded on every event by the domain mutation helpers.
		return nil
	})
	if err != nil {
		return CandidateMutationResult{}, err
	}
	if s.semantic != nil {
		candidate, readErr := s.reader.CurrentCandidate(ctx)
		if readErr != nil {
			result.SemanticIndexWarning = "semantic index is stale: canonical read-back failed"
		} else if _, indexErr := s.semantic.Reindex(ctx, candidate, true); indexErr != nil {
			result.SemanticIndexWarning = "semantic index is stale: " + indexErr.Error()
		}
	}
	return result, nil
}

func appendCandidateEvent(candidate *Candidate, action, entityType, entityID string, oldValue, newValue any, source KnowledgeSource, actor string) error {
	oldRaw, err := json.Marshal(oldValue)
	if err != nil {
		return err
	}
	newRaw, err := json.Marshal(newValue)
	if err != nil {
		return err
	}
	eventID, err := newKnowledgeID("event")
	if err != nil {
		return err
	}
	event := CandidateKnowledgeEvent{ID: eventID, Timestamp: time.Now().UTC(), Action: action, EntityType: entityType, EntityID: entityID, OldValue: oldRaw, NewValue: newRaw, Source: source, Actor: actor}
	if err := event.validate(); err != nil {
		return err
	}
	candidate.Events = append(candidate.Events, event)
	return nil
}

func verifiedKnowledgeMetadata(source KnowledgeSource, evidence string, observed time.Time) KnowledgeMetadata {
	return KnowledgeMetadata{TruthStatus: TruthStatusVerified, Sources: []KnowledgeSourceRecord{{Type: source, Evidence: []string{evidence}, ObservedAt: &observed}}, Evidence: []string{evidence}, CreatedAt: observed, UpdatedAt: observed}
}

func metadataForCanonicalMutation(actor KnowledgeActor, value knowledgeFact, kind string, update KnowledgeUpdate, verifier func(source KnowledgeSourceRecord, entityType string, value json.RawMessage) error) (KnowledgeMetadata, error) {
	if !reflectKnowledgeMetadataEmpty(value.knowledgeMetadata()) {
		return KnowledgeMetadata{}, errors.New("update payload cannot supply truth status, confirmation, sources or timestamps; use KnowledgeUpdate")
	}
	return knowledgeMutationMetadata(actor, verifier, value, kind, update, time.Now().UTC())
}

func reflectKnowledgeMetadataEmpty(m KnowledgeMetadata) bool {
	return m.Confidence == nil && m.TruthStatus == "" && len(m.Sources) == 0 && len(m.Evidence) == 0 && m.CreatedAt.IsZero() && m.UpdatedAt.IsZero() && m.ConfirmedAt == nil
}

func appendCanonicalClaim(candidate *Candidate, subjectType, subjectID, field, value string, polarity CanonicalClaimPolarity, metadata KnowledgeMetadata, userCorrection bool) (string, error) {
	claimID, err := newKnowledgeID("claim")
	if err != nil {
		return "", err
	}
	claim := CanonicalCandidateClaim{ID: claimID, SubjectType: subjectType, SubjectID: subjectID, Field: field, Value: value, Polarity: polarity, State: CanonicalClaimActive, Metadata: metadata}
	for i := range candidate.Claims {
		old := &candidate.Claims[i]
		if old.SubjectType != subjectType || old.SubjectID != subjectID || old.Field != field || old.State != CanonicalClaimActive {
			continue
		}
		if old.Polarity == polarity && old.Value == value {
			return old.ID, nil
		}
		claim.SupersedesID = old.ID
		if userCorrection {
			old.State = CanonicalClaimSuperseded
		} else {
			old.State = CanonicalClaimDisputed
			claim.State = CanonicalClaimDisputed
			claim.ConflictSetID = firstNonEmpty(old.ConflictSetID, "conflict-"+claimID)
			old.ConflictSetID = claim.ConflictSetID
		}
	}
	candidate.Claims = append(candidate.Claims, claim)
	return claimID, nil
}

func mutateCanonicalSkill(candidate *Candidate, value CandidateSkillDetailed, update KnowledgeUpdate, actor KnowledgeActor, verifier func(source KnowledgeSourceRecord, entityType string, value json.RawMessage) error) (CandidateMutationResult, error) {
	meta, err := metadataForCanonicalMutation(actor, value, "skill", update, verifier)
	if err != nil {
		return CandidateMutationResult{}, err
	}
	if value.Level == "" {
		value.Level = SkillLevelUnknown
	}
	id := value.ID
	index := -1
	for i := range candidate.Skills {
		if (id != "" && candidate.Skills[i].ID == id) || (id == "" && canonicalSkillName(candidate.Skills[i].Name) == canonicalSkillName(value.Name)) {
			index = i
			break
		}
	}
	if index >= 0 && id == "" {
		value.ID = candidate.Skills[index].ID
	}
	if value.ID == "" {
		value.ID, err = newKnowledgeID("skill")
		if err != nil {
			return CandidateMutationResult{}, err
		}
		index = -1
	}
	value.KnowledgeMetadata = meta
	if update.Source.Type == KnowledgeSourceUnknown {
		return createCanonicalUnknownFromValue(candidate, value, meta, actor)
	}
	direct := update.Source.Type == KnowledgeSourceUserConfirmed || update.Source.Type == KnowledgeSourceHHResume
	if update.Source.Type != KnowledgeSourceUserConfirmed && index >= 0 && candidate.Skills[index].Metadata.TruthStatus == TruthStatusConfirmed {
		direct = false
	}
	if !direct {
		return createCanonicalProposal(candidate, "skill", value.ID, value, update, actor)
	}
	if index < 0 {
		candidate.Skills = append(candidate.Skills, canonicalSkillFromDetailed(value))
		if _, err := appendCanonicalClaim(candidate, "skill", value.ID, "fact", mustJSON(value), claimPolarity(value.Negative), meta, update.Source.Type == KnowledgeSourceUserConfirmed); err != nil {
			return CandidateMutationResult{}, err
		}
		if err := appendCandidateEvent(candidate, "add", "skill", value.ID, nil, value, update.Source.Type, string(actor)); err != nil {
			return CandidateMutationResult{}, err
		}
		return CandidateMutationResult{EntityID: value.ID}, nil
	}
	old := candidate.Skills[index]
	if old.Negative != value.Negative && old.Metadata.TruthStatus == TruthStatusConfirmed && update.Source.Type != KnowledgeSourceUserConfirmed {
		claimID, _ := appendCanonicalClaim(candidate, "skill", value.ID, "fact", mustJSON(value), claimPolarity(value.Negative), meta, false)
		candidate.Skills[index].State = CanonicalClaimDisputed
		candidate.Skills[index].SourceAssertions = append(candidate.Skills[index].SourceAssertions, canonicalAssertionFromDetailed(value))
		if err := appendCandidateEvent(candidate, "conflict", "skill", value.ID, old, candidate.Skills[index], update.Source.Type, string(actor)); err != nil {
			return CandidateMutationResult{}, err
		}
		return CandidateMutationResult{EntityID: value.ID, ProposalID: claimID}, nil
	}
	claimID, err := appendCanonicalClaim(candidate, "skill", value.ID, "fact", mustJSON(value), claimPolarity(value.Negative), meta, update.Source.Type == KnowledgeSourceUserConfirmed)
	if err != nil {
		return CandidateMutationResult{}, err
	}
	candidate.Skills[index] = canonicalSkillFromDetailed(value)
	if old.State == CanonicalClaimDisputed {
		candidate.Skills[index].State = CanonicalClaimDisputed
	}
	if err := appendCandidateEvent(candidate, "update", "skill", value.ID, old, candidate.Skills[index], update.Source.Type, string(actor)); err != nil {
		return CandidateMutationResult{}, err
	}
	return CandidateMutationResult{EntityID: value.ID, ProposalID: claimID}, nil
}

func mutateCanonicalProject(candidate *Candidate, value CandidateProject, update KnowledgeUpdate, actor KnowledgeActor, verifier func(source KnowledgeSourceRecord, entityType string, value json.RawMessage) error) (CandidateMutationResult, error) {
	meta, err := metadataForCanonicalMutation(actor, value, "project", update, verifier)
	if err != nil {
		return CandidateMutationResult{}, err
	}
	value.KnowledgeMetadata = meta
	if value.ID == "" {
		value.ID, err = newKnowledgeID("project")
		if err != nil {
			return CandidateMutationResult{}, err
		}
	}
	for i := range candidate.Projects {
		if candidate.Projects[i].ID == value.ID {
			if update.Source.Type != KnowledgeSourceUserConfirmed && candidate.Projects[i].Metadata.TruthStatus == TruthStatusConfirmed {
				return createCanonicalProposal(candidate, "project", value.ID, value, update, actor)
			}
			old := candidate.Projects[i]
			claimID := ""
			if len(old.ClaimIDs) > 0 {
				claimID = old.ClaimIDs[0]
			}
			candidate.Projects[i] = canonicalProjectFromKnowledge(value, claimID)
			if err := appendCandidateEvent(candidate, "update", "project", value.ID, old, value, update.Source.Type, string(actor)); err != nil {
				return CandidateMutationResult{}, err
			}
			return CandidateMutationResult{EntityID: value.ID}, nil
		}
	}
	if update.Source.Type != KnowledgeSourceUserConfirmed && update.Source.Type != KnowledgeSourceHHResume {
		return createCanonicalProposal(candidate, "project", value.ID, value, update, actor)
	}
	claimID, err := appendCanonicalClaim(candidate, "project", value.ID, "entity", mustJSON(value), CanonicalClaimPositive, meta, update.Source.Type == KnowledgeSourceUserConfirmed)
	if err != nil {
		return CandidateMutationResult{}, err
	}
	candidate.Projects = append(candidate.Projects, canonicalProjectFromKnowledge(value, claimID))
	if err := appendCandidateEvent(candidate, "add", "project", value.ID, nil, value, update.Source.Type, string(actor)); err != nil {
		return CandidateMutationResult{}, err
	}
	return CandidateMutationResult{EntityID: value.ID}, nil
}

func mutateCanonicalAchievement(candidate *Candidate, value CandidateAchievement, update KnowledgeUpdate, actor KnowledgeActor, verifier func(source KnowledgeSourceRecord, entityType string, value json.RawMessage) error) (CandidateMutationResult, error) {
	meta, err := metadataForCanonicalMutation(actor, value, "achievement", update, verifier)
	if err != nil {
		return CandidateMutationResult{}, err
	}
	value.KnowledgeMetadata = meta
	if value.ID == "" {
		value.ID, err = newKnowledgeID("achievement")
		if err != nil {
			return CandidateMutationResult{}, err
		}
	}
	if update.Source.Type != KnowledgeSourceUserConfirmed && update.Source.Type != KnowledgeSourceHHResume {
		return createCanonicalProposal(candidate, "achievement", value.ID, value, update, actor)
	}
	for i := range candidate.Achievements {
		if candidate.Achievements[i].ID == value.ID {
			old := candidate.Achievements[i]
			candidate.Achievements[i] = value
			if err := appendCandidateEvent(candidate, "update", "achievement", value.ID, old, value, update.Source.Type, string(actor)); err != nil {
				return CandidateMutationResult{}, err
			}
			return CandidateMutationResult{EntityID: value.ID}, nil
		}
	}
	if _, err := appendCanonicalClaim(candidate, "achievement", value.ID, "entity", mustJSON(value), CanonicalClaimPositive, meta, update.Source.Type == KnowledgeSourceUserConfirmed); err != nil {
		return CandidateMutationResult{}, err
	}
	candidate.Achievements = append(candidate.Achievements, value)
	if err := appendCandidateEvent(candidate, "add", "achievement", value.ID, nil, value, update.Source.Type, string(actor)); err != nil {
		return CandidateMutationResult{}, err
	}
	return CandidateMutationResult{EntityID: value.ID}, nil
}

func mutateCanonicalUnknown(candidate *Candidate, value CandidateUnknown, update KnowledgeUpdate, actor KnowledgeActor) (CandidateMutationResult, error) {
	if update.Source.Type == KnowledgeSourceUserConfirmed {
		return CandidateMutationResult{}, errors.New("an unknown cannot become a fact through an ask operation")
	}
	meta, err := metadataForCanonicalMutation(actor, value, "unknown", update, nil)
	if err != nil {
		return CandidateMutationResult{}, err
	}
	value.KnowledgeMetadata = meta
	value.Status = CandidateUnknownNeedsConfirmation
	if value.ID == "" {
		value.ID, err = newKnowledgeID("unknown")
		if err != nil {
			return CandidateMutationResult{}, err
		}
	}
	for i := range candidate.Unknowns {
		if candidate.Unknowns[i].ID == value.ID {
			old := candidate.Unknowns[i]
			candidate.Unknowns[i] = value
			if err := appendCandidateEvent(candidate, "update", "unknown", value.ID, old, value, update.Source.Type, string(actor)); err != nil {
				return CandidateMutationResult{}, err
			}
			return CandidateMutationResult{EntityID: value.ID, QuestionID: value.ID}, nil
		}
	}
	candidate.Unknowns = append(candidate.Unknowns, value)
	if err := appendCandidateEvent(candidate, "add", "unknown", value.ID, nil, value, update.Source.Type, string(actor)); err != nil {
		return CandidateMutationResult{}, err
	}
	return CandidateMutationResult{EntityID: value.ID, QuestionID: value.ID}, nil
}

func createCanonicalUnknownFromValue(candidate *Candidate, value CandidateSkillDetailed, meta KnowledgeMetadata, actor KnowledgeActor) (CandidateMutationResult, error) {
	raw := mustJSON(value)
	id, err := newKnowledgeID("unknown")
	if err != nil {
		return CandidateMutationResult{}, err
	}
	now := time.Now().UTC()
	unknown := CandidateUnknown{ID: id, Question: "Подтвердите сведения: " + value.Name, RelatedEntity: value.ID, Hypothesis: string(raw), Status: CandidateUnknownNeedsConfirmation, KnowledgeMetadata: meta}
	unknown.CreatedAt, unknown.UpdatedAt = now, now
	unknown.TruthStatus, unknown.ConfirmedAt = TruthStatusUnknown, nil
	if err := unknown.validate(); err != nil {
		return CandidateMutationResult{}, err
	}
	candidate.Unknowns = append(candidate.Unknowns, unknown)
	if err := appendCandidateEvent(candidate, "add", "unknown", id, nil, unknown, KnowledgeSourceUnknown, string(actor)); err != nil {
		return CandidateMutationResult{}, err
	}
	return CandidateMutationResult{EntityID: value.ID, QuestionID: id}, nil
}

func createCanonicalProposal(candidate *Candidate, kind, entityID string, value knowledgeFact, update KnowledgeUpdate, actor KnowledgeActor) (CandidateMutationResult, error) {
	meta := value.knowledgeMetadata()
	meta.TruthStatus, meta.ConfirmedAt = TruthStatusHypothesis, nil
	value = withKnowledgeMetadata(value, meta)
	proposed := mustJSON(value)
	base := canonicalProposalBase(candidate, kind, entityID)
	for _, p := range candidate.Proposals {
		if p.EntityType == kind && p.EntityID == entityID && p.Status == KnowledgeProposalPending {
			base = p.BaseValue
			break
		}
	}
	id, err := newKnowledgeID("proposal")
	if err != nil {
		return CandidateMutationResult{}, err
	}
	proposal := KnowledgeProposal{ID: id, EntityType: kind, EntityID: entityID, ProposedValue: json.RawMessage(proposed), Reason: update.Reason, Source: update.Source.Type, Confidence: update.Confidence, Status: KnowledgeProposalPending, CreatedAt: time.Now().UTC(), BaseValue: base}
	if err := proposal.validate(); err != nil {
		return CandidateMutationResult{}, err
	}
	if err := appendCandidateEvent(candidate, "propose", "proposal", id, nil, proposal, proposal.Source, string(actor)); err != nil {
		return CandidateMutationResult{}, err
	}
	candidate.Proposals = append(candidate.Proposals, proposal)
	return CandidateMutationResult{EntityID: entityID, ProposalID: id}, nil
}

func canonicalProposalBase(candidate *Candidate, kind, entityID string) json.RawMessage {
	var value any
	switch kind {
	case "skill":
		for _, skill := range candidate.Skills {
			if skill.ID == entityID {
				if len(skill.SourceAssertions) > 0 {
					value = CandidateSkillDetailed{ID: entityID, Name: skill.DisplayName, Category: skill.Category, Level: skill.Level, CannotClaim: skill.CannotClaim, LastUsed: skill.LastUsed, Negative: skill.Negative, KnowledgeMetadata: skill.Metadata}
				} else {
					value = CandidateSkillDetailed{ID: entityID, Name: skill.DisplayName, Level: skill.Level, Category: skill.Category, CannotClaim: skill.CannotClaim, LastUsed: skill.LastUsed, Negative: skill.Negative, KnowledgeMetadata: skill.Metadata}
				}
			}
		}
	case "project":
		for _, project := range candidate.Projects {
			if project.ID == entityID && project.DetailedSource != nil {
				value = *project.DetailedSource
			}
		}
	case "achievement":
		for _, achievement := range candidate.Achievements {
			if achievement.ID == entityID {
				value = achievement
			}
		}
	}
	if value == nil {
		return json.RawMessage("null")
	}
	return json.RawMessage(mustJSON(value))
}

func resolveCanonicalProposal(candidate *Candidate, id string, confirm bool) (CandidateMutationResult, error) {
	for i := range candidate.Proposals {
		if candidate.Proposals[i].ID != id {
			continue
		}
		proposal := candidate.Proposals[i]
		if proposal.Status != KnowledgeProposalPending {
			return CandidateMutationResult{}, errors.New("knowledge proposal is already resolved")
		}
		if !confirm {
			before := proposal
			proposal.Status = KnowledgeProposalRejected
			candidate.Proposals[i] = proposal
			return CandidateMutationResult{}, appendCandidateEvent(candidate, "reject", "proposal", id, before, proposal, KnowledgeSourceUserConfirmed, string(KnowledgeActorUser))
		}
		if expected := proposal.BaseValue; len(expected) > 0 && string(expected) != "null" {
			current := canonicalProposalBase(candidate, proposal.EntityType, proposal.EntityID)
			var left, right bytes.Buffer
			if err := json.Compact(&left, expected); err != nil {
				return CandidateMutationResult{}, err
			}
			if err := json.Compact(&right, current); err != nil {
				return CandidateMutationResult{}, err
			}
			if !bytes.Equal(left.Bytes(), right.Bytes()) {
				return CandidateMutationResult{}, errors.New("knowledge changed since proposal; create and review a new proposal")
			}
		}
		value, err := decodeKnowledgeFact(proposal.EntityType, proposal.ProposedValue)
		if err != nil {
			return CandidateMutationResult{}, err
		}
		meta := value.knowledgeMetadata()
		now := time.Now().UTC()
		meta.TruthStatus, meta.UpdatedAt, meta.ConfirmedAt = TruthStatusConfirmed, now, &now
		if proposal.Source == KnowledgeSourceHHResume || proposal.Source == KnowledgeSourceGithubVerified {
			meta.TruthStatus = TruthStatusVerified
		}
		meta.Sources = append(meta.Sources, KnowledgeSourceRecord{Type: KnowledgeSourceUserConfirmed, Evidence: []string{"user explicitly confirmed proposal " + id}, ObservedAt: &now})
		value = withKnowledgeMetadata(value, meta)
		switch v := value.(type) {
		case CandidateSkillDetailed:
			if _, err := mutateConfirmedCanonicalSkill(candidate, v, proposal, meta); err != nil {
				return CandidateMutationResult{}, err
			}
		case CandidateProject:
			if _, err := mutateConfirmedCanonicalProject(candidate, v, proposal, meta); err != nil {
				return CandidateMutationResult{}, err
			}
		case CandidateAchievement:
			if _, err := mutateConfirmedCanonicalAchievement(candidate, v, proposal, meta); err != nil {
				return CandidateMutationResult{}, err
			}
		default:
			return CandidateMutationResult{}, errors.New("unsupported proposal entity")
		}
		beforeProposal := proposal
		proposal.Status = KnowledgeProposalConfirmed
		candidate.Proposals[i] = proposal
		if err := appendCandidateEvent(candidate, "confirm", "proposal", id, beforeProposal, proposal, KnowledgeSourceUserConfirmed, string(KnowledgeActorUser)); err != nil {
			return CandidateMutationResult{}, err
		}
		return CandidateMutationResult{EntityID: proposal.EntityID}, nil
	}
	return CandidateMutationResult{}, errors.New("knowledge proposal not found")
}

func mutateConfirmedCanonicalSkill(candidate *Candidate, value CandidateSkillDetailed, proposal KnowledgeProposal, meta KnowledgeMetadata) (string, error) {
	value.ID = proposal.EntityID
	for i := range candidate.Skills {
		if candidate.Skills[i].ID == value.ID {
			candidate.Skills[i] = canonicalSkillFromDetailed(value)
			return appendCanonicalClaim(candidate, "skill", value.ID, "fact", mustJSON(value), claimPolarity(value.Negative), meta, true)
		}
	}
	candidate.Skills = append(candidate.Skills, canonicalSkillFromDetailed(value))
	return appendCanonicalClaim(candidate, "skill", value.ID, "fact", mustJSON(value), claimPolarity(value.Negative), meta, true)
}
func mutateConfirmedCanonicalProject(candidate *Candidate, value CandidateProject, proposal KnowledgeProposal, meta KnowledgeMetadata) (string, error) {
	value.ID = proposal.EntityID
	for i := range candidate.Projects {
		if candidate.Projects[i].ID == value.ID {
			oldClaim := ""
			if len(candidate.Projects[i].ClaimIDs) > 0 {
				oldClaim = candidate.Projects[i].ClaimIDs[0]
			}
			candidate.Projects[i] = canonicalProjectFromKnowledge(value, oldClaim)
			return oldClaim, nil
		}
	}
	id, err := appendCanonicalClaim(candidate, "project", value.ID, "entity", mustJSON(value), CanonicalClaimPositive, meta, true)
	if err != nil {
		return "", err
	}
	candidate.Projects = append(candidate.Projects, canonicalProjectFromKnowledge(value, id))
	return id, nil
}
func mutateConfirmedCanonicalAchievement(candidate *Candidate, value CandidateAchievement, proposal KnowledgeProposal, meta KnowledgeMetadata) (string, error) {
	value.ID = proposal.EntityID
	for i := range candidate.Achievements {
		if candidate.Achievements[i].ID == value.ID {
			candidate.Achievements[i] = value
			return value.ID, nil
		}
	}
	if _, err := appendCanonicalClaim(candidate, "achievement", value.ID, "entity", mustJSON(value), CanonicalClaimPositive, meta, true); err != nil {
		return "", err
	}
	candidate.Achievements = append(candidate.Achievements, value)
	return value.ID, nil
}

func canonicalSkillFromDetailed(value CandidateSkillDetailed) CanonicalCandidateSkill {
	return CanonicalCandidateSkill{ID: value.ID, SourceIDs: []string{value.ID}, Name: canonicalSkillName(value.Name), DisplayName: value.Name, Level: value.Level, Category: value.Category, CannotClaim: append([]string{}, value.CannotClaim...), LastUsed: value.LastUsed, Negative: value.Negative, State: CanonicalClaimActive, Metadata: value.KnowledgeMetadata, SourceAssertions: []CanonicalCandidateSkillAssertion{canonicalAssertionFromDetailed(value)}}
}
func canonicalAssertionFromDetailed(value CandidateSkillDetailed) CanonicalCandidateSkillAssertion {
	return CanonicalCandidateSkillAssertion{ID: value.ID, Name: value.Name, Detailed: true, Category: value.Category, Level: value.Level, Projects: append([]string{}, value.Projects...), CanDo: append([]string{}, value.CanDo...), CannotClaim: append([]string{}, value.CannotClaim...), LastUsed: value.LastUsed, Negative: value.Negative, Metadata: value.KnowledgeMetadata}
}
func claimPolarity(negative bool) CanonicalClaimPolarity {
	if negative {
		return CanonicalClaimNegative
	}
	return CanonicalClaimPositive
}
func mustJSON(value any) string { raw, _ := json.Marshal(value); return string(raw) }
