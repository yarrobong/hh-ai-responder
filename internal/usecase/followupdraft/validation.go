package followupdraft

import (
	"errors"
	"strings"

	"hh-ai-responder/internal/usecase/conversationpolicy"
	employerreply "hh-ai-responder/internal/usecase/employerreply"
)

func validateGeneratedDraft(value employerreply.Decision, context employerreply.Context) error {
	if value.Action != employerreply.ActionDraftReply {
		return nil
	}
	if conversationpolicy.HighRiskMessageReason(value.Draft) != "" {
		return errors.New("generated follow-up contains a high-risk topic")
	}
	if len([]rune(value.Draft)) > 600 {
		return errors.New("generated follow-up is too long")
	}
	if len(value.Warnings) > 0 {
		return errors.New("generated follow-up contains warnings")
	}
	if err := employerreply.ValidateUsedFacts(value.UsedFacts, context.CandidateContext); err != nil {
		return err
	}
	if err := employerreply.ValidateDraft(value.Draft, context.CandidateContext); err != nil {
		return err
	}
	return nil
}

// ValidateSavedDraft rechecks a previously generated follow-up against the
// current safe Candidate context before the root workflow reuses it.
func ValidateSavedDraft(text string, usedFacts []string, context employerreply.Context) error {
	if err := employerreply.ValidateUsedFacts(usedFacts, context.CandidateContext); err != nil {
		return err
	}
	return employerreply.ValidateDraft(text, context.CandidateContext)
}

func manualReview(reason string, warnings []string, topics []string) employerreply.Decision {
	if strings.TrimSpace(reason) == "" {
		reason = "Follow-up draft requires review"
	}
	return employerreply.Decision{
		Action:                 employerreply.ActionManualReview,
		Reason:                 reason,
		Confidence:             1,
		UsedFacts:              []string{},
		MissingInformation:     []employerreply.MissingInformation{},
		ForbiddenClaimsChecked: true,
		ConversationTopicsUsed: uniqueStrings(topics),
		Warnings:               uniqueStrings(warnings),
	}
}

func uniqueStrings(values []string) []string {
	result := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	return result
}
