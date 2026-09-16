package runtime

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"hh-ai-responder/internal/careeragent"
)

func (r *HHAIResponder) initializeCareerAgentProfiles(cfg Config) {
	if r == nil {
		return
	}
	profiles := careeragent.NormalizeResumes(r.resumes)
	if cfg.ResumeRegistryPath != "" {
		overrides, err := (careeragent.RegistryStore{Path: cfg.ResumeRegistryPath}).Load()
		if err == nil {
			profiles = careeragent.ApplyRegistryOverrides(profiles, overrides)
		}
		if err != nil && logger != nil {
			logger.Warn("resume registry overrides ignored: %v", err)
		}
	}
	r.careerAgentResumes = profiles
	r.rebuildCareerAgentSearchProfiles(profiles)
	if len(r.careerAgentProfiles) > 0 {
		return
	}
	if r.resumeFactsByHash == nil {
		r.resumeFactsByHash = map[string]ResumeFacts{}
	}
}

func (r *HHAIResponder) rebuildCareerAgentSearchProfiles(profiles []careeragent.ResumeProfile) {
	if r == nil {
		return
	}
	roles := []string{}
	if value := r.candidateProfile.WorkPreferences.PrimaryRoles.Value; value != "" {
		roles = append(roles, value)
	}
	if value := r.candidateProfile.WorkPreferences.SecondaryRoles.Value; value != "" {
		roles = append(roles, value)
	}
	skills := []string{}
	for _, skill := range r.candidateProfile.Skills {
		if skill.Name != "" {
			skills = append(skills, skill.Name)
		}
	}
	planned := careeragent.PlanSearches(profiles, careeragent.CandidateSignals{Roles: roles, Skills: skills, PreferredLocation: r.candidateProfile.Identity.Location.Value}, careeragent.SearchConstraints{MaxProfiles: r.careerAgentMaxSearchProfiles, SearchPeriodDays: r.searchPeriodDays, IncludeKeywords: r.careerAgentIncludeKeywords, ExcludeKeywords: r.careerAgentExcludeKeywords})
	r.careerAgentProfiles = planned
	r.searchProfiles = nil
	for _, plannedProfile := range planned {
		params := cloneValues(plannedProfile.Params)
		base := r.baseURL
		if base == nil {
			continue
		}
		r.searchProfiles = append(r.searchProfiles, vacancySearchProfile{ID: plannedProfile.ID, ResumeID: plannedProfile.ResumeID, Name: plannedProfile.Query + " / " + plannedProfile.ResumeTitle, BaseURL: base, Params: params, URL: searchProfileURL(base, params)})
	}
	if len(r.searchProfiles) > 0 {
		r.searchParams = cloneValues(r.searchProfiles[0].Params)
	}
	if r.resumeFactsByHash == nil {
		r.resumeFactsByHash = map[string]ResumeFacts{}
	}
}

func (r *HHAIResponder) resumeProfileForHash(hash string) (careeragent.ResumeProfile, bool) {
	for _, profile := range r.careerAgentResumes {
		if profile.Hash == hash || profile.ID == hash {
			return profile, profile.Enabled
		}
	}
	return careeragent.ResumeProfile{}, false
}

func (r *HHAIResponder) resumeHashForProfile(id string) string {
	for _, profile := range r.careerAgentResumes {
		if profile.ID == id {
			return profile.Hash
		}
	}
	return ""
}

func (r *HHAIResponder) resumeProfileID(hash string) string {
	if r == nil {
		return ""
	}
	for _, profile := range r.careerAgentResumes {
		if profile.ID == hash || profile.Hash == hash {
			return profile.ID
		}
	}
	return ""
}

func (r *HHAIResponder) routeResumeForVacancy(value Vacancy) careeragent.RouteDecision {
	if r == nil {
		return careeragent.RouteDecision{Status: careeragent.RouteNoResume, Reasons: []string{"responder is nil"}}
	}
	decision := careeragent.RouteResume(r.careerAgentVacancyInput(value), r.careerAgentResumes)
	if r.careerAgentRoutes == nil {
		r.careerAgentRoutes = map[int]careeragent.RouteDecision{}
	}
	r.careerAgentRoutes[value.ID] = decision
	return decision
}

