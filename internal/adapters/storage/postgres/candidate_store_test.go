package postgresstorage

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestCandidateStoreRequiresPoolBeforeCallback(t *testing.T) {
	called := false
	err := (*CandidateStore)(nil).WithTx(context.Background(), func(CandidateTx) error {
		called = true
		return nil
	})
	if err == nil || err.Error() != "postgres candidate store is not configured" {
		t.Fatalf("unexpected nil-store error: %v", err)
	}
	if called {
		t.Fatal("transaction callback ran for an unconfigured store")
	}
}

func TestCandidateStoreRequiresCallback(t *testing.T) {
	err := NewCandidateStore(&pgxpool.Pool{}).WithTx(context.Background(), nil)
	if err == nil || err.Error() != "candidate transaction callback is required" {
		t.Fatalf("unexpected nil-callback error: %v", err)
	}
}
