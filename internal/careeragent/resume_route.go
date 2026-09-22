package careeragent

import "sort"

// ResumeRouteStatus is the safe product vocabulary for resume selection.
// MATCH is a deterministic fit signal, not an application authorization.
type ResumeRouteStatus string

const (
	ResumeRouteMatch          ResumeRouteStatus = "MATCH"
	ResumeRouteReviewRequired ResumeRouteStatus = "REVIEW_REQUIRED"
	ResumeRouteNoMatch        ResumeRouteStatus = "NO_MATCH"
)

func (s ResumeRouteStatus) valid() bool {
	switch s {
	case ResumeRouteMatch, ResumeRouteReviewRequired, ResumeRouteNoMatch:
		return true
	default:
		return false
	}
}

// ResumeRoute is an additive, review-friendly projection of RouteDecision.
// Evidence is derived from trusted resume/vacancy inputs and deterministic
// routing output; no AI output can add a candidate fact here.
type ResumeRoute struct {
	VacancyID           int               `json:"vacancy_id"`
	ResumeID            string            `json:"resume_id,omitempty"`
	Confidence          string            `json:"confidence"`
	Status              ResumeRouteStatus `json:"status"`
	Evidence            []string          `json:"evidence,omitempty"`
	MatchedSkills       []string          `json:"matched_skills,omitempty"`
	MissingRequirements []string          `json:"missing_requirements,omitempty"`
}

// ResolveResumeRoute adapts the established deterministic router without
// changing its legacy status strings or selection algorithm.
func ResolveResumeRoute(vacancy VacancyInput, resumes []ResumeProfile) ResumeRoute {
	decision := RouteResume(vacancy, resumes)
	result := ResumeRoute{
		VacancyID:  vacancy.ID,
		Confidence: decision.Confidence,
		Status:     ResumeRouteReviewRequired,
		Evidence:   append([]string(nil), decision.Reasons...),
	}
	if decision.ReasonCode != "" {
		result.Evidence = appendUniqueStrings(result.Evidence, "reason:"+decision.ReasonCode)
	}
	if decision.RoleEvidence.EvidenceAvailable {
		result.Evidence = appendUniqueStrings(result.Evidence, decision.RoleEvidence.SpecificSignals...)
	}
	if decision.Status == RouteNoResume {
		result.Status = ResumeRouteNoMatch
		return result
	}
	if decision.Status == RouteReviewRequired && (decision.ReasonCode == RouteReasonNoSuitable || decision.ReasonCode == RouteReasonOutOfScope) {
		result.Status = ResumeRouteNoMatch
		return result
	}
	if decision.SelectedResumeID == "" {
		return result
	}
	result.ResumeID = decision.SelectedResumeID

	for _, candidate := range decision.AlternativeScores {
		if candidate.ResumeID != decision.SelectedResumeID {
			continue
		}
		result.MatchedSkills = appendUniqueStrings(result.MatchedSkills, candidate.SpecificMatches...)
		result.MatchedSkills = appendUniqueStrings(result.MatchedSkills, candidate.PartialMatches...)
		result.Evidence = appendUniqueStrings(result.Evidence, candidate.StrongRoleEvidence...)
		result.Evidence = appendUniqueStrings(result.Evidence, candidate.Reasons...)
		result.Evidence = appendUniqueStrings(result.Evidence, candidate.HardBlockers...)
		break
	}
	for _, requirement := range decision.HardRequirements {
		if requirement.Status != "met" {
			result.MissingRequirements = appendUniqueStrings(result.MissingRequirements, requirement.Requirement)
		}
	}
	sort.Strings(result.MatchedSkills)
	sort.Strings(result.MissingRequirements)
	if decision.Status == RouteSelected && decision.Confidence != ConfidenceLow && len(result.MissingRequirements) == 0 && len(decision.HardBlockers) == 0 {
		result.Status = ResumeRouteMatch
	}
	return result
}
