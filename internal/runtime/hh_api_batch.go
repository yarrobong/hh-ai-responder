package runtime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"
)

const batchMaxApplications = 3

type BatchRunStatus string

const (
	BatchRunCompleted        BatchRunStatus = "COMPLETED"
	BatchRunStoppedUncertain BatchRunStatus = "STOPPED_UNCERTAIN"
	BatchRunDryRun           BatchRunStatus = "DRY_RUN"
)

type BatchItemStatus string

const (
	BatchItemWouldAttempt      BatchItemStatus = "WOULD_ATTEMPT"
	BatchItemBlockedPreSend    BatchItemStatus = "BLOCKED_PRE_SEND"
	BatchItemAppliedReconciled BatchItemStatus = "APPLIED_RECONCILED"
	BatchItemAlreadyApplied    BatchItemStatus = "ALREADY_APPLIED_RECONCILED"
	BatchItemStoppedUncertain  BatchItemStatus = "STOPPED_UNCERTAIN"
)

type BatchApplicationItem struct {
	ApprovalPath       string
	VacancyID          int
	ProviderResumeID   string
	Status             BatchItemStatus
	AttemptID          string
	ApplicationClass   string
	FinalOutcome       APIApplicationFinalOutcome
	TransportAttempted bool
	Error              string
}

type BatchApplicationRun struct {
	Status BatchRunStatus
	Items  []BatchApplicationItem
}

type controlledAPIApplicationExecutor interface {
	Execute(context.Context, string, time.Time) (controlledAPIApplicationResult, error)
}

func parseHHAPIApplyBatchArgs(args []string) ([]string, error) {
	paths := make([]string, 0, batchMaxApplications)
	seen := make(map[string]struct{}, batchMaxApplications)
	for index := 0; index < len(args); index++ {
		arg := args[index]
		var path string
		switch {
		case arg == "--approval-file":
			if index+1 >= len(args) || strings.HasPrefix(args[index+1], "-") || strings.TrimSpace(args[index+1]) == "" {
				return nil, errors.New("hh-api apply-batch requires --approval-file followed by a path")
			}
			path = strings.TrimSpace(args[index+1])
			index++
		case strings.HasPrefix(arg, "--approval-file="):
			path = strings.TrimSpace(strings.TrimPrefix(arg, "--approval-file="))
			if path == "" {
				return nil, errors.New("hh-api apply-batch requires a non-empty --approval-file path")
			}
		default:
			return nil, errors.New("hh-api apply-batch accepts only repeated explicit --approval-file values")
		}
		if len(paths) == batchMaxApplications {
			return nil, fmt.Errorf("hh-api apply-batch accepts at most %d approval files", batchMaxApplications)
		}
		normalized, err := filepath.Abs(filepath.Clean(path))
		if err != nil {
			return nil, errors.New("hh-api apply-batch approval path is invalid")
		}
		if _, exists := seen[normalized]; exists {
			return nil, errors.New("hh-api apply-batch rejects duplicate approval paths")
		}
		seen[normalized] = struct{}{}
		paths = append(paths, path)
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("hh-api apply-batch requires one through %d --approval-file values", batchMaxApplications)
	}
	return paths, nil
}

func validateBatchApprovalFiles(paths []string, now time.Time) error {
	seenVacancies := make(map[int]string, len(paths))
	for _, path := range paths {
		approval, err := loadAPIApplicationApproval(path)
		if err != nil {
			return fmt.Errorf("approval %s is invalid: %w", filepath.Base(path), err)
		}
		providerResumeID := approvalProviderResumeID(approval)
		if err := validateAPIApplicationApproval(approval, approval.VacancyID, providerResumeID, now); err != nil {
			return fmt.Errorf("approval %s is not executable: %w", filepath.Base(path), err)
		}
		if previous, exists := seenVacancies[approval.VacancyID]; exists {
			return fmt.Errorf("hh-api apply-batch rejects duplicate vacancy %d in %s and %s", approval.VacancyID, filepath.Base(previous), filepath.Base(path))
		}
		seenVacancies[approval.VacancyID] = path
	}
	return nil
}

