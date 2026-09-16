package runtime

import "hh-ai-responder/internal/careeragent"

type CareerAgentResumeRouteResult struct {
	Type                string                         `json:"type"`
	VacancyID           int                            `json:"vacancy_id"`
	Status              string                         `json:"status"`
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
	return CareerAgentResumeRouteResult{Type: "career_agent_resume_route", VacancyID: value.VacancyID, Status: value.Status, SelectedResumeID: value.SelectedResumeID, SelectedResumeTitle: value.SelectedResumeTitle, Score: value.Score, AlternativeScores: value.AlternativeScores, Reasons: value.Reasons, Confidence: value.Confidence, HardRequirements: value.HardRequirements, HardBlockers: value.HardBlockers}
}
