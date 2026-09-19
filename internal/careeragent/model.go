// Package careeragent contains deterministic planning, routing, discovery and
// operator-feedback primitives for the Career Agent flow. It deliberately has
// no HH transport, AI, or persistence side effects.
package careeragent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"math"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"hh-ai-responder/internal/candidate"
)

type ResumeProfile struct {
	ID              string         `json:"id"`
	HHID            int64          `json:"hh_id,omitempty"`
	Hash            string         `json:"hash,omitempty"`
	Title           string         `json:"title"`
	DesiredRole     string         `json:"desired_role,omitempty"`
	Skills          []string       `json:"skills,omitempty"`
	Experience      string         `json:"experience,omitempty"`
	Location        string         `json:"location,omitempty"`
	Salary          string         `json:"salary,omitempty"`
	Employment      string         `json:"employment,omitempty"`
	Schedule        string         `json:"schedule,omitempty"`
	SearchHints     []string       `json:"search_hints,omitempty"`
	IncludeKeywords []string       `json:"include_keywords,omitempty"`
	ExcludeKeywords []string       `json:"exclude_keywords,omitempty"`
	Identity        ResumeIdentity `json:"identity,omitempty"`
	Enabled         bool           `json:"enabled"`
}

// ResumeIdentity is a deterministic, explainable summary of the resume's
// actual title and HH skills. It is a routing aid, not additional candidate
// knowledge: every value is derived from fields already present in the
// profile.
type ResumeIdentity struct {
	PrimaryRoles            []string     `json:"primary_roles,omitempty"`
	StrongSkills            []string     `json:"strong_skills,omitempty"`
	SupportingSkills        []string     `json:"supporting_skills,omitempty"`
	DomainSignals           []string     `json:"domain_signals,omitempty"`
	NegativeSignals         []string     `json:"negative_signals,omitempty"`
	PrimaryRoleFamilies     []RoleFamily `json:"primary_role_families,omitempty"`
	SecondaryRoleFamilies   []RoleFamily `json:"secondary_role_families,omitempty"`
	StrongPositiveAnchors   []string     `json:"strong_positive_anchors,omitempty"`
	GenericAnchors          []string     `json:"generic_anchors,omitempty"`
	NegativeMismatchAnchors []string     `json:"negative_mismatch_anchors,omitempty"`
	CoreSkills              []string     `json:"core_skills,omitempty"`
	AdjacentSkills          []string     `json:"adjacent_skills,omitempty"`
}

// StableResumeID is independent of list order and therefore safe to use in
// persisted route decisions. Hash is preferred, then the HH numeric id, then
// a normalized title fallback for old fixtures.
func StableResumeID(hash string, hhID int64, title string) string {
	if value := strings.TrimSpace(hash); value != "" {
		return "hh-resume-" + value
	}
	if hhID > 0 {
		return "hh-resume-id-" + formatInt(hhID)
	}
	seed := normalizeText(title)
	if seed == "" {
		seed = "unknown"
	}
	sum := sha256.Sum256([]byte(seed))
	return "hh-resume-generated-" + hex.EncodeToString(sum[:8])
}

