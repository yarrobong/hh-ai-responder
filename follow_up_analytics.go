package main

import "time"

type FollowUpAnalytics struct {
	AverageEmployerResponseHours *float64 `json:"average_employer_response_hours"`
	EmployerResponseSamples      int      `json:"employer_response_samples"`
	AverageWaitingHours          *float64 `json:"average_waiting_hours"`
	WaitingSamples               int      `json:"waiting_samples"`
	FollowUpEligible             int      `json:"follow_up_eligible"`
	FollowUpDrafted              int      `json:"follow_up_drafted"`
	CandidateActionRequired      int      `json:"candidate_action_required"`
	MinimumSamples               int      `json:"minimum_samples"`
	Note                         string   `json:"note"`
}

func (d CareerSnapshot) FollowUpAnalytics(policy FollowUpPolicy, now time.Time) FollowUpAnalytics {
	r := FollowUpAnalytics{MinimumSamples: 3, Note: "Descriptive local metrics only; averages are withheld below 3 samples. Incomplete or conflicting histories are excluded. Response time measures the first employer reply after the start of each unanswered candidate turn, including a confirmed application."}
	responseHours, waitingHours := 0.0, 0.0
	for _, c := range d.Conversations {
		if d.ResolveConversation(c, now).Status == ConversationCandidateActionRequired {
			r.CandidateActionRequired++
		}
	}
	seen := map[string]bool{}
	for _, a := range d.Applications {
		in := d.input(a)
		c := in.Conversation
		if seen[c.ID] || c.ID == "" {
			continue
		}
		seen[c.ID] = true
		resolved := (ConversationStateResolver{}).Resolve(a, c, in.AppliedAt, in.PendingClarification, in.Warnings, now)
		if resolved.Status == ConversationWaitingEmployer && resolved.WaitingSince != nil {
			waitingHours += now.Sub(*resolved.WaitingSince).Hours()
			r.WaitingSamples++
		}
		if len(resolved.Warnings) > 0 {
			continue
		}
		waiting := in.AppliedAt
		for _, m := range deliveredMessages(c.Messages) {
			if m.Timestamp.After(now) {
				continue
			}
			if m.Sender == ConversationSenderCandidate {
				if waiting == nil {
					t := m.Timestamp
					waiting = &t
				}
			} else if waiting != nil && !m.Timestamp.Before(*waiting) {
				responseHours += m.Timestamp.Sub(*waiting).Hours()
				r.EmployerResponseSamples++
				waiting = nil
			}
		}
	}
	if r.EmployerResponseSamples >= r.MinimumSamples {
		v := responseHours / float64(r.EmployerResponseSamples)
		r.AverageEmployerResponseHours = &v
	}
	if r.WaitingSamples >= r.MinimumSamples {
		v := waitingHours / float64(r.WaitingSamples)
		r.AverageWaitingHours = &v
	}
	for _, f := range d.FollowUps(policy, now) {
		if f.Status == FollowUpEligible {
			r.FollowUpEligible++
		}
	}
	drafted := map[string]bool{}
	for _, v := range d.Drafts {
		if v.Type == AIDraftFollowUp && v.Status == AIDraftGenerated {
			drafted[v.ApplicationID] = true
		}
	}
	r.FollowUpDrafted = len(drafted)
	return r
}
