package main

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
	Errors               int       `json:"errors"`
	StartedAt            time.Time `json:"started_at"`
	FinishedAt           time.Time `json:"finished_at,omitempty"`
}
type hhSyncCall struct {
	meter    *operationMeter
	done     chan struct{}
	result   SyncResult
	err      error
	progress HHSyncProgress
}

func (s *HHReadSyncService) syncRead(ctx context.Context, target string) (SyncResult, error) {
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
		s.callsMu.Unlock()
		select {
		case <-call.done:
			return call.result, call.err
		case <-ctx.Done():
			return SyncResult{}, ctx.Err()
		}
	}
	call := &hhSyncCall{done: make(chan struct{}), progress: HHSyncProgress{Target: target, StartedAt: time.Now().UTC()}}
	if s.calls == nil {
		s.calls = map[string]*hhSyncCall{}
	}
	s.calls[target] = call
	s.callsMu.Unlock()
	result, err := s.lockedSyncRead(ctx, target, call)
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
func (s *HHReadSyncService) lockedSyncRead(ctx context.Context, target string, call *hhSyncCall) (SyncResult, error) {
	key := fmt.Sprintf("%x", sha256.Sum256([]byte(target)))
	lease, err := acquireLocalProcessLock(s.statePath+".sync-"+key[:16], 30*time.Minute)
	if err != nil {
		return SyncResult{Errors: []string{"Sync already in progress in another process"}}, nil
	}
	defer lease.Release()
	return s.fetchAndCommit(ctx, target, call)
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
	call.progress.Errors = len(result.Errors)
}
func (s *HHReadSyncService) fetchAndCommit(ctx context.Context, target string, call *hhSyncCall) (result SyncResult, err error) {
	if target == "inbox" {
		s.commitMutex().Lock()
		known := map[string]EmployerConversation{}
		if s.conversations != nil {
			for _, c := range s.conversations.conversations {
				known[c.HHConversationID] = c
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
	var vacancies []HHVacancyRecord
	var applications []HHApplicationRecord
	var conversations []HHConversationRecord
	cursor := ""
	seen := map[string]bool{}
	ctx = context.WithValue(ctx, hhProgressContextKey{}, func(total, done int) {
		s.callsMu.Lock()
		defer s.callsMu.Unlock()
		call.progress.Fetched = result.Fetched + total
		call.progress.Processed = result.Fetched + done
	})
	for {
		if err = ctx.Err(); err != nil {
			return result, err
		}
		if seen[cursor] {
			result.Errors = append(result.Errors, "HH pagination repeated cursor; sync incomplete")
			break
		}
		seen[cursor] = true
		next := ""
		var readErr error
		switch {
		case target == "vacancies":
			var page HHVacancyPage
			page, readErr = s.client.ReadVacancies(ctx, cursor)
			vacancies = append(vacancies, page.Items...)
			result.Fetched += len(page.Items)
			next = page.NextCursor
		case target == "applications":
			var page HHApplicationPage
			page, readErr = s.client.ReadApplications(ctx, cursor)
			applications = append(applications, page.Items...)
			result.Fetched += len(page.Items)
			next = page.NextCursor
		case target == "conversations" || target == "inbox":
			var page HHConversationPage
			page, readErr = s.client.ReadConversations(ctx, cursor)
			conversations = append(conversations, page.Items...)
			result.Fetched += len(page.Items)
			result.MetadataChecked += page.MetadataChecked
			result.HistoryReused += page.HistoryReused
			result.DetailedChatsFetched += page.DetailedChatsFetched
			next = page.NextCursor
		default:
			reader, ok := s.client.(HHConversationRecordReader)
			if !ok {
				readErr = errors.New("targeted HH conversation read is unavailable")
			} else {
				id := strings.TrimPrefix(target, "conversation:")
				var record HHConversationRecord
				record, readErr = reader.ReadConversation(ctx, id)
				if readErr == nil && record.ExternalID != id {
					readErr = errors.New("targeted HH conversation identity mismatch")
				}
				if readErr == nil {
					if conversations == nil {
						conversations = []HHConversationRecord{}
					}
					if record.Metadata == nil {
						record.Metadata = map[string]string{}
					}
					record.Metadata["sync_detail_at"] = time.Now().UTC().Format(time.RFC3339Nano)
					conversations = append(conversations, record)
					result.Fetched = 1
				}
			}
		}
		if readErr != nil {
			result.Errors = append(result.Errors, safeSyncError(readErr))
			break
		}
		s.progress(call, result)
		cursor = strings.TrimSpace(next)
		if cursor == "" {
			break
		}
	}
	for _, record := range conversations {
		if !record.MetadataUnchanged && strings.TrimSpace(record.ExternalID) != "" {
			result.ChangedConversationIDs = append(result.ChangedConversationIDs, record.ExternalID)
		}
	}
	// Network is finished before either the in-process or file locks are acquired.
	s.commitMutex().Lock()
	defer s.commitMutex().Unlock()
	if err := ctx.Err(); err != nil {
		return result, err
	}
	err = s.commitReadBatch(vacancies, applications, conversations, &result, ctx)
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
func (s *HHReadSyncService) commitMutex() *sync.Mutex {
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
	stage := NewHHReadSyncService(s.client, s.statePath)
	stage.analyzer = s.analyzer
	stage.candidate = s.candidate
	stage.statusMapper = s.statusMapper
	stage.clarifications = s.clarifications
	stage.drafts = s.drafts
	paths := []string{}
	if s.conversations != nil {
		x := *s.conversations
		var err error
		x.conversations, err = cloneKnowledge(x.conversations)
		if err != nil {
			return err
		}
		x.reindex()
		stage.conversations = &x
		paths = append(paths, x.path)
	}
	if s.applications != nil {
		x := *s.applications
		var err error
		x.applications, err = cloneKnowledge(x.applications)
		if err != nil {
			return err
		}
		x.events, err = cloneKnowledge(x.events)
		if err != nil {
			return err
		}
		stage.applications = &x
		x.conversationStore = stage.conversations
		paths = append(paths, x.path)
	}
	if s.vacancies != nil {
		x := *s.vacancies
		var err error
		x.vacancies, err = cloneKnowledge(x.vacancies)
		if err != nil {
			return err
		}
		stage.vacancies = &x
		paths = append(paths, x.path)
	}
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
			stage.analyzer = prepared
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
		apply := func(outcome syncOutcome, err error) {
			if err != nil {
				result.Skipped++
				result.Errors = append(result.Errors, safeSyncError(err))
				return
			}
			switch outcome {
			case syncCreated:
				result.Created++
			case syncUpdated:
				result.Updated++
			case syncUnchanged:
				result.Unchanged++
			default:
				result.Skipped++
			}
		}
		if len(v) > 0 && stage.vacancies == nil || len(a) > 0 && stage.applications == nil || len(c) > 0 && stage.conversations == nil {
			return errors.New("HH sync store unavailable")
		}
		for _, record := range v {
			apply(stage.importVacancy(record))
		}
		for _, record := range a {
			apply(stage.importApplication(record))
			if warning := s.statusMapper.Warning(record.Status); warning != "" {
				result.Warnings = append(result.Warnings, warning)
			}
		}
		for _, record := range c {
			if record.MetadataUnchanged {
				result.Unchanged++
				continue
			}
			apply(stage.importConversation(record))
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
			*s.conversations = *stage.conversations
		}
		if s.applications != nil {
			oldConversation := s.applications.conversationStore
			*s.applications = *stage.applications
			s.applications.conversationStore = oldConversation
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
		c = s.conversations.conversations
	}
	if s.applications != nil {
		a = applicationStoreFile{Version: 1, Applications: s.applications.applications, Events: s.applications.events}
	}
	if s.vacancies != nil {
		v = s.vacancies.vacancies
	}
	hash := func(value any) [32]byte { raw, _ := json.Marshal(value); return sha256.Sum256(raw) }
	return hash(c), hash(a), hash(v)
}

type preparedVacancyMatch struct {
	vacancy Vacancy
	match   MatchResult
}
type preparedVacancyAnalyzer []preparedVacancyMatch

func (a preparedVacancyAnalyzer) Analyze(v Vacancy, _ any) MatchResult {
	for _, item := range a {
		if vacancySourceEquivalent(item.vacancy, v) {
			return item.match
		}
	}
	// The plan is checked against the durable vacancy snapshot before importing.
	// Unplanned input must never acquire a positive recommendation.
	return MatchResult{Recommendation: &ApplicationRecommendation{Decision: RecommendationMaybe, Reason: "Vacancy changed while preparing analysis; review required"}}
}
func (s *HHReadSyncService) prepareVacancyAnalysis(records []HHVacancyRecord) (preparedVacancyAnalyzer, error) {
	if s.analyzer == nil || s.vacancies == nil {
		return nil, nil
	}
	start := time.Now()
	defer perfRecord("sync.vacancy_analysis", start, len(records))
	prepared := preparedVacancyAnalyzer{}
	for _, record := range records {
		value, err := mapHHVacancy(record)
		if err != nil {
			continue
		}
		old, err := s.vacancies.GetByExternalID(value.ExternalID)
		if err == nil && vacancySourceEquivalent(old, value) {
			continue
		}
		prepared = append(prepared, preparedVacancyMatch{value, s.analyzer.Analyze(value, s.candidate)})
	}
	return prepared, nil
}
