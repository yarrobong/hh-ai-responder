package candidate

import (
	"errors"
	"strings"
)

// CandidateStory is a narrative value. Stories are context for communication,
// never independent evidence for a candidate fact.
type CandidateStory struct {
	ID            string   `json:"id,omitempty"`
	Title         string   `json:"title"`
	Situation     string   `json:"situation,omitempty"`
	Context       string   `json:"context,omitempty"`
	Summary       string   `json:"summary,omitempty"`
	Description   string   `json:"description,omitempty"`
	Story         string   `json:"story,omitempty"`
	Task          string   `json:"task,omitempty"`
	Problem       string   `json:"problem,omitempty"`
	Action        string   `json:"action,omitempty"`
	Actions       string   `json:"actions,omitempty"`
	Contribution  string   `json:"contribution,omitempty"`
	Result        string   `json:"result,omitempty"`
	Outcome       string   `json:"outcome,omitempty"`
	Achievement   string   `json:"achievement,omitempty"`
	Achievements  string   `json:"achievements,omitempty"`
	Technologies  []string `json:"technologies,omitempty"`
	Skills        []string `json:"skills,omitempty"`
	Keywords      []string `json:"keywords,omitempty"`
	Tags          []string `json:"tags,omitempty"`
	Roles         []string `json:"roles,omitempty"`
	Relevance     []string `json:"relevance,omitempty"`
	RelevantFor   []string `json:"relevant_for,omitempty"`
	RelevantRoles []string `json:"relevant_roles,omitempty"`
	ProfileRefs   []string `json:"profile_refs,omitempty"`
}

func (s CandidateStory) Validate() error {
	if strings.TrimSpace(s.Title) == "" {
		return errors.New("title must not be empty")
	}
	if strings.TrimSpace(s.Situation+s.Context+s.Summary+s.Description+s.Story+s.Task+s.Problem+s.Action+s.Actions+s.Contribution+s.Result+s.Outcome+s.Achievement+s.Achievements) == "" {
		return errors.New("story must contain experience details")
	}
	return nil
}
