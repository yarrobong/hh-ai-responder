package runtime

import (
	"context"
	"errors"
	"strings"
	"time"

	"hh-ai-responder/internal/usecase/candidatemutation"
)

func (u *CandidateKnowledgeUpdater) ApplyAcquisitionChoice(command CandidateAcquisitionChoiceCommand) (KnowledgeUpdateResult, error) {
	if err := u.check(); err != nil {
		return KnowledgeUpdateResult{}, err
	}
	if command.Actor != KnowledgeActorUser {
		return KnowledgeUpdateResult{}, errors.New("structured acquisition choice requires explicit user actor")
	}
	if strings.TrimSpace(command.UnknownID) == "" || strings.TrimSpace(command.Topic) == "" || strings.TrimSpace(command.Answer.ChoiceID) == "" {
		return KnowledgeUpdateResult{}, errors.New("structured acquisition choice is incomplete")
	}
	var result KnowledgeUpdateResult
	err := u.transaction(func(next *CandidateKnowledgeBase) error {
		unknown := currentKnowledgeFact(next, "unknown", command.UnknownID)
		if unknown == nil {
			return errors.New("candidate unknown not found")
		}
		currentUnknown := unknown.(CandidateUnknown)
		if currentUnknown.Status != CandidateUnknownNeedsConfirmation {
			return errors.New("candidate unknown is already resolved")
		}
		value := CandidateSkillDetailed{Name: command.Topic, Level: SkillLevelUnknown}
		if command.Answer.ChoiceID == "explicitly_not_used" {
			value.Negative = true
		} else {
			value.Uses = []CanonicalSkillUse{{ID: "use-" + command.UnknownID, Context: CanonicalSkillUsageContext(command.Answer.ChoiceID), Evidence: []string{command.Answer.Raw}}}
		}
		update := KnowledgeUpdate{Source: KnowledgeSourceRecord{Type: KnowledgeSourceUserConfirmed, Evidence: []string{"explicit candidate structured choice: " + command.Answer.ChoiceID}}, Reason: "Candidate explicitly selected clarification option", UnknownID: command.UnknownID, ClarificationID: command.ClarificationID, ConversationID: command.ConversationID, EmployerMessageID: command.EmployerMessageID}
		resolved, err := resolveKnowledgeIdentity(next, "skill", value)
		if err != nil {
			return err
		}
		value = resolved.(CandidateSkillDetailed)
		meta, err := u.metadata(value, "skill", update, time.Now().UTC())
		if err != nil {
			return err
		}
		old := currentKnowledgeFact(next, "skill", value.KnowledgeID())
		value = withKnowledgeMetadata(value, meta).(CandidateSkillDetailed)
		refs := KnowledgeEventReferences{UnknownID: command.UnknownID, ClarificationID: command.ClarificationID, ConversationID: command.ConversationID, EmployerMessageID: command.EmployerMessageID}
		if err := u.writeFactWithReferences(next, "skill", old, value, KnowledgeSourceUserConfirmed, "confirm", refs); err != nil {
			return err
		}
		if err := resolveJSONUnknownWithReferences(next, command.UnknownID, command.Answer.Raw, KnowledgeEventReferences{UnknownID: command.UnknownID, ClarificationID: command.ClarificationID, ConversationID: command.ConversationID, EmployerMessageID: command.EmployerMessageID}); err != nil {
			return err
		}
		result.EntityID = value.KnowledgeID()
		return nil
	})
	return result, err
}

func (u *CandidateKnowledgeUpdater) DismissUnknown(unknownID, evidence string, refs KnowledgeEventReferences) error {
	if err := u.check(); err != nil {
		return err
	}
	if u.options.Actor != KnowledgeActorUser {
		return errors.New("only an explicit user action may dismiss a candidate unknown")
	}
	return u.transaction(func(next *CandidateKnowledgeBase) error {
		for i := range next.Unknowns {
			unknown := &next.Unknowns[i]
			if unknown.ID != unknownID {
				continue
			}
			if unknown.Status != CandidateUnknownNeedsConfirmation {
				return errors.New("candidate unknown is already resolved")
			}
			old := *unknown
			resolved, err := candidatemutation.ResolveUnknown(*unknown, candidatemutation.UnknownResolutionInput{
				Operation: candidatemutation.DismissUnknown, Evidence: evidence, Now: time.Now().UTC(),
			})
			if err != nil {
				return err
			}
			*unknown = resolved
			if err := unknown.Validate(); err != nil {
				return err
			}
			return u.eventWithReferences(next, "dismiss", "unknown", unknown.ID, old, *unknown, KnowledgeSourceUserConfirmed, refs)
		}
		return errors.New("candidate unknown not found")
	})
}

