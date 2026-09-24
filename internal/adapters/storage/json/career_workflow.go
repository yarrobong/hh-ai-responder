package jsonstorage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"hh-ai-responder/internal/careeragent"
	"hh-ai-responder/internal/platform"
	"hh-ai-responder/internal/ports"
)

const CareerWorkflowFilename = "career_agent_workflow.json"

type careerWorkflowFile struct {
	Version      int                                  `json:"version"`
	Runs         []careeragent.AgentRun               `json:"runs"`
	Items        []careeragent.AgentRunItem           `json:"items"`
	Preparations []careeragent.ApplicationPreparation `json:"preparations"`
}

// CareerWorkflowRepository is a versioned, atomic compatibility store for
// workflow telemetry. It has no HH or approval capability.
type CareerWorkflowRepository struct {
	mu           sync.RWMutex
	path         string
	runs         []careeragent.AgentRun
	items        []careeragent.AgentRunItem
	preparations []careeragent.ApplicationPreparation
}

func NewCareerWorkflowRepository(path string) *CareerWorkflowRepository {
	if strings.TrimSpace(path) == "" {
		path = CareerWorkflowFilename
	}
	return &CareerWorkflowRepository{path: path, runs: []careeragent.AgentRun{}, items: []careeragent.AgentRunItem{}, preparations: []careeragent.ApplicationPreparation{}}
}

func (r *CareerWorkflowRepository) Load() error {
	if r == nil || strings.TrimSpace(r.path) == "" {
		return errors.New("career workflow store requires a path")
	}
	file, err := readCareerWorkflowFile(r.path)
	if err != nil {
		return err
	}
	r.mu.Lock()
	r.replaceMemory(file)
	r.mu.Unlock()
	return nil
}

func (r *CareerWorkflowRepository) StartRun(ctx context.Context, run careeragent.AgentRun) error {
	if err := workflowContextError(ctx); err != nil {
		return err
	}
	if run.Status != careeragent.AgentRunStatusRunning {
		return errors.New("career workflow start requires a running agent run")
	}
	if strings.TrimSpace(run.RunType) == "" {
		run.RunType = "career_agent"
	}
	if err := run.Validate(); err != nil {
		return err
	}
	return r.withLockedFile(func(file *careerWorkflowFile) error {
		for _, existing := range file.Runs {
			if existing.ID != run.ID {
				continue
			}
			if existing.StartedAt.Equal(run.StartedAt) && existing.Stage == run.Stage && existing.RunType == run.RunType {
				return nil
			}
			return careeragent.ErrWorkflowConflict
		}
		file.Runs = append(file.Runs, run)
		return nil
	})
}

func (r *CareerWorkflowRepository) AcquireRun(ctx context.Context, run careeragent.AgentRun) (bool, error) {
	if err := workflowContextError(ctx); err != nil {
		return false, err
	}
	if run.Status != careeragent.AgentRunStatusRunning || strings.TrimSpace(run.ID) == "" {
		return false, errors.New("career workflow acquire requires a running identified run")
	}
	if err := run.Validate(); err != nil {
		return false, err
	}
	claimed := false
	err := r.withLockedFile(func(file *careerWorkflowFile) error {
		for _, existing := range file.Runs {
			if existing.ID != run.ID {
				continue
			}
			if existing.StartedAt.Equal(run.StartedAt) && existing.Stage == run.Stage && existing.RunType == run.RunType && existing.Status == careeragent.AgentRunStatusRunning {
				return nil
			}
			if existing.Status == careeragent.AgentRunStatusRunning {
				return careeragent.ErrWorkflowConflict
			}
			return nil
		}
		file.Runs = append(file.Runs, run)
		claimed = true
		return nil
	})
	return claimed, err
}

