package runtime

const JobApplicationsFilename = "job_applications.json"

type ApplicationStats struct {
	Total          int     `json:"total"`
	Discovered     int     `json:"discovered"`
	Applied        int     `json:"applied"`
	WaitingReply   int     `json:"waiting_reply"`
	Interviews     int     `json:"interviews"`
	Offers         int     `json:"offers"`
	Rejected       int     `json:"rejected"`
	ResponseRate   float64 `json:"response_rate"`
	ConversionRate float64 `json:"conversion_rate"`
}

// ApplicationStatistics is kept as a descriptive alias for callers that use
// the full domain name.
type ApplicationStatistics = ApplicationStats

// ApplicationContext is a read-only aggregate. Empty MatchResult and
// Conversation mean that the corresponding optional link has not been saved.
type ApplicationContext struct {
	Application       JobApplication            `json:"application"`
	MatchResult       MatchResult               `json:"match_result"`
	Conversation      EmployerConversation      `json:"conversation"`
	Timeline          []ApplicationEvent        `json:"timeline"`
	CandidateContext  CandidateContext          `json:"candidate_context"`
	RelevantExamples  []SafeSemanticSelection   `json:"relevant_examples,omitempty"`
	RelevantKnowledge RelevantKnowledgeSnapshot `json:"relevant_knowledge_snapshot,omitempty"`
}