func NormalizeResumes(values []candidate.ResumeItem) []ResumeProfile {
	result := make([]ResumeProfile, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		id := StableResumeID(value.Hash, value.Id, value.Title)
		if seen[id] {
			continue
		}
		seen[id] = true
		profile := ResumeProfile{
			ID: id, HHID: value.Id, Hash: strings.TrimSpace(value.Hash),
			Title: strings.TrimSpace(value.Title), DesiredRole: strings.TrimSpace(value.Title),
			Skills: splitList(value.Skills), Location: strings.TrimSpace(value.Area), Salary: strings.TrimSpace(value.Salary), Enabled: true,
		}
		profile.Identity = DeriveResumeIdentity(profile)
		result = append(result, profile)
	}
	sort.SliceStable(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}

type RegistryOverrides struct {
	Enabled map[string]bool `json:"enabled,omitempty"`
}

func ApplyRegistryOverrides(profiles []ResumeProfile, overrides RegistryOverrides) []ResumeProfile {
	result := append([]ResumeProfile(nil), profiles...)
	for i := range result {
		if value, ok := overrides.Enabled[result[i].ID]; ok {
			result[i].Enabled = value
		}
	}
	return result
}

type CandidateSignals struct {
	Roles             []string
	Skills            []string
	PreferredLocation string
}

type SearchConstraints struct {
	MaxProfiles      int
	SearchPeriodDays int
	Area             string
	IncludeKeywords  []string
	ExcludeKeywords  []string
}

type SearchProfile struct {
	ID               string     `json:"id"`
	ResumeID         string     `json:"resume_id"`
	ResumeTitle      string     `json:"resume_title"`
	Query            string     `json:"query"`
	Reason           string     `json:"reason"`
	SearchPeriodDays int        `json:"search_period_days"`
	Params           url.Values `json:"params"`
}

func PlanSearches(resumes []ResumeProfile, signals CandidateSignals, constraints SearchConstraints) []SearchProfile {
	max := constraints.MaxProfiles
	if max <= 0 {
		max = 16
	}
	period := constraints.SearchPeriodDays
	if period <= 0 {
		period = 7
	}
	type plannedTerm struct {
		resume ResumeProfile
		query  string
		reason string
	}
	queues := make([][]plannedTerm, 0, len(resumes))
	for _, resume := range resumes {
		if !resume.Enabled {
			continue
		}
		terms := make([]plannedTerm, 0)
		add := func(values []string, reason string) {
			for _, raw := range values {
				query := normalizeQuery(raw)
				if query == "" || isNoiseQuery(query) || containsKeyword(query, constraints.ExcludeKeywords) {
					continue
				}
				duplicate := false
				for _, existing := range terms {
					if existing.query == query {
						duplicate = true
						break
					}
				}
				if !duplicate {
					terms = append(terms, plannedTerm{resume: resume, query: query, reason: reason})
				}
			}
		}
		add(resume.SearchHints, "resume search hint")
		add([]string{resume.DesiredRole, resume.Title}, "resume role/title")
		add(signals.Roles, "candidate role signal")
		add(roleExpansions(resume.Title, resume.DesiredRole), "deterministic role expansion")
		if len(terms) == 0 {
			add(signals.Skills, "candidate skill signal")
		}
		queues = append(queues, terms)
	}
	// Round-robin over enabled resumes. This keeps a small profile budget from
	// silently becoming "all searches for the first resume".
	result := make([]SearchProfile, 0, max)
	seen := map[string]bool{}
	for round := 0; len(result) < max; round++ {
		added := 0
		for _, queue := range queues {
			if round >= len(queue) || len(result) >= max {
				continue
			}
			term := queue[round]
			seenKey := term.resume.ID + "\x00" + term.query
			if seen[seenKey] {
				continue
			}
			seen[seenKey] = true
			params := url.Values{}
			params.Set("text", term.query)
			if constraints.Area != "" {
				params.Set("area", strings.TrimSpace(constraints.Area))
			}
			if term.resume.Hash != "" {
				params.Set("resume", term.resume.Hash)
			}
			params.Set("order_by", "publication_time")
			params.Set("search_period", strconv.Itoa(period))
			params.Set("items_on_page", "50")
			if len(constraints.IncludeKeywords) > 0 {
				params.Set("career_agent_include", strings.Join(normalizeKeywords(constraints.IncludeKeywords), ","))
			}
			if len(constraints.ExcludeKeywords) > 0 {
				params.Set("career_agent_exclude", strings.Join(normalizeKeywords(constraints.ExcludeKeywords), ","))
			}
			id := stableSearchID(term.resume.ID, term.query)
			result = append(result, SearchProfile{ID: id, ResumeID: term.resume.ID, ResumeTitle: term.resume.Title, Query: term.query, Reason: term.reason, SearchPeriodDays: period, Params: params})
			added++
		}
		if added == 0 {
			break
		}
	}
	return result
}

type VacancyInput struct {
	ID                int
	Title             string
	Description       string
	RequiredSkills    []string
	KeySkills         []string
	ProfessionalRoles []string
	Experience        string
	Employment        string
	Schedule          string
	Salary            string
	Location          string
	WorkFormat        string
	SearchProfiles    []SearchProfileEvidence
	DetailAvailable   bool
}

// SearchProfileEvidence is intentionally weak routing evidence. A vacancy
// found by a resume-specific search is not thereby assigned to that resume.
type SearchProfileEvidence struct {
	ID       string `json:"id,omitempty"`
	ResumeID string `json:"resume_id,omitempty"`
	Label    string `json:"label,omitempty"`
}

type ResumeScore struct {
	ResumeID              string       `json:"resume_id"`
	Title                 string       `json:"title"`
	Score                 int          `json:"score"`
	FitScore              int          `json:"fit_score,omitempty"`
	ProvenanceScore       int          `json:"provenance_score,omitempty"`
	RoleScore             int          `json:"role_score,omitempty"`
	SkillScore            int          `json:"skill_score,omitempty"`
	DomainScore           int          `json:"domain_score,omitempty"`
	ExperienceScore       int          `json:"experience_score,omitempty"`
	GenericEvidenceScore  int          `json:"generic_evidence_score,omitempty"`
	RawFit                int          `json:"raw_fit,omitempty"`
	NormalizedScore       int          `json:"normalized_score,omitempty"`
	SpecificMatches       []string     `json:"specific_matches,omitempty"`
	PartialMatches        []string     `json:"partial_matches,omitempty"`
	Reasons               []string     `json:"reasons,omitempty"`
	HardBlockers          []string     `json:"hard_blockers,omitempty"`
	MatchedRoleFamilies   []RoleFamily `json:"matched_role_families,omitempty"`
	StrongRoleEvidence    []string     `json:"strong_role_evidence,omitempty"`
	SpecificEvidenceCount int          `json:"specific_evidence_count,omitempty"`
	GenericEvidenceRatio  float64      `json:"generic_evidence_ratio,omitempty"`
	MismatchSignals       []string     `json:"mismatch_signals,omitempty"`
}

type RouteDecision struct {
	VacancyID             int                 `json:"vacancy_id"`
	Status                string              `json:"status"`
	SelectedResumeID      string              `json:"selected_resume_id,omitempty"`
	SelectedResumeTitle   string              `json:"selected_resume_title,omitempty"`
	Score                 int                 `json:"score"`
	TopRawScore           int                 `json:"top_raw_score,omitempty"`
	SecondRawScore        int                 `json:"second_raw_score,omitempty"`
	TopNormalizedScore    int                 `json:"top_normalized_score,omitempty"`
	SecondNormalizedScore int                 `json:"second_normalized_score,omitempty"`
	AbsoluteMargin        int                 `json:"absolute_margin,omitempty"`
	RelativeMargin        float64             `json:"relative_margin,omitempty"`
	AlternativeScores     []ResumeScore       `json:"alternative_resume_scores,omitempty"`
	Reasons               []string            `json:"reasons,omitempty"`
	Confidence            string              `json:"confidence"`
	HardRequirements      []RequirementState  `json:"hard_requirements,omitempty"`
	HardBlockers          []string            `json:"hard_blockers,omitempty"`
	ReasonCode            string              `json:"reason_code,omitempty"`
	RoleEvidence          VacancyRoleEvidence `json:"role_evidence,omitempty"`
}

type PreliminaryRouteDecision struct {
	VacancyID       int           `json:"vacancy_id"`
	Status          string        `json:"status"`
	ReasonCode      string        `json:"reason_code"`
	Reasons         []string      `json:"reasons,omitempty"`
	TopCandidates   []ResumeScore `json:"top_candidates,omitempty"`
	DetailAvailable bool          `json:"detail_available"`
}

const (
	PreliminaryClearRoute           = "CLEAR_ROUTE"
	PreliminaryNeedsDetail          = "NEEDS_DETAIL"
	PreliminaryObviousReject        = "OBVIOUS_REJECT"
	PreliminaryNoResume             = "NO_ENABLED_RESUME"
	RouteReasonNeedsDetail          = "ROUTE_NEEDS_DETAIL"
	RouteReasonAmbiguous            = "ROUTE_AMBIGUOUS"
	RouteReasonLowEvidence          = "ROUTE_LOW_EVIDENCE"
	RouteReasonOutOfScope           = "ROLE_OUT_OF_SCOPE"
	RouteReasonNoSuitable           = "NO_SUITABLE_RESUME"
	RouteReasonAmbiguousAfterDetail = RouteReasonAmbiguous
	RouteReasonUnknownHard          = "UNKNOWN_HARD_REQUIREMENT"
	RouteReasonNoStrong             = RouteReasonNoSuitable
	RouteReasonSelected             = "ROUTE_SELECTED"
)

const (
	RouteSelected       = "SELECTED"
	RouteReviewRequired = "REVIEW_REQUIRED"
	RouteNoResume       = "NO_ENABLED_RESUME"
	ConfidenceHigh      = "HIGH"
	ConfidenceMedium    = "MEDIUM"
	ConfidenceLow       = "LOW"
)

type RequirementState struct {
	Requirement string `json:"requirement"`
	Status      string `json:"status"`
}

func RouteResume(vacancy VacancyInput, resumes []ResumeProfile) RouteDecision {
	evidence := ClassifyVacancyRole(vacancy, resumes)
	candidates := make([]ResumeScore, 0)
	for _, resume := range resumes {
		if resume.Enabled {
			candidates = append(candidates, scoreResume(vacancy, resume))
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].RawFit != candidates[j].RawFit {
			return candidates[i].RawFit > candidates[j].RawFit
		}
		if candidates[i].Score != candidates[j].Score {
			return candidates[i].Score > candidates[j].Score
		}
		return candidates[i].ResumeID < candidates[j].ResumeID
	})
	return finalizeRouteDecision(vacancy, resumes, evidence, candidates)
}

