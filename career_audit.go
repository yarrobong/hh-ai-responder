package main

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type CareerSnapshot struct {
	Vacancies      []Vacancy
	Applications   []JobApplication
	Conversations  []EmployerConversation
	Events         []ApplicationEvent
	Drafts         []AIDraft
	Clarifications []CandidateClarificationRequest
	Sync           HHSyncState
	StoreErrors    []string
	Consistency    map[string][]string
}
type CareerAuditWarning struct {
	Code       string `json:"code"`
	RecordType string `json:"record_type"`
	RecordID   string `json:"record_id,omitempty"`
	Severity   string `json:"severity"`
	Category   string `json:"category"`
}
type CareerAuditReport struct {
	GeneratedAt             time.Time            `json:"generated_at"`
	Stores                  map[string]int       `json:"stores"`
	StoreErrors             []string             `json:"store_errors"`
	Sync                    HHSyncState          `json:"sync"`
	Relations               map[string]int       `json:"relations"`
	Warnings                []CareerAuditWarning `json:"warnings"`
	WarningCounts           map[string]int       `json:"warning_counts"`
	Critical                []CareerAuditWarning `json:"critical"`
	ActualWarnings          []CareerAuditWarning `json:"actual_warnings"`
	IncompleteData          []CareerAuditWarning `json:"incomplete_data"`
	Informational           []CareerAuditWarning `json:"informational"`
	ActualWarningCounts     map[string]int       `json:"actual_warning_counts"`
	DataCompleteness        map[string]int       `json:"data_completeness"`
	UnknownHHStatuses       int                  `json:"unknown_hh_statuses"`
	FollowUpEligible        int                  `json:"follow_up_eligible"`
	CandidateActionRequired int                  `json:"candidate_action_required"`
}

