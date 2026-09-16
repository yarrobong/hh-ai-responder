// Package careeragent contains deterministic planning, routing, discovery and
// operator-feedback primitives for the Career Agent flow. It deliberately has
// no HH transport, AI, or persistence side effects.
package careeragent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"hh-ai-responder/internal/candidate"
)

type ResumeProfile struct {
	ID              string   `json:"id"`
	HHID            int64    `json:"hh_id,omitempty"`
	Hash            string   `json:"hash,omitempty"`
	Title           string   `json:"title"`
	DesiredRole     string   `json:"desired_role,omitempty"`
	Skills          []string `json:"skills,omitempty"`
	Experience      string   `json:"experience,omitempty"`
	Location        string   `json:"location,omitempty"`
	Salary          string   `json:"salary,omitempty"`
	Employment      string   `json:"employment,omitempty"`
	Schedule        string   `json:"schedule,omitempty"`
	SearchHints     []string `json:"search_hints,omitempty"`
	IncludeKeywords []string `json:"include_keywords,omitempty"`
	ExcludeKeywords []string `json:"exclude_keywords,omitempty"`
	Enabled         bool     `json:"enabled"`
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
		result = append(result, ResumeProfile{
			ID: id, HHID: value.Id, Hash: strings.TrimSpace(value.Hash),
			Title: strings.TrimSpace(value.Title), DesiredRole: strings.TrimSpace(value.Title),
			Skills: splitList(value.Skills), Location: strings.TrimSpace(value.Area), Salary: strings.TrimSpace(value.Salary), Enabled: true,
		})
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
	ResumeID        string   `json:"resume_id"`
	Title           string   `json:"title"`
	Score           int      `json:"score"`
	FitScore        int      `json:"fit_score,omitempty"`
	ProvenanceScore int      `json:"provenance_score,omitempty"`
	Reasons         []string `json:"reasons,omitempty"`
	HardBlockers    []string `json:"hard_blockers,omitempty"`
}