// PreliminaryRouteResume is deliberately non-terminal for incomplete search
// cards. It only rejects an obvious mismatch when the card itself contains a
// reliable hard blocker, or when a caller has supplied no provenance and no
// relevance signal at all. Otherwise the caller must obtain provider detail
// and call RouteResume again.
func PreliminaryRouteResume(vacancy VacancyInput, resumes []ResumeProfile) PreliminaryRouteDecision {
	decision := PreliminaryRouteDecision{VacancyID: vacancy.ID, Status: PreliminaryNeedsDetail, ReasonCode: RouteReasonNeedsDetail, DetailAvailable: vacancy.DetailAvailable}
	final := RouteResume(vacancy, resumes)
	decision.TopCandidates = append([]ResumeScore(nil), final.AlternativeScores...)
	if len(final.AlternativeScores) > 0 && len(final.AlternativeScores[0].HardBlockers) > 0 {
		allBlocked := true
		for _, candidate := range final.AlternativeScores {
			if len(candidate.HardBlockers) == 0 {
				allBlocked = false
				break
			}
		}
		if allBlocked {
			decision.Status, decision.ReasonCode = PreliminaryObviousReject, RouteReasonNoStrong
			decision.Reasons = append(decision.Reasons, "all enabled resumes have explicit hard incompatibilities")
			return decision
		}
	}
	if final.Status == RouteNoResume {
		decision.Status, decision.ReasonCode = PreliminaryNoResume, RouteReasonNoStrong
		decision.Reasons = append(decision.Reasons, final.Reasons...)
		return decision
	}
	if !vacancy.DetailAvailable {
		decision.Reasons = []string{"search card is incomplete; full vacancy detail is required"}
		return decision
	}
	if final.Status == RouteSelected && final.Confidence != ConfidenceLow {
		decision.Status, decision.ReasonCode = PreliminaryClearRoute, RouteReasonSelected
		decision.Reasons = append(decision.Reasons, "search card contains strong deterministic evidence")
		return decision
	}
	if final.Status == RouteReviewRequired && len(vacancy.SearchProfiles) == 0 && final.Score == 0 {
		decision.Status, decision.ReasonCode = PreliminaryObviousReject, RouteReasonNoStrong
		decision.Reasons = append(decision.Reasons, "no relevance signal in an unassociated search card")
		return decision
	}
	decision.Reasons = append(decision.Reasons, "preliminary evidence is insufficient for final resume selection")
	return decision
}

