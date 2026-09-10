package postgresstorage

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"hh-ai-responder/internal/conversation"
)

type conversationScannerFixture struct{ values []interface{} }

func (f conversationScannerFixture) Scan(dest ...interface{}) error {
	if len(dest) != len(f.values) {
		return errors.New("unexpected conversation scan shape")
	}
	for i, value := range f.values {
		target := reflect.ValueOf(dest[i])
		if target.Kind() != reflect.Ptr || target.IsNil() {
			return errors.New("conversation scan target is not a pointer")
		}
		if value == nil {
			target.Elem().Set(reflect.Zero(target.Elem().Type()))
			continue
		}
		source := reflect.ValueOf(value)
		if source.Type().AssignableTo(target.Elem().Type()) {
			target.Elem().Set(source)
			continue
		}
		if source.Type().ConvertibleTo(target.Elem().Type()) {
			target.Elem().Set(source.Convert(target.Elem().Type()))
			continue
		}
		return errors.New("conversation scan value has incompatible type")
	}
	return nil
}

func TestScanPostgresConversationPreservesAllPersistedFields(t *testing.T) {
	at := time.Date(2026, 9, 8, 12, 13, 14, 123456789, time.UTC)
	lastEmployer := at.Add(time.Minute)
	lastCandidate := at.Add(2 * time.Minute)
	waiting := at.Add(3 * time.Minute)
	lastActivity := at.Add(2 * time.Minute)
	applicationID := "application-1"
	summary := `{"topics_discussed":["APIs"],"candidate_claims":[]}`
	metadata := `{"sync":"complete"}`
	got, err := scanPostgresConversation(conversationScannerFixture{values: []interface{}{
		"conversation-1", "hh-conversation-1", 42, &applicationID, "Company", "Python", "Integrate APIs",
		conversation.StatusWaitingEmployer, pgtype.Timestamptz{Time: at, Valid: true}, pgtype.Timestamptz{Time: at, Valid: true},
		pgtype.Int8{Int64: at.UnixNano(), Valid: true}, pgtype.Int8{Int64: at.UnixNano(), Valid: true},
		pgtype.Timestamptz{Time: at, Valid: true}, pgtype.Int8{Int64: at.UnixNano(), Valid: true},
		pgtype.Timestamptz{Time: lastEmployer, Valid: true}, pgtype.Int8{Int64: lastEmployer.UnixNano(), Valid: true},
		pgtype.Timestamptz{Time: lastCandidate, Valid: true}, pgtype.Int8{Int64: lastCandidate.UnixNano(), Valid: true},
		[]byte(summary), "wait for employer", pgtype.Timestamptz{Time: waiting, Valid: true}, pgtype.Int8{Int64: waiting.UnixNano(), Valid: true},
		pgtype.Timestamptz{Time: lastActivity, Valid: true}, pgtype.Int8{Int64: lastActivity.UnixNano(), Valid: true},
		conversation.FollowUpEligible, "WAITING", []byte(metadata),
	}})
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "conversation-1" || got.HHConversationID != "hh-conversation-1" || got.VacancyID != 42 || got.ApplicationID != applicationID ||
		got.Status != conversation.StatusWaitingEmployer || got.NextAction != "wait for employer" || got.RawStatus != "WAITING" ||
		!got.CreatedAt.Equal(at) || got.LastEmployerMessageAt == nil || !got.LastEmployerMessageAt.Equal(lastEmployer) || got.LastCandidateMessageAt == nil || !got.LastCandidateMessageAt.Equal(lastCandidate) ||
		got.WaitingSince == nil || !got.WaitingSince.Equal(waiting) || got.LastActivityAt == nil || !got.LastActivityAt.Equal(lastActivity) || got.FollowUpState != conversation.FollowUpEligible ||
		got.HHMetadata["sync"] != "complete" || len(got.Summary.TopicsDiscussed) != 1 {
		t.Fatalf("conversation scan lost persisted values: %+v", got)
	}
}

func TestScanPostgresConversationPreservesNullableFields(t *testing.T) {
	got, err := scanPostgresConversation(conversationScannerFixture{values: []interface{}{
		"conversation-1", "", 42, nil, "Company", "Python", "", conversation.StatusApplied,
		pgtype.Timestamptz{Valid: true, Time: time.Unix(0, 1).UTC()}, pgtype.Timestamptz{Valid: true, Time: time.Unix(0, 2).UTC()},
		pgtype.Int8{}, pgtype.Int8{}, pgtype.Timestamptz{}, pgtype.Int8{}, pgtype.Timestamptz{}, pgtype.Int8{}, pgtype.Timestamptz{}, pgtype.Int8{},
		nil, "", pgtype.Timestamptz{}, pgtype.Int8{}, pgtype.Timestamptz{}, pgtype.Int8{}, conversation.FollowUpNone, "", nil,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if got.ApplicationID != "" || !got.HHUpdatedAt.IsZero() || got.LastEmployerMessageAt != nil || got.LastCandidateMessageAt != nil || got.WaitingSince != nil || got.LastActivityAt != nil || got.HHMetadata != nil {
		t.Fatalf("nullable conversation fields changed shape: %+v", got)
	}
}

func TestMapPostgresConversationErrorsPreservesIdentityAndRelations(t *testing.T) {
	tests := []struct {
		name       string
		code       string
		constraint string
		want       error
	}{
		{name: "external conversation", code: "23505", constraint: "conversations_external_id_unique", want: ErrDuplicateConversationExternal},
		{name: "message external identity", code: "23505", constraint: "conversation_messages_external_id_unique", want: ErrRepositoryConflict},
		{name: "vacancy relation", code: "23503", constraint: "conversations_vacancy_fk", want: ErrVacancyNotFound},
		{name: "conversation relation", code: "23503", constraint: "conversation_messages_conversation_fk", want: conversation.ErrConversationNotFound},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := mapPostgresConversationError("conversation test", &pgconn.PgError{Code: test.code, ConstraintName: test.constraint})
			if !errors.Is(got, test.want) {
				t.Fatalf("error=%v does not preserve %v", got, test.want)
			}
		})
	}
}
