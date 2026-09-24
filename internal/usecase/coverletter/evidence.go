package coverletter

import (
	"fmt"
	"sort"
	"strings"

	"hh-ai-responder/internal/candidate"
	"hh-ai-responder/internal/vacancy"
)

func draftEvidence(input Input, stories []candidate.CandidateStory) []DraftEvidence {
	result := []DraftEvidence{
		{Claim: "vacancy scope", Kind: "vacancy", Reference: vacancyReference(input.Vacancy)},
		{Claim: "employer-safe candidate facts", Kind: "candidate_context", Reference: "candidate:employer_safe_context"},
	}
	if strings.TrimSpace(input.Candidate.ResumeTitle) != "" {
		result = append(result, DraftEvidence{Claim: "selected resume scope", Kind: "resume", Reference: "resume:" + strings.TrimSpace(input.Candidate.ResumeTitle)})
	}
	for _, story := range stories {
		if strings.TrimSpace(story.ID) == "" {
			continue
		}
		result = append(result, DraftEvidence{Claim: "narrative context only", Kind: "candidate_story", Reference: strings.TrimSpace(story.ID)})
	}
	return result
}

func storyIDs(stories []candidate.CandidateStory) []string {
	result := make([]string, 0, len(stories))
	seen := map[string]bool{}
	for _, story := range stories {
		id := strings.TrimSpace(story.ID)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		result = append(result, id)
	}
	sort.Strings(result)
	return result
}

func vacancyReference(value vacancy.Vacancy) string {
	if strings.TrimSpace(value.ExternalID) != "" {
		return "vacancy:" + strings.TrimSpace(value.ExternalID)
	}
	if value.ID != 0 {
		return fmt.Sprintf("vacancy:%d", value.ID)
	}
	if strings.TrimSpace(value.Name) != "" {
		return "vacancy:title:" + strings.TrimSpace(value.Name)
	}
	return "vacancy:unknown"
}
