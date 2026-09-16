package runtime

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"hh-ai-responder/internal/vacancy"
	"hh-ai-responder/internal/vacancyreview"
)

// This opt-in contract test exercises the migration and the two transactional
// PostgreSQL paths. Ordinary tests remain database-free.
func TestPostgresVacancyFreshnessAndReviewContract(t *testing.T) {
	dsn := os.Getenv("POSTGRES_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("POSTGRES_TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	pool, err := OpenPostgres(ctx, PostgresConfig{DatabaseURL: dsn})
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err := ApplyPostgresMigrations(ctx, pool); err != nil {
		t.Fatal(err)
	}
	id := int(time.Now().UnixNano() % 1_000_000_000)
	if id <= 0 {
		id = 1
	}
	defer func() {
		for _, query := range []string{
			"DELETE FROM vacancy_review_events WHERE vacancy_id=$1",
			"DELETE FROM vacancy_review_states WHERE vacancy_id=$1",
			"DELETE FROM vacancy_freshness WHERE vacancy_id=$1",
			"DELETE FROM vacancies WHERE id=$1",
		} {
			_, _ = pool.Exec(ctx, query, id)
		}
	}()

	vacancies := NewPostgresVacancyRepository(pool)
	reviewStore := NewPostgresVacancyReviewRepository(pool)
	review := vacancyreview.NewService(reviewStore)
	t1 := time.Date(2026, 9, 11, 10, 0, 0, 123456789, time.UTC)
	t2 := t1.Add(time.Minute)
	value := Vacancy{ID: id, ExternalID: "p1-1-contract-" + time.Now().UTC().Format("150405.000000000"), Name: "Backend", Title: "Backend", Description: "API", CreatedAt: t1, UpdatedAt: t1}
	first, err := vacancies.ObserveVacancy(ctx, value, t1)
	if err != nil || !first.Created {
		t.Fatalf("first observation=%+v err=%v", first, err)
	}
	unchanged, err := vacancies.ObserveVacancy(ctx, value, t2)
	if err != nil || unchanged.Created || unchanged.Updated {
		t.Fatalf("unchanged observation=%+v err=%v", unchanged, err)
	}
	fresh, err := reviewStore.GetFreshness(ctx, id)
	if err != nil || !fresh.FirstSeenAt.Equal(t1) || !fresh.LastSeenAt.Equal(t2) {
		t.Fatalf("freshness=%+v err=%v", fresh, err)
	}
	if err := review.Dismiss(ctx, id, "old", t2); err != nil {
		t.Fatal(err)
	}
	decision, err := reviewStore.GetReviewState(ctx, id)
	if err != nil || decision.FingerprintVersion != vacancy.FingerprintVersion {
		t.Fatalf("review decision fingerprint version=%d err=%v, want %d", decision.FingerprintVersion, err, vacancy.FingerprintVersion)
	}
	value.Description = "API and support"
	third, err := vacancies.ObserveVacancy(ctx, value, t2.Add(time.Minute))
	if err != nil || !third.Updated || !third.MaterialChanged {
		t.Fatalf("changed observation=%+v err=%v", third, err)
	}
	effective, err := review.Effective(ctx, id, nil)
	if err != nil {
		t.Fatal(err)
	}
	if effective.ChangedSinceReview == nil || !*effective.ChangedSinceReview || effective.State != vacancyreview.StateDismissed {
		t.Fatalf("effective review=%+v", effective)
	}
	events, err := reviewStore.ListReviewEvents(ctx, id)
	if err != nil || len(events) != 1 || events[0].Type != vacancyreview.StateDismissed {
		t.Fatalf("events=%+v err=%v", events, err)
	}
	if err := review.Dismiss(ctx, id, "duplicate", t2); err != nil {
		t.Fatal(err)
	}
	events, err = reviewStore.ListReviewEvents(ctx, id)
	if err != nil || len(events) != 2 {
		t.Fatalf("changed-fingerprint review events=%+v err=%v", events, err)
	}
	var group sync.WaitGroup
	errs := make(chan error, 2)
	for _, state := range []vacancyreview.State{vacancyreview.StateInteresting, vacancyreview.StateDismissed} {
		group.Add(1)
		go func(state vacancyreview.State) {
			defer group.Done()
			errs <- review.Record(ctx, vacancyreview.Action{VacancyID: id, State: state, OccurredAt: t2.Add(2 * time.Minute)})
		}(state)
	}
	group.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent review action: %v", err)
		}
	}
	events, err = reviewStore.ListReviewEvents(ctx, id)
	// The concurrent duplicate dismiss may legitimately be idempotent if it
	// obtains the row lock before the competing interesting transition. Both
	// orderings are safe; the important contract is that no malformed event is
	// written and at least the distinct post-change action is durable.
	if err != nil || len(events) < 3 || len(events) > 4 {
		t.Fatalf("concurrent events=%+v err=%v", events, err)
	}
	for _, event := range events {
		if event.FingerprintVersion != vacancy.FingerprintVersion {
			t.Fatalf("review event fingerprint version=%d, want %d", event.FingerprintVersion, vacancy.FingerprintVersion)
		}
	}
}
