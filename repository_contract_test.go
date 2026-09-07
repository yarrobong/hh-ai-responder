package main

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

var (
	_ VacancyRepository      = (*JSONVacancyRepository)(nil)
	_ ApplicationRepository  = (*JSONApplicationRepository)(nil)
	_ ConversationRepository = (*JSONConversationRepository)(nil)
	_ CandidateRepository    = (*JSONCandidateRepository)(nil)
)

func TestJSONCandidateRepositoryContract(t *testing.T) {
	profile := NewCandidateProfile(time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC))
	meta := knowledgeTestMetadata(KnowledgeSourceUserConfirmed, TruthStatusConfirmed)
	kb := &CandidateKnowledgeBase{ProfilePath: filepath.Join(t.TempDir(), "candidate_profile.json"), Profile: profile, Skills: []CandidateSkillDetailed{{ID: "skill-python", Name: "Python", Level: SkillLevelWorking, KnowledgeMetadata: meta}}}
	repo := NewJSONCandidateRepositoryFromLegacy(kb, []CandidateStory{{ID: "story-1", Title: "Narrative", Summary: "A narrative", Skills: []string{"UnknownSkill"}}}, "candidate@example.test", "https://github.com/example")

	first, err := repo.CurrentCandidate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	second, err := repo.CurrentCandidate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID || first.Skills[0].ID != second.Skills[0].ID || first.Skills[0].State != second.Skills[0].State || first.Claims[0].ID != second.Claims[0].ID || first.Stories[0].ID != second.Stories[0].ID || first.ID == "" || first.Skills[0].DisplayName != "Python" {
		t.Fatalf("candidate snapshot is not stable/canonical: first=%+v second=%+v", first, second)
	}
	if len(first.Stories) != 1 || len(first.Claims) == 0 {
		t.Fatalf("canonical candidate lost source data: %+v", first)
	}
	if first.Skills[0].State != CanonicalClaimActive {
		t.Fatalf("confirmed skill was not preserved as active: %+v", first.Skills[0])
	}

	unknown := first
	unknown.Skills = append(unknown.Skills, CanonicalCandidateSkill{Name: "UnknownSkill", Level: SkillLevelUnknown, State: CanonicalClaimActive, Metadata: KnowledgeMetadata{TruthStatus: TruthStatusUnknown}})
	for _, skill := range unknown.Skills {
		if skill.Name == "UnknownSkill" && skill.Negative {
			t.Fatal("unknown skill was converted to a negative claim")
		}
	}

	disputedKB := &CandidateKnowledgeBase{Profile: profile, Skills: []CandidateSkillDetailed{
		{ID: "positive", Name: "Django", Level: SkillLevelWorking, KnowledgeMetadata: meta},
		{ID: "negative", Name: "Django", Level: SkillLevelUnknown, Negative: true, KnowledgeMetadata: meta},
	}}
	disputed, err := NewJSONCandidateRepositoryFromLegacy(disputedKB, nil, "", "").CurrentCandidate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(disputed.Skills) != 1 || disputed.Skills[0].State != CanonicalClaimDisputed {
		t.Fatalf("disputed truth state was not preserved: %+v", disputed.Skills)
	}
	if _, err := CanonicalEmployerSafeProjection(disputed); err == nil {
		t.Fatal("disputed candidate was not fail-closed")
	}
}

func TestJSONVacancyRepositoryContract(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), VacanciesFilename)
	createdAt := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	store := NewVacancyStore(path)
	repo := NewJSONVacancyRepository(store)
	want, err := repo.Create(ctx, Vacancy{ID: 42, ExternalID: "hh-42", Name: "Python", CreatedAt: createdAt, UpdatedAt: createdAt, MatchResult: &MatchResult{Score: 80}})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx); err != nil {
		t.Fatal(err)
	}
	restarted := NewJSONVacancyRepository(NewVacancyStore(path))
	if err := restarted.store.Load(); err != nil {
		t.Fatal(err)
	}
	got, err := restarted.GetByExternalID(ctx, "hh-42")
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("vacancy restart changed repository state: got=%+v want=%+v err=%v", got, want, err)
	}
	if _, err := repo.Create(ctx, Vacancy{ID: 43, ExternalID: "hh-42", Name: "duplicate"}); !errors.Is(err, ErrDuplicateVacancyExternal) {
		t.Fatalf("duplicate external vacancy id was accepted: %v", err)
	}
	if _, err := repo.Get(ctx, 404); !errors.Is(err, ErrVacancyNotFound) {
		t.Fatalf("missing vacancy error changed: %v", err)
	}
}

