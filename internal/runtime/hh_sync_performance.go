package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"hh-ai-responder/internal/platform"
	"hh-ai-responder/internal/ports"
	"hh-ai-responder/internal/usecase/hhreadsync"
)

type HHSyncProgress struct {
	Target               string    `json:"target"`
	Requests             int       `json:"requests"`
	RequestsPerSecond    float64   `json:"requests_per_second"`
	Fetched              int       `json:"fetched"`
	Processed            int       `json:"processed"`
	Changed              int       `json:"changed"`
	Unchanged            int       `json:"unchanged"`
	MetadataChecked      int       `json:"metadata_checked,omitempty"`
	HistoryReused        int       `json:"history_reused,omitempty"`
	DetailedChatsFetched int       `json:"detailed_chats_fetched,omitempty"`
	DetailRequested      int       `json:"detail_requested,omitempty"`
	DetailSucceeded      int       `json:"detail_succeeded,omitempty"`
	DetailSkipped        int       `json:"detail_skipped,omitempty"`
	DetailFailed         int       `json:"detail_failed,omitempty"`
	DetailFieldsEnriched int       `json:"detail_fields_enriched,omitempty"`
	Errors               int       `json:"errors"`
	StartedAt            time.Time `json:"started_at"`
	FinishedAt           time.Time `json:"finished_at,omitempty"`
}
type hhSyncCall struct {
	meter            *operationMeter
	done             chan struct{}
	result           SyncResult
	err              error
	progress         HHSyncProgress
	maxConversations int
}

func (s *HHReadSyncService) syncRead(ctx context.Context, target string) (SyncResult, error) {
	return s.syncReadWithOptions(ctx, target, hhreadsync.ReadOptions{})
}

func (s *HHReadSyncService) syncReadWithOptions(ctx context.Context, target string, options hhreadsync.ReadOptions) (SyncResult, error) {
	if s == nil || s.client == nil {
		return SyncResult{Errors: []string{"HH sync is not configured"}}, nil
	}
	if target == "inbox" && hhReadPriorityFromContext(ctx) == 0 {
		ctx = withHHReadPriority(ctx, hhReadForegroundInbox)
	} else if strings.HasPrefix(target, "conversation:") && hhReadPriorityFromContext(ctx) == 0 {
		ctx = withHHReadPriority(ctx, hhReadTargeted)
	}
	s.callsMu.Lock()
	if call := s.calls[target]; call != nil {
		if options.MaxConversations != call.maxConversations {
			s.callsMu.Unlock()
			return SyncResult{Errors: []string{"another Career inbox refresh is already in progress with a different conversation bound"}}, errors.New("another Career inbox refresh is already in progress with a different conversation bound")
		}
		s.callsMu.Unlock()
		select {
		case <-call.done:
			return call.result, call.err
		case <-ctx.Done():
			return SyncResult{}, ctx.Err()
		}
	}
	call := &hhSyncCall{done: make(chan struct{}), maxConversations: options.MaxConversations, progress: HHSyncProgress{Target: target, StartedAt: time.Now().UTC()}}
	if s.calls == nil {
		s.calls = map[string]*hhSyncCall{}
	}
	s.calls[target] = call
	s.callsMu.Unlock()
	result, err := s.lockedSyncRead(ctx, target, call, options)
	s.callsMu.Lock()
	call.result, call.err = result, err
	call.progress.Requests = result.Performance.Requests
	if result.Performance.Duration > 0 {
		call.progress.RequestsPerSecond = float64(call.progress.Requests) / result.Performance.Duration.Seconds()
	}
	call.progress.FinishedAt = time.Now().UTC()
	s.lastProgress = call.progress
	delete(s.calls, target)
	close(call.done)
	s.callsMu.Unlock()
	return result, err
}

