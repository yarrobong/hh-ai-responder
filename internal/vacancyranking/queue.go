package vacancyranking

import (
	"context"
	"errors"
	"strings"
	"time"

	"hh-ai-responder/internal/vacancy"
	"hh-ai-responder/internal/vacancyreview"
)

// QueueSection is a workflow projection, not a second ranking dimension.
// Ordering inside every section remains the P1.2 Sort comparator.
type QueueSection string

const (
	SectionToReview         QueueSection = "to_review"
	SectionWorthAnotherLook QueueSection = "worth_another_look"
	SectionStretchManual    QueueSection = "stretch_manual_review"
	SectionClosedExcluded   QueueSection = "closed_excluded"
)

type QueueFilter struct {
	Section       QueueSection
	Eligibility   Eligibility
	FitBand       FitBand
	ReviewState   vacancyreview.State
	AnalysisState AnalysisState
	Query         string
	Limit         int
	Offset        int
}

type QueueCounts struct {
	Total               int `json:"total"`
	ToReview            int `json:"to_review"`
	WorthAnotherLook    int `json:"worth_another_look"`
	StretchManualReview int `json:"stretch_manual_review"`
	ClosedExcluded      int `json:"closed_excluded"`
	Rankable            int `json:"rankable"`
	Eligible            int `json:"eligible"`
	ReviewRequired      int `json:"review_required"`
	Ineligible          int `json:"ineligible"`
	Unavailable         int `json:"unavailable"`
	ApplicationLinked   int `json:"application_linked"`
}

type QueuePage struct {
	Items      []QueueItem `json:"items"`
	Counts     QueueCounts `json:"counts"`
	Total      int         `json:"total"`
	Limit      int         `json:"limit"`
	Offset     int         `json:"offset"`
	NextOffset *int        `json:"next_offset,omitempty"`
	HasMore    bool        `json:"has_more"`
	Algorithm  string      `json:"algorithm_version"`
}

// QueueItem intentionally does not embed vacancy.Vacancy: descriptions and
// other full-detail fields must stay on the detail endpoint.
type QueueItem struct {
	VacancyID          int                 `json:"vacancy_id"`
	ExternalID         string              `json:"external_id,omitempty"`
	Title              string              `json:"title"`
	Company            string              `json:"company,omitempty"`
	Salary             string              `json:"salary,omitempty"`
	SalaryCurrency     string              `json:"salary_currency,omitempty"`
	Location           string              `json:"location,omitempty"`
	WorkFormat         string              `json:"work_format,omitempty"`
	Eligibility        Eligibility         `json:"eligibility"`
	FitBand            FitBand             `json:"fit_band"`
	BaseRankScore      int                 `json:"base_rank_score"`
	Confidence         Confidence          `json:"confidence"`
	AnalysisState      AnalysisState       `json:"analysis_state"`
	PositiveReasons    []Reason            `json:"positive_reasons"`
	Concerns           []Reason            `json:"concerns"`
	Unknowns           []Reason            `json:"unknowns"`
	HardReasons        []Reason            `json:"hard_reasons"`
	UnknownCount       int                 `json:"unknown_count"`
	ReviewState        vacancyreview.State `json:"review_state"`
	ApplicationState   string              `json:"application_state"`
	FirstSeenAt        *time.Time          `json:"first_seen_at,omitempty"`
	LastSeenAt         *time.Time          `json:"last_seen_at,omitempty"`
	FreshnessKnown     bool                `json:"freshness_known"`
	FreshnessLabel     string              `json:"freshness_label"`
	ChangedSinceReview *bool               `json:"changed_since_review,omitempty"`
	Rankable           bool                `json:"rankable"`
	Section            QueueSection        `json:"section"`
	DetailPath         string              `json:"detail_path"`
}

// QueueService is the canonical UI projection over the P1.2 read model.
// It computes the full deterministic result set exactly once per request,
// then classifies, filters, counts, and paginates that snapshot.
type QueueService struct {
	ReadModel       ReadModel
	DefaultPageSize int
	MaxPageSize     int
	Now             func() time.Time
}