func (r *HHAIResponder) careerAgentVacancyInput(value Vacancy) careeragent.VacancyInput {
	var sources []careeragent.SearchProfileEvidence
	if r != nil {
		sources = append(sources, r.careerAgentSearchSources[value.ID]...)
	}
	return careeragent.VacancyInput{
		ID: value.ID, Title: firstNonEmpty(value.Title, value.Name), Description: value.Description,
		RequiredSkills: append(append([]string{}, value.Requirements...), value.Skills...), KeySkills: append([]string{}, value.Skills...),
		ProfessionalRoles: append([]string{}, value.ProfessionalRoles...), Experience: value.WorkExperience, Employment: value.EmploymentType,
		Schedule: value.WorkSchedule, Salary: firstNonEmpty(value.Salary, FormatCompensation(&value.Compensation)), Location: firstNonEmpty(value.Location, value.Area.Name),
		WorkFormat: value.WorkFormat, SearchProfiles: sources, DetailAvailable: value.DataCompleteness == DataCompletenessFull,
	}
}

func (r *HHAIResponder) preliminaryRouteForVacancy(value Vacancy) careeragent.PreliminaryRouteDecision {
	if r == nil {
		return careeragent.PreliminaryRouteDecision{VacancyID: value.ID, Status: careeragent.PreliminaryNoResume, ReasonCode: careeragent.RouteReasonNoStrong, Reasons: []string{"responder is nil"}}
	}
	return careeragent.PreliminaryRouteResume(r.careerAgentVacancyInput(value), r.careerAgentResumes)
}

func appendCareerAgentSearchSource(values []careeragent.SearchProfileEvidence, value careeragent.SearchProfileEvidence) []careeragent.SearchProfileEvidence {
	for _, existing := range values {
		if existing.ID == value.ID && existing.ResumeID == value.ResumeID && existing.Label == value.Label {
			return values
		}
	}
	return append(values, value)
}

func (r *HHAIResponder) fetchCareerAgentDetail(ctx context.Context, value Vacancy) (Vacancy, error) {
	if r == nil {
		return Vacancy{}, errors.New("HH responder is not configured")
	}
	if r.careerAgentDetailCache == nil {
		r.careerAgentDetailCache = map[int]Vacancy{}
	}
	if cached, ok := r.careerAgentDetailCache[value.ID]; ok {
		return cached, nil
	}
	reader := r.hhReadClient()
	if reader == nil {
		return Vacancy{}, errors.New("HH read client is not configured")
	}
	record, err := reader.ReadVacancyDetail(ctx, value.ID)
	if err != nil {
		return Vacancy{}, err
	}
	detail, err := mapHHVacancy(record)
	if err != nil {
		return Vacancy{}, err
	}
	enriched := mergeCareerAgentVacancy(value, detail)
	r.careerAgentDetailCache[value.ID] = enriched
	return enriched, nil
}

func mergeCareerAgentVacancy(search, detail Vacancy) Vacancy {
	merged := search
	merged.Title = firstNonEmpty(detail.Title, search.Title)
	merged.Name = firstNonEmpty(detail.Name, search.Name)
	merged.Description = firstNonEmpty(detail.Description, search.Description)
	merged.Salary = firstNonEmpty(detail.Salary, search.Salary)
	merged.SalaryCurrency = firstNonEmpty(detail.SalaryCurrency, search.SalaryCurrency)
	merged.WorkFormat = firstNonEmpty(detail.WorkFormat, search.WorkFormat)
	merged.EmploymentType = firstNonEmpty(detail.EmploymentType, search.EmploymentType)
	merged.WorkSchedule = firstNonEmpty(detail.WorkSchedule, search.WorkSchedule)
	merged.WorkExperience = firstNonEmpty(detail.WorkExperience, search.WorkExperience)
	merged.Area.Name = firstNonEmpty(detail.Area.Name, search.Area.Name)
	merged.Location = firstNonEmpty(detail.Location, search.Location)
	merged.Company.Name = firstNonEmpty(detail.Company.Name, search.Company.Name)
	merged.ResponseURL = firstNonEmpty(detail.ResponseURL, search.ResponseURL)
	merged.Archived = detail.Archived || search.Archived
	merged.ResponseLetterRequired = detail.ResponseLetterRequired || search.ResponseLetterRequired
	merged.UserTestPresent = detail.UserTestPresent || search.UserTestPresent
	if len(detail.Requirements) > 0 {
		merged.Requirements = append([]string(nil), detail.Requirements...)
	}
	if len(detail.Skills) > 0 {
		merged.Skills = append([]string(nil), detail.Skills...)
	}
	if len(detail.ProfessionalRoles) > 0 {
		merged.ProfessionalRoles = append([]string(nil), detail.ProfessionalRoles...)
	}
	if len(detail.Links) > 0 {
		merged.Links = detail.Links
	}
	merged.DataCompleteness = detail.DataCompleteness
	return merged
}

