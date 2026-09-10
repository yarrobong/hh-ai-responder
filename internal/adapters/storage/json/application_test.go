package jsonstorage

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sync"
	"testing"
	"time"

	"hh-ai-responder/internal/application"
	"hh-ai-responder/internal/conversation"
	"hh-ai-responder/internal/vacancy"
)

func applicationFixture() application.JobApplication {
	at := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	return application.JobApplication{
		ID: "application-fixed", VacancyID: 42, ExternalID: "hh-negotiation-42",
		CompanyName: "Fixture Company", VacancyTitle: "Python/Django", VacancyURL: "https://hh.example/42",
		Source: application.SourceHH, CreatedAt: at, UpdatedAt: at, Status: application.StatusDiscovered,
		MatchResult: &vacancy.MatchResult{Score: 82, Confidence: 0.75, MatchedSkills: []string{"Python"}, UnknownSkills: []string{"Kubernetes"}, Recommendation: &vacancy.ApplicationRecommendation{Decision: vacancy.RecommendationMaybe, Reason: "review"}},
		HHMetadata:  map[string]string{"negotiation_id": "hh-negotiation-42"}, Partial: true,
		DataCompleteness:       vacancy.DataCompletenessPartial,
		ReconciliationEvidence: []vacancy.ReconciliationEvidence{{Method: "hh_negotiation_id", Source: "hh", Confidence: 1, ReconciledAt: at}},
	}
}

func TestApplicationRepositoryRoundTripRestartAndExplicitSave(t *testing.T) {
	path := filepath.Join(t.TempDir(), ApplicationsFilename)
	ctx := context.Background()
	repo := NewApplicationRepository(path)
	created, err := repo.Create(ctx, applicationFixture())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("create wrote before explicit Save")
	}
	if err := repo.SetFollowUpState(ctx, created.ID, conversation.FollowUpEligible); err != nil {
		t.Fatal(err)
	}
	if err := repo.AttachConversation(ctx, created.ID, "conversation-fixed"); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpdateStatus(ctx, created.ID, application.StatusApplied); err != nil {
		t.Fatal(err)
	}
	if err := repo.SaveMatchResult(ctx, created.ID, *created.MatchResult); err != nil {
		t.Fatal(err)
	}
	if err := repo.AppendEvent(ctx, created.ID, created.CreatedAt.Add(time.Hour), application.EventMessageReceived, "received"); err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx); err != nil {
		t.Fatal(err)
	}

	restarted := NewApplicationRepository(path)
	if err := restarted.Load(); err != nil {
		t.Fatal(err)
	}
	got, err := restarted.Get(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ExternalID != created.ExternalID || got.VacancyID != 42 || got.ConversationID != "conversation-fixed" || got.Status != application.StatusApplied || got.FollowUpState != conversation.FollowUpEligible || got.MatchResult == nil || got.MatchResult.Score != 82 || got.DataCompleteness != vacancy.DataCompletenessPartial || len(got.ReconciliationEvidence) != 1 {
		t.Fatalf("restart lost application values: %+v", got)
	}
	events, err := restarted.Timeline(ctx, created.ID)
	if err != nil || len(events) != 3 {
		t.Fatalf("unexpected timeline: %+v err=%v", events, err)
	}
	if events[0].Type != application.EventCreated || events[1].Type != application.EventMessageReceived || events[2].Type != application.EventApplied {
		t.Fatalf("event order changed: %+v", events)
	}
}

func TestApplicationRepositoryMalformedFilesPreserveMemory(t *testing.T) {
	path := filepath.Join(t.TempDir(), ApplicationsFilename)
	repo := NewApplicationRepository(path)
	keep, err := repo.Create(context.Background(), application.JobApplication{ID: "keep", VacancyID: 1, Source: application.SourceManual, Status: application.StatusDiscovered})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(context.Background()); err != nil {
		t.Fatal(err)
	}
	for name, raw := range map[string]string{
		"corrupt":  "{",
		"version":  `{"version":2,"applications":[],"events":[]}`,
		"null":     `{"version":1,"applications":null,"events":[]}`,
		"extra":    `{"version":1,"applications":[],"events":[],"extra":true}`,
		"trailing": `{"version":1,"applications":[],"events":[]} {}`,
	} {
		t.Run(name, func(t *testing.T) {
			if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := repo.Load(); err == nil {
				t.Fatal("malformed application file was accepted")
			}
			got, err := repo.Get(context.Background(), keep.ID)
			if err != nil || got.ID != keep.ID {
				t.Fatalf("failed Load replaced memory: %+v err=%v", got, err)
			}
		})
	}
}