func TestJSONApplicationRepositoryContract(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), JobApplicationsFilename)
	repo := NewJSONApplicationRepository(NewApplicationStore(path))
	created, err := repo.Create(ctx, JobApplication{ID: "app-1", VacancyID: 42, ExternalID: "hh-app-1", CompanyName: "Company", VacancyTitle: "Python", Source: ApplicationSourceHH, Status: ApplicationDiscovered})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.AppendEvent(ctx, created.ID, time.Now().UTC(), ApplicationEventMessageReceived, "received"); err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx); err != nil {
		t.Fatal(err)
	}
	restarted := NewJSONApplicationRepository(NewApplicationStore(path))
	if err := restarted.store.Load(); err != nil {
		t.Fatal(err)
	}
	got, err := restarted.GetByExternalID(ctx, "hh-app-1")
	if err != nil || got.ID != created.ID {
		t.Fatalf("application identity did not survive restart: %+v err=%v", got, err)
	}
	events, err := restarted.Timeline(ctx, created.ID)
	if err != nil || len(events) != 2 || events[1].Type != ApplicationEventMessageReceived {
		t.Fatalf("application event history was not append-only/persistent: %+v err=%v", events, err)
	}
	if _, err := repo.Create(ctx, JobApplication{ID: "app-2", ExternalID: "hh-app-1", VacancyID: 42, CompanyName: "Company", VacancyTitle: "Python", Source: ApplicationSourceHH, Status: ApplicationDiscovered}); !errors.Is(err, ErrDuplicateExternalID) {
		t.Fatalf("duplicate application external id was accepted: %v", err)
	}
}

func TestJSONConversationRepositoryContract(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), EmployerConversationsFilename)
	repo := NewJSONConversationRepository(NewConversationStore(path))
	conversation, err := repo.Upsert(ctx, EmployerConversation{ID: "conversation-1", VacancyID: 42, HHConversationID: "hh-chat-1", CompanyName: "Company", VacancyTitle: "Python"})
	if err != nil {
		t.Fatal(err)
	}
	written, err := repo.AppendMessage(ctx, conversation.ID, ConversationMessage{ExternalID: "hh-message-1", Timestamp: time.Now().UTC(), Sender: ConversationSenderEmployer, Text: "Hello", Source: ConversationSourceHH, Direction: ConversationIncoming})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.AppendMessage(ctx, conversation.ID, written); err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx); err != nil {
		t.Fatal(err)
	}
	restarted := NewJSONConversationRepository(NewConversationStore(path))
	if err := restarted.store.Load(); err != nil {
		t.Fatal(err)
	}
	got, err := restarted.GetByHHConversationID(ctx, "hh-chat-1")
	if err != nil || len(got.Messages) != 1 || got.Messages[0].ExternalID != "hh-message-1" {
		t.Fatalf("conversation/message contract changed after restart: %+v err=%v", got, err)
	}
	if _, err := restarted.AppendMessage(ctx, got.ID, ConversationMessage{ID: got.Messages[0].ID, ExternalID: "hh-message-1", Timestamp: got.Messages[0].Timestamp, Sender: ConversationSenderEmployer, Text: "rewritten", Source: ConversationSourceHH, Direction: ConversationIncoming}); err == nil {
		t.Fatal("conflicting duplicate message rewrote immutable history")
	}
	if _, err := restarted.Get(ctx, "missing"); !errors.Is(err, ErrConversationNotFound) {
		t.Fatalf("missing conversation error changed: %v", err)
	}
}
