package vacancyranking

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"hh-ai-responder/internal/candidate"
	"hh-ai-responder/internal/vacancy"
	"hh-ai-responder/internal/vacancyreview"
)

// CandidateContext is an immutable request-scoped projection of canonical
// candidate truth. Only confirmed/verified knowledge enters the projection.
type CandidateContext = candidateContext

func NewCandidateContext(value candidate.Candidate) (CandidateContext, error) {
	safe, err := candidate.CanonicalEmployerSafeProjection(value)
	if err != nil {
		return CandidateContext{}, fmt.Errorf("project candidate truth: %w", err)
	}
	result := CandidateContext{Skills: []candidateSkill{}, Roles: []string{}, Languages: []string{}}
	for _, skill := range safe.Skills {
		result.Skills = append(result.Skills, candidateSkill{Name: skill.Name, Negative: skill.Negative, Known: skill.Level != candidate.SkillLevelUnknown && skill.Level != candidate.SkillLevelHeardOf})
	}
	for _, raw := range []string{safe.Profile.PrimaryRoles, safe.Profile.SecondaryRoles, safe.Profile.PreferredRoles} {
		result.Roles = appendUnique(result.Roles, splitValues(raw)...)
	}
	if safe.Profile.TotalExperienceMonths != nil {
		value := *safe.Profile.TotalExperienceMonths
		result.TotalExperience = &value
	}
	result.OfficeLocation = candidateLocation(value, safe.Profile)
	result.WorkMode = safe.Profile.WorkMode
	result.Relocation = safe.Profile.Relocation
	result.BusinessTrips = safe.Profile.BusinessTrips
	if safe.Profile.SalaryMinimum != nil {
		value := *safe.Profile.SalaryMinimum
		result.SalaryMinimum = &value
	}
	for _, language := range safe.Profile.Languages {
		result.Languages = appendUnique(result.Languages, language.Name)
	}
	sort.Strings(result.Roles)
	sort.Strings(result.Languages)
	sort.Slice(result.Skills, func(i, j int) bool { return normalize(result.Skills[i].Name) < normalize(result.Skills[j].Name) })
	return result, nil
}

func candidateLocation(value candidate.Candidate, _ candidate.EmployerSafeProfileKnowledge) string {
	if value.Identity.Location != "" && candidate.CanExposeToEmployer(value.Identity.LocationMetadata) {
		return strings.TrimSpace(value.Identity.Location)
	}
	return ""
}

type Evaluator struct {
	Analyzer analyzerAdapter
}

// Evaluate is pure with respect to external state. Now is supplied by the
// caller so repeated evaluation of the same snapshot is deterministic.
func (e Evaluator) Evaluate(input EvaluateInput) Result {
	v := input.Vacancy
	ctx := input.Candidate
	r := Result{
		AlgorithmVersion:   AlgorithmVersion,
		Vacancy:            v,
		Eligibility:        EligibilityEligible,
		FitBand:            FitCompatible,
		AnalysisState:      analysisState(v),
		Rankable:           true,
		ReviewState:        input.Effective.State,
		ApplicationLinked:  input.Effective.ApplicationLinked,
		ChangedSinceReview: input.Effective.ChangedSinceReview,
		Freshness:          input.Effective.Freshness,
		PositiveReasons:    []Reason{}, Concerns: []Reason{}, Unknowns: []Reason{}, HardReasons: []Reason{},
	}
	if r.ReviewState == "" {
		r.ReviewState = vacancyreview.StateUnseen
	}
	if r.AnalysisState == AnalysisInsufficientEvidence {
		r.addUnknown("insufficient_vacancy_evidence", SourceDerived, "title and structured/detail evidence are absent")
	}

	text := vacancyText(v)
	role := roleSignal(text, v, ctx, &r)
	skills := skillSignal(text, v, ctx, &r)
	experience := experienceSignal(v, text, ctx, &r)
	location := locationSignal(v, text, ctx, &r)
	salary := salarySignal(v, ctx, &r)
	freshness := freshnessSignal(input.Now, input.Effective.Freshness, &r)

	r.Components = ScoreComponents{
		RoleFit: role, SkillFit: skills, ExperienceFit: experience,
		LocationWorkFit: location, SalaryFit: salary, Freshness: freshness,
	}
	r.BaseRankScore = clamp(role+skills+experience+location+salary+freshness, 0, 100)
	if r.AnalysisState == AnalysisInsufficientEvidence && r.BaseRankScore > 55 {
		r.BaseRankScore = 55
	}
	r.applyEligibility(text, v, ctx)
	r.Confidence = confidenceFor(v, r.AnalysisState, len(r.Unknowns))
	r.FitBand = fitBandFor(r, experience)
	r.applyWorkflowExclusion()
	r.Legacy = legacyEvidence(v)
	if r.Legacy.Available {
		r.addConcern("legacy_match_available", SourceLegacyMatch, "persisted deterministic match is advisory; base rank is computed independently")
	}
	sortReasons(&r.PositiveReasons)
	sortReasons(&r.Concerns)
	sortReasons(&r.Unknowns)
	sortReasons(&r.HardReasons)
	return r
}

