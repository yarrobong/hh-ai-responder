package runtime

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"hh-ai-responder/internal/vacancyranking"
	"hh-ai-responder/internal/vacancyreview"
)

var errRankedQueueUnavailable = errors.New("ranked vacancy queue is unavailable")
var errInvalidRankedQueueFilter = errors.New("invalid ranked queue filter")

func (s *DashboardServer) rankedQueue(q url.Values) (vacancyranking.QueuePage, error) {
	if s.RankedQueue == nil {
		return vacancyranking.QueuePage{}, errRankedQueueUnavailable
	}
	filter, err := parseRankedQueueFilter(q)
	if err != nil {
		return vacancyranking.QueuePage{}, err
	}
	page, err := s.RankedQueue.List(context.Background(), filter)
	if err != nil {
		return vacancyranking.QueuePage{}, fmt.Errorf("%w: pagination", errInvalidRankedQueueFilter)
	}
	return page, nil
}

func (s *DashboardServer) rankedVacancyDetail(rawID string) (map[string]any, error) {
	if s.RankedQueue == nil {
		return nil, errRankedQueueUnavailable
	}
	id, err := strconv.Atoi(rawID)
	if err != nil || id <= 0 {
		return nil, ErrVacancyNotFound
	}
	v, err := s.Vacancies.Get(id)
	if err != nil {
		return nil, err
	}
	detail, err := s.RankedQueue.Detail(context.Background(), id)
	if err != nil {
		return nil, err
	}
	events := []vacancyreview.Event{}
	if s.VacancyReviews != nil {
		events, err = s.VacancyReviews.ListReviewEvents(context.Background(), id)
		if err != nil {
			return nil, err
		}
	}
	return map[string]any{"vacancy": v, "ranking": detail, "review_events": events}, nil
}

func (s *DashboardServer) recordVacancyReview(ctx context.Context, rawID, rawAction, reason string) (map[string]any, error) {
	if s.VacancyReviews == nil || s.RankedQueue == nil {
		return nil, errRankedQueueUnavailable
	}
	id, err := strconv.Atoi(rawID)
	if err != nil || id <= 0 {
		return nil, ErrVacancyNotFound
	}
	if _, err := s.Vacancies.Get(id); err != nil {
		return nil, err
	}
	reason = strings.TrimSpace(reason)
	if len(reason) > 500 {
		return nil, errors.New("vacancy review reason is limited to 500 characters")
	}
	service := vacancyreview.NewService(s.VacancyReviews)
	at := time.Now().UTC()
	switch rawAction {
	case "seen":
		err = service.MarkSeen(ctx, id, at)
	case "interesting":
		err = service.MarkInteresting(ctx, id, at)
	case "dismiss":
		err = service.Dismiss(ctx, id, reason, at)
	default:
		return nil, errors.New("invalid vacancy review action")
	}
	if err != nil {
		return nil, err
	}
	item, err := s.RankedQueue.Item(ctx, id)
	if err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "vacancy_id": id, "review_state": item.ReviewState, "changed_since_review": item.ChangedSinceReview, "section": item.Section, "item": item}, nil
}

func parseRankedQueueFilter(q url.Values) (vacancyranking.QueueFilter, error) {
	filter := vacancyranking.QueueFilter{
		Section:       vacancyranking.QueueSection(q.Get("section")),
		Eligibility:   vacancyranking.Eligibility(q.Get("eligibility")),
		FitBand:       vacancyranking.FitBand(q.Get("fit_band")),
		ReviewState:   vacancyreview.State(q.Get("review_state")),
		AnalysisState: vacancyranking.AnalysisState(q.Get("analysis_state")),
		Query:         q.Get("query"),
	}
	if filter.Query == "" {
		filter.Query = q.Get("search")
	}
	if filter.Section != "" && filter.Section != vacancyranking.SectionToReview && filter.Section != vacancyranking.SectionWorthAnotherLook && filter.Section != vacancyranking.SectionStretchManual && filter.Section != vacancyranking.SectionClosedExcluded {
		return vacancyranking.QueueFilter{}, fmt.Errorf("%w: section", errInvalidRankedQueueFilter)
	}
	if filter.Eligibility != "" && filter.Eligibility != vacancyranking.EligibilityEligible && filter.Eligibility != vacancyranking.EligibilityReviewRequired && filter.Eligibility != vacancyranking.EligibilityIneligible && filter.Eligibility != vacancyranking.EligibilityUnavailable {
		return vacancyranking.QueueFilter{}, fmt.Errorf("%w: eligibility", errInvalidRankedQueueFilter)
	}
	if filter.FitBand != "" && filter.FitBand != vacancyranking.FitCompatible && filter.FitBand != vacancyranking.FitStretch && filter.FitBand != vacancyranking.FitUnlikely && filter.FitBand != vacancyranking.FitHardIncompatible {
		return vacancyranking.QueueFilter{}, fmt.Errorf("%w: fit band", errInvalidRankedQueueFilter)
	}
	if filter.ReviewState != "" && filter.ReviewState != vacancyreview.StateUnseen && !filter.ReviewState.Valid() {
		return vacancyranking.QueueFilter{}, fmt.Errorf("%w: review state", errInvalidRankedQueueFilter)
	}
	if filter.AnalysisState != "" && filter.AnalysisState != vacancyranking.AnalysisComplete && filter.AnalysisState != vacancyranking.AnalysisPartial && filter.AnalysisState != vacancyranking.AnalysisInsufficientEvidence {
		return vacancyranking.QueueFilter{}, fmt.Errorf("%w: analysis state", errInvalidRankedQueueFilter)
	}
	for key, target := range map[string]*int{"limit": &filter.Limit, "offset": &filter.Offset} {
		if raw := strings.TrimSpace(q.Get(key)); raw != "" {
			value, err := strconv.Atoi(raw)
			if err != nil {
				return vacancyranking.QueueFilter{}, fmt.Errorf("%w: pagination", errInvalidRankedQueueFilter)
			}
			*target = value
		}
	}
	return filter, nil
}