// This is an operation lease, not a data-store lock. Another process cannot
// start the identical network job; unrelated targeted jobs remain available.
func (s *HHReadSyncService) lockedSyncRead(ctx context.Context, target string, call *hhSyncCall, options hhreadsync.ReadOptions) (SyncResult, error) {
	key := fmt.Sprintf("%x", sha256.Sum256([]byte(target)))
	lease, err := platform.AcquireProcessLock(s.statePath+".sync-"+key[:16], 30*time.Minute)
	if err != nil {
		return SyncResult{Errors: []string{"Sync already in progress in another process"}}, nil
	}
	defer lease.Release()
	return s.fetchAndCommit(ctx, target, call, options)
}
func (s *HHReadSyncService) Progress() []HHSyncProgress {
	s.callsMu.Lock()
	defer s.callsMu.Unlock()
	out := []HHSyncProgress{}
	for _, call := range s.calls {
		p := call.progress
		if call.meter != nil {
			call.meter.Lock()
			p.Requests = call.meter.value.Requests
			call.meter.Unlock()
			if elapsed := time.Since(p.StartedAt).Seconds(); elapsed > 0 {
				p.RequestsPerSecond = float64(p.Requests) / elapsed
			}
		}
		out = append(out, p)
	}
	if len(out) == 0 && s.lastProgress.Target != "" {
		out = append(out, s.lastProgress)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Target < out[j].Target })
	return out
}
func (s *HHReadSyncService) progress(call *hhSyncCall, result SyncResult) {
	s.callsMu.Lock()
	defer s.callsMu.Unlock()
	call.progress.Fetched = result.Fetched
	call.progress.Processed = result.Created + result.Updated + result.Unchanged + result.Skipped
	call.progress.Changed = result.Created + result.Updated
	call.progress.Unchanged = result.Unchanged
	call.progress.MetadataChecked = result.MetadataChecked
	call.progress.HistoryReused = result.HistoryReused
	call.progress.DetailedChatsFetched = result.DetailedChatsFetched
	call.progress.DetailRequested = result.DetailRequested
	call.progress.DetailSucceeded = result.DetailSucceeded
	call.progress.DetailSkipped = result.DetailSkipped
	call.progress.DetailFailed = result.DetailFailed
	call.progress.DetailFieldsEnriched = result.DetailFieldsEnriched
	call.progress.Errors = len(result.Errors)
}
func (s *HHReadSyncService) fetchAndCommit(ctx context.Context, target string, call *hhSyncCall, options hhreadsync.ReadOptions) (result SyncResult, err error) {
	if target == "inbox" {
		s.commitMutex().Lock()
		known := map[string]EmployerConversation{}
		if s.conversations != nil {
			if conversations, listErr := s.conversations.ListConversations(); listErr == nil {
				for _, c := range conversations {
					known[c.HHConversationID] = c
				}
			}
		}
		s.commitMutex().Unlock()
		ctx = context.WithValue(ctx, hhMetadataContextKey{}, known)
	}
	meter := &operationMeter{}
	s.callsMu.Lock()
	call.meter = meter
	s.callsMu.Unlock()
	ctx = context.WithValue(ctx, operationMeterKey{}, meter)
	result.StartedAt = time.Now().UTC()
	defer func() {
		result.FinishedAt = time.Now().UTC()
		meter.Lock()
		result.Performance = meter.value
		meter.Unlock()
		result.Performance.Duration = time.Since(result.StartedAt)
		perfRecord("sync."+strings.Split(target, ":")[0], result.StartedAt, result.Fetched)
	}()
	ctx = context.WithValue(ctx, hhProgressContextKey{}, func(total, done int) {
		s.callsMu.Lock()
		defer s.callsMu.Unlock()
		call.progress.Fetched = result.Fetched + total
		call.progress.Processed = result.Fetched + done
	})
	options.OnPage = func(page hhreadsync.Result) {
		result.Fetched = page.Fetched
		result.MetadataChecked = page.MetadataChecked
		result.HistoryReused = page.HistoryReused
		result.DetailedChatsFetched = page.DetailedChatsFetched
		result.DetailRequested = page.DetailRequested
		result.DetailSucceeded = page.DetailSucceeded
		result.DetailSkipped = page.DetailSkipped
		result.DetailFailed = page.DetailFailed
		result.DetailFieldsEnriched = page.DetailFieldsEnriched
		s.progress(call, result)
	}
	batch, readResult, readErr := s.syncUsecase().ReadBatch(ctx, hhreadsync.Target(target), options)
	result.Fetched = readResult.Fetched
	result.MetadataChecked = readResult.MetadataChecked
	result.HistoryReused = readResult.HistoryReused
	result.DetailedChatsFetched = readResult.DetailedChatsFetched
	result.DetailRequested = readResult.DetailRequested
	result.DetailSucceeded = readResult.DetailSucceeded
	result.DetailSkipped = readResult.DetailSkipped
	result.DetailFailed = readResult.DetailFailed
	result.DetailFieldsEnriched = readResult.DetailFieldsEnriched
	result.SelectedConversationIDs = append([]string{}, readResult.SelectedConversationIDs...)
	result.Errors = append(result.Errors, readResult.Errors...)
	if readErr != nil {
		return result, readErr
	}
	// Network is finished before either the in-process or file locks are acquired.
	s.commitMutex().Lock()
	defer s.commitMutex().Unlock()
	if err := ctx.Err(); err != nil {
		return result, err
	}
	err = s.commitReadBatch(batch.Vacancies, batch.Applications, batch.Conversations, &result, ctx)
	if err != nil {
		result.Errors = append(result.Errors, safeSyncError(err))
	}
	s.progress(call, result)
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	// Reload/merge sync state under its own file lock; targeted reads do not
	// advance the timestamp of a complete inbox sync.
	stateErr := withStoreLock(s.statePath, func() error {
		if _, e := os.Stat(s.statePath); e == nil {
			if _, e = readPrivateJSON(s.statePath, &s.state); e != nil {
				return e
			}
		}
		now := time.Now().UTC()
		if len(result.Errors) == 0 {
			switch target {
			case "vacancies":
				s.state.LastVacancySyncAt = now
				s.state.VacancyCursor = ""
			case "applications":
				s.state.LastApplicationSyncAt = now
				s.state.ApplicationCursor = ""
			case "conversations":
				s.state.LastConversationSyncAt = now
				s.state.ConversationCursor = ""
			case "inbox":
				s.state.LastInboxRefreshAt = now
			}
			s.state.LastSuccess = now
		}
		s.state.LastError = firstSyncError(result.Errors)
		return s.saveStateUnlocked()
	})
	if stateErr != nil {
		result.Errors = append(result.Errors, safeSyncError(stateErr))
	}
	return result, nil
}
func (s *HHReadSyncService) commitMutex() sync.Locker {
	if s.externalCommitMu != nil {
		return s.externalCommitMu
	}
	return &s.commitMu
}

