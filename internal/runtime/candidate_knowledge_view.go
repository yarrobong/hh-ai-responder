package runtime

import domaincandidate "hh-ai-responder/internal/candidate"

// EmployerSafeKnowledge deliberately contains no legacy profile, questions,
// proposals or audit history. Its values are detached from storage.
type EmployerSafeKnowledge struct {
	Skills       []CandidateSkillDetailed `json:"skills"`
	Projects     []CandidateProject       `json:"projects"`
	Achievements []CandidateAchievement   `json:"achievements"`
}

func employerSafeFacts[T knowledgeFact](values []T) ([]T, error) {
	safe := make([]T, 0, len(values))
	for _, value := range values {
		if err := validateKnowledgeValue(value); err != nil {
			return nil, err
		}
		if domaincandidate.CanExposeToEmployer(value.KnowledgeMetadataValue()) {
			safe = append(safe, value)
		}
	}
	return cloneKnowledge(safe)
}

func (kb *CandidateKnowledgeBase) GetEmployerSafeKnowledge() (EmployerSafeKnowledge, error) {
	var view EmployerSafeKnowledge
	var err error
	if view.Skills, err = employerSafeFacts(kb.Skills); err != nil {
		return EmployerSafeKnowledge{}, err
	}
	if view.Projects, err = employerSafeFacts(kb.Projects); err != nil {
		return EmployerSafeKnowledge{}, err
	}
	if view.Achievements, err = employerSafeFacts(kb.Achievements); err != nil {
		return EmployerSafeKnowledge{}, err
	}
	return view, nil
}