type VacancyCandidate struct {
	ID               int       `json:"id"`
	Title            string    `json:"title"`
	Company          string    `json:"company,omitempty"`
	URL              string    `json:"url,omitempty"`
	Description      string    `json:"description,omitempty"`
	PublishedAt      time.Time `json:"published_at,omitempty"`
	SearchProfileIDs []string  `json:"search_profile_ids,omitempty"`
}

type SearchPage struct {
	Items      []VacancyCandidate
	NextCursor string
}

type Searcher interface {
	Search(context.Context, SearchProfile, string) (SearchPage, error)
}

type DiscoveryOptions struct {
	MaxVacancies int
	Now          func() time.Time
	FreshAfter   time.Duration
	CheapFilter  func(VacancyCandidate) (bool, string)
}

type DiscoverySummary struct {
	SearchProfiles int `json:"search_profiles"`
	RawResults     int `json:"raw_results"`
	Unique         int `json:"unique"`
	CheapFiltered  int `json:"cheap_filtered"`
	Pages          int `json:"pages"`
}

type DiscoveryResult struct {
	Items   []VacancyCandidate `json:"items"`
	Summary DiscoverySummary   `json:"summary"`
}

func Discover(ctx context.Context, profiles []SearchProfile, searcher Searcher, options DiscoveryOptions) (DiscoveryResult, error) {
	if searcher == nil {
		return DiscoveryResult{}, errors.New("career discovery searcher is nil")
	}
	if options.Now == nil {
		options.Now = func() time.Time { return time.Now().UTC() }
	}
	result := DiscoveryResult{Items: []VacancyCandidate{}, Summary: DiscoverySummary{SearchProfiles: len(profiles)}}
	byID := map[int]int{}
	seenIDs := map[int]bool{}
	for _, profile := range profiles {
		cursor := ""
		for {
			if err := ctx.Err(); err != nil {
				return result, err
			}
			page, err := searcher.Search(ctx, profile, cursor)
			if err != nil {
				return result, err
			}
			result.Summary.Pages++
			result.Summary.RawResults += len(page.Items)
			for _, item := range page.Items {
				if item.ID <= 0 {
					continue
				}
				if seenIDs[item.ID] {
					if index, ok := byID[item.ID]; ok {
						result.Items[index].SearchProfileIDs = appendUnique(result.Items[index].SearchProfileIDs, profile.ID)
					}
					continue
				}
				seenIDs[item.ID] = true
				item.SearchProfileIDs = appendUnique(item.SearchProfileIDs, profile.ID)
				if options.FreshAfter > 0 && !item.PublishedAt.IsZero() && options.Now().Sub(item.PublishedAt) > options.FreshAfter {
					result.Summary.CheapFiltered++
					continue
				}
				if options.CheapFilter != nil {
					pass, _ := options.CheapFilter(item)
					if !pass {
						result.Summary.CheapFiltered++
						continue
					}
				}
				if options.MaxVacancies > 0 && len(result.Items) >= options.MaxVacancies {
					continue
				}
				byID[item.ID] = len(result.Items)
				result.Items = append(result.Items, item)
			}
			if page.NextCursor == "" || len(page.Items) == 0 {
				break
			}
			cursor = page.NextCursor
		}
	}
	result.Summary.Unique = len(result.Items)
	return result, nil
}

type FeedbackType string

const (
	FeedbackAccept      FeedbackType = "ACCEPT"
	FeedbackReject      FeedbackType = "REJECT"
	FeedbackWrongResume FeedbackType = "WRONG_RESUME"
	FeedbackGoodMatch   FeedbackType = "GOOD_MATCH"
	FeedbackBadMatch    FeedbackType = "BAD_MATCH"
)

type Feedback struct {
	ID        string       `json:"id"`
	VacancyID int          `json:"vacancy_id"`
	Type      FeedbackType `json:"type"`
	ResumeID  string       `json:"resume_id,omitempty"`
	Note      string       `json:"note,omitempty"`
	CreatedAt time.Time    `json:"created_at"`
}

func FeedbackID(value Feedback) string {
	if strings.TrimSpace(value.ID) != "" {
		return value.ID
	}
	seed := formatInt(int64(value.VacancyID)) + "\x00" + strings.TrimSpace(value.ResumeID) + "\x00" + string(value.Type)
	sum := sha256.Sum256([]byte(seed))
	return "career-feedback-" + hex.EncodeToString(sum[:8])
}

func ValidFeedbackType(value FeedbackType) bool {
	switch value {
	case FeedbackAccept, FeedbackReject, FeedbackWrongResume, FeedbackGoodMatch, FeedbackBadMatch:
		return true
	}
	return false
}

func formatInt(value int64) string {
	return strconv.FormatInt(value, 10)
}

