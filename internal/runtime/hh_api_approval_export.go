package runtime

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"hh-ai-responder/internal/platform"
)

const pilotProviderResumePrefix = "hh-resume-provider-id-"

func validateManualPilotArtifact(artifact PilotArtifact, reviewedLetter string, now time.Time) (string, error) {
	if artifact.Version != pilotArtifactVersion || artifact.VacancyID <= 0 || artifact.Status != pilotManualReviewStatus || artifact.FinalDecision != "REVIEW_REQUIRED" {
		return "", errors.New("pilot artifact is not an eligible manual review")
	}
	if artifact.AIScore == nil || strings.TrimSpace(artifact.AIRecommendation) == "" || artifact.AIRecommendation != "UNCERTAIN" {
		return "", errors.New("pilot artifact AI assessment is not uncertain and populated")
	}
	if len(artifact.HardMissing) != 0 || len(artifact.HardUnknown) != 0 {
		return "", errors.New("pilot artifact has hard requirement blockers")
	}
	if artifact.Preflight.ObservedAt.IsZero() || now.IsZero() || artifact.Preflight.ObservedAt.After(now) || now.Sub(artifact.Preflight.ObservedAt) > apiApplicationApprovalMaxAge {
		return "", errors.New("pilot artifact preflight is stale")
	}
	if artifact.PreviewFreshAt.IsZero() || artifact.PreviewFreshAt.After(now) || now.Sub(artifact.PreviewFreshAt) > apiApplicationApprovalMaxAge {
		return "", errors.New("pilot artifact freshness is missing or stale")
	}
	if artifact.Preflight.Active == nil || !*artifact.Preflight.Active || artifact.Preflight.AlreadyResponded == nil || *artifact.Preflight.AlreadyResponded || artifact.Preflight.AlreadyRespondedValue != string(AlreadyRespondedNo) {
		return "", errors.New("pilot artifact provider response state is not a proven fresh NO")
	}
	if artifact.Preflight.CanApply == nil || !*artifact.Preflight.CanApply {
		return "", errors.New("pilot artifact can-apply state is not a proven YES")
	}
	if artifact.Preflight.TestRequired == nil || *artifact.Preflight.TestRequired {
		return "", errors.New("pilot artifact test state is not a proven NO")
	}
	if strings.TrimSpace(artifact.Nonce) != "" || artifact.NonceUsedAt != nil {
		return "", errors.New("manual pilot artifact must not contain a nonce")
	}
	if strings.TrimSpace(artifact.ContentHash) == "" || contentHash(artifact.CoverLetter) != strings.TrimSpace(artifact.ContentHash) {
		return "", errors.New("pilot artifact content hash does not match the source cover letter")
	}
	providerResumeID, err := pilotProviderResumeID(artifact)
	if err != nil {
		return "", err
	}
	if reviewedLetter == "" {
		if artifact.Preflight.CoverLetterRequired == nil || *artifact.Preflight.CoverLetterRequired {
			return "", errors.New("empty reviewed letter requires a known false cover-letter requirement")
		}
	} else if err := validatePilotCoverLetter(reviewedLetter); err != nil {
		return "", fmt.Errorf("reviewed cover letter is invalid: %w", err)
	}
	return providerResumeID, nil
}

func buildManualAPIApplicationApproval(artifact PilotArtifact, providerResumeID, reviewedLetter, pilotHash, nonce string, approvedAt time.Time) APIApplicationApproval {
	score := *artifact.AIScore
	return APIApplicationApproval{
		Version: apiApplicationApprovalVersion, VacancyID: artifact.VacancyID,
		ProviderResumeID: providerResumeID, SelectedResumeID: providerResumeID,
		CoverLetter: reviewedLetter, ContentHash: contentHash(reviewedLetter), Nonce: nonce,
		Status: pilotManualReviewStatus, FinalDecision: "REVIEW_REQUIRED", PreviewFreshAt: artifact.PreviewFreshAt,
		ApprovalBasis: manualApprovalBasis, OperatorApproved: true, OperatorApprovalTimestamp: approvedAt,
		OriginalAIScore: &score, OriginalAIRecommendation: artifact.AIRecommendation,
		OriginalAIRecommendationReasons: append([]string(nil), artifact.AIReasons...),
		OriginalFinalDecision:           artifact.FinalDecision, PilotArtifactHash: pilotHash,
	}
}

