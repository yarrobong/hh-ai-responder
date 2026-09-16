// Package applicationpilot contains the small, deterministic safety contract
// shared by the explicit Career Agent pilot preview and its later send phase.
// It has no HH client and cannot perform a write.
package applicationpilot

import (
	"errors"
	"fmt"
	"strings"
)

const (
	StatusReady   = "READY_FOR_EXPLICIT_SEND"
	StatusBlocked = "BLOCKED"

	OutcomeConfirmed = "CONFIRMED"
	OutcomeUnknown   = "UNKNOWN"
	OutcomeFailed    = "FAILED"
)

var (
	ErrVacancyChanged   = errors.New("pilot vacancy changed")
	ErrResumeChanged    = errors.New("pilot selected resume changed")
	ErrContentChanged   = errors.New("pilot cover-letter content hash mismatch")
	ErrNonceConsumed    = errors.New("pilot nonce is already used")
	ErrNonceInvalid     = errors.New("pilot nonce is missing")
	ErrLimitReached     = errors.New("pilot write limit reached")
	ErrApprovalRequired = errors.New("pilot requires an explicit approval")
)

// Approval is the identity material that must remain stable between preview
// and explicit send. The exact letter is hashed by the runtime before it is
// persisted; this type deliberately does not contain candidate data.
type Approval struct {
	VacancyID   int
	ResumeID    string
	ContentHash string
	Nonce       string
	NonceUsed   bool
}

// CurrentIdentity is read again immediately before a future send.
type CurrentIdentity struct {
	VacancyID   int
	ResumeID    string
	ContentHash string
}

func VerifyApproval(approval Approval, current CurrentIdentity) error {
	if approval.VacancyID <= 0 || current.VacancyID != approval.VacancyID {
		return ErrVacancyChanged
	}
	if strings.TrimSpace(approval.ResumeID) == "" || strings.TrimSpace(current.ResumeID) != approval.ResumeID {
		return ErrResumeChanged
	}
	if strings.TrimSpace(approval.ContentHash) == "" || strings.TrimSpace(current.ContentHash) != approval.ContentHash {
		return ErrContentChanged
	}
	if strings.TrimSpace(approval.Nonce) == "" {
		return ErrNonceInvalid
	}
	if approval.NonceUsed {
		return ErrNonceConsumed
	}
	return nil
}

// Budget is intentionally stricter than the production defaults. A pilot can
// reserve exactly one application and exactly one total HH write.
type Budget struct {
	Applications int
	TotalWrites  int
}

func (b *Budget) ReserveApplication() error {
	if b == nil || b.Applications >= 1 || b.TotalWrites >= 1 {
		return ErrLimitReached
	}
	b.Applications++
	b.TotalWrites++
	return nil
}

// ReconciliationEvidence is the provider-read result after a transport
// attempt. An ambiguous transport remains UNKNOWN by contract, even if a
// later read happens to expose a weak positive marker.
type ReconciliationEvidence struct {
	TransportAccepted  bool
	TransportAmbiguous bool
	TransportRejected  bool
	ProviderConfirmed  bool
	ProviderRead       bool
}

func ClassifyReconciliation(value ReconciliationEvidence) string {
	if value.TransportAmbiguous || !value.ProviderRead {
		return OutcomeUnknown
	}
	if value.TransportRejected {
		return OutcomeFailed
	}
	if value.TransportAccepted && value.ProviderConfirmed {
		return OutcomeConfirmed
	}
	return OutcomeUnknown
}

func ExplainVerification(err error) string {
	if err == nil {
		return ""
	}
	return fmt.Sprintf("%v", err)
}