func (r *CareerWorkflowRepository) FinishRun(ctx context.Context, run careeragent.AgentRun) error {
	if err := workflowContextError(ctx); err != nil {
		return err
	}
	if err := run.Validate(); err != nil {
		return err
	}
	if run.Status == careeragent.AgentRunStatusRunning {
		return errors.New("career workflow finish requires a terminal run")
	}
	return r.withLockedFile(func(file *careerWorkflowFile) error {
		for index, existing := range file.Runs {
			if existing.ID != run.ID {
				continue
			}
			if existing.FinishedAt != nil && !sameRunTerminalState(existing, run) {
				return careeragent.ErrWorkflowConflict
			}
			file.Runs[index] = run
			return nil
		}
		return careeragent.ErrAgentRunNotFound
	})
}

func (r *CareerWorkflowRepository) UpsertRunItem(ctx context.Context, item careeragent.AgentRunItem) error {
	if err := workflowContextError(ctx); err != nil {
		return err
	}
	item.NormalizeTarget()
	if err := item.Validate(); err != nil {
		return err
	}
	return r.withLockedFile(func(file *careerWorkflowFile) error {
		for index, existing := range file.Items {
			existing.NormalizeTarget()
			if existing.RunID == item.RunID && existing.TargetKey() == item.TargetKey() {
				file.Items[index] = item
				return nil
			}
		}
		file.Items = append(file.Items, item)
		return nil
	})
}

func (r *CareerWorkflowRepository) UpsertPreparation(ctx context.Context, preparation careeragent.ApplicationPreparation) error {
	if err := workflowContextError(ctx); err != nil {
		return err
	}
	if err := preparation.Validate(); err != nil {
		return err
	}
	return r.withLockedFile(func(file *careerWorkflowFile) error {
		for index, existing := range file.Preparations {
			if existing.VacancyID == preparation.VacancyID && existing.InputFingerprint == preparation.InputFingerprint {
				preparation.ID = existing.ID
				file.Preparations[index] = preparation
				return nil
			}
		}
		file.Preparations = append(file.Preparations, preparation)
		return nil
	})
}

func (r *CareerWorkflowRepository) GetRun(ctx context.Context, id string) (careeragent.AgentRun, error) {
	if err := workflowContextError(ctx); err != nil {
		return careeragent.AgentRun{}, err
	}
	var result careeragent.AgentRun
	err := r.withLockedFile(func(file *careerWorkflowFile) error {
		for _, run := range file.Runs {
			if run.ID == id {
				result = run
				return nil
			}
		}
		return careeragent.ErrAgentRunNotFound
	})
	return result, err
}

func (r *CareerWorkflowRepository) ListRuns(ctx context.Context, query careeragent.RunQuery) ([]careeragent.AgentRun, error) {
	if err := workflowContextError(ctx); err != nil {
		return nil, err
	}
	limit := workflowLimit(query.Limit)
	var result []careeragent.AgentRun
	err := r.withLockedFile(func(file *careerWorkflowFile) error {
		result = append([]careeragent.AgentRun(nil), file.Runs...)
		if query.Status != nil {
			filtered := result[:0]
			for _, run := range result {
				if run.Status == *query.Status {
					filtered = append(filtered, run)
				}
			}
			result = filtered
		}
		sort.SliceStable(result, func(i, j int) bool {
			if !result[i].StartedAt.Equal(result[j].StartedAt) {
				return result[i].StartedAt.After(result[j].StartedAt)
			}
			return result[i].ID < result[j].ID
		})
		if len(result) > limit {
			result = result[:limit]
		}
		return nil
	})
	return result, err
}

func (r *CareerWorkflowRepository) ListRunItems(ctx context.Context, runID string, limit int) ([]careeragent.AgentRunItem, error) {
	if err := workflowContextError(ctx); err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 1000 {
		limit = 1000
	}
	var result []careeragent.AgentRunItem
	err := r.withLockedFile(func(file *careerWorkflowFile) error {
		for _, item := range file.Items {
			item.NormalizeTarget()
			if runID != "" && item.RunID != runID {
				continue
			}
			result = append(result, item)
		}
		sort.SliceStable(result, func(i, j int) bool {
			if !result[i].CreatedAt.Equal(result[j].CreatedAt) {
				return result[i].CreatedAt.After(result[j].CreatedAt)
			}
			return result[i].TargetKey() < result[j].TargetKey()
		})
		if len(result) > limit {
			result = result[:limit]
		}
		return nil
	})
	return result, err
}