func normalizeText(value string) string {
	return strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(value))), " ")
}
func normalizeQuery(value string) string {
	return strings.TrimSpace(strings.Join(strings.Fields(strings.NewReplacer("/", " ", "|", " ", ",", " ", "–", " ").Replace(value)), " "))
}
func splitList(value string) []string {
	parts := strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == ';' || r == '\n' || r == '|' })
	result := []string{}
	for _, part := range parts {
		if v := strings.TrimSpace(part); v != "" {
			result = append(result, v)
		}
	}
	return result
}
func roleExpansions(values ...string) []string {
	text := normalizeText(strings.Join(values, " "))
	expansions := []string{}
	if strings.Contains(text, "python") {
		expansions = append(expansions, "Python backend", "Python developer")
	}
	if strings.Contains(text, "django") {
		expansions = append(expansions, "Django developer")
	}
	if strings.Contains(text, "support") {
		expansions = append(expansions, "technical support", "technical specialist")
	}
	if strings.Contains(text, "integration") || strings.Contains(text, "implementation") {
		expansions = append(expansions, "implementation specialist", "integration specialist")
	}
	if strings.Contains(text, "automation") {
		expansions = append(expansions, "automation engineer", "AI automation")
	}
	return expansions
}
func isNoiseQuery(value string) bool {
	return len(strings.Fields(value)) == 0 || len([]rune(value)) < 3
}
func normalizeKeywords(values []string) []string {
	result := []string{}
	seen := map[string]bool{}
	for _, value := range values {
		value = normalizeQuery(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}
func containsKeyword(value string, keywords []string) bool {
	value = normalizeText(value)
	for _, keyword := range normalizeKeywords(keywords) {
		if strings.Contains(value, normalizeText(keyword)) {
			return true
		}
	}
	return false
}
func stableSearchID(resumeID, query string) string {
	sum := sha256.Sum256([]byte(resumeID + "\x00" + normalizeText(query)))
	return "search-" + hex.EncodeToString(sum[:8])
}
func tokens(value string) map[string]bool {
	result := map[string]bool{}
	value = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(value), "ё", "е"))
	value = strings.NewReplacer("/", " ", "|", " ", ",", " ", ";", " ", "–", " ", "—", " ", "-", " ", "(", " ", ")", " ", ":", " ").Replace(value)
	for _, raw := range strings.Fields(value) {
		token := canonicalToken(raw)
		if len([]rune(token)) >= 2 {
			result[token] = true
		}
	}
	return result
}

// canonicalToken is intentionally a small, reviewable vocabulary for the
// candidate's target roles. It handles common RU/EN inflections and aliases;
// it is not intended to be a general Russian stemmer or thesaurus.
func canonicalToken(value string) string {
	value = strings.Trim(strings.ToLower(strings.ReplaceAll(value, "ё", "е")), ".!?\"'`")
	aliases := map[string]string{
		"автоматизация": "automation", "автоматизации": "automation", "автоматизацию": "automation", "автоматизировать": "automation", "automation": "automation", "automations": "automation",
		"интеграция": "integration", "интеграции": "integration", "интеграциям": "integration", "интеграциями": "integration", "integration": "integration", "integrations": "integration",
		"поддержка": "support", "поддержки": "support", "поддержку": "support", "поддержкой": "support", "support": "support",
		"внедрение": "implementation", "внедрения": "implementation", "внедрению": "implementation", "внедрении": "implementation", "implementation": "implementation",
		"разработчик": "developer", "разработчика": "developer", "разработчики": "developer", "разработка": "developer", "разработке": "developer", "developer": "developer", "developers": "developer",
		"бэкенд": "backend", "бекенд": "backend", "бэкенда": "backend", "бекенда": "backend", "backend": "backend", "back-end": "backend",
		"технический": "technical", "техническая": "technical", "техническое": "technical", "технической": "technical", "technical": "technical",
		"специалист": "specialist", "специалиста": "specialist", "специалисты": "specialist", "specialist": "specialist",
		"инженер": "engineer", "инженера": "engineer", "инженеры": "engineer", "engineer": "engineer",
		"администратор": "administrator", "администратора": "administrator", "администрирование": "administrator", "administrator": "administrator",
		"системный": "system", "системного": "system", "system": "system",
		"аналитик": "analyst", "аналитика": "analyst", "analyst": "analyst",
		"фронтенд": "frontend", "фронтенда": "frontend", "frontend": "frontend",
		"флаттер": "flutter", "flutter": "flutter", "dart": "dart",
		"линукс": "linux", "linux": "linux", "сетевая": "network", "сетевой": "network", "сети": "network", "network": "network",
		"днс": "dns", "dns": "dns", "девопс": "devops", "devops": "devops",
		"1с": "1c", "1c": "1c", "onec": "onec",
		"диагностика": "diagnostics", "диагностики": "diagnostics", "diagnostics": "diagnostics",
		"разработчикa": "developer",
		"rest":         "rest_api", "restful": "rest_api", "api": "api", "apis": "api",
		"postgres": "postgresql", "postgresql": "postgresql",
		"node": "nodejs", "nodejs": "nodejs",
		"vue": "vuejs", "vue.js": "vuejs", "vuejs": "vuejs",
	}
	if canonical, ok := aliases[value]; ok {
		return canonical
	}
	return value
}

var genericEvidenceTokens = map[string]bool{
	"developer": true, "specialist": true, "technical": true, "engineer": true,
	"api": true, "backend": true,
}

