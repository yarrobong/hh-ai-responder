package runtime

import "strings"

// HHStatusMapper is deliberately separate from synchronization. Unknown HH
// states are retained as unknown instead of being guessed into a terminal or
// actionable application state.
type HHStatusMapper struct {
	statuses map[string]ApplicationStatus
}

func NewHHStatusMapper() HHStatusMapper {
	return HHStatusMapper{statuses: map[string]ApplicationStatus{
		"response": ApplicationApplied, "responded": ApplicationApplied,
		"applied": ApplicationApplied, "sent": ApplicationApplied,
		"viewed": ApplicationApplied, "considered": ApplicationApplied,
		"invited": ApplicationInterview, "interview": ApplicationInterview,
		"offer": ApplicationOffer, "hired": ApplicationOffer,
		"rejected": ApplicationRejected, "declined": ApplicationRejected,
		"discard": ApplicationRejected, "discarded": ApplicationRejected, "cancelled": ApplicationArchived,
		"closed": ApplicationArchived, "canceled": ApplicationArchived, "archived": ApplicationArchived,
	}}
}

// Map returns (internal status, known). The raw status must be stored by the
// caller even when known is false.
func (m HHStatusMapper) Map(raw string) (ApplicationStatus, bool) {
	key := strings.ToLower(strings.TrimSpace(raw))
	if key == "" {
		return ApplicationUnknown, false
	}
	if value, ok := m.statuses[key]; ok {
		return value, true
	}
	return ApplicationUnknown, false
}

func (m HHStatusMapper) Warning(raw string) string {
	if _, known := m.Map(raw); known {
		return ""
	}
	if strings.TrimSpace(raw) == "" {
		return "HH application status is absent; imported as unknown"
	}
	return "unknown HH application status " + raw + "; imported as unknown"
}

func MapHHApplicationStatus(raw string) (ApplicationStatus, bool) {
	return NewHHStatusMapper().Map(raw)
}
