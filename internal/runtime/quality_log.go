package runtime

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"hh-ai-responder/internal/platform"
)

const QualityLogFilename = "quality_log.json"

// QualityLogEvent is deliberately an observation/feedback record, not a
// copy of a conversation. Message bodies are never stored here. Draft text
// is retained only for the explicit draft-learning feedback requested by the
// candidate.
type QualityLogEvent struct {
	ID                     string                       `json:"id"`
	ObservationKey         string                       `json:"observation_key,omitempty"`
	EventType              string                       `json:"event_type"`
	ConversationID         string                       `json:"conversation_id,omitempty"`
	EmployerMessageID      string                       `json:"employer_message_id,omitempty"`
	DraftID                string                       `json:"draft_id,omitempty"`
	NotificationID         string                       `json:"notification_id,omitempty"`
	Timestamp              time.Time                    `json:"timestamp"`
	WorkflowState          CareerWorkflowState          `json:"workflow_state,omitempty"`
	ReplyPolicy            ConversationReplyRequirement `json:"reply_policy,omitempty"`
	CandidateContextStatus string                       `json:"candidate_context_status,omitempty"`
	DraftGenerated         bool                         `json:"draft_generated,omitempty"`
	NotificationCreated    bool                         `json:"notification_created,omitempty"`
	FollowUpEligible       bool                         `json:"followup_eligible,omitempty"`
	DecisionReasonCodes    []string                     `json:"decision_reason_codes,omitempty"`

	ClassificationCorrect  *bool  `json:"classification_correct,omitempty"`
	CorrectedState         string `json:"corrected_state,omitempty"`
	DraftAcceptedUnchanged *bool  `json:"draft_accepted_unchanged,omitempty"`
	DraftEdited            bool   `json:"draft_edited,omitempty"`
	DraftRejected          bool   `json:"draft_rejected,omitempty"`
	OriginalDraft          string `json:"original_draft,omitempty"`
	EditedDraft            string `json:"edited_draft,omitempty"`
	DraftReasonCategory    string `json:"draft_reason_category,omitempty"`

	NotificationEvent string `json:"notification_event,omitempty"`
}

type qualityLogFile struct {
	Version int               `json:"version"`
	Events  []QualityLogEvent `json:"events"`
}

type QualityLogStore struct {
	path    string
	events  []QualityLogEvent
	mu      sync.RWMutex
	persist func(string, []byte) error
}

func NewQualityLogStore(path string) *QualityLogStore {
	return &QualityLogStore{path: path, events: []QualityLogEvent{}, persist: persistQualityLogFile}
}

func (s *QualityLogStore) Load() error {
	if s == nil {
		return errors.New("quality log store is nil")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if strings.TrimSpace(s.path) == "" {
		s.events = []QualityLogEvent{}
		return nil
	}
	raw, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		s.events = []QualityLogEvent{}
		return nil
	}
	if err != nil || profileContainsSecret(raw) {
		return errors.New("cannot read quality log")
	}
	var file qualityLogFile
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&file); err != nil || file.Version != 1 || file.Events == nil {
		return errors.New("invalid quality log")
	}
	if err := validateQualityEvents(file.Events); err != nil {
		return err
	}
	s.events = append([]QualityLogEvent{}, file.Events...)
	return nil
}

func validateQualityEvents(events []QualityLogEvent) error {
	ids := map[string]bool{}
	for _, event := range events {
		if strings.TrimSpace(event.ID) == "" || strings.TrimSpace(event.EventType) == "" || event.Timestamp.IsZero() {
			return errors.New("invalid quality log event")
		}
		if ids[event.ID] {
			return errors.New("duplicate quality log event id")
		}
		ids[event.ID] = true
		if event.ClassificationCorrect != nil && event.CorrectedState == "" && !*event.ClassificationCorrect {
			return errors.New("incorrect classification feedback requires corrected state")
		}
		if event.OriginalDraft != "" && profileContainsSecret([]byte(event.OriginalDraft)) || event.EditedDraft != "" && profileContainsSecret([]byte(event.EditedDraft)) {
			return errors.New("quality log contains a forbidden secret marker")
		}
	}
	return nil
}

