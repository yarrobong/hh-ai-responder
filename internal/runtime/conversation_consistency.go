package runtime

import (
	"errors"

	domaincandidate "hh-ai-responder/internal/candidate"
	"hh-ai-responder/internal/usecase/conversationpolicy"
)

type ConversationConsistencyWarning = conversationpolicy.ConsistencyWarning

// conversationConsistencyWarnings is a root compatibility adapter. The
// deterministic comparison is implemented in conversationpolicy; the root
// resolver only supplies the already-prepared safe candidate inputs that its
// legacy API can provide.
func conversationConsistencyWarnings(claims []CandidateConversationClaim, safe EmployerSafeKnowledge, resolver *CandidateContextResolver) ([]ConversationConsistencyWarning, error) {
	if resolver == nil {
		return nil, errors.New("candidate context resolver is required")
	}
	input := conversationpolicy.ConsistencyInput{
		Claims: claims,
		Candidate: domaincandidate.EmployerSafeCandidateKnowledge{
			Skills: safe.Skills, Projects: safe.Projects, Achievements: safe.Achievements,
		},
	}
	for _, claim := range claims {
		context, err := resolver.GetEmployerSafeContext(claim.Text)
		if err != nil {
			return nil, err
		}
		input.ForbiddenClaims = append(input.ForbiddenClaims, context.ForbiddenClaims...)
	}
	if total, known, err := resolver.canonicalTotalExperience(); err != nil {
		return nil, errors.New("invalid trusted total experience; conversation context withheld")
	} else if known {
		value := total.Value
		input.TotalExperienceMonths = &value
	}
	return conversationpolicy.CheckConsistency(input), nil
}