func (r *CareerWorkflowRepository) GetPreparation(ctx context.Context, vacancyID int) (careeragent.ApplicationPreparation, error) {
	if err := workflowContextError(ctx); err != nil {
		return careeragent.ApplicationPreparation{}, err
	}
	var result careeragent.ApplicationPreparation
	err := r.withLockedFile(func(file *careerWorkflowFile) error {
		for _, preparation := range file.Preparations {
			if preparation.VacancyID != vacancyID || result.UpdatedAt.After(preparation.UpdatedAt) {
				continue
			}
			result = preparation
		}
		if result.ID == "" {
			return careeragent.ErrPreparationNotFound
		}
		return nil
	})
	return result, err
}

func (r *CareerWorkflowRepository) ListPreparations(ctx context.Context, query careeragent.PreparationQuery) ([]careeragent.ApplicationPreparation, error) {
	if err := workflowContextError(ctx); err != nil {
		return nil, err
	}
	limit := workflowLimit(query.Limit)
	var result []careeragent.ApplicationPreparation
	err := r.withLockedFile(func(file *careerWorkflowFile) error {
		for _, preparation := range file.Preparations {
			if query.VacancyID != nil && preparation.VacancyID != *query.VacancyID || !preparationStatusSelected(preparation.Status, query.Statuses) {
				continue
			}
			result = append(result, preparation)
		}
		sort.SliceStable(result, func(i, j int) bool {
			if !result[i].UpdatedAt.Equal(result[j].UpdatedAt) {
				return result[i].UpdatedAt.After(result[j].UpdatedAt)
			}
			return result[i].ID < result[j].ID
		})
		if len(result) > limit {
			result = result[:limit]
		}
		return nil
	})
	return result, err
}

func (r *CareerWorkflowRepository) RecoverInterruptedRuns(ctx context.Context, before time.Time) error {
	if err := workflowContextError(ctx); err != nil {
		return err
	}
	if before.IsZero() {
		return errors.New("career workflow recovery cutoff is required")
	}
	now := before.UTC()
	return r.withLockedFile(func(file *careerWorkflowFile) error {
		for index := range file.Runs {
			if file.Runs[index].Status != careeragent.AgentRunStatusRunning || file.Runs[index].StartedAt.After(before) {
				continue
			}
			if err := file.Runs[index].RecoverInterrupted(now); err != nil {
				return err
			}
		}
		return nil
	})
}

func (r *CareerWorkflowRepository) withLockedFile(fn func(*careerWorkflowFile) error) error {
	if r == nil || strings.TrimSpace(r.path) == "" {
		return errors.New("career workflow store requires a path")
	}
	return platform.WithPrivateFileLock(r.path, 30*time.Minute, func() error {
		file, err := readCareerWorkflowFile(r.path)
		if err != nil {
			return err
		}
		if err := fn(&file); err != nil {
			return err
		}
		if err := writeCareerWorkflowFile(r.path, file); err != nil {
			return err
		}
		r.mu.Lock()
		r.replaceMemory(file)
		r.mu.Unlock()
		return nil
	})
}

func (r *CareerWorkflowRepository) replaceMemory(file careerWorkflowFile) {
	r.runs = append([]careeragent.AgentRun(nil), file.Runs...)
	r.items = append([]careeragent.AgentRunItem(nil), file.Items...)
	r.preparations = append([]careeragent.ApplicationPreparation(nil), file.Preparations...)
}