// analyzerAdapter is deliberately tiny. It documents that persisted
// match_result remains an input/compatibility signal, not the final rank.
type analyzerAdapter struct{}

func (analyzerAdapter) unused() {}

func (r *Result) addPositive(code string, source ReasonSource, evidence string) {
	r.PositiveReasons = append(r.PositiveReasons, Reason{Code: code, Outcome: OutcomePositive, Source: source, Evidence: concise(evidence), Confidence: ConfidenceHigh})
}
func (r *Result) addConcern(code string, source ReasonSource, evidence string) {
	r.Concerns = append(r.Concerns, Reason{Code: code, Outcome: OutcomeConcern, Source: source, Evidence: concise(evidence), Confidence: ConfidenceMedium})
}
func (r *Result) addUnknown(code string, source ReasonSource, evidence string) {
	r.Unknowns = append(r.Unknowns, Reason{Code: code, Outcome: OutcomeUnknown, Source: source, Evidence: concise(evidence), Confidence: ConfidenceLow})
}
func (r *Result) addHard(code string, source ReasonSource, evidence string) {
	r.HardReasons = append(r.HardReasons, Reason{Code: code, Outcome: OutcomeConflict, Source: source, Evidence: concise(evidence), Confidence: ConfidenceHigh})
}

func (r *Result) applyEligibility(text string, v vacancy.Vacancy, ctx CandidateContext) {
	if v.Archived {
		r.Eligibility, r.FitBand, r.Rankable, r.ExclusionCode = EligibilityUnavailable, FitHardIncompatible, false, "archived"
		r.addHard("archived", SourceStructuredProvider, "archived=true")
		return
	}
	for _, hard := range hardConflicts(text, v, ctx) {
		r.addHard(hard.code, hard.source, hard.evidence)
	}
	if len(r.HardReasons) > 0 {
		r.Eligibility, r.FitBand, r.Rankable, r.ExclusionCode = EligibilityIneligible, FitHardIncompatible, false, r.HardReasons[0].Code
		return
	}
	if mandatoryRelocation(text) && strings.TrimSpace(ctx.Relocation) == "" {
		r.addUnknown("relocation_unknown", SourceCandidateTruth, "candidate relocation preference is unavailable")
	}
	if mandatoryTravel(text) && strings.TrimSpace(ctx.BusinessTrips) == "" {
		r.addUnknown("business_trips_unknown", SourceCandidateTruth, "candidate business-travel preference is unavailable")
	}
	if strings.TrimSpace(v.WorkFormat) == "" && strings.TrimSpace(v.WorkSchedule) == "" && !allowsRemote(v, text) {
		r.addUnknown("work_format_unknown", SourceStructuredProvider, "work format is not reliably known")
	}
	if hasCriticalUnknown(r.Unknowns) || hasReviewConcern(r.Concerns) {
		r.Eligibility = EligibilityReviewRequired
	}
}