// Keep original stores visible until a validated batch has been durably saved.
// JSON files remain independent atomic documents, as before; this is not a
// multi-file transaction. A failed later commit is reconciled on the next sync.
func (s *HHReadSyncService) commitReadBatch(v []HHVacancyRecord, a []HHApplicationRecord, c []HHConversationRecord, result *SyncResult, contexts ...context.Context) error {
	ctx := syncContext(contexts)
	if s.career != nil && s.career.Postgres != nil {
		return s.commitPostgresReadBatch(ctx, v, a, c, result)
	}
	start := time.Now()
	defer perfRecord("sync.durable_merge", start, len(v)+len(a)+len(c))
	stage := NewHHReadSyncServiceWithOptions(s.client, HHReadSyncServiceOptions{
		Analyzer: s.analyzer, Candidate: s.candidate, StatusMapper: s.statusMapper,
		Clarifications: s.clarifications, Drafts: s.drafts, StatePath: s.statePath,
	})
	var preparedVacancyMatches preparedVacancyAnalyzer
	paths := []string{}
	if s.conversations != nil {
		x, err := s.conversations.cloneForSync()
		if err != nil {
			return err
		}
		stage.conversations = x
		paths = append(paths, x.path)
	}
	if s.applications != nil {
		var err error
		stage.applications, err = s.applications.cloneForSync()
		if err != nil {
			return err
		}
		stage.applications.conversationStore = stage.conversations
		paths = append(paths, stage.applications.path)
	}
	if s.vacancies != nil {
		x, err := s.vacancies.cloneForSync()
		if err != nil {
			return err
		}
		stage.vacancies = x
		paths = append(paths, x.path)
	}
	stage.syncPolicy = stage.buildSyncUsecase()
	sort.Strings(paths)
	// Analysis happens outside file locks. A changed durable vacancy file cancels
	// this plan instead of evaluating new input while holding a store lock.
	var plannedVacancies [32]byte
	if len(v) > 0 && stage.vacancies != nil {
		if _, err := os.Stat(stage.vacancies.path); err == nil {
			if err := stage.vacancies.Load(); err != nil {
				return err
			}
		}
		_, _, plannedVacancies = syncStoreHashes(stage)
		if stage.analyzer != nil {
			computeStart := time.Now()
			prepared, err := stage.prepareVacancyAnalysis(v)
			meterRecord(ctx, "compute", time.Since(computeStart))
			if err != nil {
				return err
			}
			preparedVacancyMatches = prepared
		}
	}
	return withBatchLocks(paths, func() error {
		// Existing durable files win over a stale process. Missing files retain
		// explicitly prepared in-memory fixtures/new stores.
		loads := map[string]func() error{}
		if stage.conversations != nil {
			loads[stage.conversations.path] = stage.conversations.Load
		}
		if stage.applications != nil {
			loads[stage.applications.path] = stage.applications.Load
		}
		if stage.vacancies != nil {
			loads[stage.vacancies.path] = stage.vacancies.Load
		}
		loadStart := time.Now()
		for path, load := range loads {
			if _, err := os.Stat(path); err == nil {
				if err = load(); err != nil {
					return err
				}
			} else if !os.IsNotExist(err) {
				return err
			}
		}
		meterRecord(ctx, "disk", time.Since(loadStart))
		computeStart := time.Now()
		beforeC, beforeA, beforeV := syncStoreHashes(stage)
		if len(v) > 0 && stage.vacancies != nil && beforeV != plannedVacancies {
			return errors.New("vacancy store changed during analysis; retry read-only sync")
		}
		if len(v) > 0 && stage.vacancies == nil || len(a) > 0 && stage.applications == nil || len(c) > 0 && stage.conversations == nil {
			return errors.New("HH sync store unavailable")
		}
		imported, importErr := stage.syncUsecase().ImportBatch(ctx, hhreadsync.Batch{
			Vacancies: v, Applications: a, Conversations: c,
		}, hhreadsync.ImportOptions{})
		result.Created += imported.Created
		result.Updated += imported.Updated
		result.Unchanged += imported.Unchanged
		result.Skipped += imported.Skipped
		result.Warnings = append(result.Warnings, imported.Warnings...)
		result.Errors = append(result.Errors, imported.Errors...)
		if importErr != nil {
			return importErr
		}
		if err := stage.applyPreparedVacancyAnalysis(ctx, NewJSONVacancyRepository(stage.vacancies), v, preparedVacancyMatches); err != nil {
			return err
		}
		afterC, afterA, afterV := syncStoreHashes(stage)
		meterRecord(ctx, "compute", time.Since(computeStart))
		saveStart := time.Now()
		defer func() { meterRecord(ctx, "disk", time.Since(saveStart)) }()
		if beforeC != afterC {
			if err := stage.conversations.saveUnlocked(); err != nil {
				return err
			}
		}
		if beforeA != afterA {
			if err := stage.applications.saveUnlocked(); err != nil {
				return err
			}
		}
		if beforeV != afterV {
			if err := stage.vacancies.saveUnlocked(); err != nil {
				return err
			}
		}
		if s.conversations != nil {
			if err := s.conversations.adoptFromSync(stage.conversations); err != nil {
				return err
			}
		}
		if s.applications != nil {
			s.applications.repository = stage.applications.repository
			if err := s.applications.refreshCompatibilityMirror(); err != nil {
				return err
			}
		}
		if s.vacancies != nil {
			*s.vacancies = *stage.vacancies
		}
		return nil
	})
}
func withBatchLocks(paths []string, fn func() error) error {
	if len(paths) == 0 {
		return fn()
	}
	if paths[0] == "" {
		return withBatchLocks(paths[1:], fn)
	}
	return withStoreLock(paths[0], func() error { return withBatchLocks(paths[1:], fn) })
}