func (s QueueService) List(ctx context.Context, filter QueueFilter) (QueuePage, error) {
	values, err := s.ReadModel.ListRankedVacancies(ctx)
	if err != nil {
		return QueuePage{}, err
	}
	limit, offset, err := s.pageBounds(filter.Limit, filter.Offset)
	if err != nil {
		return QueuePage{}, err
	}
	counts := QueueCounts{Total: len(values)}
	all := make([]QueueItem, 0, len(values))
	for _, value := range values {
		section := SectionFor(value)
		counts.add(value, section)
		all = append(all, ProjectItem(value, section, s.now()))
	}
	filtered := make([]QueueItem, 0, len(all))
	for _, item := range all {
		if matchesQueueFilter(item, filter) {
			filtered = append(filtered, item)
		}
	}
	start := offset
	if start > len(filtered) {
		start = len(filtered)
	}
	end := start + limit
	if end > len(filtered) {
		end = len(filtered)
	}
	pageItems := append([]QueueItem(nil), filtered[start:end]...)
	page := QueuePage{Items: pageItems, Counts: counts, Total: len(filtered), Limit: limit, Offset: offset, Algorithm: AlgorithmVersion}
	page.HasMore = end < len(filtered)
	if page.HasMore {
		next := end
		page.NextOffset = &next
	}
	return page, nil
}

func (s QueueService) Item(ctx context.Context, id int) (QueueItem, error) {
	result, err := s.ReadModel.EvaluateVacancy(ctx, id)
	if err != nil {
		return QueueItem{}, err
	}
	return ProjectItem(result, SectionFor(result), s.now()), nil
}

func (s QueueService) Detail(ctx context.Context, id int) (Detail, error) {
	result, err := s.ReadModel.EvaluateVacancy(ctx, id)
	if err != nil {
		return Detail{}, err
	}
	return Detail{QueueItem: ProjectItem(result, SectionFor(result), s.now()), PositiveReasons: result.PositiveReasons, Concerns: result.Concerns, Unknowns: result.Unknowns, HardReasons: result.HardReasons, Legacy: result.Legacy}, nil
}

type Detail struct {
	QueueItem
	PositiveReasons []Reason       `json:"positive_reasons_all"`
	Concerns        []Reason       `json:"concerns_all"`
	Unknowns        []Reason       `json:"unknowns_all"`
	HardReasons     []Reason       `json:"hard_reasons_all"`
	Legacy          LegacyEvidence `json:"legacy_match"`
}

func (s QueueService) pageBounds(limit, offset int) (int, int, error) {
	if limit == 0 {
		limit = s.DefaultPageSize
		if limit == 0 {
			limit = 25
		}
	}
	max := s.MaxPageSize
	if max == 0 {
		max = 100
	}
	if limit < 1 || limit > max || offset < 0 {
		return 0, 0, errors.New("invalid ranked queue pagination")
	}
	return limit, offset, nil
}

func (s QueueService) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	if s.ReadModel.Now != nil {
		return s.ReadModel.Now().UTC()
	}
	return time.Now().UTC()
}

func SectionFor(value Result) QueueSection {
	if !value.Rankable || value.Eligibility == EligibilityIneligible || value.Eligibility == EligibilityUnavailable {
		return SectionClosedExcluded
	}
	if value.ReviewState == vacancyreview.StateInteresting || value.ReviewState == vacancyreview.StateSeen || value.ReviewState == vacancyreview.StateDismissed && value.ChangedSinceReview != nil && *value.ChangedSinceReview {
		return SectionWorthAnotherLook
	}
	if value.FitBand == FitStretch || value.FitBand == FitUnlikely || value.Eligibility == EligibilityReviewRequired && value.FitBand != FitCompatible {
		return SectionStretchManual
	}
	return SectionToReview
}