func hasCriticalUnknown(values []Reason) bool {
	for _, value := range values {
		switch value.Code {
		case "insufficient_vacancy_evidence", "skills_unknown", "skill_unknown", "experience_unknown", "candidate_experience_unknown", "insufficient_location_evidence", "candidate_location_unknown", "role_fit_unknown", "relocation_unknown", "business_trips_unknown", "work_format_unknown", "salary_unknown":
			return true
		}
	}
	return false
}

func hasReviewConcern(values []Reason) bool {
	for _, value := range values {
		if value.Code == "mixed_role_ambiguous" || value.Code == "role_fit_uncertain" {
			return true
		}
	}
	return false
}

func (r *Result) applyWorkflowExclusion() {
	if r.Eligibility == EligibilityUnavailable || r.Eligibility == EligibilityIneligible {
		return
	}
	if r.ApplicationLinked || r.ReviewState == vacancyreview.StateApplied {
		r.Rankable, r.ExclusionCode = false, "already_application_linked"
		r.addConcern("already_applied", SourceApplicationState, "durable application relation exists")
		return
	}
	if r.ReviewState == vacancyreview.StatePrepared {
		r.Rankable, r.ExclusionCode = false, "prepared"
		r.addConcern("prepared", SourceReviewState, "vacancy is in application preparation")
		return
	}
	if r.ReviewState == vacancyreview.StateDismissed && (r.ChangedSinceReview == nil || !*r.ChangedSinceReview) {
		r.Rankable, r.ExclusionCode = false, "dismissed"
		r.addConcern("dismissed", SourceReviewState, "dismissed without a known material update")
		return
	}
	if r.ReviewState == vacancyreview.StateDismissed && r.ChangedSinceReview != nil && *r.ChangedSinceReview {
		r.addPositive("material_change_reconsideration", SourceReviewState, "material vacancy fingerprint changed after dismissal")
	}
	if r.ReviewState == vacancyreview.StateInteresting {
		r.addPositive("interesting", SourceReviewState, "candidate marked vacancy interesting")
	}
}

type hardConflict struct {
	code     string
	source   ReasonSource
	evidence string
}

func hardConflicts(text string, v vacancy.Vacancy, ctx CandidateContext) []hardConflict {
	result := []hardConflict{}
	if explicitOnly(text, []string{"1c", "1с", "1с-битрикс"}, []string{"python", "django", "backend", "поддерж", "интеграц", "автоматизац", "api", "разработ"}) {
		result = append(result, hardConflict{"role_1c_only", SourceDescription, "vacancy describes 1C-only work"})
	}
	if containsAny(text, []string{"холодные звонки", "cold calls", "cold calling", "активные продажи", "cold sales"}) {
		result = append(result, hardConflict{"role_sales_only", SourceDescription, "explicit cold-sales responsibility"})
	}
	if explicitOnly(text, []string{"qa", "тестиров", "quality assurance"}, []string{"python", "django", "backend", "support", "поддерж", "интеграц", "автоматизац", "api", "разработ"}) {
		result = append(result, hardConflict{"role_qa_only", SourceDescription, "vacancy describes QA-only work"})
	}
	if explicitOnly(text, []string{"руководитель", "директор", "head of", "team lead", "тимлид", "management"}, []string{"разработ", "интеграц", "поддерж", "backend"}) {
		result = append(result, hardConflict{"role_management_only", SourceDescription, "vacancy describes management-only work"})
	}
	if mandatoryRelocation(text) && no(ctx.Relocation, []string{"да", "yes", "готов", "relocat"}) {
		result = append(result, hardConflict{"relocation_required", SourceDescription, "mandatory relocation is stated"})
	}
	if mandatoryTravel(text) && no(ctx.BusinessTrips, []string{"да", "yes", "готов", "possible", "возмож"}) {
		result = append(result, hardConflict{"business_travel_required", SourceDescription, "mandatory business travel is stated"})
	}
	if minimum, known, _ := requiredExperience(v, text); known && minimum >= 36 && containsAny(text, []string{"python", "django", "backend", "developer", "разработ"}) && ctx.TotalExperience != nil && *ctx.TotalExperience < minimum {
		result = append(result, hardConflict{"experience_specialist_gap", SourceDescription, fmt.Sprintf("explicit specialist requirement starts at %d months; candidate has exactly %d months", minimum, *ctx.TotalExperience)})
	}
	if location := explicitOfficeLocation(v); location != "" && ctx.OfficeLocation != "" && !sameCity(location, ctx.OfficeLocation) && !allowsRemote(v, text) {
		result = append(result, hardConflict{"office_location_conflict", SourceStructuredProvider, location})
	}
	if location := descriptionOfficeLocation(text); location != "" && ctx.OfficeLocation != "" && !sameCity(location, ctx.OfficeLocation) && !allowsRemote(v, text) {
		result = append(result, hardConflict{"office_location_conflict", SourceDescription, location})
	}
	return dedupeHard(result)
}