var specificEvidenceTokens = map[string]bool{
	"python": true, "django": true, "postgresql": true, "sql": true, "rest_api": true,
	"docker": true, "redis": true, "nginx": true, "typescript": true, "javascript": true,
	"react": true, "nodejs": true, "express": true, "webhooks": true, "bitrix24": true,
	"crm": true, "linux": true, "soap": true, "vuejs": true, "laravel": true,
	"php": true, "html": true, "css": true, "automation": true, "integration": true,
	"implementation": true, "support": true, "troubleshooting": true, "diagnostics": true,
	"installation": true, "network": true, "cloud": true, "devops": true, "kubernetes": true,
	"kafka": true, "celery": true, "xml": true, "dns": true, "fullstack": true,
}

var domainEvidenceTokens = map[string]bool{
	"automation": true, "integration": true, "implementation": true, "support": true,
	"crm": true, "backend": true, "fullstack": true, "data": true, "analytics": true,
}

var roleEvidenceTokens = map[string]bool{
	"python": true, "django": true, "backend": true, "support": true, "automation": true,
	"integration": true, "implementation": true, "fullstack": true, "devops": true,
	"data": true, "analytics": true,
}

// DeriveResumeIdentity exposes distinctions already present in the profile.
// It never adds candidate facts: generic title words remain separate from
// strong, specific evidence.
func DeriveResumeIdentity(resume ResumeProfile) ResumeIdentity {
	identity := ResumeIdentity{}
	roleTokens := tokens(resume.Title + " " + resume.DesiredRole)
	allTokens := tokens(strings.Join(append([]string{resume.Title, resume.DesiredRole}, resume.Skills...), " "))
	for _, token := range sortedTokenNames(roleTokens) {
		if roleEvidenceTokens[token] || genericEvidenceTokens[token] {
			identity.PrimaryRoles = append(identity.PrimaryRoles, token)
		}
	}
	for _, token := range sortedTokenNames(allTokens) {
		if domainEvidenceTokens[token] {
			identity.DomainSignals = append(identity.DomainSignals, token)
		}
	}
	for _, skill := range resume.Skills {
		name := strings.TrimSpace(skill)
		if name == "" {
			continue
		}
		key := strings.Join(sortedTokenNames(tokens(name)), " ")
		if key == "" {
			continue
		}
		specific := false
		for token := range tokens(name) {
			if specificEvidenceTokens[token] && !genericEvidenceTokens[token] {
				specific = true
				break
			}
		}
		if specific {
			identity.StrongSkills = appendUniqueCanonical(identity.StrongSkills, name)
		} else {
			identity.SupportingSkills = appendUniqueCanonical(identity.SupportingSkills, name)
		}
	}
	for _, value := range resume.ExcludeKeywords {
		if normalized := normalizeQuery(value); normalized != "" {
			identity.NegativeSignals = appendUnique(identity.NegativeSignals, normalized)
			identity.NegativeMismatchAnchors = appendUnique(identity.NegativeMismatchAnchors, normalized)
		}
	}
	identity.PrimaryRoleFamilies, identity.SecondaryRoleFamilies = resumeRoleFamilies(resume)
	identity.CoreSkills = append([]string(nil), identity.StrongSkills...)
	identity.AdjacentSkills = append([]string(nil), identity.SupportingSkills...)
	for _, skill := range resume.Skills {
		name := strings.TrimSpace(skill)
		if name == "" {
			continue
		}
		matchedSpecific := false
		for token := range tokens(name) {
			if reset6ResumeStrongAnchorTokens[token] {
				matchedSpecific = true
				break
			}
		}
		if matchedSpecific {
			identity.StrongPositiveAnchors = appendUniqueCanonical(identity.StrongPositiveAnchors, name)
		} else {
			identity.GenericAnchors = appendUniqueCanonical(identity.GenericAnchors, name)
		}
	}
	return identity
}

type skillMatchKind string

const (
	skillMatchNone        skillMatchKind = "none"
	skillMatchExact       skillMatchKind = "exact normalized phrase"
	skillMatchSubstantial skillMatchKind = "multi-token substantial overlap"
	skillMatchSpecific    skillMatchKind = "single specific token"
	skillMatchGeneric     skillMatchKind = "single generic token"
)

