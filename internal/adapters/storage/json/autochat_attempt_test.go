package jsonstorage

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	domain "hh-ai-responder/internal/autochatattempt"
	autochatport "hh-ai-responder/internal/ports/autochatattempt"
)

func jsonAttempt(id, trigger string) domain.Attempt {
	now := time.Unix(100, 0).UTC()
	return domain.Attempt{AttemptID: id, ConversationID: "conversation-1", TriggerMessageID: trigger, ActionType: domain.ActionReply, State: domain.StateSending, CreatedAt: now, UpdatedAt: now, RequestKey: id}
}

func TestAutoChatAttemptRepositoryReservesReloadsAndBlocksSameTrigger(t *testing.T) {
	path := filepath.Join(t.TempDir(), AutoChatAttemptsFilename)
	store := NewAutoChatAttemptRepository(path)
	reserved, err := store.Reserve(context.Background(), jsonAttempt("attempt-1", "message-1"))
	if err != nil || !reserved.Reserved {
		t.Fatalf("first reserve: %+v err=%v", reserved, err)
	}
	if _, err := store.Reserve(context.Background(), jsonAttempt("attempt-2", "message-1")); !errors.Is(err, domain.ErrTriggerBlocked) {
		t.Fatalf("same trigger was reserved twice: %v", err)
	}
	if err := store.RecordOutcome(context.Background(), "attempt-1", domain.StateAccepted, time.Unix(101, 0).UTC(), "out-1", 200, ""); err != nil {
		t.Fatal(err)
	}
	restarted := NewAutoChatAttemptRepository(path)
	if err := restarted.Load(); err != nil {
		t.Fatal(err)
	}
	got, err := restarted.FindBlockingForTrigger(context.Background(), "conversation-1", "message-1")
	if err != nil || got.State != domain.StateAccepted || got.ProviderOutgoingMessageID != "out-1" {
		t.Fatalf("reload: %+v err=%v", got, err)
	}
	other, err := restarted.Reserve(context.Background(), jsonAttempt("attempt-3", "message-2"))
	if err != nil || !other.Reserved {
		t.Fatalf("new trigger was blocked: %+v err=%v", other, err)
	}
}

func TestAutoChatAttemptRepositoryConcurrentReserveHasOneWinner(t *testing.T) {
	path := filepath.Join(t.TempDir(), AutoChatAttemptsFilename)
	left, right := NewAutoChatAttemptRepository(path), NewAutoChatAttemptRepository(path)
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, store := range []*AutoChatAttemptRepository{left, right} {
		wg.Add(1)
		go func(store *AutoChatAttemptRepository) {
			defer wg.Done()
			_, err := store.Reserve(context.Background(), jsonAttempt("attempt-"+time.Now().Format("150405.000000000"), "message-1"))
			results <- err
		}(store)
	}
	wg.Wait()
	close(results)
	wins := 0
	for err := range results {
		if err == nil {
			wins++
		} else if !errors.Is(err, domain.ErrTriggerBlocked) {
			t.Fatalf("unexpected concurrent reserve error: %v", err)
		}
	}
	if wins != 1 {
		t.Fatalf("concurrent winners=%d, want 1", wins)
	}
}

func TestAutoChatAttemptRepositoryCorruptionFailsClosed(t *testing.T) {
	path := filepath.Join(t.TempDir(), AutoChatAttemptsFilename)
	if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := NewAutoChatAttemptRepository(path)
	if err := store.Load(); err == nil {
		t.Fatal("corrupt store loaded successfully")
	}
	if _, err := store.FindBlockingForTrigger(context.Background(), "conversation-1", "message-1"); err == nil {
		t.Fatal("corrupt store was treated as empty")
	}
}

func TestAutoChatAttemptRepositoryBoundedReadPreservesTriggers(t *testing.T) {
	path := filepath.Join(t.TempDir(), AutoChatAttemptsFilename)
	store := NewAutoChatAttemptRepository(path)
	for _, value := range []domain.Attempt{jsonAttempt("m1", "M1"), jsonAttempt("m2", "M2"), jsonAttempt("m3", "M3")} {
		if _, err := store.Reserve(context.Background(), value); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.RecordOutcome(context.Background(), "m1", domain.StateDeliveryUncertain, time.Unix(101, 0).UTC(), "", 502, "uncertain"); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordOutcome(context.Background(), "m2", domain.StateAccepted, time.Unix(102, 0).UTC(), "", 202, ""); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordOutcome(context.Background(), "m3", domain.StateRejected, time.Unix(103, 0).UTC(), "", 400, "rejected"); err != nil {
		t.Fatal(err)
	}
	conversation := "conversation-1"
	items, err := store.List(context.Background(), autochatport.ReadQuery{Limit: 10, ConversationID: &conversation})
	if err != nil || len(items) != 3 {
		t.Fatalf("conversation read = %+v err=%v", items, err)
	}
	seen := map[string]bool{}
	for _, item := range items {
		seen[item.TriggerMessageID] = true
	}
	if !seen["M1"] || !seen["M2"] || !seen["M3"] {
		t.Fatalf("trigger identities collapsed: %+v", seen)
	}
	items, err = store.List(context.Background(), autochatport.ReadQuery{Limit: 1, States: []domain.State{domain.StateDeliveryUncertain}})
	if err != nil || len(items) != 1 || items[0].TriggerMessageID != "M1" {
		t.Fatalf("state filter = %+v err=%v", items, err)
	}
}