func (s *QualityLogStore) Save() error {
	if s == nil {
		return errors.New("quality log store is nil")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.saveEvents(s.events)
}

func persistQualityLogFile(path string, raw []byte) error {
	return platform.WritePrivateFileAtomic(path, raw, ".quality-log-*.tmp")
}

// saveEvents validates and durably writes a complete quality-log snapshot. The
// caller must hold the appropriate store lock when accessing store state.
func (s *QualityLogStore) saveEvents(events []QualityLogEvent) error {
	if err := validateQualityEvents(events); err != nil {
		return err
	}
	if strings.TrimSpace(s.path) == "" {
		return nil
	}
	raw, err := json.MarshalIndent(qualityLogFile{Version: 1, Events: events}, "", "  ")
	if err != nil {
		return errors.New("cannot encode quality log")
	}
	persist := s.persist
	if persist == nil {
		persist = persistQualityLogFile
	}
	return withStoreLock(s.path, func() error {
		return persist(s.path, append(raw, '\n'))
	})
}

func (s *QualityLogStore) List() []QualityLogEvent {
	if s == nil {
		return []QualityLogEvent{}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]QualityLogEvent{}, s.events...)
}

func (s *QualityLogStore) Record(event QualityLogEvent) error {
	return s.RecordMany([]QualityLogEvent{event})
}

func (s *QualityLogStore) RecordMany(events []QualityLogEvent) error {
	if s == nil {
		return errors.New("quality log store is nil")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(events) == 0 {
		return nil
	}
	nextEvents := append([]QualityLogEvent{}, s.events...)
	changed := false
	for _, event := range events {
		if event.ID == "" {
			var err error
			event.ID, err = newKnowledgeID("quality")
			if err != nil {
				return err
			}
		}
		if event.Timestamp.IsZero() {
			event.Timestamp = time.Now().UTC()
		}
		if event.DecisionReasonCodes == nil {
			event.DecisionReasonCodes = []string{}
		}
		if event.ObservationKey != "" {
			duplicate := false
			for _, existing := range nextEvents {
				if existing.ObservationKey == event.ObservationKey {
					duplicate = true
					break
				}
			}
			if duplicate {
				continue
			}
		}
		if err := validateQualityEvents([]QualityLogEvent{event}); err != nil {
			return err
		}
		nextEvents = append(nextEvents, event)
		changed = true
	}
	if !changed {
		return nil
	}
	start := time.Now()
	if err := s.saveEvents(nextEvents); err != nil {
		return err
	}
	perfRecord("quality_log.durable_save", start, len(nextEvents)-len(s.events))
	s.events = nextEvents
	return nil
}

type QualityCount struct {
	Value string `json:"value"`
	Count int    `json:"count"`
}

type QualityReport struct {
	ConversationsReviewed      int            `json:"conversations_reviewed"`
	NewEmployerMessages        int            `json:"new_employer_messages"`
	TotalReviewed              int            `json:"total_reviewed"`
	ClassificationCorrect      int            `json:"classification_correct"`
	ClassificationIncorrect    int            `json:"classification_incorrect"`
	DraftsGenerated            int            `json:"drafts_generated"`
	DraftsAcceptedUnchanged    int            `json:"drafts_accepted_unchanged"`
	DraftsEdited               int            `json:"drafts_edited"`
	DraftsRejected             int            `json:"drafts_rejected"`
	NotificationsShown         int            `json:"notifications_shown"`
	NotificationsOpened        int            `json:"notifications_opened"`
	NotificationsDismissed     int            `json:"notifications_dismissed"`
	NotificationsResolved      int            `json:"notifications_resolved"`
	NotificationsIrrelevant    int            `json:"notifications_marked_irrelevant"`
	IrrelevantNotificationRate float64        `json:"irrelevant_notification_rate"`
	FollowUpFalsePositives     int            `json:"follow_up_false_positives"`
	TopClassificationMistakes  []QualityCount `json:"top_classification_mistakes"`
	TopDraftProblems           []QualityCount `json:"top_draft_problems"`
}

func BuildQualityReport(events []QualityLogEvent) QualityReport {
	report := QualityReport{TopClassificationMistakes: []QualityCount{}, TopDraftProblems: []QualityCount{}}
	classification := map[string]int{}
	draftProblems := map[string]int{}
	conversations := map[string]bool{}
	messages := map[string]bool{}
	for _, event := range events {
		if event.ConversationID != "" {
			conversations[event.ConversationID] = true
		}
		if event.EmployerMessageID != "" && event.EventType == "classification" {
			messages[event.EmployerMessageID] = true
		}
		switch event.EventType {
		case "classification":
			if event.ClassificationCorrect != nil {
				report.TotalReviewed++
				if *event.ClassificationCorrect {
					report.ClassificationCorrect++
				} else {
					report.ClassificationIncorrect++
					classification[event.CorrectedState]++
					if event.FollowUpEligible && (event.CorrectedState == "TERMINAL" || event.CorrectedState == "NO_REPLY_NEEDED" || event.CorrectedState == "terminal" || event.CorrectedState == "no_reply_needed") {
						report.FollowUpFalsePositives++
					}
				}
			}
		case "draft_generated":
			report.DraftsGenerated++
		case "draft_feedback":
			if event.DraftRejected {
				report.DraftsRejected++
			}
			if event.DraftAcceptedUnchanged != nil && *event.DraftAcceptedUnchanged {
				report.DraftsAcceptedUnchanged++
			}
			if event.DraftEdited {
				report.DraftsEdited++
				draftProblems[event.DraftReasonCategory]++
			}
		case "notification_feedback":
			switch event.NotificationEvent {
			case "shown":
				report.NotificationsShown++
			case "opened":
				report.NotificationsOpened++
			case "dismissed":
				report.NotificationsDismissed++
			case "resolved":
				report.NotificationsResolved++
			case "marked_irrelevant":
				report.NotificationsIrrelevant++
			}
		case "notification_created":
			// Creation is represented in classification observations as a
			// boolean; this event exists for notification-only changes.
		}
	}
	report.ConversationsReviewed, report.NewEmployerMessages = len(conversations), len(messages)
	if report.NotificationsShown > 0 {
		report.IrrelevantNotificationRate = float64(report.NotificationsIrrelevant) * 100 / float64(report.NotificationsShown)
	}
	report.TopClassificationMistakes = qualityCounts(classification)
	report.TopDraftProblems = qualityCounts(draftProblems)
	return report
}

func qualityCounts(values map[string]int) []QualityCount {
	result := make([]QualityCount, 0, len(values))
	for value, count := range values {
		if strings.TrimSpace(value) == "" {
			continue
		}
		result = append(result, QualityCount{Value: value, Count: count})
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Count != result[j].Count {
			return result[i].Count > result[j].Count
		}
		return result[i].Value < result[j].Value
	})
	if len(result) > 10 {
		result = result[:10]
	}
	return result
}