func scoreResume(v VacancyInput, resume ResumeProfile) ResumeScore {
	identity := resume.Identity
	if len(identity.PrimaryRoles) == 0 && len(identity.StrongSkills) == 0 && len(identity.SupportingSkills) == 0 && len(identity.DomainSignals) == 0 || len(identity.PrimaryRoleFamilies) == 0 {
		identity = DeriveResumeIdentity(resume)
	}
	roleEvidence := ClassifyVacancyRole(v, nil)
	vacancyText := strings.Join([]string{v.Title, v.Description, strings.Join(v.RequiredSkills, " "), strings.Join(v.KeySkills, " "), strings.Join(v.ProfessionalRoles, " "), v.Experience, v.Employment, v.Schedule, v.Location, v.WorkFormat, v.Salary}, " ")
	structuredText := strings.Join([]string{v.Title, strings.Join(v.RequiredSkills, " "), strings.Join(v.KeySkills, " "), strings.Join(v.ProfessionalRoles, " ")}, " ")
	structuredTokens := tokens(structuredText)
	descriptionTokens := tokens(v.Description)
	roleTokens := tokens(strings.Join([]string{v.Title, strings.Join(v.ProfessionalRoles, " ")}, " "))
	hardBlockers := []string{}
	for _, keyword := range resume.ExcludeKeywords {
		if containsKeyword(vacancyText, []string{keyword}) {
			hardBlockers = append(hardBlockers, "excluded keyword: "+keyword)
		}
	}

	roleScore, genericEvidence := 0, 0
	matchedRoleFamilies := []RoleFamily{}
	strongRoleEvidence := []string{}
	mismatchSignals := []string{}
	for _, familyEvidence := range roleEvidence.Families {
		if resumeSupportsRoleFamily(identity, familyEvidence.Family) {
			matchedRoleFamilies = appendUniqueRoleFamily(matchedRoleFamilies, familyEvidence.Family)
			if familyEvidence.Strength == RoleEvidenceStrong {
				roleScore += 24
				strongRoleEvidence = appendUniqueStrings(strongRoleEvidence, familyEvidence.TitleAnchors...)
				strongRoleEvidence = appendUniqueStrings(strongRoleEvidence, familyEvidence.ExplicitRoleSignals...)
			}
		} else if familyEvidence.Strength == RoleEvidenceStrong {
			mismatchSignals = appendUniqueStrings(mismatchSignals, "unsupported resume family for vacancy role: "+string(familyEvidence.Family))
		}
	}
	for token := range tokens(resume.Title + " " + resume.DesiredRole) {
		if !roleTokens[token] {
			continue
		}
		if roleEvidenceTokens[token] && !genericEvidenceTokens[token] {
			roleScore += 12
		} else if genericEvidenceTokens[token] {
			roleScore += 2
			genericEvidence++
		}
	}

	skillScore := 0
	specificMatches, partialMatches := []string{}, []string{}
	seenSkills := map[string]bool{}
	for _, skill := range resume.Skills {
		key := strings.Join(sortedTokenNames(tokens(skill)), " ")
		if key == "" || seenSkills[key] {
			continue
		}
		seenSkills[key] = true
		kind := classifySkillMatch(skill, structuredText, structuredTokens)
		weight := 1
		if kind == skillMatchNone {
			kind = classifySkillMatch(skill, v.Description, descriptionTokens)
			weight = 2 // free-text description is useful, but weaker than fields
		}
		switch kind {
		case skillMatchExact:
			if weight == 1 {
				skillScore += 20
			} else {
				skillScore += 5
			}
			specificMatches = append(specificMatches, strings.TrimSpace(skill)+" (exact normalized phrase)")
		case skillMatchSubstantial:
			if weight == 1 {
				skillScore += 12
			} else {
				skillScore += 4
			}
			partialMatches = append(partialMatches, strings.TrimSpace(skill)+" (multi-token substantial overlap)")
		case skillMatchSpecific:
			if weight == 1 {
				skillScore += 7
			} else {
				skillScore += 2
			}
			partialMatches = append(partialMatches, strings.TrimSpace(skill)+" (single specific token)")
		case skillMatchGeneric:
			genericEvidence++
		}
	}
	domainScore := 0
	for token := range domainEvidenceTokens {
		if structuredTokens[token] && (containsToken(identity.DomainSignals, token) || tokensContain(tokens(resume.Title+" "+strings.Join(resume.Skills, " ")), token)) {
			domainScore += 8
		}
	}
	experienceScore := experienceFitScore(v, resume)
	provenanceScore := 0
	for _, source := range v.SearchProfiles {
		if source.ResumeID != "" && (source.ResumeID == resume.ID || source.ResumeID == resume.Hash) {
			provenanceScore = 5
			break
		}
	}
	genericScore := minInt(genericEvidence, 5)
	specificEvidenceCount := len(specificMatches) + len(partialMatches)
	evidenceDenominator := specificEvidenceCount + genericScore + len(strongRoleEvidence)
	genericRatio := 0.0
	if evidenceDenominator > 0 {
		genericRatio = float64(genericScore) / float64(evidenceDenominator)
	}
	fitScore := roleScore + skillScore + domainScore + experienceScore + genericScore
	rawFit := fitScore + provenanceScore
	normalized := normalizeResumeScore(rawFit)
	reasons := []string{}
	if roleScore > 0 {
		reasons = append(reasons, fmtScoreReason("role fit", roleScore))
		reasons = append(reasons, "role/title overlap")
	}
	if skillScore > 0 {
		reasons = append(reasons, fmtScoreReason("specific skill fit", skillScore))
	}
	if domainScore > 0 {
		reasons = append(reasons, fmtScoreReason("domain fit", domainScore))
	}
	if experienceScore > 0 {
		reasons = append(reasons, fmtScoreReason("seniority/experience fit", experienceScore))
	}
	if provenanceScore > 0 {
		reasons = append(reasons, "search provenance (+5 bounded soft signal)")
	}
	if genericScore > 0 {
		reasons = append(reasons, fmtScoreReason("generic evidence (bounded)", genericScore))
	}
	for _, family := range matchedRoleFamilies {
		reasons = append(reasons, "matched role family: "+string(family))
	}
	for _, mismatch := range mismatchSignals {
		reasons = append(reasons, "routing mismatch: "+mismatch)
	}
	for _, match := range specificMatches {
		reasons = append(reasons, "specific skill: "+match)
	}
	for _, match := range partialMatches {
		reasons = append(reasons, "partial skill: "+match)
	}
	return ResumeScore{ResumeID: resume.ID, Title: resume.Title, Score: normalized, FitScore: fitScore, ProvenanceScore: provenanceScore, RoleScore: roleScore, SkillScore: skillScore, DomainScore: domainScore, ExperienceScore: experienceScore, GenericEvidenceScore: genericScore, RawFit: rawFit, NormalizedScore: normalized, SpecificMatches: specificMatches, PartialMatches: partialMatches, Reasons: reasons, HardBlockers: hardBlockers, MatchedRoleFamilies: matchedRoleFamilies, StrongRoleEvidence: strongRoleEvidence, SpecificEvidenceCount: specificEvidenceCount, GenericEvidenceRatio: genericRatio, MismatchSignals: mismatchSignals}
}