func TestApplicationRepositoryDuplicateAndAppendOnlySemantics(t *testing.T) {
	repo := NewApplicationRepository(filepath.Join(t.TempDir(), ApplicationsFilename))
	ctx := context.Background()
	first, err := repo.Create(ctx, application.JobApplication{ID: "first", ExternalID: "external", VacancyID: 1, Source: application.SourceManual, Status: application.StatusDiscovered})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Create(ctx, application.JobApplication{ID: "first", VacancyID: 2, Source: application.SourceManual, Status: application.StatusDiscovered}); !errors.Is(err, ErrDuplicateApplicationID) {
		t.Fatalf("duplicate local ID was accepted: %v", err)
	}
	if _, err := repo.Create(ctx, application.JobApplication{ID: "second", ExternalID: "external", VacancyID: 2, Source: application.SourceManual, Status: application.StatusDiscovered}); !errors.Is(err, ErrDuplicateExternalID) {
		t.Fatalf("duplicate external ID was accepted: %v", err)
	}
	at := first.CreatedAt.Add(time.Hour)
	if err := repo.AppendEvent(ctx, first.ID, at, application.EventMessageReceived, "same type 1"); err != nil {
		t.Fatal(err)
	}
	if err := repo.AppendEvent(ctx, first.ID, at, application.EventMessageReceived, "same type 2"); err != nil {
		t.Fatal(err)
	}
	events, err := repo.Timeline(ctx, first.ID)
	if err != nil || len(events) != 3 || events[1].Description != "same type 1" || events[2].Description != "same type 2" {
		t.Fatalf("append-only ordering changed: %+v err=%v", events, err)
	}
	if events[0].ID == events[1].ID || events[1].ID == events[2].ID {
		t.Fatal("event IDs are not unique")
	}
}

func TestApplicationRepositoryResultsAreDetachedAndContextCancellationIsHonored(t *testing.T) {
	repo := NewApplicationRepository(filepath.Join(t.TempDir(), ApplicationsFilename))
	created, err := repo.Create(context.Background(), applicationFixture())
	if err != nil {
		t.Fatal(err)
	}
	list, err := repo.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	list[0].HHMetadata["mutated"] = "yes"
	list[0].MatchResult.MatchedSkills[0] = "mutated"
	list[0].ReconciliationEvidence[0].Method = "mutated"
	got, err := repo.Get(context.Background(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got.HHMetadata["mutated"]; ok || got.MatchResult.MatchedSkills[0] == "mutated" || got.ReconciliationEvidence[0].Method == "mutated" {
		t.Fatal("List returned repository-owned nested state")
	}
	events, err := repo.Timeline(context.Background(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	events[0].Description = "mutated"
	again, err := repo.Timeline(context.Background(), created.ID)
	if err != nil || again[0].Description == "mutated" {
		t.Fatal("Timeline returned repository-owned event state")
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := repo.Get(canceled, created.ID); !errors.Is(err, context.Canceled) {
		t.Fatalf("Get ignored cancellation: %v", err)
	}
	if err := repo.Save(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("Save ignored cancellation: %v", err)
	}
}

func TestApplicationRepositoryPrivatePermissionsAndParallelReads(t *testing.T) {
	path := filepath.Join(t.TempDir(), ApplicationsFilename)
	repo := NewApplicationRepository(path)
	created, err := repo.Create(context.Background(), application.JobApplication{ID: "parallel", VacancyID: 1, Source: application.SourceManual, Status: application.StatusDiscovered})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(context.Background()); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("application file is not private: %v %v", info, err)
		}
	}
	var group sync.WaitGroup
	for i := 0; i < 16; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			for j := 0; j < 50; j++ {
				value, err := repo.Get(context.Background(), created.ID)
				if err != nil || !reflect.DeepEqual(value, created) {
					t.Errorf("parallel read changed value: %+v err=%v", value, err)
					return
				}
			}
		}()
	}
	group.Wait()
}