func roleSignal(text string, v vacancy.Vacancy, ctx CandidateContext, r *Result) int {
	if containsAny(text, []string{"поддерж", "support"}) && containsAny(text, []string{"продаж", "sales"}) && !containsAny(text, []string{"холодные звонки", "cold calls", "cold calling", "cold sales"}) {
		r.addConcern("mixed_role_ambiguous", SourceDescription, "support and sales responsibilities are mixed without an explicit cold-sales rule")
	}
	relevant := containsAny(text, []string{"python", "django", "backend", "back-end", "бекенд", "интеграц", "integration", "автоматизац", "automation", "техническ", "technical support", "поддержк", "api", "разработ"})
	matched := false
	for _, role := range ctx.Roles {
		if containsAny(text, strings.Fields(normalize(role))) || roleCategoryMatch(text, role) {
			matched = true
			break
		}
	}
	// A candidate direction must not turn an explicitly unrelated profession
	// into a positive match merely because both strings contain a generic word.
	roleText := strings.Join([]string{v.Title, v.Description, strings.Join(v.Requirements, " "), strings.Join(v.Skills, " ")}, "\n")
	if unrelatedRoleTitle(firstNonEmpty(v.Title, v.Name)) && !relevantRoleContext(roleText) {
		matched, relevant = false, false
		r.addConcern("role_fit_uncertain", SourceDescription, "title names an unrelated profession without technical/support evidence")
	}
	if matched {
		r.addPositive("role_fit", SourceCandidateTruth, "vacancy role aligns with a confirmed candidate direction")
		return 25
	}
	if relevant {
		r.addPositive("relevant_role_evidence", SourceDescription, "vacancy contains a target technical/support/integration direction")
		return 19
	}
	r.addUnknown("role_fit_unknown", SourceDescription, firstNonEmpty(v.Title, v.Name))
	return 13
}

func unrelatedRoleTitle(title string) bool {
	return containsAny(title, []string{"преподаватель математики", "логист", "финансовый аналитик", "портфельный менеджер", "бухгалтер", "юрист", "врач", "воспитатель"})
}

func relevantRoleContext(text string) bool {
	return containsAny(text, []string{"python", "django", "backend", "интеграц", "автоматизац", "поддержк", "api", "technical support", "разработчик backend"})
}