func WriteQualityReport(w interface{ Write([]byte) (int, error) }, report QualityReport) error {
	_, err := fmt.Fprintf(w, "QUALITY REPORT\n\nTotal reviewed: %d\nConversations reviewed: %d\nNew employer messages: %d\nClassification correct: %d\nClassification incorrect: %d\nDrafts generated: %d\nDraft accepted unchanged: %d\nDraft edited: %d\nDraft rejected: %d\nNotifications shown: %d\nNotifications opened: %d\nNotifications dismissed: %d\nNotifications resolved: %d\nNotifications marked irrelevant: %d\nIrrelevant notification rate: %.1f%%\nFollow-up false positives: %d\n\nTop classification mistakes:\n", report.TotalReviewed, report.ConversationsReviewed, report.NewEmployerMessages, report.ClassificationCorrect, report.ClassificationIncorrect, report.DraftsGenerated, report.DraftsAcceptedUnchanged, report.DraftsEdited, report.DraftsRejected, report.NotificationsShown, report.NotificationsOpened, report.NotificationsDismissed, report.NotificationsResolved, report.NotificationsIrrelevant, report.IrrelevantNotificationRate, report.FollowUpFalsePositives)
	if err != nil {
		return err
	}
	for _, item := range report.TopClassificationMistakes {
		if _, err = fmt.Fprintf(w, "- %s: %d\n", item.Value, item.Count); err != nil {
			return err
		}
	}
	if _, err = fmt.Fprintln(w, "\nTop draft problems:"); err != nil {
		return err
	}
	for _, item := range report.TopDraftProblems {
		if _, err = fmt.Fprintf(w, "- %s: %d\n", item.Value, item.Count); err != nil {
			return err
		}
	}
	return nil
}
