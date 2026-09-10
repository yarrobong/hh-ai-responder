package jsonstorage

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"hh-ai-responder/internal/conversation"
)

func adapterConversation(t *testing.T) (context.Context, *ConversationRepository, conversation.EmployerConversation) {
	t.Helper()
	ctx := context.Background()
	repo := NewConversationRepository(filepath.Join(t.TempDir(), EmployerConversationsFilename))
	if err := repo.Load(); err != nil {
		t.Fatal(err)
	}
	created, err := repo.Upsert(ctx, conversation.EmployerConversation{VacancyID: 42, ApplicationID: "app-1", HHConversationID: "hh-1", CompanyName: "Company", VacancyTitle: "Python"})
	if err != nil {
		t.Fatal(err)
	}
	return ctx, repo, created
}

func adapterMessage(external string, sender conversation.Sender, text string, at time.Time) conversation.Message {
	direction := conversation.DirectionIncoming
	if sender == conversation.SenderCandidate {
		direction = conversation.DirectionOutgoing
	}
	return conversation.Message{ID: "", ExternalID: external, Timestamp: at, Sender: sender, Text: text, Source: conversation.SourceHH, Direction: direction}
}

func TestConversationRepositoryMissingAndMalformedFilesFailClosed(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), EmployerConversationsFilename)
	repo := NewConversationRepository(path)
	if err := repo.Load(); err != nil {
		t.Fatal(err)
	}
	created, err := repo.Upsert(ctx, conversation.EmployerConversation{ID: "conversation-fixed", VacancyID: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{
		"{", `{"version":2,"conversations":[]}`, `{"version":1,"conversations":null}`,
		`{"version":1,"conversations":[],"unknown":true}`,
	} {
		if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := repo.Load(); err == nil {
			t.Fatalf("malformed storage accepted: %s", raw)
		}
		got, err := repo.Get(ctx, created.ID)
		if err != nil || got.ID != created.ID {
			t.Fatalf("failed load replaced in-memory state: got=%+v err=%v", got, err)
		}
	}
}

func TestConversationRepositoryRestartIdentityDedupAndSaveSemantics(t *testing.T) {
	ctx, repo, created := adapterConversation(t)
	at := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	employer, err := repo.AppendMessage(ctx, created.ID, adapterMessage("m-employer", conversation.SenderEmployer, "Есть ли опыт Django?", at.Add(time.Minute)))
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := repo.AppendMessage(ctx, created.ID, adapterMessage("m-candidate", conversation.SenderCandidate, "Да, использовал Django.", at.Add(2*time.Minute)))
	if err != nil {
		t.Fatal(err)
	}
	claim := conversation.Claim{Text: "использовал Django", RelatedSkill: "Django", MessageID: candidate.ID, CreatedAt: at.Add(3 * time.Minute), Experience: &conversation.ExperienceClaim{Months: 12, Scope: "total_professional"}}
	if err := repo.AppendCandidateClaim(ctx, created.ID, claim); err != nil {
		t.Fatal(err)
	}
	waiting := at.Add(4 * time.Minute)
	if err := repo.UpdateState(ctx, created.ID, conversation.State{Status: conversation.StatusWaitingEmployer, NextAction: "wait", WaitingSince: &waiting, FollowUpState: conversation.FollowUpEligible}); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpdateSummary(ctx, created.ID, conversation.Summary{TopicsDiscussed: []string{"Django"}, CandidateClaims: []conversation.Claim{claim}}); err != nil {
		t.Fatal(err)
	}
	beforeSave, err := repo.Get(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx); err != nil {
		t.Fatal(err)
	}
	restarted := NewConversationRepository(repoPath(repo))
	if err := restarted.Load(); err != nil {
		t.Fatal(err)
	}
	got, err := restarted.GetByHHConversationID(ctx, "hh-1")
	if err != nil || !reflect.DeepEqual(got, beforeSave) {
		t.Fatalf("restart changed conversation: got=%+v want=%+v err=%v", got, beforeSave, err)
	}
	duplicate, err := restarted.AppendMessage(ctx, got.ID, conversation.Message{ID: employer.ID, ExternalID: employer.ExternalID, Timestamp: employer.Timestamp, Sender: employer.Sender, Text: employer.Text, Source: employer.Source, Direction: employer.Direction})
	if err != nil || duplicate.ID != employer.ID {
		t.Fatalf("persisted duplicate was not idempotent: %+v err=%v", duplicate, err)
	}
	if _, err := restarted.AppendMessage(ctx, got.ID, conversation.Message{ID: employer.ID, ExternalID: employer.ExternalID, Timestamp: employer.Timestamp, Sender: employer.Sender, Text: "rewritten", Source: employer.Source, Direction: employer.Direction}); err == nil {
		t.Fatal("rewritten historical message was accepted")
	}
	if _, err := restarted.AppendMessage(ctx, got.ID, conversation.Message{ExternalID: "m-service", Timestamp: at.Add(5 * time.Minute), Sender: conversation.SenderSystem, Text: "", Source: conversation.SourceHH, Direction: conversation.DirectionIncoming, HHSystemEvent: true}); err != nil {
		t.Fatal(err)
	}
	if err := restarted.Save(ctx); err != nil {
		t.Fatal(err)
	}
	final, err := restarted.Get(ctx, got.ID)
	if err != nil || len(final.Messages) != 3 || final.Messages[2].Sender != conversation.SenderSystem {
		t.Fatalf("service message was not preserved: %+v err=%v", final, err)
	}
}

func TestConversationRepositoryOrderingDetachmentAndContext(t *testing.T) {
	ctx, repo, created := adapterConversation(t)
	at := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	first, err := repo.AppendMessage(ctx, created.ID, adapterMessage("first", conversation.SenderEmployer, "first", at))
	if err != nil {
		t.Fatal(err)
	}
	second, err := repo.AppendMessage(ctx, created.ID, adapterMessage("second", conversation.SenderCandidate, "second", at))
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.AppendCandidateClaim(ctx, created.ID, conversation.Claim{Text: "second", MessageID: second.ID, CreatedAt: at.Add(time.Minute)}); err != nil {
		t.Fatal(err)
	}
	timeline, err := repo.Timeline(ctx, created.ID)
	if err != nil || len(timeline) != 2 || timeline[0].ID != first.ID || timeline[1].ID != second.ID {
		t.Fatalf("unstable timeline: %+v err=%v", timeline, err)
	}
	timeline[0].Text = "changed"
	timeline[1].Metadata = map[string]string{"changed": "yes"}
	got, err := repo.Get(ctx, created.ID)
	if err != nil || got.Messages[0].Text != "first" || got.Messages[1].Metadata != nil {
		t.Fatalf("timeline aliased repository state: %+v err=%v", got, err)
	}
	list, err := repo.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	list[0].Summary.TopicsDiscussed = []string{"changed"}
	again, err := repo.Get(ctx, created.ID)
	if err != nil || len(again.Summary.TopicsDiscussed) != 0 {
		t.Fatal("list result aliased nested summary")
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := repo.Get(cancelled, created.ID); !errors.Is(err, context.Canceled) {
		t.Fatalf("context cancellation changed: %v", err)
	}
	if runtime.GOOS != "windows" {
		if err := repo.Save(ctx); err != nil {
			t.Fatal(err)
		}
		info, err := os.Stat(repoPath(repo))
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("private file mode=%v err=%v", info.Mode().Perm(), err)
		}
	}
}

func repoPath(repo *ConversationRepository) string {
	return repo.path
}

func TestConversationRepositoryDuplicateExternalIdentityAndClaimsStayLocal(t *testing.T) {
	ctx, repo, created := adapterConversation(t)
	if _, err := repo.Upsert(ctx, conversation.EmployerConversation{ID: "conversation-2", VacancyID: 43, HHConversationID: created.HHConversationID}); err == nil {
		t.Fatal("duplicate external conversation identity was accepted")
	}
	if err := repo.AppendCandidateClaim(ctx, created.ID, conversation.Claim{Text: "Kubernetes production experience", MessageID: "missing", CreatedAt: time.Now().UTC()}); err == nil || !strings.Contains(err.Error(), "claim") {
		t.Fatalf("invalid claim was not rejected by intrinsic conversation validation: %v", err)
	}
	if _, err := repo.Get(ctx, "missing"); !errors.Is(err, ErrConversationNotFound) {
		t.Fatalf("missing conversation error changed: %v", err)
	}
}