func skillSignal(text string, v vacancy.Vacancy, ctx CandidateContext, r *Result) int {
	required := extractSkills(text, append(append([]string{}, v.Skills...), v.Requirements...))
	if len(required) == 0 {
		r.addUnknown("skills_unknown", SourceDescription, "no reliable vacancy skill evidence")
		return 13
	}
	matched, missing, unknown := 0, 0, 0
	for _, req := range required {
		status, candidateName := matchCandidateSkill(req, ctx.Skills)
		switch status {
		case "matched":
			matched++
			r.addPositive("skill_match", SourceCandidateTruth, fmt.Sprintf("%s is explicitly required and confirmed in candidate truth", candidateName))
		case "missing":
			missing++
			r.addConcern("skill_missing", SourceCandidateTruth, fmt.Sprintf("%s is explicitly marked unavailable", req))
		case "unknown":
			unknown++
			r.addUnknown("skill_unknown", SourceCandidateTruth, fmt.Sprintf("candidate evidence for %s is not confirmed", req))
		}
	}
	value := int(float64(matched)*25/float64(len(required)) + float64(unknown)*12/float64(len(required)))
	if missing == 0 && matched > 0 {
		value += 2
	}
	return clamp(value, 0, 25)
}

func experienceSignal(v vacancy.Vacancy, text string, ctx CandidateContext, r *Result) int {
	minimum, known, noExperience := requiredExperience(v, text)
	if noExperience {
		r.addPositive("no_experience_required", SourceStructuredProvider, "vacancy accepts candidates without prior experience")
		return 20
	}
	if !known {
		r.addUnknown("experience_unknown", SourceStructuredProvider, "vacancy experience requirement is not reliably stated")
		return 10
	}
	if ctx.TotalExperience == nil {
		r.addUnknown("candidate_experience_unknown", SourceCandidateTruth, "confirmed total experience is unavailable")
		return 10
	}
	if *ctx.TotalExperience >= minimum {
		r.addPositive("experience_compatible", SourceCandidateTruth, fmt.Sprintf("confirmed total experience is %d months", *ctx.TotalExperience))
		return 20
	}
	if minimum <= 12 && *ctx.TotalExperience >= 9 {
		r.addConcern("experience_stretch", SourceDerived, fmt.Sprintf("vacancy band starts at %d months; candidate has exactly %d months", minimum, *ctx.TotalExperience))
		return 15
	}
	if minimum >= 36 && containsAny(text, []string{"django", "python", "backend", "разработчик"}) {
		r.addConcern("experience_strong_gap", SourceDerived, fmt.Sprintf("explicit specialist requirement starts at %d months; candidate has exactly %d months", minimum, *ctx.TotalExperience))
		return 4
	}
	r.addConcern("experience_gap", SourceDerived, fmt.Sprintf("requirement starts at %d months; candidate has exactly %d months", minimum, *ctx.TotalExperience))
	return 7
}

func locationSignal(v vacancy.Vacancy, text string, ctx CandidateContext, r *Result) int {
	if allowsRemote(v, text) {
		r.addPositive("remote_compatible", SourceDescription, "remote work is explicitly available")
		return 15
	}
	location := explicitOfficeLocation(v)
	if location == "" {
		location = descriptionOfficeLocation(text)
	}
	if location == "" {
		r.addUnknown("insufficient_location_evidence", SourceStructuredProvider, "work format and location are not reliably known")
		return 8
	}
	if ctx.OfficeLocation == "" {
		r.addUnknown("candidate_location_unknown", SourceCandidateTruth, location)
		return 8
	}
	if sameCity(location, ctx.OfficeLocation) {
		r.addPositive("office_location_compatible", SourceStructuredProvider, location)
		return 15
	}
	r.addConcern("office_location_review", SourceStructuredProvider, location)
	return 5
}

