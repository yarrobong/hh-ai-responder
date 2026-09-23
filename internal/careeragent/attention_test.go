package careeragent

import (
	"testing"
	"time"
)

func TestBuildAttentionQueueDeduplicatesAndSortsDeterministically(t *testing.T) {
	now := time.Date(2026, 9, 24, 9, 0, 0, 0, time.UTC)
	items := BuildAttentionQueue([]AttentionItem{
		{ID: "later", Priority: 2, UpdatedAt: now.Add(time.Hour)},
		{ID: "same", Priority: 1, UpdatedAt: now.Add(time.Hour), Summary: "old"},
		{ID: "same", Priority: 1, UpdatedAt: now, Summary: "fresh"},
		{ID: "urgent", Priority: 0, UpdatedAt: now.Add(time.Hour)},
	})
	if len(items) != 3 || items[0].ID != "urgent" || items[1].ID != "same" || items[2].ID != "later" || items[1].Summary != "fresh" {
		t.Fatalf("unexpected attention order: %+v", items)
	}
}