func ProjectItem(value Result, section QueueSection, now time.Time) QueueItem {
	v := value.Vacancy
	item := QueueItem{
		VacancyID: v.ID, ExternalID: v.ExternalID, Title: firstNonEmpty(v.Title, v.Name), Company: v.Company.Name,
		Salary: firstNonEmpty(v.Salary, vacancyCompensation(v)), SalaryCurrency: v.SalaryCurrency, Location: firstNonEmpty(v.Location, v.Area.Name), WorkFormat: firstNonEmpty(v.WorkFormat, v.WorkSchedule),
		Eligibility: value.Eligibility, FitBand: value.FitBand, BaseRankScore: value.BaseRankScore, Confidence: value.Confidence, AnalysisState: value.AnalysisState,
		PositiveReasons: boundedReasons(value.PositiveReasons, 3), Concerns: boundedReasons(value.Concerns, 2), Unknowns: boundedReasons(value.Unknowns, 3), HardReasons: boundedReasons(value.HardReasons, 3), UnknownCount: len(value.Unknowns),
		ReviewState: value.ReviewState, ApplicationState: applicationState(value), FreshnessKnown: !value.Freshness.FirstSeenAt.IsZero() || !value.Freshness.LastSeenAt.IsZero(), ChangedSinceReview: value.ChangedSinceReview, Rankable: value.Rankable,
		Section: section, DetailPath: "/vacancies/" + itoa(v.ID), FreshnessLabel: freshnessLabel(value.Freshness.FirstSeenAt, now),
	}
	if !value.Freshness.FirstSeenAt.IsZero() {
		at := value.Freshness.FirstSeenAt
		item.FirstSeenAt = &at
	}
	if !value.Freshness.LastSeenAt.IsZero() {
		at := value.Freshness.LastSeenAt
		item.LastSeenAt = &at
	}
	return item
}

func matchesQueueFilter(item QueueItem, filter QueueFilter) bool {
	if filter.Section != "" && item.Section != filter.Section || filter.Eligibility != "" && item.Eligibility != filter.Eligibility || filter.FitBand != "" && item.FitBand != filter.FitBand || filter.ReviewState != "" && item.ReviewState != filter.ReviewState || filter.AnalysisState != "" && item.AnalysisState != filter.AnalysisState {
		return false
	}
	query := strings.ToLower(strings.TrimSpace(filter.Query))
	if query != "" && !strings.Contains(strings.ToLower(strings.Join([]string{item.Title, item.Company, item.Location, item.WorkFormat}, " ")), query) {
		return false
	}
	return true
}

func (c *QueueCounts) add(value Result, section QueueSection) {
	switch section {
	case SectionToReview:
		c.ToReview++
	case SectionWorthAnotherLook:
		c.WorthAnotherLook++
	case SectionStretchManual:
		c.StretchManualReview++
	case SectionClosedExcluded:
		c.ClosedExcluded++
	}
	if value.Rankable {
		c.Rankable++
	}
	switch value.Eligibility {
	case EligibilityEligible:
		c.Eligible++
	case EligibilityReviewRequired:
		c.ReviewRequired++
	case EligibilityIneligible:
		c.Ineligible++
	case EligibilityUnavailable:
		c.Unavailable++
	}
	if value.ApplicationLinked {
		c.ApplicationLinked++
	}
}

func applicationState(value Result) string {
	if value.ApplicationLinked || value.ReviewState == vacancyreview.StateApplied {
		return "application_linked"
	}
	if value.ReviewState == vacancyreview.StatePrepared {
		return "prepared"
	}
	return "none"
}

func freshnessLabel(first, now time.Time) string {
	if first.IsZero() {
		return "freshness unknown"
	}
	if now.IsZero() {
		return "first seen date known"
	}
	days := int(now.Truncate(24*time.Hour).Sub(first.Truncate(24*time.Hour)) / (24 * time.Hour))
	if days <= 0 {
		return "first seen today"
	}
	return "first seen " + itoa(days) + "d ago"
}

func boundedReasons(values []Reason, limit int) []Reason {
	if len(values) > limit {
		values = values[:limit]
	}
	return append([]Reason(nil), values...)
}

func vacancyCompensation(value vacancy.Vacancy) string {
	return vacancy.FormatCompensation(&value.Compensation)
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	negative := value < 0
	if negative {
		value = -value
	}
	var buf [20]byte
	i := len(buf)
	for value > 0 {
		i--
		buf[i] = byte('0' + value%10)
		value /= 10
	}
	if negative {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