func classifySkillMatch(skill, vacancyText string, vacancyTokens map[string]bool) skillMatchKind {
	skillTokens := tokens(skill)
	if len(skillTokens) == 0 {
		return skillMatchNone
	}
	specificCount, overlapSpecific := 0, 0
	overlapGeneric := false
	for token := range skillTokens {
		if specificEvidenceTokens[token] && !genericEvidenceTokens[token] {
			specificCount++
			if vacancyTokens[token] {
				overlapSpecific++
			}
		} else if genericEvidenceTokens[token] && vacancyTokens[token] {
			overlapGeneric = true
		}
	}
	if overlapSpecific == 0 {
		if specificCount == 0 && overlapGeneric {
			return skillMatchGeneric
		}
		return skillMatchNone
	}
	if containsCanonicalPhrase(vacancyText, skill) {
		return skillMatchExact
	}
	if specificCount >= 2 && overlapSpecific*2 >= specificCount {
		return skillMatchSubstantial
	}
	return skillMatchSpecific
}

func containsCanonicalPhrase(haystack, phrase string) bool {
	want := canonicalTokenSequence(phrase)
	if len(want) == 0 {
		return false
	}
	have := canonicalTokenSequence(haystack)
	for start := 0; start+len(want) <= len(have); start++ {
		match := true
		for index, token := range want {
			if have[start+index] != token {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

func canonicalTokenSequence(value string) []string {
	value = strings.ToLower(strings.ReplaceAll(value, "ё", "е"))
	value = strings.NewReplacer("/", " ", "|", " ", ",", " ", ";", " ", "–", " ", "—", " ", "-", " ", "(", " ", ")", " ", ":", " ").Replace(value)
	result := []string{}
	for _, raw := range strings.Fields(value) {
		if token := canonicalToken(raw); token != "" {
			result = append(result, token)
		}
	}
	return result
}

func experienceFitScore(v VacancyInput, resume ResumeProfile) int {
	vacancy := strings.ToLower(v.Experience + " " + v.Title)
	resumeText := strings.ToLower(resume.Title + " " + resume.DesiredRole)
	for _, level := range []string{"senior", "middle", "middle+", "junior", "стажер", "ведущий"} {
		if strings.Contains(vacancy, level) && strings.Contains(resumeText, level) {
			return 8
		}
	}
	return 0
}

func normalizeResumeScore(raw int) int {
	if raw <= 0 {
		return 0
	}
	// Monotonic compression keeps a 0..100 UI score while retaining ordering
	// in RawFit for routing and margins. There is no early cap on the fit sum.
	return int(math.Round(100 * float64(raw) / float64(raw+60)))
}

func fmtScoreReason(component string, value int) string {
	return component + " (+" + strconv.Itoa(value) + ")"
}

func tokensContain(values map[string]bool, value string) bool {
	return values[value]
}

func containsToken(values []string, value string) bool {
	for _, item := range values {
		if item == value {
			return true
		}
	}
	return false
}

func appendUniqueCanonical(values []string, value string) []string {
	key := strings.Join(sortedTokenNames(tokens(value)), " ")
	for _, existing := range values {
		if strings.Join(sortedTokenNames(tokens(existing)), " ") == key {
			return values
		}
	}
	return append(values, value)
}

func requirementStates(v VacancyInput, selected ResumeScore, resumes []ResumeProfile) []RequirementState {
	var profile ResumeProfile
	for _, resume := range resumes {
		if resume.ID == selected.ResumeID {
			profile = resume
			break
		}
	}
	skills := tokens(strings.Join(profile.Skills, " "))
	result := []RequirementState{}
	seen := map[string]bool{}
	for _, requirement := range v.RequiredSkills {
		requirement = strings.TrimSpace(requirement)
		key := strings.Join(sortedTokenNames(tokens(requirement)), " ")
		if requirement == "" || seen[key] {
			continue
		}
		seen[key] = true
		status := "unknown"
		if hasTokenOverlap(tokens(requirement), skills) {
			status = "met"
		}
		result = append(result, RequirementState{Requirement: requirement, Status: status})
	}
	return result
}

func sortedTokenNames(values map[string]bool) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}
func allRequirementsMet(values []RequirementState) bool {
	for _, value := range values {
		if value.Status != "met" {
			return false
		}
	}
	return true
}
func hasTokenOverlap(left, right map[string]bool) bool {
	return overlappingToken(left, right) != ""
}

func overlappingToken(left, right map[string]bool) string {
	for token := range left {
		if right[token] {
			return token
		}
	}
	return ""
}
func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}
func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
