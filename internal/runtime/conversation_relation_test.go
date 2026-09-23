package runtime

import (
	"testing"

	"hh-ai-responder/internal/application"
	"hh-ai-responder/internal/conversation"
)

func TestResolveConversationRelationRequiresExactIdentity(t *testing.T) {
	base := conversation.EmployerConversation{ID: "conversation-1", HHConversationID: "hh-1", VacancyID: 42}
	tests := []struct {
		name   string
		conv   conversation.EmployerConversation
		apps   []application.JobApplication
		status ConversationRelationStatus
	}{
		{"exact provider link", base, []application.JobApplication{{ID: "app-1", VacancyID: 42, HHMetadata: map[string]string{"conversation_external_id": "hh-1"}}}, ConversationRelationLinked},
		{"unknown", base, []application.JobApplication{{ID: "app-1", VacancyID: 42}}, ConversationRelationUnresolved},
		{"ambiguous vacancy candidates", base, []application.JobApplication{{ID: "app-1", VacancyID: 42}, {ID: "app-2", VacancyID: 42}}, ConversationRelationAmbiguous},
		{"conflicting exact links", base, []application.JobApplication{{ID: "app-1", VacancyID: 42, HHMetadata: map[string]string{"conversation_external_id": "hh-1"}}, {ID: "app-2", VacancyID: 42, HHMetadata: map[string]string{"conversation_external_id": "hh-1"}}}, ConversationRelationConflicting},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveConversationRelation(tt.conv, tt.apps)
			if got.Status != tt.status {
				t.Fatalf("relation=%+v want=%s", got, tt.status)
			}
			if tt.status != ConversationRelationLinked && got.RequiresReview != true {
				t.Fatalf("unsafe relation did not require review: %+v", got)
			}
		})
	}
}