func firstNonEmptyString(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func resolveJSONUnknown(kb *CandidateKnowledgeBase, unknownID, evidence string) error {
	return resolveJSONUnknownWithReferences(kb, unknownID, evidence, KnowledgeEventReferences{})
}

func resolveJSONUnknownWithReferences(kb *CandidateKnowledgeBase, unknownID, evidence string, refs KnowledgeEventReferences) error {
	for i := range kb.Unknowns {
		unknown := &kb.Unknowns[i]
		if unknown.ID != unknownID {
			continue
		}
		if unknown.Status != CandidateUnknownNeedsConfirmation {
			return errors.New("candidate unknown is already resolved")
		}
		resolved, err := candidatemutation.ResolveUnknown(*unknown, candidatemutation.UnknownResolutionInput{
			Operation: candidatemutation.ConfirmUnknown, Answer: evidence, Evidence: evidence, Now: time.Now().UTC(),
		})
		if err != nil {
			return err
		}
		*unknown = resolved
		if err := unknown.Validate(); err != nil {
			return err
		}
		return appendJSONKnowledgeEvent(kb, "resolve", "unknown", unknown.ID, nil, *unknown, KnowledgeSourceUserConfirmed, string(KnowledgeActorUser), refs)
	}
	return errors.New("candidate unknown not found")
}

type CandidateAcquisitionChoiceCommand struct {
	Actor             KnowledgeActor
	Candidate         Candidate
	UnknownID         string
	ClarificationID   string
	Answer            CandidateAnswer
	Topic             string
	ConversationID    string
	EmployerMessageID string
}

func (s *CandidateMutationService) ApplyAcquisitionChoice(ctx context.Context, command CandidateAcquisitionChoiceCommand) (CandidateMutationResult, error) {
	if command.Actor != KnowledgeActorUser {
		return CandidateMutationResult{}, errors.New("structured acquisition choice requires explicit user actor")
	}
	if command.Answer.ChoiceID == "" {
		return CandidateMutationResult{}, errors.New("structured acquisition choice is required")
	}
	if s == nil {
		return CandidateMutationResult{}, errors.New("candidate mutation service is not configured")
	}
	if s.backend == storageBackendJSON {
		kb, err := s.loadJSON()
		if err != nil {
			return CandidateMutationResult{}, err
		}
		updater := NewCandidateKnowledgeUpdater(kb, KnowledgeUpdaterOptions{Actor: KnowledgeActorUser, VerifyGitHub: s.verifyGitHub})
		var result KnowledgeUpdateResult
		err = updater.transaction(func(next *CandidateKnowledgeBase) error {
			value := CandidateSkillDetailed{Name: command.Topic, Level: SkillLevelUnknown}
			if command.Answer.ChoiceID == "explicitly_not_used" {
				value.Level, value.Negative = SkillLevelUnknown, true
			} else {
				value.Uses = []CanonicalSkillUse{{ID: "use-" + command.UnknownID, Context: CanonicalSkillUsageContext(command.Answer.ChoiceID), Evidence: []string{command.Answer.Raw}}}
			}
			update := KnowledgeUpdate{Source: KnowledgeSourceRecord{Type: KnowledgeSourceUserConfirmed, Evidence: []string{"explicit candidate structured choice: " + command.Answer.ChoiceID}}, Reason: "Candidate explicitly selected clarification option", UnknownID: command.UnknownID, ClarificationID: command.ClarificationID, ConversationID: command.ConversationID, EmployerMessageID: command.EmployerMessageID}
			resolved, err := resolveKnowledgeIdentity(next, "skill", value)
			if err != nil {
				return err
			}
			value = resolved.(CandidateSkillDetailed)
			meta, err := updater.metadata(value, "skill", update, time.Now().UTC())
			if err != nil {
				return err
			}
			old := currentKnowledgeFact(next, "skill", value.KnowledgeID())
			value = withKnowledgeMetadata(value, meta).(CandidateSkillDetailed)
			if err := updater.writeFactWithReferences(next, "skill", old, value, KnowledgeSourceUserConfirmed, "confirm", KnowledgeEventReferences{UnknownID: command.UnknownID, ClarificationID: command.ClarificationID, ConversationID: command.ConversationID, EmployerMessageID: command.EmployerMessageID}); err != nil {
				return err
			}
			result.EntityID = value.KnowledgeID()
			unknownIndex := -1
			for i := range next.Unknowns {
				if next.Unknowns[i].ID == command.UnknownID {
					unknownIndex = i
					break
				}
			}
			if unknownIndex < 0 {
				return errors.New("candidate unknown not found")
			}
			unknown := next.Unknowns[unknownIndex]
			if unknown.Status != CandidateUnknownNeedsConfirmation {
				return errors.New("candidate unknown is already resolved")
			}
			oldUnknown := unknown
			resolvedUnknown, err := candidatemutation.ResolveUnknown(unknown, candidatemutation.UnknownResolutionInput{
				Operation: candidatemutation.ConfirmUnknown, Answer: command.Answer.Raw, Evidence: command.Answer.Raw, Now: time.Now().UTC(),
			})
			if err != nil {
				return err
			}
			unknown = resolvedUnknown
			if err := unknown.Validate(); err != nil {
				return err
			}
			next.Unknowns[unknownIndex] = unknown
			if err := updater.eventWithReferences(next, "resolve", "unknown", unknown.ID, oldUnknown, unknown, KnowledgeSourceUserConfirmed, KnowledgeEventReferences{UnknownID: command.UnknownID, ClarificationID: command.ClarificationID, ConversationID: command.ConversationID, EmployerMessageID: command.EmployerMessageID}); err != nil {
				return err
			}
			return nil
		})
		if err != nil {
			return CandidateMutationResult{}, err
		}
		if err := kb.Save(); err != nil {
			return CandidateMutationResult{}, err
		}
		return CandidateMutationResult{EntityID: result.EntityID}, nil
	}
	result, err := s.mutatePostgres(ctx, command.Actor, func(candidate *Candidate) (CandidateMutationResult, error) {
		for _, current := range candidate.Unknowns {
			if current.ID == command.UnknownID && current.Status != CandidateUnknownNeedsConfirmation {
				return CandidateMutationResult{}, errors.New("candidate unknown is already resolved")
			}
		}
		value := CandidateSkillDetailed{Name: command.Topic, Level: SkillLevelUnknown}
		if command.Answer.ChoiceID == "explicitly_not_used" {
			value.Level, value.Negative = SkillLevelUnknown, true
		}
		if command.Answer.ChoiceID != "explicitly_not_used" {
			value.Uses = []CanonicalSkillUse{{ID: "use-" + command.UnknownID, Context: CanonicalSkillUsageContext(command.Answer.ChoiceID), Evidence: []string{command.Answer.Raw}}}
		}
		update := KnowledgeUpdate{Source: KnowledgeSourceRecord{Type: KnowledgeSourceUserConfirmed, Evidence: []string{"explicit candidate structured choice: " + command.Answer.ChoiceID}}, Reason: "Candidate explicitly selected clarification option", UnknownID: command.UnknownID, ClarificationID: command.ClarificationID, ConversationID: command.ConversationID, EmployerMessageID: command.EmployerMessageID}
		mutated, err := mutateCanonicalSkill(candidate, value, update, command.Actor, s.verifyGitHub)
		if err != nil {
			return CandidateMutationResult{}, err
		}
		if err := resolveCanonicalUnknown(candidate, command.UnknownID, command.Answer.Raw, command.ClarificationID, KnowledgeEventReferences{UnknownID: command.UnknownID, ClarificationID: command.ClarificationID, ConversationID: command.ConversationID, EmployerMessageID: command.EmployerMessageID}); err != nil {
			return CandidateMutationResult{}, err
		}
		if err := appendCandidateEventWithReferences(candidate, "acquisition_confirmed", "skill", mutated.EntityID, nil, value, KnowledgeSourceUserConfirmed, string(command.Actor), KnowledgeEventReferences{UnknownID: command.UnknownID, ClarificationID: command.ClarificationID, ConversationID: command.ConversationID, EmployerMessageID: command.EmployerMessageID}); err != nil {
			return CandidateMutationResult{}, err
		}
		return mutated, nil
	})
	return result, err
}

func (s *CandidateMutationService) DismissUnknown(ctx context.Context, unknownID, evidence string, refs KnowledgeEventReferences) error {
	if s == nil {
		return errors.New("candidate mutation service is not configured")
	}
	if s.backend == storageBackendJSON {
		kb, err := s.loadJSON()
		if err != nil {
			return err
		}
		updater := NewCandidateKnowledgeUpdater(kb, KnowledgeUpdaterOptions{Actor: KnowledgeActorUser, VerifyGitHub: s.verifyGitHub})
		if err := updater.DismissUnknown(unknownID, evidence, refs); err != nil {
			return err
		}
		return kb.Save()
	}
	_, err := s.mutatePostgres(ctx, KnowledgeActorUser, func(candidate *Candidate) (CandidateMutationResult, error) {
		for i := range candidate.Unknowns {
			unknown := &candidate.Unknowns[i]
			if unknown.ID != unknownID {
				continue
			}
			if unknown.Status != CandidateUnknownNeedsConfirmation {
				return CandidateMutationResult{}, errors.New("candidate unknown is already resolved")
			}
			old := *unknown
			resolved, err := candidatemutation.ResolveUnknown(*unknown, candidatemutation.UnknownResolutionInput{
				Operation: candidatemutation.DismissUnknown, Evidence: evidence, Now: time.Now().UTC(),
			})
			if err != nil {
				return CandidateMutationResult{}, err
			}
			*unknown = resolved
			if err := unknown.Validate(); err != nil {
				return CandidateMutationResult{}, err
			}
			if err := appendCandidateEventWithReferences(candidate, "dismiss", "unknown", unknown.ID, old, *unknown, KnowledgeSourceUserConfirmed, string(KnowledgeActorUser), refs); err != nil {
				return CandidateMutationResult{}, err
			}
			return CandidateMutationResult{EntityID: unknown.ID}, nil
		}
		return CandidateMutationResult{}, errors.New("candidate unknown not found")
	})
	return err
}