func readCareerWorkflowFile(path string) (careerWorkflowFile, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return careerWorkflowFile{Version: 1, Runs: []careeragent.AgentRun{}, Items: []careeragent.AgentRunItem{}, Preparations: []careeragent.ApplicationPreparation{}}, nil
	}
	if err != nil {
		return careerWorkflowFile{}, fmt.Errorf("read career workflow store: %w", err)
	}
	var file careerWorkflowFile
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&file); err != nil || decoder.Decode(new(struct{})) != io.EOF || file.Version != 1 || file.Runs == nil || file.Items == nil || file.Preparations == nil {
		return careerWorkflowFile{}, errors.New("invalid career workflow store: expected version 1 and all collections")
	}
	if err := validateCareerWorkflowFile(file); err != nil {
		return careerWorkflowFile{}, err
	}
	return file, nil
}

func writeCareerWorkflowFile(path string, file careerWorkflowFile) error {
	if err := validateCareerWorkflowFile(file); err != nil {
		return err
	}
	sort.SliceStable(file.Runs, func(i, j int) bool { return file.Runs[i].ID < file.Runs[j].ID })
	sort.SliceStable(file.Items, func(i, j int) bool {
		if file.Items[i].RunID != file.Items[j].RunID {
			return file.Items[i].RunID < file.Items[j].RunID
		}
		return file.Items[i].TargetKey() < file.Items[j].TargetKey()
	})
	sort.SliceStable(file.Preparations, func(i, j int) bool {
		if file.Preparations[i].VacancyID != file.Preparations[j].VacancyID {
			return file.Preparations[i].VacancyID < file.Preparations[j].VacancyID
		}
		return file.Preparations[i].InputFingerprint < file.Preparations[j].InputFingerprint
	})
	raw, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return fmt.Errorf("encode career workflow store: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create career workflow directory: %w", err)
	}
	return platform.WritePrivateFileAtomic(path, append(raw, '\n'), ".career-agent-workflow-*.tmp")
}

func validateCareerWorkflowFile(file careerWorkflowFile) error {
	if file.Version != 1 || file.Runs == nil || file.Items == nil || file.Preparations == nil {
		return errors.New("invalid career workflow store shape")
	}
	seenRuns := map[string]struct{}{}
	for _, run := range file.Runs {
		if err := run.Validate(); err != nil {
			return err
		}
		if _, exists := seenRuns[run.ID]; exists {
			return fmt.Errorf("duplicate agent run id %q", run.ID)
		}
		seenRuns[run.ID] = struct{}{}
	}
	seenItems := map[string]struct{}{}
	for index := range file.Items {
		item := &file.Items[index]
		item.NormalizeTarget()
		if err := item.Validate(); err != nil {
			return err
		}
		if _, exists := seenRuns[item.RunID]; !exists {
			return fmt.Errorf("agent run item %q references unknown run %q", item.ID, item.RunID)
		}
		key := item.RunID + "/" + item.TargetKey()
		if _, exists := seenItems[key]; exists {
			return fmt.Errorf("duplicate agent run item %q", key)
		}
		seenItems[key] = struct{}{}
	}
	seenPreparations := map[string]struct{}{}
	for _, preparation := range file.Preparations {
		if err := preparation.Validate(); err != nil {
			return err
		}
		key := fmt.Sprintf("%d/%s", preparation.VacancyID, preparation.InputFingerprint)
		if _, exists := seenPreparations[key]; exists {
			return fmt.Errorf("duplicate application preparation %q", key)
		}
		seenPreparations[key] = struct{}{}
	}
	return nil
}

func workflowLimit(value int) int {
	if value <= 0 || value > 100 {
		return 100
	}
	return value
}

func preparationStatusSelected(status careeragent.PreparationStatus, selected []careeragent.PreparationStatus) bool {
	if len(selected) == 0 {
		return true
	}
	for _, value := range selected {
		if status == value {
			return true
		}
	}
	return false
}

func sameRunTerminalState(left, right careeragent.AgentRun) bool {
	return left.Status == right.Status && left.FinishedAt != nil && right.FinishedAt != nil && left.FinishedAt.Equal(*right.FinishedAt) && left.ResultCode == right.ResultCode
}

func workflowContextError(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}

var _ ports.CareerWorkflowStore = (*CareerWorkflowRepository)(nil)