func syncStoreHashes(s *HHReadSyncService) ([32]byte, [32]byte, [32]byte) {
	var c, a, v any
	if s.conversations != nil {
		c, _ = s.conversations.ListConversations()
	}
	if s.applications != nil {
		applications, _ := s.applications.ListApplications()
		events, _ := s.applications.ListEvents()
		a = struct {
			Applications []JobApplication   `json:"applications"`
			Events       []ApplicationEvent `json:"events"`
		}{Applications: applications, Events: events}
	}
	if s.vacancies != nil {
		values, err := s.vacancies.List()
		if err == nil {
			v = values
		}
	}
	hash := func(value any) [32]byte { raw, _ := json.Marshal(value); return sha256.Sum256(raw) }
	return hash(c), hash(a), hash(v)
}

type preparedVacancyMatch struct {
	vacancy Vacancy
	match   MatchResult
}
type preparedVacancyAnalyzer []preparedVacancyMatch

func (a preparedVacancyAnalyzer) Match(value Vacancy) (MatchResult, bool) {
	for _, item := range a {
		if vacancySourceEquivalent(item.vacancy, value) {
			return item.match, true
		}
	}
	return MatchResult{}, false
}

func (a preparedVacancyAnalyzer) Analyze(v Vacancy, _ any) MatchResult {
	if match, ok := a.Match(v); ok {
		return match
	}
	// The plan is checked against the durable vacancy snapshot before importing.
	// Unplanned input must never acquire a positive recommendation.
	return MatchResult{Recommendation: &ApplicationRecommendation{Decision: RecommendationMaybe, Reason: "Vacancy changed while preparing analysis; review required"}}
}
func (s *HHReadSyncService) prepareVacancyAnalysis(records []HHVacancyRecord) (preparedVacancyAnalyzer, error) {
	if s.analyzer == nil || s.vacancies == nil {
		return nil, nil
	}
	return s.prepareVacancyAnalysisAgainst(context.Background(), s.vacancyRepository(), records)
}

