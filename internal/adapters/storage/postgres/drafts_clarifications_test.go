package postgresstorage

import (
	"context"
	"testing"

	"hh-ai-responder/internal/usecase/aidraft"
	"hh-ai-responder/internal/usecase/candidateacquisition"
)

func TestOperationalRepositoriesFailClosedWithoutDatabase(t *testing.T) {
	ctx := context.Background()
	drafts := NewAIDraftRepository(nil)
	if _, err := drafts.List(ctx); err == nil {
		t.Fatal("unconfigured draft repository returned success")
	}
	if _, err := drafts.Create(ctx, aidraft.Draft{}); err == nil {
		t.Fatal("unconfigured draft repository accepted a create")
	}
	clarifications := NewCandidateClarificationRepository(nil)
	if _, err := clarifications.List(ctx); err == nil {
		t.Fatal("unconfigured clarification repository returned success")
	}
	if _, _, err := clarifications.UpsertByIdentity(ctx, candidateacquisition.CandidateClarificationRequest{}); err == nil {
		t.Fatal("unconfigured clarification repository accepted an upsert")
	}
}
