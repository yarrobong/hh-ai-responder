package application

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"hh-ai-responder/internal/vacancy"
)

func TestJobApplicationJSONContractRoundTrip(t *testing.T) {
	created := time.Date(2026, 8, 4, 9, 0, 0, 0, time.UTC)
	value := JobApplication{
		FollowUpState: FollowUpDismissed, ID: "application-fixed", VacancyID: 42,
		ExternalID: "hh-negotiation-42", CompanyName: "Fixture Company", VacancyTitle: "Python/Django",
		VacancyURL: "https://hh.example/vacancy/42", Source: SourceHH, CreatedAt: created,
		UpdatedAt: created.Add(time.Hour), Status: StatusApplied, MatchResult: &vacancy.MatchResult{
			Score: 82, Confidence: 0.75, MatchedSkills: []string{"Python"}, UnknownSkills: []string{"Kubernetes"},
		}, ConversationID: "conversation-fixed", Notes: "fixture", NextAction: "waiting_employer_reply",
		RawStatus: "response", HHMetadata: map[string]string{"negotiation_id": "hh-negotiation-42"},
		Partial: true, DataCompleteness: vacancy.DataCompletenessPartial,
		ReconciliationEvidence: []vacancy.ReconciliationEvidence{{Method: "hh_negotiation_id", Source: "hh", Confidence: 1, ReconciledAt: created}},
	}
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var got JobApplication
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, value) {
		t.Fatalf("round trip changed application: got=%+v want=%+v json=%s", got, value, raw)
	}
}

func TestJobApplicationJSONContractOmitsOptionalZeroValues(t *testing.T) {
	value := JobApplication{ID: "application-fixed", VacancyID: 42, CompanyName: "Company", VacancyTitle: "Role", Source: SourceManual, Status: StatusUnknown, CreatedAt: time.Unix(1, 0).UTC(), UpdatedAt: time.Unix(1, 0).UTC()}
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"follow_up_state", "external_id", "vacancy_url", "match_result", "conversation_id", "notes", "next_action", "raw_status", "hh_metadata", "partial", "data_completeness", "reconciliation_evidence"} {
		var object map[string]json.RawMessage
		if err := json.Unmarshal(raw, &object); err != nil {
			t.Fatal(err)
		}
		if _, ok := object[key]; ok {
			t.Fatalf("optional zero field %q was emitted: %s", key, raw)
		}
	}
}

func TestApplicationValidationAndEventMapping(t *testing.T) {
	at := time.Date(2026, 8, 4, 9, 0, 0, 0, time.UTC)
	valid := JobApplication{ID: "application-fixed", VacancyID: 1, Source: SourceManual, Status: StatusDiscovered, CreatedAt: at, UpdatedAt: at}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	invalid := valid
	invalid.Status = Status("not-a-status")
	if err := invalid.Validate(); err == nil {
		t.Fatal("invalid status was accepted")
	}
	event := Event{ID: "event-fixed", ApplicationID: valid.ID, Timestamp: at, Type: EventCreated, Description: "created"}
	if err := event.Validate(); err != nil {
		t.Fatal(err)
	}
	if got := EventTypeForStatus(StatusApplied); got != EventApplied {
		t.Fatalf("unexpected applied event type: %q", got)
	}
	if got := EventTypeForStatus(StatusShortlisted); got != EventStatusChanged {
		t.Fatalf("unexpected default event type: %q", got)
	}
}

func TestApplicationEventJSONContract(t *testing.T) {
	at := time.Date(2026, 8, 4, 9, 0, 0, 0, time.UTC)
	event := Event{ID: "event-fixed", ApplicationID: "application-fixed", Timestamp: at, Type: EventCreated, Description: "created"}
	raw, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"id":"event-fixed","application_id":"application-fixed","timestamp":"2026-08-04T09:00:00Z","type":"created","description":"created"}`
	if string(raw) != want {
		t.Fatalf("event JSON changed: got=%s want=%s", raw, want)
	}
	var got Event
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, event) {
		t.Fatalf("event round trip changed value: got=%+v want=%+v", got, event)
	}
}
