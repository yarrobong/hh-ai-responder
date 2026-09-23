package runtime

import (
	"strings"

	"hh-ai-responder/internal/application"
	"hh-ai-responder/internal/conversation"
)

type ConversationRelationStatus string

const (
	ConversationRelationLinked      ConversationRelationStatus = "LINKED"
	ConversationRelationUnresolved  ConversationRelationStatus = "UNRESOLVED"
	ConversationRelationAmbiguous   ConversationRelationStatus = "AMBIGUOUS"
	ConversationRelationConflicting ConversationRelationStatus = "CONFLICTING"
)

type ConversationRelation struct {
	Status         ConversationRelationStatus `json:"status"`
	ApplicationID  string                     `json:"application_id,omitempty"`
	VacancyID      int                        `json:"vacancy_id"`
	Evidence       []string                   `json:"evidence,omitempty"`
	RequiresReview bool                       `json:"requires_review"`
}

// ResolveConversationRelation links only on exact stable identity. A unique
// vacancy match is deliberately insufficient because a candidate can have
// multiple applications for one vacancy over time.
func ResolveConversationRelation(value conversation.EmployerConversation, applications []application.JobApplication) ConversationRelation {
	result := ConversationRelation{Status: ConversationRelationUnresolved, VacancyID: value.VacancyID, Evidence: []string{}, RequiresReview: true}
	matches := make([]application.JobApplication, 0, 2)
	for _, candidate := range applications {
		if strings.TrimSpace(value.ApplicationID) != "" && candidate.ID == value.ApplicationID {
			matches = append(matches, candidate)
			continue
		}
		if strings.TrimSpace(candidate.ConversationID) != "" && candidate.ConversationID == value.ID {
			matches = append(matches, candidate)
			continue
		}
		if strings.TrimSpace(value.HHConversationID) != "" && candidate.HHMetadata["conversation_external_id"] == value.HHConversationID {
			matches = append(matches, candidate)
		}
	}
	if len(matches) == 1 {
		result.Status = ConversationRelationLinked
		result.ApplicationID = matches[0].ID
		result.RequiresReview = false
		result.Evidence = []string{"exact conversation/application identity"}
		if matches[0].VacancyID != value.VacancyID {
			result.Status = ConversationRelationConflicting
			result.RequiresReview = true
			result.Evidence = []string{"exact identity points to a different vacancy"}
		}
		return result
	}
	if len(matches) > 1 {
		result.Status = ConversationRelationConflicting
		result.Evidence = []string{"multiple applications claim the same conversation identity"}
		return result
	}
	vacancyMatches := 0
	for _, candidate := range applications {
		if candidate.VacancyID == value.VacancyID {
			vacancyMatches++
		}
	}
	if vacancyMatches > 1 {
		result.Status = ConversationRelationAmbiguous
		result.Evidence = []string{"multiple applications share vacancy identity without conversation identity"}
	} else {
		result.Evidence = []string{"no exact conversation/application identity"}
	}
	return result
}