func (s *HHReadSyncService) prepareVacancyAnalysisAgainst(ctx context.Context, store ports.VacancyStore, records []HHVacancyRecord) (preparedVacancyAnalyzer, error) {
	if s == nil || s.analyzer == nil || store == nil {
		return nil, nil
	}
	start := time.Now()
	defer perfRecord("sync.vacancy_analysis", start, len(records))
	prepared := preparedVacancyAnalyzer{}
	for _, record := range records {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		value, err := mapHHVacancy(record)
		if err != nil {
			continue
		}
		old, err := store.GetByExternalID(ctx, value.ExternalID)
		if err == nil {
			value = hhreadsync.MergeVacancy(old, value)
			if vacancySourceEquivalent(old, value) {
				continue
			}
		}
		if err != nil && !errors.Is(err, ErrVacancyNotFound) {
			return nil, err
		}
		prepared = append(prepared, preparedVacancyMatch{value, s.analyzer.Analyze(value, s.candidate)})
	}
	return prepared, nil
}

// applyPreparedVacancyAnalysis is root compatibility work. Synchronization
// imports provider state first; matching then updates only the derived local
// fields, keeping Candidate/AI policy out of hhreadsync.
func (s *HHReadSyncService) applyPreparedVacancyAnalysis(ctx context.Context, store ports.VacancyStore, records []HHVacancyRecord, prepared preparedVacancyAnalyzer) error {
	if s == nil || s.analyzer == nil || store == nil || len(prepared) == 0 {
		return nil
	}
	for _, record := range records {
		if err := ctx.Err(); err != nil {
			return err
		}
		value, err := mapHHVacancy(record)
		if err != nil {
			continue
		}
		current, err := store.GetByExternalID(ctx, value.ExternalID)
		if err != nil {
			if errors.Is(err, ErrVacancyNotFound) {
				continue
			}
			return err
		}
		merged := hhreadsync.MergeVacancy(current, value)
		match, ok := prepared.Match(merged)
		if !ok {
			continue
		}
		if !vacancySourceEquivalent(merged, current) {
			match = prepared.Analyze(merged, nil)
		}
		current.MatchResult = &match
		current.ApplicationRecommendation = match.Recommendation
		if err := store.Update(ctx, current); err != nil {
			return err
		}
	}
	return nil
}