func executeControlledApplicationBatch(ctx context.Context, paths []string, now time.Time, executor controlledAPIApplicationExecutor) (BatchApplicationRun, error) {
	run := BatchApplicationRun{Status: BatchRunCompleted, Items: make([]BatchApplicationItem, 0, len(paths))}
	for _, path := range paths {
		item := BatchApplicationItem{ApprovalPath: path}
		result, err := executor.Execute(ctx, path, now)
		item.VacancyID = result.Approval.VacancyID
		item.ProviderResumeID = approvalProviderResumeID(result.Approval)
		item.AttemptID = result.AttemptID
		item.ApplicationClass = string(result.Submission.Execution.ApplicationClass)
		item.FinalOutcome = result.FinalOutcome
		item.TransportAttempted = result.TransportAttempted
		if err != nil {
			item.Error = err.Error()
		}
		if !result.TransportAttempted {
			if err == nil {
				item.Status = BatchItemWouldAttempt
			} else {
				item.Status = BatchItemBlockedPreSend
			}
			run.Items = append(run.Items, item)
			continue
		}
		// A transport-attempted execution error is uncertain even when a
		// provisional final outcome was populated. In particular, a
		// reconciliation persistence error must not release the batch to the
		// next item.
		if err != nil {
			item.Status = BatchItemStoppedUncertain
			run.Status = BatchRunStoppedUncertain
			run.Items = append(run.Items, item)
			return run, nil
		}
		if batchFinalOutcomeConfirmed(result.FinalOutcome) {
			if result.FinalOutcome == APIApplicationFinalAlreadyAppliedReconciled {
				item.Status = BatchItemAlreadyApplied
			} else {
				item.Status = BatchItemAppliedReconciled
			}
			run.Items = append(run.Items, item)
			continue
		}
		item.Status = BatchItemStoppedUncertain
		run.Status = BatchRunStoppedUncertain
		run.Items = append(run.Items, item)
		return run, nil
	}
	return run, nil
}

func batchFinalOutcomeConfirmed(outcome APIApplicationFinalOutcome) bool {
	switch outcome {
	case APIApplicationFinalPostSuccessReconciled, APIApplicationFinalAlreadyAppliedReconciled, APIApplicationFinalUnknownSendReconciledSuccess:
		return true
	default:
		return false
	}
}

func runHHAPIApplyBatch(ctx context.Context, args []string, cfg Config, stdout, stderr io.Writer, deps HHAPICommandDeps) error {
	_ = stderr
	paths, err := parseHHAPIApplyBatchArgs(args)
	if err != nil {
		return err
	}
	if !strings.EqualFold(strings.TrimSpace(cfg.HHTransport), "api") {
		return errors.New("hh-api apply-batch requires HH_TRANSPORT=api")
	}
	if !cfg.DryRun && !cfg.HHWriteEnabled {
		return errors.New("hh-api apply-batch requires HH_WRITE_ENABLED=true when HH_DRY_RUN=false")
	}
	now := time.Now().UTC()
	if deps.Now != nil {
		now = deps.Now().UTC()
	}
	if err := validateBatchApprovalFiles(paths, now); err != nil {
		return err
	}
	service, err := newControlledApplicationService(ctx, cfg, deps, batchMaxApplications)
	if err != nil {
		return err
	}
	run, batchErr := executeControlledApplicationBatch(ctx, paths, now, service)
	if batchErr != nil {
		return batchErr
	}
	if cfg.DryRun {
		run.Status = BatchRunDryRun
	}
	for _, item := range run.Items {
		_, _ = fmt.Fprintf(stdout, "BATCH_ITEM status=%s vacancy_id=%d resume_id=%s transport_attempted=%t approval_file=%s\n", item.Status, item.VacancyID, safeHHAPIResumeID(item.ProviderResumeID), item.TransportAttempted, filepath.Base(item.ApprovalPath))
		if item.Error != "" {
			_, _ = fmt.Fprintf(stdout, "BATCH_ITEM_ERROR vacancy_id=%d error=%s\n", item.VacancyID, item.Error)
		}
	}
	_, _ = fmt.Fprintf(stdout, "BATCH_RESULT status=%s items=%d\n", run.Status, len(run.Items))
	if run.Status == BatchRunStoppedUncertain {
		return errors.New("hh-api apply-batch stopped after uncertain transport outcome")
	}
	return nil
}
