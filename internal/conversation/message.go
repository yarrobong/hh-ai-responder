package conversation

import (
	"errors"
	"strings"
	"time"
)

// Sender identifies the normalized conversation participant.
type Sender string

// Source identifies the normalized origin of a persisted message.
type Source string

// Direction identifies message travel relative to the candidate.
type Direction string

const (
	SenderUnknown   Sender = "unknown"
	SenderCandidate Sender = "candidate"
	SenderEmployer  Sender = "employer"
	SenderSystem    Sender = "system"

	SourceHH      Source = "hh"
	SourceHHWrite Source = "hh_write"
	SourceManual  Source = "manual"
	SourceAIDraft Source = "ai_draft"

	DirectionUnknown  Direction = "unknown"
	DirectionIncoming Direction = "incoming"
	DirectionOutgoing Direction = "outgoing"
)

// Message is the normalized persisted representation of one conversation
// event. HH transport DTOs and sender mapping are owned by the root adapters.
type Message struct {
	HHSystemEvent      bool              `json:"hh_system_event,omitempty"`
	ContentUnavailable bool              `json:"content_unavailable,omitempty"`
	Metadata           map[string]string `json:"metadata,omitempty"`
	ID                 string            `json:"id"`
	ExternalID         string            `json:"external_id,omitempty"`
	Timestamp          time.Time         `json:"timestamp"`
	Sender             Sender            `json:"sender"`
	Text               string            `json:"text"`
	Source             Source            `json:"source"`
	Direction          Direction         `json:"direction"`
}

// Validate checks the intrinsic persisted message shape. It does not inspect
// HH payloads or apply reply/workflow policy.
func (m Message) Validate() error {
	if m.HHSystemEvent && (m.Source != SourceHH || strings.TrimSpace(m.Text) != "") {
		return errors.New("HH system event annotation requires a textless HH record")
	}
	if strings.TrimSpace(m.ID) == "" || m.Timestamp.IsZero() || (strings.TrimSpace(m.Text) == "" && m.Sender != SenderSystem && m.Sender != SenderUnknown && !(m.Source == SourceHH && m.ContentUnavailable)) {
		return errors.New("message requires id, timestamp and text")
	}
	if m.Direction != DirectionIncoming && m.Direction != DirectionOutgoing && !(m.Sender == SenderUnknown && m.Direction == DirectionUnknown) {
		return errors.New("invalid message direction")
	}
	switch m.Sender {
	case SenderUnknown:
		if m.Direction != DirectionUnknown || m.Source != SourceHH {
			return errors.New("unknown HH sender requires unknown direction")
		}
	case SenderEmployer:
		if m.Direction != DirectionIncoming {
			return errors.New("employer message must be incoming")
		}
	case SenderCandidate:
		if m.Direction != DirectionOutgoing {
			return errors.New("candidate message must be outgoing")
		}
	case SenderSystem:
	default:
		return errors.New("invalid message sender")
	}
	switch m.Source {
	case SourceHH, SourceHHWrite, SourceManual:
	case SourceAIDraft:
		if m.Sender != SenderCandidate || m.ExternalID != "" {
			return errors.New("AI draft must be a candidate message without an external delivery id")
		}
	default:
		return errors.New("invalid message source")
	}
	return nil
}

// SameMessage preserves the existing immutable-history comparison. In
// particular, system-event annotation is intentionally not part of this
// comparison; import reconciliation owns the additional HH-system-event
// conflict rule where required.
func SameMessage(a, b Message) bool {
	return a.ContentUnavailable == b.ContentUnavailable && a.ExternalID == b.ExternalID && a.Timestamp.Equal(b.Timestamp) && a.Sender == b.Sender &&
		a.Text == b.Text && a.Source == b.Source && a.Direction == b.Direction && mapsEqual(a.Metadata, b.Metadata)
}

func mapsEqual(a, b map[string]string) bool {
	if (a == nil) != (b == nil) {
		return false
	}
	if len(a) != len(b) {
		return false
	}
	for key, value := range a {
		other, ok := b[key]
		if !ok || other != value {
			return false
		}
	}
	return true
}