func salarySignal(v vacancy.Vacancy, ctx CandidateContext, r *Result) int {
	if ctx.SalaryMinimum == nil {
		r.addUnknown("salary_preference_unknown", SourceCandidateTruth, "candidate salary preference is unavailable")
		return 5
	}
	amount, currency, known := salaryValue(v)
	if !known {
		r.addUnknown("salary_unknown", SourceStructuredProvider, "salary is absent or currency conversion is not configured")
		return 5
	}
	if currency != "RUR" && currency != "RUB" {
		r.addUnknown("salary_currency_unknown", SourceStructuredProvider, currency)
		return 5
	}
	if amount >= *ctx.SalaryMinimum {
		r.addPositive("salary_compatible", SourceStructuredProvider, fmt.Sprintf("salary floor %d meets candidate minimum %d RUR", amount, *ctx.SalaryMinimum))
		return 10
	}
	r.addConcern("salary_below_preference", SourceStructuredProvider, fmt.Sprintf("salary ceiling/floor %d RUR is below candidate minimum %d RUR", amount, *ctx.SalaryMinimum))
	return 3
}

func freshnessSignal(now time.Time, freshness vacancy.Freshness, r *Result) int {
	if freshness.LastSeenAt.IsZero() && freshness.FirstSeenAt.IsZero() {
		r.addUnknown("freshness_unknown", SourceDerived, "P1-aware observation timestamp is unavailable")
		return 3
	}
	seen := freshness.LastSeenAt
	if seen.IsZero() {
		seen = freshness.FirstSeenAt
	}
	if now.IsZero() {
		r.addUnknown("freshness_age_unknown", SourceDerived, "ranking time was not supplied")
		return 3
	}
	age := now.Sub(seen)
	switch {
	case age <= 24*time.Hour:
		r.addPositive("fresh_observation", SourceDerived, "observed within the last 24 hours")
		return 5
	case age <= 7*24*time.Hour:
		return 4
	default:
		return 2
	}
}

func fitBandFor(r Result, experience int) FitBand {
	if r.Eligibility == EligibilityIneligible || r.Eligibility == EligibilityUnavailable {
		return FitHardIncompatible
	}
	for _, reason := range append(r.Concerns, r.Unknowns...) {
		if reason.Code == "skill_missing" || reason.Code == "experience_strong_gap" || reason.Code == "experience_specialist_gap" || reason.Code == "role_fit_uncertain" || reason.Code == "role_fit_unknown" && r.AnalysisState == AnalysisInsufficientEvidence {
			return FitUnlikely
		}
	}
	if experience < 20 {
		return FitStretch
	}
	if r.BaseRankScore < 50 {
		return FitUnlikely
	}
	return FitCompatible
}

func confidenceFor(v vacancy.Vacancy, state AnalysisState, unknowns int) Confidence {
	if state == AnalysisInsufficientEvidence || (strings.TrimSpace(v.Description) == "" && unknowns >= 3) {
		return ConfidenceLow
	}
	if unknowns >= 2 || strings.TrimSpace(v.Description) == "" {
		return ConfidenceMedium
	}
	return ConfidenceHigh
}

func legacyEvidence(v vacancy.Vacancy) LegacyEvidence {
	if v.MatchResult == nil {
		return LegacyEvidence{}
	}
	m := v.MatchResult
	return LegacyEvidence{Available: true, Score: m.Score, Confidence: m.Confidence, MatchedSkills: sortedCopy(m.MatchedSkills), UnknownSkills: sortedCopy(m.UnknownSkills), MissingSkills: sortedCopy(m.MissingSkills), MatchedRoles: sortedCopy(m.MatchedRoles), Risks: sortedCopy(m.Risks)}
}

func analysisState(v vacancy.Vacancy) AnalysisState {
	if strings.TrimSpace(v.Title) == "" && strings.TrimSpace(v.Name) == "" && strings.TrimSpace(v.Description) == "" && len(v.Requirements) == 0 && len(v.Skills) == 0 {
		return AnalysisInsufficientEvidence
	}
	if v.MatchResult != nil || strings.TrimSpace(v.Description) != "" || len(v.Requirements) > 0 || len(v.Skills) > 0 {
		return AnalysisComplete
	}
	return AnalysisPartial
}

