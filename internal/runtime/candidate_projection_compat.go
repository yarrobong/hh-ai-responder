package runtime

import domaincandidate "hh-ai-responder/internal/candidate"

// Transitional compatibility adapters. Pure projection ownership lives in
// internal/candidate; these names remain for root consumers.
func canonicalSafeProfile(value Candidate) EmployerSafeProfileKnowledge {
	return domaincandidate.SafeProfile(value)
}