func (d CareerSnapshot) input(a JobApplication) FollowUpInput {
	in := FollowUpInput{Application: a, AppliedAt: knownApplicationTime(a, d.Events), Warnings: []string{}}
	if len(d.StoreErrors) > 0 {
		in.Warnings = append(in.Warnings, "store_unavailable")
	}
	matches := 0
	for _, c := range d.Conversations {
		if c.ID == a.ConversationID && a.ConversationID != "" {
			in.Conversation = c
			matches++
		}
	}
	for _, c := range d.Conversations {
		if c.ID != in.Conversation.ID && c.HHConversationID != "" && c.HHConversationID == in.Conversation.HHConversationID {
			in.Warnings = append(in.Warnings, "duplicate_hh_conversation_id")
		}
	}
	ids := map[string]bool{}
	for _, m := range in.Conversation.Messages {
		key := string(m.Source) + "/" + m.ExternalID
		if m.ExternalID != "" && ids[key] {
			in.Warnings = append(in.Warnings, "duplicate_message_external_id")
		}
		ids[key] = true
		if m.validate() != nil {
			in.Warnings = append(in.Warnings, "invalid_message_fields")
		}
	}
	if matches != 1 {
		in.Warnings = append(in.Warnings, "missing_or_ambiguous_conversation")
	}
	if in.Conversation.ID != "" && (a.VacancyID <= 0 || in.Conversation.VacancyID != a.VacancyID) {
		in.Warnings = append(in.Warnings, "vacancy_relation_conflict")
	}
	for _, other := range d.Applications {
		if other.ID != a.ID && (a.ExternalID != "" && a.ExternalID == other.ExternalID || a.ConversationID != "" && a.ConversationID == other.ConversationID) {
			in.Warnings = append(in.Warnings, "ambiguous_application_relation")
		}
	}
	for _, q := range d.Clarifications {
		if q.Status == ClarificationPending && (a.ID != "" && q.ApplicationID == a.ID || in.Conversation.ID != "" && q.ConversationID == in.Conversation.ID) {
			in.PendingClarification = true
		}
	}
	for _, event := range d.Events {
		if event.ApplicationID == a.ID && event.Type == ApplicationEventFollowUpSent {
			in.PreviousFollowUps = append(in.PreviousFollowUps, event.Timestamp)
		}
	}
	// Legacy 'sent' marker lacks a trustworthy count/date; never reset it to zero.
	if in.Conversation.FollowUpState == ConversationFollowUpSent && len(in.PreviousFollowUps) == 0 {
		in.Warnings = append(in.Warnings, "undated_follow_up_history")
	}
	for k, v := range a.HHMetadata {
		if strings.HasPrefix(k, "warning") && v != "" && v != "false" {
			in.Warnings = append(in.Warnings, "application_consistency_warning")
		}
	}
	in.Warnings = append(in.Warnings, d.Consistency[in.Conversation.ID]...)
	return in
}
func (d CareerSnapshot) FollowUps(policy FollowUpPolicy, now time.Time) []FollowUpCandidate {
	result := []FollowUpCandidate{}
	for _, a := range d.Applications {
		result = append(result, (FollowUpEngine{policy}).Evaluate(d.input(a), now))
	}
	return result
}
func BuildCareerAuditReport(d CareerSnapshot, policy FollowUpPolicy, now time.Time) CareerAuditReport {
	r := CareerAuditReport{GeneratedAt: now, Stores: map[string]int{"vacancies": len(d.Vacancies), "applications": len(d.Applications), "conversations": len(d.Conversations), "messages": 0}, StoreErrors: append([]string{}, d.StoreErrors...), Sync: d.Sync, Relations: map[string]int{}, Warnings: []CareerAuditWarning{}, WarningCounts: map[string]int{}, ActualWarningCounts: map[string]int{}, DataCompleteness: map[string]int{}, Critical: []CareerAuditWarning{}, ActualWarnings: []CareerAuditWarning{}, IncompleteData: []CareerAuditWarning{}, Informational: []CareerAuditWarning{}}
	seenWarning := map[string]bool{}
	add := func(code, kind, id string) {
		key := code + "/" + kind + "/" + id
		if !seenWarning[key] {
			seenWarning[key] = true
			severity, category := auditSeverity(code)
			finding := CareerAuditWarning{Code: code, RecordType: kind, RecordID: id, Severity: severity, Category: category}
			r.Warnings = append(r.Warnings, finding)
			r.WarningCounts[code]++
			switch severity {
			case "CRITICAL":
				r.Critical = append(r.Critical, finding)
			case "WARNING":
				r.ActualWarnings = append(r.ActualWarnings, finding)
				r.ActualWarningCounts[code]++
			case "INCOMPLETE_DATA":
				r.IncompleteData = append(r.IncompleteData, finding)
			default:
				r.Informational = append(r.Informational, finding)
			}
			if code == "unknown_hh_status" {
				r.UnknownHHStatuses++
			}
		}
	}
	vs := map[int]bool{}
	external := map[string]bool{}
	for _, v := range d.Vacancies {
		completeness := string(v.DataCompleteness)
		if completeness == "" {
			completeness = string(inferVacancyCompleteness(v))
		}
		r.DataCompleteness[completeness]++
		if vs[v.ID] {
			add("duplicate_local_id", "vacancy", strconvID(v.ID))
		}
		vs[v.ID] = true
		if v.ExternalID != "" && external[v.ExternalID] {
			add("duplicate_hh_external_id", "vacancy", strconvID(v.ID))
		}
		external[v.ExternalID] = true
	}
	cs := map[string]EmployerConversation{}
	external = map[string]bool{}
	for _, c := range d.Conversations {
		if _, ok := cs[c.ID]; ok {
			add("duplicate_local_id", "conversation", c.ID)
		}
		cs[c.ID] = c
		if c.HHConversationID != "" && external[c.HHConversationID] {
			add("duplicate_hh_external_id", "conversation", c.ID)
		}
		external[c.HHConversationID] = true
	}
	linked := map[string]int{}
	external = map[string]bool{}
	apps := map[string]JobApplication{}
	for _, a := range d.Applications {
		if _, ok := apps[a.ID]; ok {
			add("duplicate_local_id", "application", a.ID)
		}
		apps[a.ID] = a
		if !vs[a.VacancyID] {
			add("application_without_vacancy", "application", a.ID)
		}
		if a.MatchResult == nil {
			add("application_without_match_result", "application", a.ID)
		}
		if a.ExternalID != "" && external[a.ExternalID] {
			add("duplicate_hh_external_id", "application", a.ID)
		}
		external[a.ExternalID] = true
		if a.RawStatus != "" || a.Source == ApplicationSourceHH {
			mapped, known := MapHHApplicationStatus(a.RawStatus)
			if !known {
				add("unknown_hh_status", "application", a.ID)
			} else if mapped != a.Status && !(mapped == ApplicationApplied && a.Status == ApplicationEmployerReplied) {
				add("hh_internal_status_conflict", "application", a.ID)
			}
		}
		if a.ConversationID != "" {
			linked[a.ConversationID]++
			if c, ok := cs[a.ConversationID]; !ok {
				add("missing_conversation", "application", a.ID)
			} else if c.VacancyID != a.VacancyID {
				add("vacancy_relation_conflict", "application", a.ID)
			} else {
				r.Relations["linked_applications"]++
			}
		}
	}
	for _, c := range d.Conversations {
		if linked[c.ID] == 0 {
			add("conversation_without_application", "conversation", c.ID)
			r.Relations["unresolved_conversations"]++
		}
		if linked[c.ID] > 1 {
			add("conversation_multiple_applications", "conversation", c.ID)
		}
		if c.RawStatus != "" || c.HHConversationID != "" {
			if _, known := MapHHApplicationStatus(c.RawStatus); !known {
				add("unknown_hh_status", "conversation", c.ID)
			}
		}
		ids := map[string]bool{}
		mext := map[string]bool{}
		for _, m := range c.Messages {
			r.Stores["messages"]++
			if m.Sender == ConversationSenderUnknown {
				add("unknown_message_sender", "conversation", c.ID)
			}
			if m.validate() != nil {
				add("invalid_message_direction_or_fields", "conversation", c.ID)
			}
			if ids[m.ID] || m.ExternalID != "" && mext[string(m.Source)+m.ExternalID] {
				add("duplicate_message_external_id", "conversation", c.ID)
			}
			ids[m.ID] = true
			mext[string(m.Source)+m.ExternalID] = true
		}
		state := d.ResolveConversation(c, now)
		for _, w := range state.Warnings {
			add(w, "conversation", c.ID)
		}
		last := state.LatestMessage
		if last != nil && (c.Status == ConversationWaitingEmployer && last.Sender == ConversationSenderEmployer || c.Status == ConversationCandidateActionRequired && last.Sender == ConversationSenderCandidate) {
			add("status_last_message_conflict", "conversation", c.ID)
		}
		if state.Status != ConversationManualReview && state.Status != c.Status {
			add("resolved_status_differs", "conversation", c.ID)
		}
		if state.Status == ConversationWaitingEmployer && (c.WaitingSince == nil || state.WaitingSince != nil && !c.WaitingSince.Equal(*state.WaitingSince)) {
			add("waiting_since_mismatch", "conversation", c.ID)
		}
		if state.Status == ConversationCandidateActionRequired {
			r.CandidateActionRequired++
		}
	}
	for _, q := range d.Clarifications {
		if q.Status == ClarificationPending {
			add("unresolved_clarification", "clarification", q.ID)
		}
	}
	for _, draft := range d.Drafts {
		if draft.Status != AIDraftGenerated && draft.Status != AIDraftApproved {
			continue
		}
		stale := false
		if draft.ConversationID != "" {
			c, ok := cs[draft.ConversationID]
			stale = !ok
			for _, m := range c.Messages {
				if m.Source != ConversationSourceAIDraft && m.Timestamp.After(draft.CreatedAt) {
					stale = true
				}
			}
			for _, a := range d.Applications {
				if a.ConversationID == c.ID && draft.Type == AIDraftFollowUp {
					stale = stale || (FollowUpEngine{policy}).Evaluate(d.input(a), now).Status != FollowUpEligible || draft.InputFingerprint != followUpFingerprint(d.input(a))
				}
			}
		}
		if a, ok := apps[draft.ApplicationID]; draft.ApplicationID != "" && (!ok || a.UpdatedAt.After(draft.CreatedAt)) {
			stale = true
		}
		if stale {
			add("stale_ai_draft", "draft", draft.ID)
		}
	}
	for _, f := range d.FollowUps(policy, now) {
		if f.Status == FollowUpEligible {
			r.FollowUpEligible++
		}
	}
	sort.Slice(r.Warnings, func(i, j int) bool {
		a, b := r.Warnings[i], r.Warnings[j]
		return a.Code+a.RecordType+a.RecordID < b.Code+b.RecordType+b.RecordID
	})
	return r
}