func vacancyText(v vacancy.Vacancy) string {
	return strings.Join([]string{v.Name, v.Title, v.Description, strings.Join(v.Requirements, " "), strings.Join(v.Skills, " "), strings.Join(v.ProfessionalRoles, " "), v.WorkSchedule, v.WorkFormat, v.Location, v.Area.Name, v.WorkExperience}, "\n")
}

func requiredExperience(v vacancy.Vacancy, text string) (int, bool, bool) {
	compact := strings.ToLower(strings.Join(strings.Fields(v.WorkExperience), ""))
	if compact == "noexperience" || containsAny(text, []string{"без опыта", "опыт не требуется"}) {
		return 0, true, true
	}
	if strings.Contains(compact, "between1and3") || strings.Contains(compact, "1-3") {
		return 12, true, false
	}
	if strings.Contains(compact, "between3and6") || strings.Contains(compact, "3-6") {
		return 36, true, false
	}
	if strings.Contains(compact, "morethan6") || strings.Contains(compact, "6+") {
		return 72, true, false
	}
	for _, marker := range []string{"от 3 лет", "3+ years", "3 years", "от 2 лет", "2+ years", "2 years", "от 1 года", "1+ year", "1 year"} {
		if strings.Contains(strings.ToLower(text), marker) {
			switch marker {
			case "от 3 лет", "3+ years", "3 years":
				return 36, true, false
			case "от 2 лет", "2+ years", "2 years":
				return 24, true, false
			default:
				return 12, true, false
			}
		}
	}
	return 0, false, false
}

func salaryValue(v vacancy.Vacancy) (int, string, bool) {
	if v.Compensation.To != nil {
		return *v.Compensation.To, normalizeCurrency(v.Compensation.Currency), true
	}
	if v.Compensation.From != nil {
		return *v.Compensation.From, normalizeCurrency(v.Compensation.Currency), true
	}
	fields := strings.Fields(strings.ReplaceAll(v.Salary, "–", "-"))
	for _, field := range fields {
		if n, err := strconv.Atoi(strings.TrimSpace(field)); err == nil {
			return n, normalizeCurrency(v.SalaryCurrency), true
		}
	}
	return 0, "", false
}

func normalizeCurrency(value string) string {
	value = strings.ToUpper(strings.TrimSpace(value))
	if value == "RUB" {
		return "RUR"
	}
	return value
}

func extractSkills(text string, explicit []string) []string {
	result := []string{}
	for _, raw := range explicit {
		if skillToken(raw) != "" && containsSkill(text, raw) {
			result = appendUnique(result, skillToken(raw))
		}
	}
	for _, raw := range []string{"Python", "Django", "FastAPI", "Flask", "React", "React.js", "PostgreSQL", "Postgres", "REST API", "Docker", "Git", "SQL", "JavaScript", "TypeScript", "Go"} {
		if containsSkill(text, raw) {
			result = appendUnique(result, skillToken(raw))
		}
	}
	sort.Strings(result)
	return result
}

func matchCandidateSkill(wanted string, skills []candidateSkill) (string, string) {
	wanted = skillToken(wanted)
	for _, skill := range skills {
		if skillToken(skill.Name) == wanted {
			if skill.Negative {
				return "missing", skill.Name
			}
			if skill.Known {
				return "matched", skill.Name
			}
			return "unknown", skill.Name
		}
	}
	return "unknown", wanted
}

func skillToken(value string) string {
	value = normalize(value)
	switch value {
	case "reactjs":
		return "react"
	case "postgres":
		return "postgresql"
	case "restapi":
		return "rest"
	}
	return value
}
func containsSkill(text, wanted string) bool {
	return strings.Contains(skillToken(text), skillToken(wanted)) || containsAny(text, []string{wanted})
}

