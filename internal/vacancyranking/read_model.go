package vacancyranking

import (
	"context"
	"errors"
	"sort"
	"time"

	"hh-ai-responder/internal/ports"
	"hh-ai-responder/internal/vacancyreview"
)

// ReadModel computes ranking with bounded repository groups:
// vacancies, candidate, applications, freshness, and review states. It never
// performs one persistence read per vacancy and never marks anything seen.
type ReadModel struct {
	Vacancies    ports.VacancyReader
	Candidate    ports.CandidateReader
	Applications ports.ApplicationReader
	Reviews      vacancyreview.Store
	Now          func() time.Time
}

func (m ReadModel) ListRankedVacancies(ctx context.Context) ([]Result, error) {
	if m.Vacancies == nil || m.Candidate == nil {
		return nil, errors.New("vacancy ranking requires vacancy and candidate readers")
	}
	values, err := m.Vacancies.List(ctx, ports.VacancyQuery{})
	if err != nil {
		return nil, err
	}
	candidateValue, err := m.Candidate.CurrentCandidate(ctx)
	if err != nil {
		return nil, err
	}
	candidateContext, err := NewCandidateContext(candidateValue)
	if err != nil {
		return nil, err
	}
	ids := make([]int, 0, len(values))
	for _, value := range values {
		ids = append(ids, value.ID)
	}
	effective, err := m.effective(ctx, ids)
	if err != nil {
		return nil, err
	}
	now := time.Time{}
	if m.Now != nil {
		now = m.Now()
	}
	result := make([]Result, 0, len(values))
	evaluator := Evaluator{}
	for _, value := range values {
		result = append(result, evaluator.Evaluate(EvaluateInput{Vacancy: value, Candidate: candidateContext, Effective: effective[value.ID], Now: now}))
	}
	Sort(result)
	return result, nil
}

func (m ReadModel) EvaluateVacancy(ctx context.Context, id int) (Result, error) {
	all, err := m.ListRankedVacancies(ctx)
	if err != nil {
		return Result{}, err
	}
	for _, result := range all {
		if result.Vacancy.ID == id {
			return result, nil
		}
	}
	return Result{}, errors.New("vacancy ranking vacancy not found")
}

func (m ReadModel) effective(ctx context.Context, ids []int) (map[int]vacancyreview.EffectiveState, error) {
	result := make(map[int]vacancyreview.EffectiveState, len(ids))
	if m.Reviews != nil {
		values, err := vacancyreview.NewService(m.Reviews).ListEffective(ctx, ids, m.Applications)
		if err != nil {
			return nil, err
		}
		for _, value := range values {
			result[value.VacancyID] = value
		}
		return result, nil
	}
	applicationLinked := map[int]bool{}
	if m.Applications != nil {
		values, err := m.Applications.List(ctx)
		if err != nil {
			return nil, err
		}
		for _, value := range values {
			applicationLinked[value.VacancyID] = true
		}
	}
	for _, id := range ids {
		state := vacancyreview.EffectiveState{VacancyID: id, State: vacancyreview.StateUnseen, ApplicationLinked: applicationLinked[id]}
		if applicationLinked[id] {
			state.State = vacancyreview.StateApplied
		}
		result[id] = state
	}
	return result, nil
}

// Sort is the diagnostic comparator. P1.3 may define queue sections later;
// this function only establishes a reproducible base order.
func Sort(values []Result) {
	sort.SliceStable(values, func(i, j int) bool { return less(values[i], values[j]) })
}

func less(a, b Result) bool {
	if a.Rankable != b.Rankable {
		return a.Rankable
	}
	if eligibilityRank(a.Eligibility) != eligibilityRank(b.Eligibility) {
		return eligibilityRank(a.Eligibility) < eligibilityRank(b.Eligibility)
	}
	if fitRank(a.FitBand) != fitRank(b.FitBand) {
		return fitRank(a.FitBand) < fitRank(b.FitBand)
	}
	if a.BaseRankScore != b.BaseRankScore {
		return a.BaseRankScore > b.BaseRankScore
	}
	if confidenceRank(a.Confidence) != confidenceRank(b.Confidence) {
		return confidenceRank(a.Confidence) > confidenceRank(b.Confidence)
	}
	if reviewRank(a.ReviewState) != reviewRank(b.ReviewState) {
		return reviewRank(a.ReviewState) < reviewRank(b.ReviewState)
	}
	if knownTime(a.Freshness.LastSeenAt) != knownTime(b.Freshness.LastSeenAt) {
		return knownTime(a.Freshness.LastSeenAt).After(knownTime(b.Freshness.LastSeenAt))
	}
	if !a.Vacancy.PublishedAt.IsZero() != !b.Vacancy.PublishedAt.IsZero() {
		return !a.Vacancy.PublishedAt.IsZero()
	}
	if !a.Vacancy.PublishedAt.IsZero() && !b.Vacancy.PublishedAt.IsZero() && !a.Vacancy.PublishedAt.Equal(b.Vacancy.PublishedAt) {
		return a.Vacancy.PublishedAt.After(b.Vacancy.PublishedAt)
	}
	return a.Vacancy.ID < b.Vacancy.ID
}

func knownTime(value time.Time) time.Time {
	if value.IsZero() {
		return time.Time{}
	}
	return value
}
func eligibilityRank(value Eligibility) int {
	switch value {
	case EligibilityEligible:
		return 0
	case EligibilityReviewRequired:
		return 1
	case EligibilityIneligible:
		return 2
	default:
		return 3
	}
}
func fitRank(value FitBand) int {
	switch value {
	case FitCompatible:
		return 0
	case FitStretch:
		return 1
	case FitUnlikely:
		return 2
	default:
		return 3
	}
}
func confidenceRank(value Confidence) int {
	switch value {
	case ConfidenceHigh:
		return 3
	case ConfidenceMedium:
		return 2
	default:
		return 1
	}
}
func reviewRank(value vacancyreview.State) int {
	switch value {
	case vacancyreview.StateInteresting:
		return 0
	case vacancyreview.StateUnseen:
		return 1
	case vacancyreview.StateSeen:
		return 2
	default:
		return 3
	}
}
