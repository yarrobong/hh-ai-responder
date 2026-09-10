package conversation

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

func TestEmployerConversationJSONContractRoundTrip(t *testing.T) {
	created := time.Date(2026, 8, 6, 9, 0, 0, 123456789, time.UTC)
	employerAt := created.Add(15 * time.Minute)
	candidateAt := created.Add(30 * time.Minute)
	waitingSince := created.Add(time.Hour)
	value := EmployerConversation{
		ID: "conversation-fixed", VacancyID: 42, ApplicationID: "application-fixed", HHConversationID: "hh-chat-42",
		CompanyName: "Fixture Company", VacancyTitle: "Python/Django", VacancyDescription: "Integrate APIs",
		Status: StatusEmployerReplied, CreatedAt: created, UpdatedAt: candidateAt, HHUpdatedAt: candidateAt,
		LastEmployerMessageAt: &employerAt, LastCandidateMessageAt: &candidateAt,
		Messages: []Message{
			{ID: "message-employer", ExternalID: "hh-message-1", Timestamp: employerAt, Sender: SenderEmployer, Text: "Есть ли опыт Django?", Source: SourceHH, Direction: DirectionIncoming, Metadata: map[string]string{"kind": "inbox"}},
			{ID: "message-candidate", ExternalID: "hh-message-2", Timestamp: candidateAt, Sender: SenderCandidate, Text: "Да, использовал Django.", Source: SourceHHWrite, Direction: DirectionOutgoing},
		},
		Summary: Summary{
			TopicsDiscussed: []string{"Django"}, EmployerQuestions: []string{"Есть ли опыт Django?"}, CandidateAnswers: []string{"Да"},
			CandidateClaims:      []Claim{{Text: "Да, использовал Django.", RelatedSkill: "Django", MessageID: "message-candidate", CreatedAt: candidateAt, Experience: &ExperienceClaim{Months: 24, Scope: "total_professional"}}},
			EmployerRequirements: []string{"Django"}, PendingQuestions: []string{}, Commitments: []string{"Уточнить детали"}, ImportantFacts: []string{"synthetic fixture"},
		},
		NextAction: "wait", WaitingSince: &waitingSince, LastActivityAt: &candidateAt, FollowUpState: FollowUpEligible,
		RawStatus: "RESPONSE", HHMetadata: map[string]string{"warning_message_content_conflict": "true"},
	}
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var got EmployerConversation
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, value) {
		t.Fatalf("conversation JSON round trip changed persisted model: got=%+v want=%+v json=%s", got, value, raw)
	}
	for _, key := range []string{"id", "vacancy_id", "application_id", "hh_conversation_id", "status", "messages", "summary", "follow_up_state", "raw_status", "hh_metadata"} {
		var object map[string]json.RawMessage
		if err := json.Unmarshal(raw, &object); err != nil {
			t.Fatal(err)
		}
		if _, ok := object[key]; !ok {
			t.Fatalf("conversation JSON omitted persisted key %q: %s", key, raw)
		}
	}
}

func TestEmployerConversationJSONZeroValueAndNilEmptySemantics(t *testing.T) {
	var value EmployerConversation
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var got EmployerConversation
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, value) {
		t.Fatalf("zero-value conversation JSON round trip changed nil semantics: got=%+v want=%+v", got, value)
	}
	for _, key := range []string{"application_id", "hh_conversation_id", "vacancy_description", "raw_status", "hh_metadata"} {
		var object map[string]json.RawMessage
		if err := json.Unmarshal(raw, &object); err != nil {
			t.Fatal(err)
		}
		if _, ok := object[key]; ok {
			t.Fatalf("zero-value optional key %q was emitted: %s", key, raw)
		}
	}
}

func TestMessageJSONContractIncludesNormalizedIdentityAndMetadata(t *testing.T) {
	at := time.Date(2026, 8, 6, 10, 0, 0, 0, time.UTC)
	values := []Message{
		{ID: "employer", ExternalID: "hh-employer", Timestamp: at, Sender: SenderEmployer, Text: "Вопрос", Source: SourceHH, Direction: DirectionIncoming, Metadata: map[string]string{"button": "reply"}},
		{ID: "system", ExternalID: "hh-system", Timestamp: at.Add(time.Minute), Sender: SenderSystem, Source: SourceHH, Direction: DirectionIncoming, HHSystemEvent: true},
		{ID: "unknown", ExternalID: "hh-unknown", Timestamp: at.Add(2 * time.Minute), Sender: SenderUnknown, Source: SourceHH, Direction: DirectionUnknown, ContentUnavailable: true},
	}
	for _, value := range values {
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		var got Message
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, value) {
			t.Fatalf("message JSON round trip changed value: got=%+v want=%+v json=%s", got, value, raw)
		}
		if err := value.Validate(); err != nil {
			t.Fatalf("synthetic message rejected: %v", err)
		}
	}
}

func TestEmployerConversationValidationKeepsClaimsAggregateLocal(t *testing.T) {
	at := time.Date(2026, 8, 6, 10, 0, 0, 0, time.UTC)
	value := EmployerConversation{
		ID: "conversation-fixed", VacancyID: 42, Status: StatusApplied, CreatedAt: at, UpdatedAt: at,
		Messages:      []Message{{ID: "candidate", Timestamp: at, Sender: SenderCandidate, Text: "Использовал Django.", Source: SourceHH, Direction: DirectionOutgoing}},
		Summary:       Summary{CandidateClaims: []Claim{{Text: "Использовал Django.", MessageID: "candidate", CreatedAt: at}}},
		FollowUpState: FollowUpNone,
	}
	value.RefreshActivity()
	if err := value.Validate(); err != nil {
		t.Fatal(err)
	}
	value.Summary.CandidateClaims[0].Text = "неверная цитата"
	if err := value.Validate(); err == nil {
		t.Fatal("claim not linked to a verbatim candidate message was accepted")
	}
}

func TestSameMessagePreservesExistingIdentitySemantics(t *testing.T) {
	at := time.Date(2026, 8, 6, 10, 0, 0, 0, time.UTC)
	base := Message{ID: "m", ExternalID: "external", Timestamp: at, Sender: SenderEmployer, Text: "hello", Source: SourceHH, Direction: DirectionIncoming}
	if !SameMessage(base, base) {
		t.Fatal("identical messages were not equal")
	}
	withSystemAnnotation := base
	withSystemAnnotation.HHSystemEvent = true
	if !SameMessage(base, withSystemAnnotation) {
		t.Fatal("existing identity semantics changed for system annotation")
	}
	withEmptyMetadata := base
	withEmptyMetadata.Metadata = map[string]string{}
	if SameMessage(base, withEmptyMetadata) {
		t.Fatal("existing nil-versus-empty metadata identity semantics changed")
	}
}