func careerAgentEvidence(value Vacancy) []string {
	evidence := []string{}
	if strings.TrimSpace(firstNonEmpty(value.Title, value.Name)) != "" {
		evidence = append(evidence, "title")
	}
	if strings.TrimSpace(value.Description) != "" {
		evidence = append(evidence, "description")
	}
	if len(value.Requirements) > 0 {
		evidence = append(evidence, "structured_requirements")
	}
	if len(value.Skills) > 0 {
		evidence = append(evidence, "key_skills")
	}
	if len(value.ProfessionalRoles) > 0 {
		evidence = append(evidence, "professional_roles")
	}
	if strings.TrimSpace(value.WorkExperience) != "" {
		evidence = append(evidence, "experience")
	}
	if strings.TrimSpace(value.WorkSchedule) != "" {
		evidence = append(evidence, "schedule")
	}
	if strings.TrimSpace(value.EmploymentType) != "" {
		evidence = append(evidence, "employment")
	}
	if strings.TrimSpace(value.WorkFormat) != "" {
		evidence = append(evidence, "work_format")
	}
	if strings.TrimSpace(firstNonEmpty(value.Location, value.Area.Name)) != "" {
		evidence = append(evidence, "location")
	}
	if strings.TrimSpace(firstNonEmpty(value.Salary, FormatCompensation(&value.Compensation))) != "" {
		evidence = append(evidence, "salary")
	}
	return evidence
}

func (r *HHAIResponder) activateResume(hash string) (ResumeItem, LegacyCandidateContext, *CandidateContextResolver, error) {
	for _, value := range r.resumes {
		if value.Hash != hash {
			continue
		}
		if r.resumeFactsByHash == nil {
			r.resumeFactsByHash = map[string]ResumeFacts{}
		}
		facts, ok := r.resumeFactsByHash[hash]
		if !ok {
			old := r.resumeHash
			r.resumeHash = hash
			loaded, err := r.GetResumeFacts()
			r.resumeHash = old
			if err != nil {
				return ResumeItem{}, LegacyCandidateContext{}, nil, fmt.Errorf("load facts for resume %s: %w", hash, err)
			}
			facts, ok = loaded, true
			r.resumeFactsByHash[hash] = facts
		}
		r.resumeHash, r.resumeFacts, r.resumeExperience = value.Hash, facts, facts.ExperienceText
		legacy, resolver, err := r.canonicalCandidateContext(value)
		if err != nil {
			return ResumeItem{}, LegacyCandidateContext{}, nil, err
		}
		return value, legacy, resolver, nil
	}
	return ResumeItem{}, LegacyCandidateContext{}, nil, errors.New("selected resume is not available")
}

func careerAgentProfileURL(base *url.URL, profile careeragent.SearchProfile) string {
	if base == nil {
		return ""
	}
	value := *base
	value.Path = "/search/vacancy"
	value.RawQuery = profile.Params.Encode()
	return value.String()
}

func careerAgentReasonList(value careeragent.RouteDecision) []string {
	result := append([]string(nil), value.Reasons...)
	if strings.TrimSpace(value.SelectedResumeID) != "" {
		result = append(result, "resume="+value.SelectedResumeID)
	}
	return result
}
