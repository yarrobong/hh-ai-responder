package runtime

import domaincandidate "hh-ai-responder/internal/candidate"

// BuildCanonicalCandidate is the root compatibility entry point. The
// deterministic assembly implementation lives in internal/candidate so JSON
// adapters and other readers use the same source-priority policy.
func BuildCanonicalCandidate(input CanonicalCandidateInput) (Candidate, CanonicalCandidateDiagnostics, error) {
	domainInput := domaincandidate.CanonicalCandidateInput{
		Profile: input.Profile,
		Knowledge: domaincandidate.KnowledgeSnapshot{
			Skills: input.Knowledge.Skills, Projects: input.Knowledge.Projects,
			Achievements: input.Knowledge.Achievements, Unknowns: input.Knowledge.Unknowns,
			Proposals: input.Knowledge.Proposals, Events: input.Knowledge.Events,
		},
		Stories: input.Stories, ResumeFacts: input.ResumeFacts,
		CandidateID: input.CandidateID, Contacts: input.Contacts, GitHubURL: input.GitHubURL,
	}
	return domaincandidate.BuildCanonicalCandidate(domainInput)
}

func CanonicalEmployerSafeProjection(value Candidate) (EmployerSafeCandidateKnowledge, error) {
	return domaincandidate.CanonicalEmployerSafeProjection(value)
}

func safeMetadata(metadata KnowledgeMetadata) bool {
	return domaincandidate.CanExposeToEmployer(metadata)
}

func safeProfileFact(fact ProfileFact) bool {
	return domaincandidate.CanExposeProfileFact(fact)
}

func canonicalProfileFact(metadata KnowledgeMetadata) ProfileFact {
	return domaincandidate.ProfileFactFromMetadata(metadata)
}

func parseResumeSkillNames(value string) []string {
	return domaincandidate.ParseResumeSkillNames(value)
}

func sortCanonicalCandidate(value *Candidate) {
	domaincandidate.SortCanonicalCandidate(value)
}

func canonicalProjectFromKnowledge(value CandidateProject, claimID string) CanonicalCandidateProject {
	return domaincandidate.CanonicalProjectFromKnowledge(value, claimID)
}

func appendUniqueString(values []string, value string) []string {
	return domaincandidate.AppendUniqueString(values, value)
}

func firstString(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}
