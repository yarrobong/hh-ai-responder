package runtime

import (
	"strings"
	"testing"
	"time"

	"hh-ai-responder/internal/careeragent"
)

func readyPreparationForVacancy(vacancyID int) careeragent.ApplicationPreparation {
	now := time.Date(2026, 9, 25, 10, 0, 0, 0, time.UTC)
	return careeragent.ApplicationPreparation{
		ID:                    "preparation-fixture",
		VacancyID:             vacancyID,
		CandidateID:           "candidate-fixture",
		CandidateVersion:      1,
		CandidateSnapshotHash: strings.Repeat("a", 64),
		RouteStatus:           careeragent.ResumeRouteMatch,
		InputFingerprint:      strings.Repeat("b", 64),
		Status:                careeragent.PreparationStatusReady,
		CreatedAt:             now,
		UpdatedAt:             now,
	}
}

func TestPreparationAttentionRequiresAuthoritativeApplicationEvidence(t *testing.T) {
	now := time.Date(2026, 9, 25, 10, 0, 0, 0, time.UTC)
	preparation := readyPreparationForVacancy(137609053)
	cases := []struct {
		name        string
		application JobApplication
		actionable  bool
	}{
		{
			name: "partial application is not enough",
			application: JobApplication{
				ID: "partial", VacancyID: preparation.VacancyID, Source: ApplicationSourceHH,
				Status: ApplicationApplied, ExternalID: "hh-response-1", Partial: true,
			},
			actionable: true,
		},
		{
			name: "local applied status without provider evidence is not enough",
			application: JobApplication{
				ID: "local-only", VacancyID: preparation.VacancyID, Source: ApplicationSourceHH,
				Status: ApplicationApplied, ExternalID: "hh-response-2",
			},
			actionable: true,
		},
		{
			name: "confirmed provider delivery suppresses attention",
			application: JobApplication{
				ID: "confirmed", VacancyID: preparation.VacancyID, Source: ApplicationSourceHH,
				Status: ApplicationApplied, ExternalID: "hh-response-3",
				HHMetadata: map[string]string{"delivery_confirmed": "true"},
			},
			actionable: false,
		},
		{
			name: "partial projection with confirmed provider delivery suppresses attention",
			application: JobApplication{
				ID: "partial-confirmed", VacancyID: preparation.VacancyID, Source: ApplicationSourceHH,
				Status: ApplicationApplied, ExternalID: "hh-response-3b", Partial: true,
				HHMetadata: map[string]string{"delivery_confirmed": "true"},
			},
			actionable: false,
		},
		{
			name: "reconciled provider application suppresses attention",
			application: JobApplication{
				ID: "reconciled", VacancyID: preparation.VacancyID, Source: ApplicationSourceHH,
				Status: ApplicationEmployerReplied, ExternalID: "hh-response-4",
				ReconciliationEvidence: []ReconciliationEvidence{{Method: "hh_negotiation_id", Source: "hh", Confidence: 1, ReconciledAt: now}},
			},
			actionable: false,
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if got := preparationAttentionActionable(preparation, []JobApplication{test.application}); got != test.actionable {
				t.Fatalf("actionable=%t, want %t", got, test.actionable)
			}
		})
	}
}

func TestPreparationAttentionIsSuppressedAfterConfirmedApplication(t *testing.T) {
	preparation := readyPreparationForVacancy(137609053)
	applications := []JobApplication{{
		ID: "app-1", VacancyID: 137609053, Source: ApplicationSourceHH,
		Status: ApplicationApplied, ExternalID: "hh-negotiation-1",
		HHMetadata: map[string]string{"delivery_confirmed": "true"},
	}}
	if preparationAttentionActionable(preparation, applications) {
		t.Fatal("confirmed application left preparation actionable")
	}
}

func TestUnappliedReadyPreparationRemainsActionable(t *testing.T) {
	preparation := readyPreparationForVacancy(137609053)
	if !preparationAttentionActionable(preparation, nil) {
		t.Fatal("unapplied ready preparation was suppressed")
	}
}

func TestPreparationAttentionRequiresExactVacancy(t *testing.T) {
	preparation := readyPreparationForVacancy(137609053)
	application := JobApplication{
		ID: "other-vacancy", VacancyID: 42, Source: ApplicationSourceHH,
		Status: ApplicationApplied, ExternalID: "hh-response-other",
		HHMetadata: map[string]string{"delivery_confirmed": "true"},
	}
	if !preparationAttentionActionable(preparation, []JobApplication{application}) {
		t.Fatal("application for another vacancy suppressed preparation")
	}
}

func TestBuildAttentionQueueSuppressesOnlyAuthoritativelyAppliedPreparation(t *testing.T) {
	server := dashboardTestServer(t)
	preparation := readyPreparationForVacancy(137609053)
	confirmed := JobApplication{
		ID: "confirmed", VacancyID: preparation.VacancyID, Source: ApplicationSourceHH,
		Status: ApplicationApplied, ExternalID: "hh-response-confirmed",
		HHMetadata: map[string]string{"delivery_confirmed": "true"},
	}
	queue, err := server.buildAttentionQueue(dashboardSnapshot{
		preparations: []careeragent.ApplicationPreparation{preparation},
		applications: []JobApplication{confirmed},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(queue) != 0 {
		t.Fatalf("authoritatively applied preparation remained in attention queue: %+v", queue)
	}

	confirmed.HHMetadata = nil
	queue, err = server.buildAttentionQueue(dashboardSnapshot{
		preparations: []careeragent.ApplicationPreparation{preparation},
		applications: []JobApplication{confirmed},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(queue) != 1 || queue[0].Type != dailyAttentionApplicationReady {
		t.Fatalf("unconfirmed preparation was not actionable: %+v", queue)
	}
}
