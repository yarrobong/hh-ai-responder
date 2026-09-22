package runtime

import "hh-ai-responder/internal/careeragent"

// careerReviewUnavailableMessage keeps workflow storage failures explicit and
// redacted while the legacy dashboard projections continue to render.
func careerReviewUnavailableMessage(prefix string, err error) string {
	return prefix + ": " + careeragent.RedactAgentError(err)
}
