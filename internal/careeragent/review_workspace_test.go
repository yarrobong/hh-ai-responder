package careeragent

import (
	"strings"
	"testing"
	"time"
)

func TestReviewWorkspaceSnapshotProjectsCanonicalReferences(t *testing.T) {
	now := time.Now().UTC()
	preparation := ApplicationPreparation{
		ID: "prep-42", VacancyID: 42, ResumeID: "resume-1", CandidateID: "candidate-1", CandidateVersion: 2,
		CandidateSnapshotHash: "snapshot", RouteStatus: ResumeRouteMatch, Evidence: []byte(`{"matched":["python"]}`),
		KnowledgeRequests: []KnowledgeRequest{{Topic: "availability", Question: "When can you start?"}},
		CoverLetter:       "Здравствуйте!", Status: PreparationStatusReady, CreatedAt: now, UpdatedAt: now,
	}
	preparation.CoverLetterHash = preparation.ContentHash()
	preparation.InputFingerprint = PreparationInputFingerprint(preparation)
	snapshot, err := BuildVacancyReviewSnapshot(VacancyReviewInput{
		Vacancy:            ReviewVacancyReference{ID: 42, ExternalID: "hh-42", Title: "Python developer", Company: "Acme"},
		Route:              ResumeRoute{VacancyID: 42, ResumeID: "resume-1", Status: ResumeRouteMatch},
		DeterministicMatch: ReviewMatchEvidence{Known: true, Score: 88, Recommendation: "APPLY"},
		Preparation:        &preparation,
	})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Vacancy.ID != 42 || snapshot.Preparation == nil || snapshot.Preparation.VacancyID != 42 || snapshot.PipelineState != ReviewStateClarificationNeeded {
		t.Fatalf("unexpected snapshot projection: %+v", snapshot)
	}
	if len(snapshot.KnowledgeRequests) != 1 || !strings.Contains(snapshot.NextAction, "knowledge") {
		t.Fatalf("knowledge request/next action was lost: %+v", snapshot)
	}
	if snapshot.Advisory.ReviewOnly {
		t.Fatal("non-advisory snapshot was marked review-only")
	}
}

func TestReviewWorkspaceRejectsMalformedPreparationEvidence(t *testing.T) {
	now := time.Now().UTC()
	preparation := ApplicationPreparation{
		ID: "prep-42", VacancyID: 42, CandidateID: "candidate-1", CandidateVersion: 1,
		CandidateSnapshotHash: "snapshot", RouteStatus: ResumeRouteReviewRequired,
		Evidence: []byte(`{"broken"`), CoverLetter: "letter", CreatedAt: now, UpdatedAt: now,
		Status: PreparationStatusReviewRequired,
	}
	preparation.CoverLetterHash = preparation.ContentHash()
	preparation.InputFingerprint = PreparationInputFingerprint(preparation)
	if _, err := BuildVacancyReviewSnapshot(VacancyReviewInput{Vacancy: ReviewVacancyReference{ID: 42}, Preparation: &preparation}); err == nil {
		t.Fatal("malformed preparation evidence was silently projected")
	}
}

func TestReviewQueueOrdersSafetyBeforePublication(t *testing.T) {
	old := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	newer := old.AddDate(5, 0, 0)
	snapshots := []VacancyReviewSnapshot{
		{Vacancy: ReviewVacancyReference{ID: 10, Title: "Closed", PublishedAt: newer}, PipelineState: ReviewStateClosed},
		{Vacancy: ReviewVacancyReference{ID: 20, Title: "Clarification", PublishedAt: old}, PipelineState: ReviewStateClarificationNeeded},
		{Vacancy: ReviewVacancyReference{ID: 30, Title: "Ready", PublishedAt: old}, PipelineState: ReviewStateReady},
		{Vacancy: ReviewVacancyReference{ID: 40, Title: "AI review", PublishedAt: newer}, PipelineState: ReviewStateAIReview},
		{Vacancy: ReviewVacancyReference{ID: 50, Title: "Rejected", PublishedAt: newer}, PipelineState: ReviewStateRejected},
	}
	queue := BuildReviewQueue(snapshots)
	want := []int{20, 30, 40, 50, 10}
	for i, id := range want {
		if queue[i].VacancyID != id {
			t.Fatalf("queue[%d]=%d, want %d; queue=%+v", i, queue[i].VacancyID, id, queue)
		}
	}
}

func TestReviewQueueTieBreaksByVacancyID(t *testing.T) {
	when := time.Now().UTC()
	queue := BuildReviewQueue([]VacancyReviewSnapshot{
		{Vacancy: ReviewVacancyReference{ID: 9, PublishedAt: when}, PipelineState: ReviewStateNew},
		{Vacancy: ReviewVacancyReference{ID: 3, PublishedAt: when}, PipelineState: ReviewStateNew},
	})
	if queue[0].VacancyID != 3 || queue[1].VacancyID != 9 {
		t.Fatalf("queue tie order=%d,%d", queue[0].VacancyID, queue[1].VacancyID)
	}
}
