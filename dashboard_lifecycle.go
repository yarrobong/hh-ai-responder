package main

import (
	"sync"
	"time"
)

// DashboardLifecycleEvent describes the local UI/API/gateway path. It is not
// an HH write audit event and must never be counted as a write attempt.
type DashboardLifecycleEvent struct {
	ActionID string    `json:"action_id"`
	Type     string    `json:"type"`
	Reason   string    `json:"reason,omitempty"`
	At       time.Time `json:"at"`
}

type DashboardLifecycleStore struct {
	mu     sync.Mutex
	events []DashboardLifecycleEvent
	limit  int
}

func NewDashboardLifecycleStore() *DashboardLifecycleStore {
	return &DashboardLifecycleStore{events: []DashboardLifecycleEvent{}, limit: 512}
}

func (s *DashboardLifecycleStore) Record(actionID, eventType, reason string) {
	if s == nil || eventType == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.limit <= 0 {
		s.limit = 512
	}
	s.events = append(s.events, DashboardLifecycleEvent{ActionID: actionID, Type: eventType, Reason: reason, At: time.Now().UTC()})
	if len(s.events) > s.limit {
		s.events = append([]DashboardLifecycleEvent{}, s.events[len(s.events)-s.limit:]...)
	}
}

func (s *DashboardLifecycleStore) List(actionID string) []DashboardLifecycleEvent {
	if s == nil {
		return []DashboardLifecycleEvent{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]DashboardLifecycleEvent, 0, len(s.events))
	for _, event := range s.events {
		if actionID == "" || event.ActionID == actionID {
			result = append(result, event)
		}
	}
	return result
}
