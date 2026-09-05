package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func knownVacancyPreflight() VacancyPreflight {
	return VacancyPreflight{
		VacancyID:             1,
		ArchivedKnown:         true,
		AlreadyRespondedKnown: true,
		TestPresentKnown:      true,
		LetterRequiredKnown:   true,
		CanApplyKnown:         true,
		CanApply:              true,
	}
}

func TestVacancyPreflightDecisionFailsClosed(t *testing.T) {
	tests := []struct {
		name       string
		mutate     func(*VacancyPreflight)
		want       VacancyDecision
		wantReason string
	}{
		{name: "all safe known values", want: VacancyMatch, wantReason: ""},
		{name: "archived=true", mutate: func(p *VacancyPreflight) { p.Archived = true }, want: VacancyReject, wantReason: "vacancy is archived"},
		{name: "already responded", mutate: func(p *VacancyPreflight) { p.AlreadyResponded = true }, want: VacancyReject, wantReason: "already responded according to vacancy detail"},
		{name: "archived unknown + already responded", mutate: func(p *VacancyPreflight) {
			p.ArchivedKnown = false
			p.AlreadyResponded = true
		}, want: VacancyReject, wantReason: "already responded according to vacancy detail"},
		{name: "archived unknown + cannot apply", mutate: func(p *VacancyPreflight) {
			p.ArchivedKnown = false
			p.CanApply = false
		}, want: VacancyReject, wantReason: "vacancy does not allow an application"},
		{name: "archived unknown without known blocker", mutate: func(p *VacancyPreflight) {
			p.ArchivedKnown = false
		}, want: VacancyReviewRequired, wantReason: "archived state is unknown"},
		{name: "already responded unknown", mutate: func(p *VacancyPreflight) { p.AlreadyRespondedKnown = false }, want: VacancyReviewRequired, wantReason: "already-responded state is unknown"},
		{name: "test present", mutate: func(p *VacancyPreflight) { p.TestPresent = true }, want: VacancyReviewRequired, wantReason: "vacancy has a test; safe live test flow is not enabled"},
		{name: "test unknown", mutate: func(p *VacancyPreflight) { p.TestPresentKnown = false }, want: VacancyReviewRequired, wantReason: "test state is unknown"},
		{name: "letter unknown", mutate: func(p *VacancyPreflight) { p.LetterRequiredKnown = false }, want: VacancyReviewRequired, wantReason: "cover-letter requirement is unknown"},
		{name: "cannot apply", mutate: func(p *VacancyPreflight) { p.CanApply = false }, want: VacancyReject, wantReason: "vacancy does not allow an application"},
		{name: "can apply unknown", mutate: func(p *VacancyPreflight) { p.CanApplyKnown = false }, want: VacancyReviewRequired, wantReason: "application availability is unknown"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			preflight := knownVacancyPreflight()
			if test.mutate != nil {
				test.mutate(&preflight)
			}
			got, reason := vacancyPreflightDecision(preflight)
			if got != test.want {
				t.Fatalf("decision: got %s, want %s (reason=%q)", got, test.want, reason)
			}
			if reason != test.wantReason {
				t.Fatalf("reason: got %q, want %q", reason, test.wantReason)
			}
		})
	}
}

func TestLocalStructuredHardRequirements(t *testing.T) {
	candidate := CandidateContext{Location: "Екатеринбург"}
	tests := []struct {
		name             string
		preflight        VacancyPreflight
		wantCategory     string
		wantCount        int
		wantStatus       string
		wantLocationHard bool
	}{
		{
			name:         "experience 3-6 years is local unknown",
			preflight:    VacancyPreflight{WorkExperienceKnown: true, WorkExperience: "Опыт 3-6 лет"},
			wantCategory: hardRequirementCategoryExperienceYears,
			wantCount:    1,
			wantStatus:   hardRequirementStatusUnknown,
		},
		{
			name:      "no experience does not create requirement",
			preflight: VacancyPreflight{WorkExperienceKnown: true, WorkExperience: "Без опыта"},
			wantCount: 0,
		},
		{
			name:             "onsite different city is unknown",
			preflight:        VacancyPreflight{AreaKnown: true, Area: "Ташкент", WorkScheduleKnown: true, WorkSchedule: "Офис"},
			wantCategory:     hardRequirementCategoryLocation,
			wantCount:        1,
			wantStatus:       hardRequirementStatusUnknown,
			wantLocationHard: true,
		},
		{
			name:      "remote different area has no location blocker",
			preflight: VacancyPreflight{AreaKnown: true, Area: "Ташкент", WorkScheduleKnown: true, WorkSchedule: "Можно удалённо"},
			wantCount: 0,
		},
		{
			name:             "hybrid different city is unknown",
			preflight:        VacancyPreflight{AreaKnown: true, Area: "Ташкент", WorkScheduleKnown: true, WorkSchedule: "Гибридный формат"},
			wantCategory:     hardRequirementCategoryLocation,
			wantCount:        1,
			wantStatus:       hardRequirementStatusUnknown,
			wantLocationHard: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := localStructuredHardRequirements(test.preflight, candidate)
			if len(got) != test.wantCount {
				t.Fatalf("requirements: got %+v, want %d", got, test.wantCount)
			}
			if test.wantCount == 0 {
				return
			}
			if got[0].Category != test.wantCategory || got[0].Status != test.wantStatus {
				t.Fatalf("requirement: got %+v", got[0])
			}
			if test.wantLocationHard && got[0].Category != hardRequirementCategoryLocation {
				t.Fatalf("location requirement missing: %+v", got)
			}
		})
	}
}

