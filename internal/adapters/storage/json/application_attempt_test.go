package jsonstorage

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	domain "hh-ai-responder/internal/applicationattempt"
	attemptport "hh-ai-responder/internal/ports/applicationattempt"
)

func attemptFixture(t *testing.T, vacancyID int) domain.Attempt {
	t.Helper()
	value, err := domain.New(vacancyID, "resume-1", time.Date(2026, 9, 10, 10, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestApplicationAttemptRepositoryRestartAndBlocking(t *testing.T) {
	path := filepath.Join(t.TempDir(), ApplicationAttemptsFilename)
	first := NewApplicationAttemptRepository(path)
	attempt := attemptFixture(t, 42)
	if _, err := first.Reserve(context.Background(), attempt); err != nil {
		t.Fatal(err)
	}
	restarted := NewApplicationAttemptRepository(path)
	if err := restarted.Load(); err != nil {
		t.Fatal(err)
	}
	if _, err := restarted.Reserve(context.Background(), attemptFixture(t, 42)); !errors.Is(err, domain.ErrTargetBlocked) {
		t.Fatalf("restart did not preserve SENDING block: %v", err)
	}
}

func TestApplicationAttemptRepositoryOutcomeAndReplayPolicy(t *testing.T) {
	path := filepath.Join(t.TempDir(), ApplicationAttemptsFilename)
	repo := NewApplicationAttemptRepository(path)
	attempt := attemptFixture(t, 42)
	if _, err := repo.Reserve(context.Background(), attempt); err != nil {
		t.Fatal(err)
	}
	if err := repo.RecordOutcome(context.Background(), attempt.AttemptID, domain.StateRejected, attempt.UpdatedAt.Add(time.Minute), 429, "rate_limited"); err != nil {
		t.Fatal(err)
	}
	second := attemptFixture(t, 42)
	if _, err := repo.Reserve(context.Background(), second); err != nil {
		t.Fatalf("REJECTED was not replayable: %v", err)
	}
	if err := repo.RecordOutcome(context.Background(), second.AttemptID, domain.StateDeliveryUncertain, second.UpdatedAt.Add(time.Minute), 500, "server"); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Reserve(context.Background(), attemptFixture(t, 42)); !errors.Is(err, domain.ErrTargetBlocked) {
		t.Fatalf("uncertain attempt was replayable: %v", err)
	}
}

func TestApplicationAttemptRepositoryExpectedAbsenceAndExplicitNotFound(t *testing.T) {
	path := filepath.Join(t.TempDir(), ApplicationAttemptsFilename)
	repo := NewApplicationAttemptRepository(path)
	if _, err := repo.FindBlocking(context.Background(), 41); !errors.Is(err, domain.ErrAttemptNotFound) {
		t.Fatalf("empty store lookup=%v, want canonical absence", err)
	}
	attempt := attemptFixture(t, 42)
	if _, err := repo.Reserve(context.Background(), attempt); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.FindBlocking(context.Background(), 41); !errors.Is(err, domain.ErrAttemptNotFound) {
		t.Fatalf("unrelated vacancy lookup=%v, want canonical absence", err)
	}
	if got, err := repo.FindBlocking(context.Background(), 42); err != nil || got.AttemptID != attempt.AttemptID {
		t.Fatalf("matching blocking lookup=%+v err=%v", got, err)
	}
	if _, err := repo.GetByID(context.Background(), "missing-attempt"); !errors.Is(err, domain.ErrAttemptNotFound) {
		t.Fatalf("explicit missing lookup=%v, want canonical not-found", err)
	}
}

func TestApplicationAttemptRepositoryConcurrentReserve(t *testing.T) {
	path := filepath.Join(t.TempDir(), ApplicationAttemptsFilename)
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			repo := NewApplicationAttemptRepository(path)
			_, err := repo.Reserve(context.Background(), attemptFixture(t, 42))
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	reserved := 0
	blocked := 0
	for err := range results {
		if err == nil {
			reserved++
		} else if errors.Is(err, domain.ErrTargetBlocked) {
			blocked++
		} else {
			t.Fatalf("unexpected concurrent reserve error: %v", err)
		}
	}
	if reserved != 1 || blocked != 1 {
		t.Fatalf("reserved=%d blocked=%d", reserved, blocked)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}

func TestApplicationAttemptRepositoryReconciliationPersistsAcrossRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), ApplicationAttemptsFilename)
	repo := NewApplicationAttemptRepository(path)
	attempt := attemptFixture(t, 42)
	if _, err := repo.Reserve(context.Background(), attempt); err != nil {
		t.Fatal(err)
	}
	providerAt := attempt.CreatedAt.Add(time.Minute)
	if err := repo.RecordReconciliation(context.Background(), attempt.AttemptID, domain.ReconciliationEvidence{
		Kind: domain.EvidenceConfirmedResponseExists, Strength: domain.EvidenceStrong, Source: "negotiation",
		ProviderNegotiationID: "topic-42", ProviderResponseAt: &providerAt,
	}, providerAt.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	restarted := NewApplicationAttemptRepository(path)
	if err := restarted.Load(); err != nil {
		t.Fatal(err)
	}
	got, err := restarted.Get(context.Background(), attempt.AttemptID)
	if err != nil || got.State != domain.StateTargetResponseConfirmed || got.Reconciliation == nil || got.Reconciliation.ProviderNegotiationID != "topic-42" {
		t.Fatalf("reconciliation did not survive restart: got=%+v err=%v", got, err)
	}
	if _, err := restarted.Reserve(context.Background(), attemptFixture(t, 42)); !errors.Is(err, domain.ErrTargetBlocked) {
		t.Fatalf("confirmed target became replayable: %v", err)
	}
}

func TestApplicationAttemptRepositoryBoundedRecentReadAndCorruption(t *testing.T) {
	path := filepath.Join(t.TempDir(), ApplicationAttemptsFilename)
	repo := NewApplicationAttemptRepository(path)
	first := attemptFixture(t, 1)
	if _, err := repo.Reserve(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	if err := repo.RecordOutcome(context.Background(), first.AttemptID, domain.StateRejected, first.UpdatedAt.Add(time.Minute), 400, "rejected"); err != nil {
		t.Fatal(err)
	}
	second := attemptFixture(t, 1)
	second.CreatedAt = first.CreatedAt.Add(2 * time.Minute)
	second.UpdatedAt = second.CreatedAt
	if _, err := repo.Reserve(context.Background(), second); err != nil {
		t.Fatal(err)
	}
	items, err := repo.List(context.Background(), attemptport.ReadQuery{Limit: 1})
	if err != nil || len(items) != 1 || items[0].AttemptID != second.AttemptID {
		t.Fatalf("bounded recent read = %+v err=%v", items, err)
	}
	restarted := NewApplicationAttemptRepository(path)
	items, err = restarted.List(context.Background(), attemptport.ReadQuery{Limit: 10, States: []domain.State{domain.StateRejected}})
	if err != nil || len(items) != 1 || items[0].AttemptID != first.AttemptID {
		t.Fatalf("filtered restart read = %+v err=%v", items, err)
	}
	if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := restarted.List(context.Background(), attemptport.ReadQuery{Limit: 1}); err == nil {
		t.Fatal("corrupt application store was treated as empty")
	}
}