func explicitOfficeLocation(v vacancy.Vacancy) string {
	if !allowsRemote(v, "") {
		return firstNonEmpty(v.Location, v.Area.Name)
	}
	return ""
}
func descriptionOfficeLocation(text string) string {
	if !containsAny(text, []string{"офис", "office", "в офисе", "на месте", "onsite", "on-site"}) {
		return ""
	}
	for _, city := range []string{"москва", "санкт-петербург", "екатеринбург", "новосибирск", "казань", "perm", "moscow"} {
		if strings.Contains(strings.ToLower(text), city) && !containsAny(text, []string{"удаленно", "удаленная", "remote", "дистанцион"}) {
			return city
		}
	}
	return ""
}
func allowsRemote(v vacancy.Vacancy, text string) bool {
	return containsAny(strings.Join([]string{v.WorkFormat, v.WorkSchedule, v.Location, text}, " "), []string{"remote", "удаленно", "удалённо", "дистанцион", "работа из дома"})
}
func sameCity(a, b string) bool {
	a, b = normalize(a), normalize(b)
	return a != "" && b != "" && (strings.Contains(a, b) || strings.Contains(b, a) || (strings.Contains(a, "екатеринбург") && strings.Contains(b, "екатеринбург")))
}
func mandatoryRelocation(text string) bool {
	return containsAny(text, []string{"обязательная релокация", "готовность к релокации", "relocation required", "релокация обязательна"})
}
func mandatoryTravel(text string) bool {
	return containsAny(text, []string{"обязательные командировки", "готовность к командировкам", "business trips required", "командировки обязательны"})
}
func explicitOnly(text string, positives, allowed []string) bool {
	return containsAny(text, positives) && !containsAny(text, allowed)
}
func roleCategoryMatch(text, role string) bool {
	role = normalize(role)
	return containsAny(role, []string{"backend", "support", "поддерж", "интеграц", "automation", "автоматизац", "developer", "разработ"}) && containsAny(text, []string{"python", "django", "backend", "поддерж", "интеграц", "автоматизац", "разработ", "api"})
}
func no(value string, allowed []string) bool {
	return strings.TrimSpace(value) != "" && !containsAny(value, allowed)
}
func containsAny(text string, values []string) bool {
	lower := strings.ToLower(text)
	for _, value := range values {
		if strings.Contains(lower, strings.ToLower(value)) {
			return true
		}
	}
	return false
}
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
func splitValues(value string) []string {
	return strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == ';' || r == '\n' || r == '|' })
}
func normalize(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.NewReplacer("ё", "е", "/", " ", "-", " ", "_", " ", ".", "", "+", " ").Replace(value)
	return strings.Join(strings.Fields(value), " ")
}
func concise(value string) string {
	value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	if len(value) > 180 {
		return value[:177] + "..."
	}
	return value
}
func appendUnique(values []string, additions ...string) []string {
	seen := make(map[string]bool, len(values)+len(additions))
	for _, value := range values {
		seen[normalize(value)] = true
	}
	for _, value := range additions {
		if strings.TrimSpace(value) != "" && !seen[normalize(value)] {
			values = append(values, strings.TrimSpace(value))
			seen[normalize(value)] = true
		}
	}
	return values
}
func sortedCopy(values []string) []string {
	result := append([]string{}, values...)
	sort.Strings(result)
	return result
}
func sortReasons(values *[]Reason) {
	sort.Slice(*values, func(i, j int) bool {
		if (*values)[i].Code != (*values)[j].Code {
			return (*values)[i].Code < (*values)[j].Code
		}
		return (*values)[i].Evidence < (*values)[j].Evidence
	})
}
func dedupeHard(values []hardConflict) []hardConflict {
	seen := map[string]bool{}
	result := []hardConflict{}
	for _, value := range values {
		if !seen[value.code] {
			result = append(result, value)
			seen[value.code] = true
		}
	}
	return result
}
func clamp(value, min, max int) int {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}