func TestMergeHardRequirementsKeepsLocalRequirementWhenAIIsEmpty(t *testing.T) {
	local := localStructuredHardRequirements(VacancyPreflight{
		WorkExperienceKnown: true,
		WorkExperience:      "Опыт 3-6 лет",
	}, CandidateContext{})
	merged := mergeHardRequirements(local, nil)
	if len(merged) != 1 || merged[0].Category != hardRequirementCategoryExperienceYears {
		t.Fatalf("local structured requirement was lost: %+v", merged)
	}
}

func TestParseVacancyPreflightUsesEmbeddedStateAndKnownFalse(t *testing.T) {
	body := []byte(`{"redirectConfig":{"archived":false,"alreadyResponded":false,"testPresent":false,"responseLetterRequired":true,"canApply":true,"area":{"name":"Ташкент"},"workSchedule":"Офис","workExperience":"Опыт 3-6 лет"}}`)
	preflight, err := parseVacancyPreflight(body, Vacancy{ID: 42}, "https://example.test/applicant/vacancy_response?vacancyId=42")
	if err != nil {
		t.Fatal(err)
	}
	if preflight.Archived || !preflight.ArchivedKnown || preflight.AlreadyResponded || !preflight.AlreadyRespondedKnown {
		t.Fatalf("false state was not preserved as known: %+v", preflight)
	}
	if !preflight.LetterRequired || !preflight.LetterRequiredKnown || !preflight.CanApply || !preflight.CanApplyKnown {
		t.Fatalf("critical true state was not parsed: %+v", preflight)
	}
	if preflight.Area != "Ташкент" || preflight.WorkSchedule != "Офис" || preflight.WorkExperience != "Опыт 3-6 лет" {
		t.Fatalf("structured fields were not parsed: %+v", preflight)
	}
}

func TestVacancyPreflightPerformsOnlyGET(t *testing.T) {
	previousLogger := logger
	logger = NewLogger(io.Discard, LevelDebug)
	t.Cleanup(func() { logger = previousLogger })

	var writes atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			writes.Add(1)
		}
		_, _ = io.WriteString(w, `{"redirectConfig":{"archived":false,"alreadyResponded":false,"testPresent":false,"responseLetterRequired":false,"canApply":true}}`)
	}))
	defer server.Close()

	ctx := context.Background()
	r := &HHAIResponder{ctx: ctx, baseURL: mustURL(t, server.URL), requester: NewHHRequester(ctx, server.Client(), 0)}
	preflight, err := r.GetVacancyPreflight(Vacancy{ID: 7})
	if err != nil {
		t.Fatal(err)
	}
	if !preflight.CanApply || writes.Load() != 0 {
		t.Fatalf("preflight was not read-only: %+v writes=%d", preflight, writes.Load())
	}
}

func TestBlockedLiveApplicationDoesNotPOST(t *testing.T) {
	var posts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			posts.Add(1)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	ctx := context.Background()
	r := &HHAIResponder{
		ctx:       ctx,
		baseURL:   mustURL(t, server.URL),
		requester: NewHHRequester(ctx, server.Client(), 0),
		autoApply: true,
		preflightCache: map[int]VacancyPreflight{
			1: func() VacancyPreflight {
				p := knownVacancyPreflight()
				p.AlreadyResponded = true
				return p
			}(),
		},
	}
	if _, err := r.ApplyVacancy(1, server.URL, "letter"); err == nil || !strings.Contains(err.Error(), "already responded") {
		t.Fatalf("already-responded vacancy was not blocked: %v", err)
	}
	if posts.Load() != 0 {
		t.Fatalf("blocked live application issued %d POST requests", posts.Load())
	}
}
