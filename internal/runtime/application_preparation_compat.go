package runtime

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"hh-ai-responder/internal/careeragent"
	"hh-ai-responder/internal/ports"
)

var (
	errAPIApplicationPreparation                   = errors.New("API application approval preparation binding is invalid")
	errAPIApplicationPreparationStale              = errors.New("API application approval preparation is stale")
	errAPIApplicationPreparationContentReplacement = errors.New("reviewed cover letter differs from durable preparation; regenerate the pilot/preparation with the desired content")
	errBrowserResumeBindingStale                   = errors.New("BLOCKED_STALE: approved browser resume hash changed")
	errApprovedResumeNotAvailableInBrowserSession  = errors.New("APPROVED_RESUME_NOT_AVAILABLE_IN_BROWSER_SESSION")
)

func validateBrowserResumeBinding(approval APIApplicationApproval, currentHash string) error {
	approved := strings.TrimSpace(approval.BrowserResumeHash)
	current := strings.TrimSpace(currentHash)
	if approved == "" || current == "" {
		return errApprovedResumeNotAvailableInBrowserSession
	}
	if approved != current {
		return errBrowserResumeBindingStale
	}
	return nil
}

func validateBrowserApprovalHash(approval APIApplicationApproval) error {
	if strings.TrimSpace(approval.BrowserResumeHash) == "" {
		return errApprovedResumeNotAvailableInBrowserSession
	}
	return nil
}

func validatePreparationReferenceShape(preparationID, preparationHash string) error {
	preparationID = strings.TrimSpace(preparationID)
	preparationHash = strings.TrimSpace(preparationHash)
	if (preparationID == "") != (preparationHash == "") {
		return errAPIApplicationPreparation
	}
	if preparationHash != "" {
		if len(preparationHash) != 64 {
			return errAPIApplicationPreparation
		}
		if _, err := hex.DecodeString(preparationHash); err != nil {
			return errAPIApplicationPreparation
		}
	}
	return nil
}

// validatePreparationApprovalBinding is intentionally separate from the
// legacy approval validator. Old approval JSON has no preparation fields and
// continues through the existing validation path; new references are checked
// against the durable local artifact before any nonce is consumed.
func validatePreparationApprovalBinding(ctx context.Context, store ports.CareerWorkflowReader, approval APIApplicationApproval, vacancyID int, providerResumeID string) error {
	if err := validatePreparationReferenceShape(approval.PreparationID, approval.PreparationHash); err != nil {
		return err
	}
	if strings.TrimSpace(approval.PreparationID) == "" {
		return nil
	}
	if store == nil {
		return fmt.Errorf("%w: preparation store is unavailable", errAPIApplicationPreparation)
	}
	preparation, err := store.GetPreparation(ctx, vacancyID)
	if err != nil {
		return fmt.Errorf("%w: %v", errAPIApplicationPreparation, err)
	}
	if err := preparation.Validate(); err != nil {
		return fmt.Errorf("%w: stored preparation is invalid: %v", errAPIApplicationPreparation, err)
	}
	if careeragent.PreparationInputFingerprint(preparation) != preparation.InputFingerprint {
		return fmt.Errorf("%w: preparation fingerprint is stale", errAPIApplicationPreparationStale)
	}
	if approval.VacancyID != vacancyID || preparation.ID != strings.TrimSpace(approval.PreparationID) || preparation.VacancyID != vacancyID {
		return fmt.Errorf("%w: preparation identity does not match the command", errAPIApplicationPreparation)
	}
	if preparation.InputFingerprint != strings.TrimSpace(approval.PreparationHash) {
		return fmt.Errorf("%w: preparation hash does not match", errAPIApplicationPreparation)
	}
	if preparation.CoverLetterHash != strings.TrimSpace(approval.ContentHash) {
		return fmt.Errorf("%w: preparation cover-letter hash does not match", errAPIApplicationPreparation)
	}
	if strings.TrimSpace(approval.BrowserResumeHash) != "" || strings.TrimSpace(preparation.BrowserResumeHash) != "" {
		if strings.TrimSpace(approval.BrowserResumeHash) == "" || strings.TrimSpace(preparation.BrowserResumeHash) == "" || strings.TrimSpace(approval.BrowserResumeHash) != strings.TrimSpace(preparation.BrowserResumeHash) {
			return fmt.Errorf("%w: browser resume hash does not match", errAPIApplicationPreparation)
		}
	}
	preparedResumeID := strings.TrimSpace(preparation.ResumeProviderID)
	if preparedResumeID == "" {
		preparedResumeID = strings.TrimSpace(preparation.ResumeID)
	}
	if preparedResumeID == "" || preparedResumeID != strings.TrimSpace(providerResumeID) || approvalProviderResumeID(approval) != preparedResumeID {
		return fmt.Errorf("%w: selected resume identity does not match", errAPIApplicationPreparation)
	}
	switch preparation.Status {
	case careeragent.PreparationStatusReady:
	case careeragent.PreparationStatusReviewRequired:
		if approval.ApprovalBasis != manualApprovalBasis || !approval.OperatorApproved {
			return fmt.Errorf("%w: review-required preparation needs operator approval", errAPIApplicationPreparation)
		}
	case careeragent.PreparationStatusStale, careeragent.PreparationStatusInvalid:
		return errAPIApplicationPreparationStale
	default:
		return fmt.Errorf("%w: preparation is not ready", errAPIApplicationPreparation)
	}
	return nil
}

// validatePreparationContentReplacement rejects an operator replacement once
// a durable preparation exists. The preparation binds the exact reviewed
// content; changing it requires a fresh pilot/preparation pair rather than a
// weaker binding check.
func validatePreparationContentReplacement(ctx context.Context, store ports.CareerWorkflowReader, approval APIApplicationApproval, replacement string) error {
	if strings.TrimSpace(approval.PreparationID) == "" || replacement == approval.CoverLetter {
		return nil
	}
	if err := validatePreparationApprovalBinding(ctx, store, approval, approval.VacancyID, approval.ProviderResumeID); err != nil {
		return err
	}
	return errAPIApplicationPreparationContentReplacement
}
