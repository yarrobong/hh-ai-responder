package careeragent

import (
	"path/filepath"
	"testing"
	"time"
)

func TestFeedbackStoreRoundTripsSupportedFeedbackWithVacancyAndResumeIdentity(t *testing.T) {
	store := &FeedbackStore{Path: filepath.Join(t.TempDir(), "feedback.json")}
	types := []FeedbackType{FeedbackGoodMatch, FeedbackBadMatch, FeedbackWrongResume, FeedbackAccept, FeedbackReject}
	for index, kind := range types {
		if err := store.Add(Feedback{VacancyID: 100 + index, ResumeID: "hh-resume-test", Type: kind, Note: "compact operator note", CreatedAt: time.Unix(int64(index), 0).UTC()}); err != nil {
			t.Fatalf("add %s: %v", kind, err)
		}
	}
	items, err := store.List()
	if err != nil || len(items) != len(types) {
		t.Fatalf("feedback list=%+v err=%v", items, err)
	}
	for index, item := range items {
		if item.VacancyID != 100+index || item.ResumeID != "hh-resume-test" || item.ID == "" || !ValidFeedbackType(item.Type) {
			t.Fatalf("feedback provenance was lost: %+v", item)
		}
	}
	if err := store.Add(items[0]); err != nil {
		t.Fatalf("duplicate feedback should be idempotent: %v", err)
	}
	items, err = store.List()
	if err != nil || len(items) != len(types) {
		t.Fatalf("duplicate changed feedback store: len=%d err=%v", len(items), err)
	}
}