type RouteDecision struct {
	VacancyID           int                `json:"vacancy_id"`
	Status              string             `json:"status"`
	SelectedResumeID    string             `json:"selected_resume_id,omitempty"`
	SelectedResumeTitle string             `json:"selected_resume_title,omitempty"`
	Score               int                `json:"score"`
	AlternativeScores   []ResumeScore      `json:"alternative_resume_scores,omitempty"`
	Reasons             []string           `json:"reasons,omitempty"`
	Confidence          string             `json:"confidence"`
	HardRequirements    []RequirementState `json:"hard_requirements,omitempty"`
	HardBlockers        []string           `json:"hard_blockers,omitempty"`
	ReasonCode          string             `json:"reason_code,omitempty"`
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
	PreliminaryClearRoute    = "CLEAR_ROUTE"
	PreliminaryNeedsDetail   = "NEEDS_DETAIL"
	PreliminaryObviousReject = "OBVIOUS_REJECT"
	PreliminaryNoResume      = "NO_ENABLED_RESUME"
	RouteReasonNeedsDetail   = "ROUTE_NEEDS_DETAIL"
	RouteReasonAmbiguous     = "ROUTE_AMBIGUOUS_AFTER_DETAIL"
	RouteReasonUnknownHard   = "UNKNOWN_HARD_REQUIREMENT"
	RouteReasonNoStrong      = "NO_STRONG_RESUME_AFTER_DETAIL"
	RouteReasonSelected      = "ROUTE_SELECTED"
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
	decision := RouteDecision{VacancyID: vacancy.ID, Status: RouteNoResume, Confidence: ConfidenceLow, Reasons: []string{}, ReasonCode: RouteReasonNoStrong}
	candidates := make([]ResumeScore, 0)
	for _, resume := range resumes {
		if resume.Enabled {
			candidates = append(candidates, scoreResume(vacancy, resume))
		}
	}
	if len(candidates) == 0 {
		decision.Reasons = []string{"no enabled resume is available"}
		decision.ReasonCode = RouteReasonNoStrong
		return decision
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].Score != candidates[j].Score {
			return candidates[i].Score > candidates[j].Score
		}
		return candidates[i].ResumeID < candidates[j].ResumeID
	})
	decision.AlternativeScores = append([]ResumeScore(nil), candidates...)
	compatible := candidates[:0]
	for _, candidate := range candidates {
		if len(candidate.HardBlockers) == 0 {
			compatible = append(compatible, candidate)
		}
	}
	if len(compatible) == 0 {
		decision.Reasons = []string{"all enabled resumes have explicit hard incompatibilities"}
		for _, candidate := range candidates {
			decision.Reasons = append(decision.Reasons, candidate.Title+": "+strings.Join(candidate.HardBlockers, ", "))
		}
		decision.HardBlockers = append([]string(nil), candidates[0].HardBlockers...)
		decision.ReasonCode = RouteReasonNoStrong
		return decision
	}
	candidates = compatible
	decision.Score = candidates[0].Score
	decision.SelectedResumeID, decision.SelectedResumeTitle = candidates[0].ResumeID, candidates[0].Title
	decision.HardRequirements = requirementStates(vacancy, candidates[0], resumes)
	for _, requirement := range decision.HardRequirements {
		if requirement.Status == "met" {
			decision.Reasons = append(decision.Reasons, "selected resume covers "+requirement.Requirement)
		} else {
			decision.Reasons = append(decision.Reasons, "hard requirement remains unknown: "+requirement.Requirement)
		}
	}
	// Provenance is a bounded tie-break signal, never enough to resolve a
	// genuinely close fit. Compare fit-only scores for the ambiguity gate.
	if len(candidates) > 1 && candidates[0].FitScore-candidates[1].FitScore < 8 {
		decision.Status, decision.Confidence, decision.ReasonCode = RouteReviewRequired, ConfidenceLow, RouteReasonAmbiguous
		decision.SelectedResumeID, decision.SelectedResumeTitle = "", ""
		decision.Reasons = append(decision.Reasons, "top resume scores are too close for deterministic selection")
		return decision
	}
	if decision.Score < 12 {
		decision.Status, decision.Confidence, decision.ReasonCode = RouteReviewRequired, ConfidenceLow, RouteReasonNoStrong
		decision.SelectedResumeID, decision.SelectedResumeTitle = "", ""
		decision.Reasons = append(decision.Reasons, "no strong resume signal was found")
		return decision
	}
	decision.Status = RouteSelected
	decision.ReasonCode = RouteReasonSelected
	decision.Reasons = append(decision.Reasons, "selected by deterministic title/skill overlap")
	if decision.Score >= 60 && (len(decision.HardRequirements) == 0 || allRequirementsMet(decision.HardRequirements)) {
		decision.Confidence = ConfidenceHigh
	} else if decision.Score >= 12 {
		decision.Confidence = ConfidenceMedium
	} else {
		decision.Confidence = ConfidenceLow
	}
	return decision
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
		"технический": "technical", "техническая": "technical", "техническое": "technical", "technical": "technical",
		"специалист": "specialist", "специалиста": "specialist", "специалисты": "specialist", "specialist": "specialist",
		"инженер": "engineer", "инженера": "engineer", "инженеры": "engineer", "engineer": "engineer",
		"разработчикa": "developer",
		"rest":         "api", "restful": "api", "api": "api", "apis": "api",
	}
	if canonical, ok := aliases[value]; ok {
		return canonical
	}
	return value
}
func scoreResume(v VacancyInput, resume ResumeProfile) ResumeScore {
	fitScore := 0
	reasons := []string{}
	vacancyText := strings.Join([]string{v.Title, v.Description, strings.Join(v.RequiredSkills, " "), strings.Join(v.KeySkills, " "), strings.Join(v.ProfessionalRoles, " "), v.Experience, v.Employment, v.Schedule, v.Location, v.WorkFormat, v.Salary}, " ")
	vacancyTokens := tokens(vacancyText)
	hardBlockers := []string{}
	for _, keyword := range resume.ExcludeKeywords {
		if containsKeyword(vacancyText, []string{keyword}) {
			hardBlockers = append(hardBlockers, "excluded keyword: "+keyword)
		}
	}
	matchedSkills := map[string]bool{}
	for _, skill := range resume.Skills {
		matchedToken := overlappingToken(tokens(skill), vacancyTokens)
		if matchedToken != "" && !matchedSkills[matchedToken] {
			matchedSkills[matchedToken] = true
			fitScore += 18
			reasons = append(reasons, "skill: "+skill)
		}
	}
	overlap := 0
	for token := range tokens(resume.Title + " " + resume.DesiredRole) {
		if vacancyTokens[token] {
			overlap++
		}
	}
	fitScore += overlap * 12
	if overlap > 0 {
		reasons = append(reasons, "role/title overlap")
	}
	provenanceScore := 0
	for _, source := range v.SearchProfiles {
		if source.ResumeID != "" && (source.ResumeID == resume.ID || source.ResumeID == resume.Hash) {
			provenanceScore = 4
			reasons = append(reasons, "source search profile for this resume (+4 soft signal)")
			break
		}
	}
	score := minInt(fitScore+provenanceScore, 100)
	return ResumeScore{ResumeID: resume.ID, Title: resume.Title, Score: score, FitScore: minInt(fitScore, 100), ProvenanceScore: provenanceScore, Reasons: reasons, HardBlockers: hardBlockers}
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