func auditSeverity(code string) (string, string) {
	switch code {
	case "duplicate_local_id", "duplicate_hh_external_id", "missing_conversation", "vacancy_relation_conflict", "hh_internal_status_conflict", "conflicting_terminal_status", "raw_status_conflicts_with_terminal", "status_last_message_conflict", "invalid_message_direction_or_fields", "duplicate_message_external_id", "corrupted_store":
		return "CRITICAL", "broken_relation"
	case "application_without_match_result":
		return "INCOMPLETE_DATA", "insufficient_match_data"
	case "conversation_without_application", "message_content_unavailable":
		return "INFO", "unresolved_relation"
	case "application_without_vacancy":
		return "CRITICAL", "broken_relation"
	case "unknown_hh_status":
		return "WARNING", "unknown_hh_state"
	case "sync_consistency_warning":
		return "WARNING", "sync"
	case "manual_review", "unresolved_clarification", "stale_ai_draft", "context_unavailable", "consistency_warning":
		return "WARNING", "candidate_action"
	default:
		return "WARNING", "system"
	}
}
func strconvID(n int) string { raw, _ := json.Marshal(n); return string(raw) }

// Diagnostic decoding deliberately bypasses store validation to report duplicates
// and orphans. These snapshots are never supplied to an HH or mutation path.
func readCareerAuditSnapshot(wd, syncPath string) CareerSnapshot {
	d := CareerSnapshot{Consistency: map[string][]string{}}
	load := func(name string, target any) {
		raw, err := os.ReadFile(filepath.Join(wd, name))
		if errors.Is(err, os.ErrNotExist) {
			return
		}
		var envelope struct {
			Version int `json:"version"`
		}
		var fields map[string]json.RawMessage
		arrayKey := map[string]string{VacanciesFilename: "vacancies", JobApplicationsFilename: "applications", EmployerConversationsFilename: "conversations", "ai_drafts.json": "drafts", "candidate_clarifications.json": "clarifications"}[name]
		_ = json.Unmarshal(raw, &fields)
		array, hasArray := fields[arrayKey]
		if err != nil || !hasArray || string(array) == "null" || json.Unmarshal(raw, &envelope) != nil || envelope.Version != 1 || json.Unmarshal(raw, target) != nil {
			d.StoreErrors = append(d.StoreErrors, name+": unreadable or invalid JSON")
		}
	}
	var v struct {
		Vacancies []Vacancy `json:"vacancies"`
	}
	load(VacanciesFilename, &v)
	d.Vacancies = v.Vacancies
	var a applicationStoreFile
	load(JobApplicationsFilename, &a)
	d.Applications = a.Applications
	d.Events = a.Events
	var c conversationStoreFile
	load(EmployerConversationsFilename, &c)
	d.Conversations = c.Conversations
	var drafts aiDraftStoreFile
	load("ai_drafts.json", &drafts)
	d.Drafts = drafts.Drafts
	var q clarificationStoreFile
	load("candidate_clarifications.json", &q)
	d.Clarifications = q.Clarifications
	if syncPath == "" {
		syncPath = filepath.Join(wd, HHSyncStateFilename)
	}
	raw, err := os.ReadFile(syncPath)
	if err == nil {
		if json.Unmarshal(raw, &d.Sync) != nil {
			d.StoreErrors = append(d.StoreErrors, "invalid sync state")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		d.StoreErrors = append(d.StoreErrors, "unreadable sync state")
	}
	return d
}
func runAuditCommand(cfg Config, out io.Writer) error {
	wd, err := os.Getwd()
	if err != nil {
		return err
	}
	d := readCareerAuditSnapshot(wd, cfg.HHSyncStatePath)
	kb := NewCandidateKnowledgeBase(cfg.CandidateProfilePath)
	if err := kb.Load(); err != nil {
		d.StoreErrors = append(d.StoreErrors, "candidate knowledge unavailable")
	} else {
		d = withCareerConsistency(d, NewCandidateContextResolver(kb))
	}
	return writeJSON(out, BuildCareerAuditReport(d, cfg.FollowUpPolicy, time.Now().UTC()))
}

func (d CareerSnapshot) ResolveConversation(c EmployerConversation, now time.Time) ConversationResolution {
	a := JobApplication{}
	count := 0
	warnings := append([]string{}, d.Consistency[c.ID]...)
	for _, v := range d.Applications {
		if v.ConversationID == c.ID {
			a = v
			count++
		}
	}
	if count > 1 {
		warnings = append(warnings, "ambiguous_application_relation")
	}
	pending := false
	for _, q := range d.Clarifications {
		if q.Status == ClarificationPending && (q.ConversationID == c.ID || a.ID != "" && q.ApplicationID == a.ID) {
			pending = true
		}
	}
	return (ConversationStateResolver{}).Resolve(a, c, knownApplicationTime(a, d.Events), pending, warnings, now)
}
