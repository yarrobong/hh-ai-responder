package runtime

import "hh-ai-responder/internal/careeragent"

type CareerAgentResumeRouteResult struct {
	Type                string                         `json:"type"`
	Stage               string                         `json:"stage,omitempty"`
	VacancyID           int                            `json:"vacancy_id"`
	Status              string                         `json:"status"`
	ReasonCode          string                         `json:"reason_code,omitempty"`
	SelectedResumeID    string                         `json:"selected_resume_id,omitempty"`
	SelectedResumeTitle string                         `json:"selected_resume_title,omitempty"`
	Score               int                            `json:"score"`
	AlternativeScores   []careeragent.ResumeScore      `json:"alternative_resume_scores,omitempty"`
	Reasons             []string                       `json:"reasons,omitempty"`
	Confidence          string                         `json:"confidence"`
	HardRequirements    []careeragent.RequirementState `json:"hard_requirements,omitempty"`
	HardBlockers        []string                       `json:"hard_blockers,omitempty"`
}

func careerAgentRouteEvent(value careeragent.RouteDecision) CareerAgentResumeRouteResult {
	return CareerAgentResumeRouteResult{Type: "career_agent_resume_route", Stage: "final", VacancyID: value.VacancyID, Status: value.Status, ReasonCode: value.ReasonCode, SelectedResumeID: value.SelectedResumeID, SelectedResumeTitle: value.SelectedResumeTitle, Score: value.Score, AlternativeScores: value.AlternativeScores, Reasons: value.Reasons, Confidence: value.Confidence, HardRequirements: value.HardRequirements, HardBlockers: value.HardBlockers}
}

func careerAgentPreliminaryRouteEvent(value careeragent.PreliminaryRouteDecision) CareerAgentResumeRouteResult {
	return CareerAgentResumeRouteResult{Type: "career_agent_resume_route", Stage: "preliminary", VacancyID: value.VacancyID, Status: value.Status, ReasonCode: value.ReasonCode, AlternativeScores: value.TopCandidates, Reasons: value.Reasons}
}
