package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"reflect"
	"strings"
	"time"
)

// Actor and verifier are selected by trusted application code, never decoded
// from a model response. An AI adapter must always use KnowledgeActorAI.
type KnowledgeActor string

const (
	KnowledgeActorUser     KnowledgeActor = "user"
	KnowledgeActorImporter KnowledgeActor = "importer"
	KnowledgeActorAI       KnowledgeActor = "ai"
)

// VerifyGitHub must check real, candidate-attributable repository evidence for
// the entire assertion (including level/role), not just the existence of a URL.
// No network integration is provided at this stage; a missing verifier fails closed.
type KnowledgeUpdaterOptions struct {
	Actor        KnowledgeActor
	VerifyGitHub func(source KnowledgeSourceRecord, entityType string, value json.RawMessage) error
}

type KnowledgeUpdate struct {
	Source     KnowledgeSourceRecord
	Reason     string
	Confidence *float64
}

type KnowledgeUpdateResult struct {
	EntityID   string
	ProposalID string
	QuestionID string
}

// Updates are atomic in memory and record events. Call kb.Save explicitly to
// persist, as with migration. Access to kb must be serialized by the caller.
type CandidateKnowledgeUpdater struct {
	kb      *CandidateKnowledgeBase
	options KnowledgeUpdaterOptions
}

func NewCandidateKnowledgeUpdater(kb *CandidateKnowledgeBase, options KnowledgeUpdaterOptions) *CandidateKnowledgeUpdater {
	return &CandidateKnowledgeUpdater{kb: kb, options: options}
}

func (u *CandidateKnowledgeUpdater) check() error {
	if u == nil || u.kb == nil {
		return errors.New("knowledge updater requires a database")
	}
	switch u.options.Actor {
	case KnowledgeActorUser, KnowledgeActorImporter, KnowledgeActorAI:
		return nil
	default:
		return errors.New("knowledge updater requires an explicit trusted caller identity")
	}
}

func (u *CandidateKnowledgeUpdater) transaction(fn func(*CandidateKnowledgeBase) error) error {
	if err := u.check(); err != nil {
		return err
	}
	next, err := cloneKnowledge(*u.kb)
	if err != nil {
		return err
	}
	if err := fn(&next); err != nil {
		return err
	}
	*u.kb = next
	return nil
}

func (u *CandidateKnowledgeUpdater) UpdateSkill(value CandidateSkillDetailed, update KnowledgeUpdate) (KnowledgeUpdateResult, error) {
	if value.Level == "" {
		value.Level = SkillLevelUnknown
	}
	return u.update("skill", value, update)
}
func (u *CandidateKnowledgeUpdater) UpdateProject(value CandidateProject, update KnowledgeUpdate) (KnowledgeUpdateResult, error) {
	return u.update("project", value, update)
}
func (u *CandidateKnowledgeUpdater) UpdateAchievement(value CandidateAchievement, update KnowledgeUpdate) (KnowledgeUpdateResult, error) {
	return u.update("achievement", value, update)
}
func (u *CandidateKnowledgeUpdater) UpdateUnknown(value CandidateUnknown, update KnowledgeUpdate) (KnowledgeUpdateResult, error) {
	if value.Status != "" && value.Status != CandidateUnknownNeedsConfirmation {
		return KnowledgeUpdateResult{}, errors.New("updates cannot resolve questions implicitly")
	}
	value.Status = CandidateUnknownNeedsConfirmation
	return u.update("unknown", value, update)
}

