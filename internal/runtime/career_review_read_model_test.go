package runtime

import (
	"testing"
	"time"

	"hh-ai-responder/internal/careeragent"
)

func TestCareerReviewReadModelKeepsCanonicalVacancyIDsAndClarifications(t *testing.T) {
	now := time.Now().UTC()
	preparation := careeragent.ApplicationPreparation{
		ID: "prep-42", VacancyID: 42, ResumeID: "resume-1", CandidateID: "candidate-1", CandidateVersion: 1,
		CandidateSnapshotHash: "snapshot", RouteStatus: careeragent.ResumeRouteMatch,
		Evidence: []byte(`{"route":"selected"}`), CoverLetter: "Здравствуйте!", Status: careeragent.PreparationStatusReady,
		CreatedAt: now, UpdatedAt: now,
	}
	preparation.CoverLetterHash = preparation.ContentHash()
	preparation.InputFingerprint = careeragent.PreparationInputFingerprint(preparation)

	queue, err := buildCareerReviewQueue(dashboardSnapshot{
		vacancies:      []Vacancy{{ID: 42, ExternalID: "hh-42", Name: "Python developer", DataCompleteness: DataCompletenessFull}},
		preparations:   []careeragent.ApplicationPreparation{preparation},
		clarifications: []CandidateClarificationRequest{{ID: "clarification-1", VacancyID: "42", Topic: "availability", Question: "When can you start?", Status: ClarificationPending}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(queue) != 1 || queue[0].VacancyID != 42 || queue[0].Snapshot.Vacancy.ID != 42 || queue[0].Snapshot.Preparation == nil {
		t.Fatalf("canonical references were not preserved: %+v", queue)
	}
	if queue[0].PipelineState != careeragent.ReviewStateClarificationNeeded || len(queue[0].Snapshot.KnowledgeRequests) != 1 {
		t.Fatalf("clarification did not outrank preparation: %+v", queue[0])
	}
}

func TestCareerReviewReadModelFailsClosedOnMalformedWorkflowEvidence(t *testing.T) {
	now := time.Now().UTC()
	preparation := careeragent.ApplicationPreparation{
		ID: "prep-43", VacancyID: 43, CandidateID: "candidate-1", CandidateVersion: 1,
		CandidateSnapshotHash: "snapshot", RouteStatus: careeragent.ResumeRouteReviewRequired,
		Evidence: []byte(`{"malformed"`), CoverLetter: "letter", Status: careeragent.PreparationStatusReviewRequired,
		CreatedAt: now, UpdatedAt: now,
	}
	preparation.CoverLetterHash = preparation.ContentHash()
	preparation.InputFingerprint = careeragent.PreparationInputFingerprint(preparation)
	if _, err := buildCareerReviewQueue(dashboardSnapshot{vacancies: []Vacancy{{ID: 43}}, preparations: []careeragent.ApplicationPreparation{preparation}}); err == nil {
		t.Fatal("malformed workflow evidence was downgraded to an empty queue item")
	}
}

func TestDashboardCareerSnapshotReportsUnavailableWorkflowExplicitly(t *testing.T) {
	snapshot := (dashboardSnapshot{careerError: "Career Agent workflow store is unavailable"}).career()
	if snapshot.CareerWorkflowUnavailable == "" {
		t.Fatal("workflow unavailability was hidden from dashboard snapshot")
	}
}
