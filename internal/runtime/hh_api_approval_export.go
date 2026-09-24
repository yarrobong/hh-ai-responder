package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"hh-ai-responder/internal/platform"
	"hh-ai-responder/internal/usecase/vacancyanalysis"
)

const (
	pilotProviderResumePrefix  = "hh-resume-provider-id-"
	maxReviewedCoverLetterSize = 1 << 20
)

func readReviewedCoverLetter(path string) (string, error) {
	letterBytes, err := os.ReadFile(path)
	if err != nil {
		return "", errors.New("reviewed cover-letter file could not be read")
	}
	if len(letterBytes) > maxReviewedCoverLetterSize {
		return "", errors.New("reviewed cover-letter file is too large")
	}
	return string(letterBytes), nil
}

func validateReplacementCoverLetter(artifact PilotArtifact, replacement string) error {
	if replacement == "" {
		if artifact.Preflight.CoverLetterRequired == nil || *artifact.Preflight.CoverLetterRequired {
			return errors.New("empty reviewed letter requires a known false cover-letter requirement")
		}
		return nil
	}
	if err := validatePilotCoverLetter(replacement); err != nil {
		return fmt.Errorf("reviewed cover letter is invalid: %w", err)
	}
	return nil
}

func validateManualPilotArtifact(artifact PilotArtifact, reviewedLetter string, now time.Time) (string, error) {
	if artifact.Version != pilotArtifactVersion || artifact.VacancyID <= 0 || artifact.Status != pilotManualReviewStatus || artifact.FinalDecision != "REVIEW_REQUIRED" {
		return "", errors.New("pilot artifact is not an eligible manual review")
	}
	if artifact.AIScore == nil || *artifact.AIScore < 0 || *artifact.AIScore > 100 || !isSupportedAIRecommendation(artifact.AIRecommendation) {
		return "", errors.New("pilot artifact AI assessment is missing or invalid")
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

func isSupportedAIRecommendation(value string) bool {
	switch strings.TrimSpace(value) {
	case vacancyanalysis.RecommendationApply, vacancyanalysis.RecommendationUncertain, vacancyanalysis.RecommendationDoNotApply:
		return true
	default:
		return false
	}
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
		PreparationID: artifact.PreparationID, PreparationHash: artifact.PreparationHash,
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
	if err := validatePreparationReferenceShape(artifact.PreparationID, artifact.PreparationHash); err != nil {
		return APIApplicationApproval{}, err
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
		PreparationID:    artifact.PreparationID,
		PreparationHash:  artifact.PreparationHash,
	}, nil
}

func pilotProviderResumeID(artifact PilotArtifact) (string, error) {
	selectedID := strings.TrimSpace(artifact.SelectedResumeID)
	selectedHash := strings.TrimSpace(artifact.SelectedResumeHash)
	selectedProvider := strings.TrimSpace(artifact.SelectedResumeProviderID)
	if selectedProvider != "" {
		providerID, ok := normalizeProviderResumeID(selectedProvider)
		if !ok {
			return "", errors.New("pilot artifact provider resume ID is invalid")
		}
		if strings.HasPrefix(selectedID, pilotProviderResumePrefix) {
			selectedIDProvider, idOK := normalizeProviderResumeID(selectedID)
			if !idOK || selectedIDProvider != providerID {
				return "", errors.New("pilot artifact selected resume identities conflict")
			}
		}
		// SelectedResumeHash is an optional content/version fingerprint. It is
		// intentionally not compared with providerID: those values belong to
		// different namespaces and a content update must not look like a
		// different HH resume.
		return providerID, nil
	}
	if strings.HasPrefix(selectedID, pilotProviderResumePrefix) {
		providerID, ok := normalizeProviderResumeID(selectedID)
		if !ok {
			return "", errors.New("pilot artifact provider resume ID is invalid")
		}
		return providerID, nil
	}

	const browserResumePrefix = "hh-resume-"
	if selectedHash == "" || selectedID != browserResumePrefix+selectedHash {
		return "", errors.New("pilot artifact browser resume ID and hash are missing or inconsistent")
	}
	providerID, ok := normalizeProviderResumeID(selectedHash)
	if !ok {
		return "", errors.New("pilot artifact browser resume hash is invalid")
	}
	return providerID, nil
}

func runHHAPIApprovalCommand(args []string, stdout io.Writer, deps HHAPICommandDeps) error {
	if len(args) == 0 || (args[0] != "export" && args[0] != "review") {
		return errors.New("hh-api approval requires: export or review --pilot <path> --out <path>")
	}
	if args[0] == "review" {
		return runHHAPIApprovalReview(args[1:], stdout, deps)
	}
	pilotPath, outputPath, letterPath, err := parseHHAPIApprovalExportArgs(args[1:])
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
	if letterPath != "" {
		replacement, readErr := readReviewedCoverLetter(letterPath)
		if readErr != nil {
			return readErr
		}
		if err := validateReplacementCoverLetter(artifact, replacement); err != nil {
			return err
		}
		approval.CoverLetter = replacement
		approval.ContentHash = contentHash(replacement)
	}
	if err := validatePreparationApprovalBinding(context.Background(), deps.CareerWorkflow, approval, approval.VacancyID, approval.ProviderResumeID); err != nil {
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

func runHHAPIApprovalReview(args []string, stdout io.Writer, deps HHAPICommandDeps) error {
	pilotPath, outputPath, letterPath, err := parseHHAPIApprovalReviewArgs(args)
	if err != nil {
		return err
	}
	artifact, raw, err := loadPilotArtifactForManualApproval(pilotPath)
	if err != nil {
		return fmt.Errorf("pilot artifact could not be loaded: %w", err)
	}
	now := time.Now().UTC()
	if deps.Now != nil {
		now = deps.Now().UTC()
	}
	providerResumeID, err := validateManualPilotArtifact(artifact, artifact.CoverLetter, now)
	if err != nil {
		return err
	}
	reviewedLetter := artifact.CoverLetter
	if letterPath != "" {
		reviewedLetter, err = readReviewedCoverLetter(letterPath)
		if err != nil {
			return err
		}
		if _, err := validateManualPilotArtifact(artifact, reviewedLetter, now); err != nil {
			return err
		}
	}
	nonce, err := generateUUIDv4()
	if err != nil {
		return fmt.Errorf("manual approval nonce generation failed: %w", err)
	}
	pilotHash := sha256.Sum256(raw)
	approval := buildManualAPIApplicationApproval(artifact, providerResumeID, reviewedLetter, hex.EncodeToString(pilotHash[:]), nonce, now)
	if err := validatePreparationApprovalBinding(context.Background(), deps.CareerWorkflow, approval, approval.VacancyID, approval.ProviderResumeID); err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(approval, "", "  ")
	if err != nil {
		return errors.New("API manual application approval could not be encoded")
	}
	if err := platform.WritePrivateFileAtomic(outputPath, append(encoded, '\n'), ".hh-api-approval-*.tmp"); err != nil {
		return fmt.Errorf("API manual application approval could not be written: %w", err)
	}
	if stdout != nil {
		_, _ = fmt.Fprintf(stdout, "API_MANUAL_APPROVAL_CREATED vacancy_id=%d approval_basis=%s\n", approval.VacancyID, manualApprovalBasis)
	}
	return nil
}

func parseHHAPIApprovalExportArgs(args []string) (string, string, string, error) {
	pilotPath, outputPath, letterPath := "", "", ""
	for index := 0; index < len(args); index++ {
		arg := args[index]
		switch {
		case arg == "--pilot", arg == "--out", arg == "--letter-file":
			if index+1 >= len(args) || strings.TrimSpace(args[index+1]) == "" || strings.HasPrefix(args[index+1], "-") {
				return "", "", "", fmt.Errorf("%s requires a value", arg)
			}
			value := strings.TrimSpace(args[index+1])
			switch arg {
			case "--pilot":
				if pilotPath != "" {
					return "", "", "", errors.New("hh-api approval export accepts exactly one --pilot")
				}
				pilotPath = value
			case "--out":
				if outputPath != "" {
					return "", "", "", errors.New("hh-api approval export accepts exactly one --out")
				}
				outputPath = value
			case "--letter-file":
				if letterPath != "" {
					return "", "", "", errors.New("hh-api approval export accepts exactly one --letter-file")
				}
				letterPath = value
			}
			index++
		case strings.HasPrefix(arg, "--pilot="):
			if pilotPath != "" || strings.TrimSpace(strings.TrimPrefix(arg, "--pilot=")) == "" {
				return "", "", "", errors.New("hh-api approval export requires exactly one --pilot")
			}
			pilotPath = strings.TrimSpace(strings.TrimPrefix(arg, "--pilot="))
		case strings.HasPrefix(arg, "--out="):
			if outputPath != "" || strings.TrimSpace(strings.TrimPrefix(arg, "--out=")) == "" {
				return "", "", "", errors.New("hh-api approval export requires exactly one --out")
			}
			outputPath = strings.TrimSpace(strings.TrimPrefix(arg, "--out="))
		case strings.HasPrefix(arg, "--letter-file="):
			if letterPath != "" || strings.TrimSpace(strings.TrimPrefix(arg, "--letter-file=")) == "" {
				return "", "", "", errors.New("hh-api approval export requires exactly one --letter-file")
			}
			letterPath = strings.TrimSpace(strings.TrimPrefix(arg, "--letter-file="))
		default:
			return "", "", "", errors.New("hh-api approval export accepts only --pilot, --out, and optional --letter-file")
		}
	}
	if pilotPath == "" || outputPath == "" {
		return "", "", "", errors.New("usage: hh-api approval export --pilot <path> --out <path> [--letter-file <path>]")
	}
	return pilotPath, outputPath, letterPath, nil
}

func parseHHAPIApprovalReviewArgs(args []string) (string, string, string, error) {
	pilotPath, outputPath, letterPath := "", "", ""
	for index := 0; index < len(args); index++ {
		arg := args[index]
		switch {
		case arg == "--pilot", arg == "--out", arg == "--letter-file":
			if index+1 >= len(args) || strings.TrimSpace(args[index+1]) == "" || strings.HasPrefix(args[index+1], "-") {
				return "", "", "", fmt.Errorf("%s requires a value", arg)
			}
			value := strings.TrimSpace(args[index+1])
			switch arg {
			case "--pilot":
				if pilotPath != "" {
					return "", "", "", errors.New("hh-api approval review accepts exactly one --pilot")
				}
				pilotPath = value
			case "--out":
				if outputPath != "" {
					return "", "", "", errors.New("hh-api approval review accepts exactly one --out")
				}
				outputPath = value
			case "--letter-file":
				if letterPath != "" {
					return "", "", "", errors.New("hh-api approval review accepts exactly one --letter-file")
				}
				letterPath = value
			}
			index++
		case strings.HasPrefix(arg, "--pilot="):
			if pilotPath != "" || strings.TrimSpace(strings.TrimPrefix(arg, "--pilot=")) == "" {
				return "", "", "", errors.New("hh-api approval review requires exactly one --pilot")
			}
			pilotPath = strings.TrimSpace(strings.TrimPrefix(arg, "--pilot="))
		case strings.HasPrefix(arg, "--out="):
			if outputPath != "" || strings.TrimSpace(strings.TrimPrefix(arg, "--out=")) == "" {
				return "", "", "", errors.New("hh-api approval review requires exactly one --out")
			}
			outputPath = strings.TrimSpace(strings.TrimPrefix(arg, "--out="))
		case strings.HasPrefix(arg, "--letter-file="):
			if letterPath != "" || strings.TrimSpace(strings.TrimPrefix(arg, "--letter-file=")) == "" {
				return "", "", "", errors.New("hh-api approval review requires exactly one --letter-file")
			}
			letterPath = strings.TrimSpace(strings.TrimPrefix(arg, "--letter-file="))
		default:
			return "", "", "", errors.New("hh-api approval review accepts only --pilot, --out, and optional --letter-file")
		}
	}
	if pilotPath == "" || outputPath == "" {
		return "", "", "", errors.New("usage: hh-api approval review --pilot <path> --out <path> [--letter-file <path>]")
	}
	return pilotPath, outputPath, letterPath, nil
}