func validGitHubReference(reference string) bool {
	parsed, err := url.Parse(reference)
	if err != nil || parsed.Scheme != "https" || parsed.Host != "github.com" || parsed.User != nil {
		return false
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	return len(parts) >= 2 && parts[0] != "" && parts[1] != ""
}

func (u *CandidateKnowledgeUpdater) metadata(value knowledgeFact, kind string, update KnowledgeUpdate, now time.Time) (KnowledgeMetadata, error) {
	if !reflect.DeepEqual(value.knowledgeMetadata(), KnowledgeMetadata{}) {
		return KnowledgeMetadata{}, errors.New("update payload cannot supply truth status, confirmation, sources or timestamps; use KnowledgeUpdate")
	}
	return knowledgeMutationMetadata(u.options.Actor, u.options.VerifyGitHub, value, kind, update, now)
}

// knowledgeMutationMetadata is shared by the JSON compatibility updater and
// the PostgreSQL candidate mutation service. Keeping provenance and truth
// promotion in one pure helper prevents the two backends from acquiring
// different confirmation rules.
func knowledgeMutationMetadata(actor KnowledgeActor, verifyGitHub func(source KnowledgeSourceRecord, entityType string, value json.RawMessage) error, value knowledgeFact, kind string, update KnowledgeUpdate, now time.Time) (KnowledgeMetadata, error) {
	if !validKnowledgeSource(update.Source.Type) || strings.TrimSpace(update.Reason) == "" {
		return KnowledgeMetadata{}, errors.New("update requires a valid source and reason")
	}
	meta := KnowledgeMetadata{Confidence: update.Confidence, TruthStatus: TruthStatusHypothesis,
		Sources: []KnowledgeSourceRecord{update.Source}, Evidence: update.Source.Evidence, CreatedAt: now, UpdatedAt: now}
	switch update.Source.Type {
	case KnowledgeSourceUserConfirmed:
		if actor != KnowledgeActorUser {
			return meta, errors.New("only an explicit user action may create user_confirmed knowledge")
		}
		meta.TruthStatus, meta.ConfirmedAt = TruthStatusConfirmed, &now
	case KnowledgeSourceHHResume:
		if actor == KnowledgeActorAI {
			return meta, errors.New("HH provenance requires a trusted resume importer")
		}
		if !hasKnowledgeEvidence(update.Source.Evidence) {
			return meta, errors.New("HH resume source requires evidence")
		}
		meta.TruthStatus = TruthStatusVerified
	case KnowledgeSourceGithubVerified:
		if !validGitHubReference(update.Source.Reference) || !hasKnowledgeEvidence(update.Source.Evidence) || verifyGitHub == nil {
			return meta, errors.New("GitHub source requires reference, evidence and a trusted verifier")
		}
		raw, err := json.Marshal(value)
		if err != nil {
			return meta, err
		}
		record, err := cloneKnowledge(update.Source)
		if err != nil {
			return meta, err
		}
		if err := verifyGitHub(record, kind, raw); err != nil {
			return meta, errors.New("GitHub evidence verification failed")
		}
	case KnowledgeSourceUnknown:
		meta.TruthStatus = TruthStatusUnknown
	}
	// Questions never assert facts, even if the question came from a trusted user.
	if kind == "unknown" {
		meta.TruthStatus, meta.ConfirmedAt = TruthStatusUnknown, nil
	}
	return meta, meta.validate()
}

func (u *CandidateKnowledgeUpdater) update(kind string, value knowledgeFact, update KnowledgeUpdate) (KnowledgeUpdateResult, error) {
	var result KnowledgeUpdateResult
	err := u.transaction(func(kb *CandidateKnowledgeBase) error {
		now := time.Now().UTC()
		meta, err := u.metadata(value, kind, update, now)
		if err != nil {
			return err
		}
		value, err = resolveKnowledgeIdentity(kb, kind, value)
		if err != nil {
			return err
		}
		old := currentKnowledgeFact(kb, kind, value.knowledgeID())
		if old != nil {
			meta.CreatedAt = old.knowledgeMetadata().CreatedAt
		}
		value = withKnowledgeMetadata(value, meta)
		if err := validateKnowledgeValue(value); err != nil {
			return err
		}
		result.EntityID = value.knowledgeID()
		if kind == "unknown" {
			result.QuestionID = value.knowledgeID()
			return u.writeFact(kb, kind, old, value, update.Source.Type, "")
		}
		if update.Source.Type == KnowledgeSourceUnknown {
			raw, err := json.Marshal(value)
			if err != nil {
				return err
			}
			questionID, err := newKnowledgeID("unknown")
			if err != nil {
				return err
			}
			question := CandidateUnknown{ID: questionID, Question: "Подтвердите сведения: " + knowledgeLabel(value),
				RelatedEntity: value.knowledgeID(), Hypothesis: string(raw), Status: CandidateUnknownNeedsConfirmation,
				KnowledgeMetadata: meta}
			question.CreatedAt = now
			result.QuestionID = questionID
			return u.writeFact(kb, "unknown", nil, question, update.Source.Type, "")
		}
		direct := update.Source.Type == KnowledgeSourceUserConfirmed || update.Source.Type == KnowledgeSourceHHResume
		if update.Source.Type != KnowledgeSourceUserConfirmed && old != nil && old.knowledgeMetadata().TruthStatus == TruthStatusConfirmed {
			direct = false
		}
		levelIncrease := false
		if kind == "skill" && update.Source.Type != KnowledgeSourceUserConfirmed {
			previous := SkillLevelUnknown
			// A hypothesis cannot serve as proof of an already established level.
			if old != nil && (old.knowledgeMetadata().TruthStatus == TruthStatusConfirmed || old.knowledgeMetadata().TruthStatus == TruthStatusVerified) {
				previous = old.(CandidateSkillDetailed).Level
			}
			if knowledgeSkillRank(value.(CandidateSkillDetailed).Level) > knowledgeSkillRank(previous) {
				direct = false
				levelIncrease = true
			}
		}
		if direct {
			return u.writeFact(kb, kind, old, value, update.Source.Type, "")
		}
		// Staged values can never grant truth, including verified-source proposals.
		meta.TruthStatus, meta.ConfirmedAt = TruthStatusHypothesis, nil
		value = withKnowledgeMetadata(value, meta)
		if (update.Source.Type == KnowledgeSourceDerived || update.Source.Type == KnowledgeSourceProjectAnalysis) &&
			(old == nil || (!levelIncrease && (old.knowledgeMetadata().TruthStatus == TruthStatusHypothesis || old.knowledgeMetadata().TruthStatus == TruthStatusUnknown))) {
			if err := u.writeFact(kb, kind, old, value, update.Source.Type, ""); err != nil {
				return err
			}
			old = currentKnowledgeFact(kb, kind, value.knowledgeID())
		}
		proposalID, err := newKnowledgeID("proposal")
		if err != nil {
			return err
		}
		proposed, err := json.Marshal(value)
		if err != nil {
			return err
		}
		base, err := json.Marshal(old)
		if err != nil {
			return err
		}
		proposal := KnowledgeProposal{ID: proposalID, EntityType: kind, EntityID: value.knowledgeID(), ProposedValue: proposed,
			Reason: update.Reason, Source: update.Source.Type, Confidence: update.Confidence,
			Status: KnowledgeProposalPending, CreatedAt: now, BaseValue: base}
		if err := validateKnowledgeValue(proposal); err != nil {
			return err
		}
		proposal, err = cloneKnowledge(proposal)
		if err != nil {
			return err
		}
		if err := u.event(kb, "propose", "proposal", proposal.ID, nil, proposal, proposal.Source); err != nil {
			return err
		}
		kb.Proposals = append(kb.Proposals, proposal)
		result.ProposalID = proposalID
		return nil
	})
	if err != nil {
		return KnowledgeUpdateResult{}, err
	}
	return result, nil
}

func knowledgeSkillRank(level SkillLevel) int {
	switch level {
	case SkillLevelHeardOf:
		return 1
	case SkillLevelBasic:
		return 2
	case SkillLevelWorking:
		return 3
	case SkillLevelConfident:
		return 4
	case SkillLevelAdvanced:
		return 5
	default:
		return 0
	}
}

func knowledgeLabel(value knowledgeFact) string {
	switch v := value.(type) {
	case CandidateSkillDetailed:
		return canonicalSkillName(v.Name)
	case CandidateProject:
		return normalizeProfileName(firstNonEmpty(v.Name, v.Description))
	case CandidateAchievement:
		return normalizeProfileName(v.Title)
	case CandidateUnknown:
		return normalizeProfileName(v.Question)
	default:
		return ""
	}
}

func knowledgeFacts(kb *CandidateKnowledgeBase, kind string) []knowledgeFact {
	var values []knowledgeFact
	switch kind {
	case "skill":
		for _, v := range kb.Skills {
			values = append(values, v)
		}
	case "project":
		for _, v := range kb.Projects {
			values = append(values, v)
		}
	case "achievement":
		for _, v := range kb.Achievements {
			values = append(values, v)
		}
	case "unknown":
		for _, v := range kb.Unknowns {
			values = append(values, v)
		}
	}
	return values
}

func currentKnowledgeFact(kb *CandidateKnowledgeBase, kind, id string) knowledgeFact {
	for _, v := range knowledgeFacts(kb, kind) {
		if v.knowledgeID() == id {
			return v
		}
	}
	return nil
}

// Match natural identities too: omitting/changing an ID must not bypass the
// skill-level guard or duplicate migrated skills. Conflicting legacy assertions
// require an explicit ID rather than choosing one arbitrarily.
func resolveKnowledgeIdentity(kb *CandidateKnowledgeBase, kind string, value knowledgeFact) (knowledgeFact, error) {
	id := value.knowledgeID()
	var matches []string
	for _, old := range knowledgeFacts(kb, kind) {
		if knowledgeLabel(old) == knowledgeLabel(value) {
			matches = append(matches, old.knowledgeID())
		}
		if kind == "skill" && old.knowledgeID() == id && knowledgeLabel(old) != knowledgeLabel(value) {
			return nil, errors.New("cannot rename a skill identity; submit a separate assertion")
		}
	}
	if id == "" {
		if len(matches) > 1 {
			return nil, errors.New("ambiguous knowledge identity; specify an existing entity ID")
		}
		if len(matches) == 1 {
			id = matches[0]
		}
	} else if currentKnowledgeFact(kb, kind, id) == nil && len(matches) > 0 {
		return nil, errors.New("knowledge name already exists; use its entity ID")
	}
	if id == "" {
		var err error
		id, err = newKnowledgeID(kind)
		if err != nil {
			return nil, err
		}
	}
	switch v := value.(type) {
	case CandidateSkillDetailed:
		v.ID = id
		return v, nil
	case CandidateProject:
		v.ID = id
		return v, nil
	case CandidateAchievement:
		v.ID = id
		return v, nil
	case CandidateUnknown:
		v.ID = id
		return v, nil
	}
	return nil, errors.New("unsupported knowledge entity")
}

func replaceKnowledgeFact[T knowledgeEntity](values *[]T, value T) {
	for i := range *values {
		if (*values)[i].knowledgeID() == value.knowledgeID() {
			(*values)[i] = value
			return
		}
	}
	*values = append(*values, value)
}

func (u *CandidateKnowledgeUpdater) writeFact(kb *CandidateKnowledgeBase, kind string, old, value knowledgeFact, source KnowledgeSource, action string) error {
	if err := validateKnowledgeValue(value); err != nil {
		return err
	}
	if action == "" {
		action = "add"
		if old != nil {
			action = "update"
		}
	}
	if err := u.event(kb, action, kind, value.knowledgeID(), old, value, source); err != nil {
		return err
	}
	// Marshal cloning also owns incoming slices, confidence and source pointers.
	switch v := value.(type) {
	case CandidateSkillDetailed:
		copy, err := cloneKnowledge(v)
		if err != nil {
			return err
		}
		replaceKnowledgeFact(&kb.Skills, copy)
	case CandidateProject:
		copy, err := cloneKnowledge(v)
		if err != nil {
			return err
		}
		replaceKnowledgeFact(&kb.Projects, copy)
	case CandidateAchievement:
		copy, err := cloneKnowledge(v)
		if err != nil {
			return err
		}
		replaceKnowledgeFact(&kb.Achievements, copy)
	case CandidateUnknown:
		copy, err := cloneKnowledge(v)
		if err != nil {
			return err
		}
		replaceKnowledgeFact(&kb.Unknowns, copy)
	}
	return nil
}

func (u *CandidateKnowledgeUpdater) event(kb *CandidateKnowledgeBase, action, kind, id string, old, value any, source KnowledgeSource) error {
	before, err := json.Marshal(old)
	if err != nil {
		return err
	}
	after, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return kb.AddEvent(CandidateKnowledgeEvent{Action: action, EntityType: kind, EntityID: id,
		OldValue: before, NewValue: after, Source: source, Actor: string(u.options.Actor)})
}

// Only a trusted user action may resolve proposals. The proposal ID identifies
// the complete value the user reviewed; stale or already resolved IDs fail closed.
func (u *CandidateKnowledgeUpdater) ConfirmKnowledge(id string) error { return u.resolve(id, true) }
func (u *CandidateKnowledgeUpdater) RejectKnowledge(id string) error  { return u.resolve(id, false) }

func (u *CandidateKnowledgeUpdater) resolve(id string, confirm bool) error {
	return u.transaction(func(kb *CandidateKnowledgeBase) error {
		if u.options.Actor != KnowledgeActorUser {
			return errors.New("only an explicit user action may confirm or reject knowledge")
		}
		index := -1
		for i, proposal := range kb.Proposals {
			if proposal.ID == id {
				index = i
				break
			}
		}
		if index < 0 {
			return errors.New("knowledge proposal not found")
		}
		before := kb.Proposals[index]
		if err := validateKnowledgeValue(before); err != nil {
			return err
		}
		if before.Status != KnowledgeProposalPending {
			return errors.New("knowledge proposal is already resolved")
		}
		after := before
		action := "reject"
		after.Status = KnowledgeProposalRejected
		if confirm {
			value, err := decodeKnowledgeFact(before.EntityType, before.ProposedValue)
			if err != nil {
				return err
			}
			old := currentKnowledgeFact(kb, before.EntityType, before.EntityID)
			raw, err := json.Marshal(old)
			if err != nil {
				return err
			}
			expected := before.BaseValue
			if len(expected) == 0 {
				expected = json.RawMessage("null")
			}
			var compact bytes.Buffer
			if err := json.Compact(&compact, expected); err != nil {
				return err
			}
			if !bytes.Equal(raw, compact.Bytes()) {
				return errors.New("knowledge changed since proposal; create and review a new proposal")
			}
			// A second proposal may have created the same natural identity under another ID.
			if _, err := resolveKnowledgeIdentity(kb, before.EntityType, value); err != nil {
				return err
			}
			now := time.Now().UTC()
			meta := value.knowledgeMetadata()
			meta.UpdatedAt, meta.ConfirmedAt = now, &now
			meta.TruthStatus = TruthStatusConfirmed
			if before.Source == KnowledgeSourceGithubVerified || before.Source == KnowledgeSourceHHResume {
				meta.TruthStatus = TruthStatusVerified
			}
			meta.Sources = append(meta.Sources, KnowledgeSourceRecord{Type: KnowledgeSourceUserConfirmed,
				Evidence: []string{fmt.Sprintf("user explicitly confirmed proposal %s", before.ID)}, ObservedAt: &now})
			value = withKnowledgeMetadata(value, meta)
			if err := u.writeFact(kb, before.EntityType, old, value, KnowledgeSourceUserConfirmed, "confirm"); err != nil {
				return err
			}
			after.Status, action = KnowledgeProposalConfirmed, "confirm"
		}
		if err := u.event(kb, action, "proposal", before.ID, before, after, KnowledgeSourceUserConfirmed); err != nil {
			return err
		}
		kb.Proposals[index] = after
		return nil
	})
}
