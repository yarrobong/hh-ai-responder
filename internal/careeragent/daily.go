package careeragent

import (
	"fmt"
	"sort"
	"time"
)

// DailyResultCode is the stable operator-facing result of one read-only daily
// orchestration run.
type DailyResultCode string

const (
	DailyResultSuccess        DailyResultCode = "SUCCESS"
	DailyResultPartialSuccess DailyResultCode = "PARTIAL_SUCCESS"
	DailyResultFailed         DailyResultCode = "FAILED"
)

type DailyVacancySummary struct {
	Scanned        int `json:"scanned"`
	Found          int `json:"found"`
	New            int `json:"new"`
	Rejected       int `json:"rejected"`
	Matched        int `json:"matched"`
	ReviewRequired int `json:"review_required"`
	AIReviewed     int `json:"ai_reviewed"`
	Prepared       int `json:"prepared"`
}

type DailyCommunicationSummary struct {
	ConversationsSynced int `json:"conversations_synced"`
	NewMessages         int `json:"new_messages"`
	RepliesNeeded       int `json:"replies_needed"`
	Interviews          int `json:"interviews"`
	TestTasks           int `json:"test_tasks"`
	Offers              int `json:"offers"`
	Rejections          int `json:"rejections"`
	FollowUps           int `json:"follow_ups"`
	Failures            int `json:"failures"`
}

type AIBudgetSummary struct {
	Requested int `json:"requested"`
	Succeeded int `json:"succeeded"`
	Skipped   int `json:"skipped"`
	Failed    int `json:"failed"`
}

type DailyCareerAgentSummary struct {
	Vacancy       DailyVacancySummary       `json:"vacancy"`
	Communication DailyCommunicationSummary `json:"communication"`
	AI            AIBudgetSummary           `json:"ai"`
	Attention     int                       `json:"attention"`
	HHWrites      int                       `json:"hh_writes"`
	Result        DailyResultCode           `json:"result"`
	Failures      []string                  `json:"failures,omitempty"`
}

// DailyStageSummary is returned by one read-only stage. The orchestrator
// merges it into the run summary; stages do not own AgentRun lifecycle.
type DailyStageSummary struct {
	Vacancy       DailyVacancySummary       `json:"vacancy"`
	Communication DailyCommunicationSummary `json:"communication"`
	AI            AIBudgetSummary           `json:"ai"`
	Failures      []string                  `json:"failures,omitempty"`
}

type AttentionItem struct {
	ID             string    `json:"id"`
	Type           string    `json:"type"`
	Priority       int       `json:"priority"`
	VacancyID      int       `json:"vacancy_id,omitempty"`
	ApplicationID  string    `json:"application_id,omitempty"`
	ConversationID string    `json:"conversation_id,omitempty"`
	Title          string    `json:"title"`
	Summary        string    `json:"summary"`
	Reason         string    `json:"reason"`
	Risk           string    `json:"risk"`
	NextAction     string    `json:"next_action"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type DailyStageResult struct {
	Summary   DailyStageSummary `json:"summary"`
	Items     []AgentRunItem    `json:"items,omitempty"`
	Attention []AttentionItem   `json:"attention,omitempty"`
}

type DailyCareerAgentRun struct {
	Run              AgentRun                `json:"run"`
	Summary          DailyCareerAgentSummary `json:"summary"`
	Attention        []AttentionItem         `json:"attention,omitempty"`
	IdempotentReplay bool                    `json:"idempotent_replay,omitempty"`
}

func DailyRunID(at time.Time) string {
	return "daily-career-agent-" + at.UTC().Format("2006-01-02")
}

func DailyResultForStatus(status AgentRunStatus) DailyResultCode {
	switch status {
	case AgentRunStatusCompleted:
		return DailyResultSuccess
	case AgentRunStatusPartial:
		return DailyResultPartialSuccess
	default:
		return DailyResultFailed
	}
}

func mergeDailyStageSummary(dst *DailyCareerAgentSummary, src DailyStageSummary) {
	dst.Vacancy.Scanned += src.Vacancy.Scanned
	dst.Vacancy.Found += src.Vacancy.Found
	dst.Vacancy.New += src.Vacancy.New
	dst.Vacancy.Rejected += src.Vacancy.Rejected
	dst.Vacancy.Matched += src.Vacancy.Matched
	dst.Vacancy.ReviewRequired += src.Vacancy.ReviewRequired
	dst.Vacancy.AIReviewed += src.Vacancy.AIReviewed
	dst.Vacancy.Prepared += src.Vacancy.Prepared
	dst.Communication.ConversationsSynced += src.Communication.ConversationsSynced
	dst.Communication.NewMessages += src.Communication.NewMessages
	dst.Communication.RepliesNeeded += src.Communication.RepliesNeeded
	dst.Communication.Interviews += src.Communication.Interviews
	dst.Communication.TestTasks += src.Communication.TestTasks
	dst.Communication.Offers += src.Communication.Offers
	dst.Communication.Rejections += src.Communication.Rejections
	dst.Communication.FollowUps += src.Communication.FollowUps
	dst.Communication.Failures += src.Communication.Failures
	dst.AI.Requested += src.AI.Requested
	dst.AI.Succeeded += src.AI.Succeeded
	dst.AI.Skipped += src.AI.Skipped
	dst.AI.Failed += src.AI.Failed
	dst.Failures = appendUniqueStrings(dst.Failures, src.Failures...)
}

func sortAttentionItems(items []AttentionItem) {
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Priority != items[j].Priority {
			return items[i].Priority < items[j].Priority
		}
		if !items[i].UpdatedAt.Equal(items[j].UpdatedAt) {
			return items[i].UpdatedAt.Before(items[j].UpdatedAt)
		}
		return items[i].ID < items[j].ID
	})
}

func dailyStageFailureItem(runID string, stage AgentRunStage, now time.Time, err error) AgentRunItem {
	return AgentRunItem{
		ID: "daily-stage-" + string(stage), RunID: runID, TargetType: "daily_stage", TargetID: string(stage),
		Stage: stage, Status: AgentRunItemStatusFailed, DecisionCode: "STAGE_FAILED", ErrorCode: RedactAgentError(err), CreatedAt: now,
	}
}

func dailyItemID(runID string, item AgentRunItem, index int) string {
	if item.ID != "" {
		return item.ID
	}
	return fmt.Sprintf("%s-item-%d", runID, index)
}

// Runtime adapters call these narrow helpers so the orchestration package
// cannot mutate the summary rules or item identity accidentally.
func MergeDailyStageSummaryForRuntime(dst *DailyCareerAgentSummary, src DailyStageSummary) {
	mergeDailyStageSummary(dst, src)
}

func DailyStageFailureItemForRuntime(runID string, stage AgentRunStage, now time.Time, err error) AgentRunItem {
	return dailyStageFailureItem(runID, stage, now, err)
}

func DailyItemIDForRuntime(runID string, item AgentRunItem, index int) string {
	return dailyItemID(runID, item, index)
}
