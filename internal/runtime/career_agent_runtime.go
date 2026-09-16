package runtime

import (
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
		r.searchProfiles = append(r.searchProfiles, vacancySearchProfile{Name: plannedProfile.Query + " / " + plannedProfile.ResumeTitle, BaseURL: base, Params: params, URL: searchProfileURL(base, params)})
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

func (r *HHAIResponder) routeResumeForVacancy(value Vacancy) careeragent.RouteDecision {
	if r == nil {
		return careeragent.RouteDecision{Status: careeragent.RouteNoResume, Reasons: []string{"responder is nil"}}
	}
	decision := careeragent.RouteResume(careeragent.VacancyInput{ID: value.ID, Title: firstNonEmpty(value.Title, value.Name), Description: value.Description, RequiredSkills: append(append([]string{}, value.Skills...), value.Requirements...), Location: value.Area.Name, WorkFormat: value.WorkFormat}, r.careerAgentResumes)
	if r.careerAgentRoutes == nil {
		r.careerAgentRoutes = map[int]careeragent.RouteDecision{}
	}
	r.careerAgentRoutes[value.ID] = decision
	return decision
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