func pilotArtifactToAPIApplicationApproval(artifact PilotArtifact) (APIApplicationApproval, error) {
	if artifact.Version != pilotArtifactVersion || artifact.VacancyID <= 0 || artifact.Status != pilotReadyStatus || artifact.FinalDecision != "MATCH" {
		return APIApplicationApproval{}, errors.New("pilot artifact is not ready for explicit MATCH send")
	}
	providerResumeID, err := pilotProviderResumeID(artifact)
	if err != nil {
		return APIApplicationApproval{}, err
	}
	if artifact.NonceUsedAt != nil || strings.TrimSpace(artifact.Nonce) == "" {
		return APIApplicationApproval{}, errors.New("pilot artifact nonce is invalid or already used")
	}
	if artifact.PreviewFreshAt.IsZero() {
		return APIApplicationApproval{}, errors.New("pilot artifact freshness is missing")
	}
	if artifact.CoverLetter == "" {
		if artifact.Preflight.CoverLetterRequired == nil || *artifact.Preflight.CoverLetterRequired {
			return APIApplicationApproval{}, errors.New("pilot artifact requires a validated cover letter")
		}
	} else if err := validatePilotCoverLetter(artifact.CoverLetter); err != nil {
		return APIApplicationApproval{}, fmt.Errorf("pilot artifact cover letter is invalid: %w", err)
	}
	focusedHash := contentHash(artifact.CoverLetter)
	if strings.TrimSpace(artifact.ContentHash) != "" && strings.TrimSpace(artifact.ContentHash) != focusedHash {
		return APIApplicationApproval{}, errors.New("pilot artifact content hash does not match the exact cover letter")
	}
	return APIApplicationApproval{
		Version:          apiApplicationApprovalVersion,
		VacancyID:        artifact.VacancyID,
		ProviderResumeID: providerResumeID,
		SelectedResumeID: providerResumeID,
		CoverLetter:      artifact.CoverLetter,
		ContentHash:      focusedHash,
		Nonce:            artifact.Nonce,
		PreviewFreshAt:   artifact.PreviewFreshAt,
		Status:           "READY_FOR_EXPLICIT_SEND",
		FinalDecision:    "MATCH",
	}, nil
}

func pilotProviderResumeID(artifact PilotArtifact) (string, error) {
	fromHHID := ""
	if artifact.SelectedResumeHHID > 0 {
		fromHHID = strconv.FormatInt(artifact.SelectedResumeHHID, 10)
	}
	fromKnownInternal := ""
	selectedID := strings.TrimSpace(artifact.SelectedResumeID)
	if selectedID != "" {
		if !strings.HasPrefix(selectedID, pilotProviderResumePrefix) {
			if fromHHID == "" {
				return "", errors.New("pilot artifact selected resume ID is not a provider identity")
			}
		} else {
			candidate, ok := normalizeProviderResumeID(selectedID)
			if !ok {
				return "", errors.New("pilot artifact provider resume ID is invalid")
			}
			fromKnownInternal = candidate
		}
	}
	if fromHHID != "" && fromKnownInternal != "" && fromHHID != fromKnownInternal {
		return "", errors.New("pilot artifact selected resume identities conflict")
	}
	providerID := firstNonEmpty(fromHHID, fromKnownInternal)
	if providerID == "" {
		return "", errors.New("pilot artifact has no trusted provider resume ID")
	}
	return providerID, nil
}

func runHHAPIApprovalCommand(args []string, stdout io.Writer) error {
	if len(args) == 0 || args[0] != "export" {
		return errors.New("hh-api approval requires: export --pilot <path> --out <path>")
	}
	pilotPath, outputPath, err := parseHHAPIApprovalExportArgs(args[1:])
	if err != nil {
		return err
	}
	artifact, err := loadPilotArtifact(pilotPath)
	if err != nil {
		return fmt.Errorf("pilot artifact could not be loaded: %w", err)
	}
	approval, err := pilotArtifactToAPIApplicationApproval(artifact)
	if err != nil {
		return err
	}
	raw, err := json.MarshalIndent(approval, "", "  ")
	if err != nil {
		return errors.New("API application approval could not be encoded")
	}
	if err := platform.WritePrivateFileAtomic(outputPath, append(raw, '\n'), ".hh-api-approval-*.tmp"); err != nil {
		return fmt.Errorf("API application approval could not be written: %w", err)
	}
	if stdout != nil {
		_, _ = fmt.Fprintf(stdout, "API_APPROVAL_EXPORTED vacancy_id=%d resume_id=%s\n", approval.VacancyID, safeHHAPIResumeID(approval.ProviderResumeID))
	}
	return nil
}

func parseHHAPIApprovalExportArgs(args []string) (string, string, error) {
	pilotPath, outputPath := "", ""
	for index := 0; index < len(args); index++ {
		arg := args[index]
		switch {
		case arg == "--pilot", arg == "--out":
			if index+1 >= len(args) || strings.TrimSpace(args[index+1]) == "" || strings.HasPrefix(args[index+1], "-") {
				return "", "", fmt.Errorf("%s requires a value", arg)
			}
			value := strings.TrimSpace(args[index+1])
			if arg == "--pilot" {
				if pilotPath != "" {
					return "", "", errors.New("hh-api approval export accepts exactly one --pilot")
				}
				pilotPath = value
			} else {
				if outputPath != "" {
					return "", "", errors.New("hh-api approval export accepts exactly one --out")
				}
				outputPath = value
			}
			index++
		case strings.HasPrefix(arg, "--pilot="):
			if pilotPath != "" || strings.TrimSpace(strings.TrimPrefix(arg, "--pilot=")) == "" {
				return "", "", errors.New("hh-api approval export requires exactly one --pilot")
			}
			pilotPath = strings.TrimSpace(strings.TrimPrefix(arg, "--pilot="))
		case strings.HasPrefix(arg, "--out="):
			if outputPath != "" || strings.TrimSpace(strings.TrimPrefix(arg, "--out=")) == "" {
				return "", "", errors.New("hh-api approval export requires exactly one --out")
			}
			outputPath = strings.TrimSpace(strings.TrimPrefix(arg, "--out="))
		default:
			return "", "", errors.New("hh-api approval export accepts only --pilot and --out")
		}
	}
	if pilotPath == "" || outputPath == "" {
		return "", "", errors.New("usage: hh-api approval export --pilot <path> --out <path>")
	}
	return pilotPath, outputPath, nil
}
