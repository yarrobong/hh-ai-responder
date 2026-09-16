package runtime

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

type dashboardFile struct {
	info        os.FileInfo
	initialized bool
}
type dashboardContextEntry struct {
	key      [32]byte
	warnings []string
}
type dashboardViewEntry struct {
	raw []byte
	at  time.Time
}

// Called only under Dashboard.mu. Canonical store APIs retain their explicit
// Load semantics; cached display data is never passed to the write gateway.
func (s *DashboardServer) cachedConsistency(d CareerSnapshot, only string) CareerSnapshot {
	raw, _ := json.Marshal(s.Knowledge)
	knowledge := sha256.Sum256(raw)
	if s.contextCache == nil {
		s.contextCache = map[string]dashboardContextEntry{}
	}
	d.Consistency = map[string][]string{}
	builder := NewConversationContextBuilder(s.Conversations, s.Resolver)
	for _, c := range d.Conversations {
		if only != "" && c.ID != only {
			continue
		}
		raw, _ := json.Marshal(c)
		key := sha256.Sum256(append(raw, knowledge[:]...))
		entry, ok := s.contextCache[c.ID]
		if !ok || entry.key != key {
			start := time.Now()
			ctx, err := builder.BuildForReply(c.ID)
			entry = dashboardContextEntry{key: key}
			if err != nil {
				entry.warnings = []string{"context_unavailable"}
			} else if len(ctx.ConsistencyWarnings) > 0 {
				entry.warnings = []string{"consistency_warning"}
			}
			if len(s.contextCache) > 512 {
				s.contextCache = map[string]dashboardContextEntry{}
			}
			s.contextCache[c.ID] = entry
			perfRecord("display.context_miss", start, 1)
		} else {
			perfRecord("display.context_hit", time.Now(), 1)
		}
		if len(entry.warnings) > 0 {
			d.Consistency[c.ID] = entry.warnings
		}
	}
	return d
}

func sameDashboardFile(a, b os.FileInfo) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return os.SameFile(a, b) && a.Size() == b.Size() && a.ModTime().Equal(b.ModTime()) && a.Mode() == b.Mode()
}
func (s *DashboardServer) localFiles() map[string]func() error {
	files := map[string]func() error{s.Conversations.path: s.Conversations.Load, s.Applications.path: s.Applications.Load, s.Vacancies.path: s.Vacancies.Load, s.Drafts.path: s.Drafts.Load, s.Clarifications.Path(): s.Clarifications.Load}
	files[s.Sync.statePath] = s.Sync.LoadState
	files[s.Knowledge.ProfilePath] = s.Knowledge.Load
	for _, kind := range []string{"skills", "projects", "achievements", "unknowns", "proposals", "events"} {
		files[filepath.Join(filepath.Dir(s.Knowledge.ProfilePath), "candidate_"+kind+".json")] = s.Knowledge.Load
	}
	if s.WriteGateway != nil {
		files[s.WriteGateway.Actions.path] = s.WriteGateway.Actions.Load
		files[s.WriteGateway.Audit.path] = s.WriteGateway.Audit.Load
	}
	if s.Notifications != nil {
		files[s.Notifications.path] = s.Notifications.Load
	}
	if s.QualityLog != nil && s.QualityLog.path != "" {
		files[s.QualityLog.path] = s.QualityLog.Load
	}
	if s.DailyRefreshStatePath != "" {
		files[s.DailyRefreshStatePath] = func() error {
			state, err := loadDailyRefreshState(s.DailyRefreshStatePath)
			if err == nil {
				s.DailyRefreshState = state
			}
			return err
		}
	}
	return files
}
func (s *DashboardServer) refreshLocalFiles() error {
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()
	start := time.Now()
	defer perfRecord("dashboard.refresh_local_files", start, 1)
	if s.files == nil {
		s.files = map[string]dashboardFile{}
	}
	next := map[string]dashboardFile{}
	loads := map[string]func() error{}
	for path, load := range s.localFiles() {
		if path == "" {
			continue
		}
		statStart := time.Now()
		info, err := os.Stat(path)
		perfRecord("dashboard.refresh_local_files.stat", statStart, 1)
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		prior := s.files[path]
		next[path] = dashboardFile{info: info, initialized: true}
		if prior.initialized && !sameDashboardFile(prior.info, info) {
			key := path
			if path == s.Knowledge.ProfilePath {
				key = "knowledge"
			} else {
				for _, kind := range []string{"skills", "projects", "achievements", "unknowns", "proposals", "events"} {
					if path == filepath.Join(filepath.Dir(s.Knowledge.ProfilePath), "candidate_"+kind+".json") {
						key = "knowledge"
					}
				}
			}
			loads[key] = load
		}
	}
	if len(loads) > 0 {
		s.cacheMu.Lock()
		s.viewCache = nil
		s.cacheMu.Unlock()
		for _, load := range loads {
			loadStart := time.Now()
			err := load()
			perfRecord("dashboard.refresh_local_files.reload", loadStart, 1)
			if err != nil {
				return err
			}
		}
		s.cacheMu.Lock()
		s.generation++
		s.cacheMu.Unlock()
	}
	// Record the pre-load stamps. Replacement during loading is detected next time.
	s.files = next
	return nil
}

// refreshForRead keeps file-change detection separate from the shared read
// lock. A reader only takes the exclusive dashboard lock when a reload is
// actually needed; concurrent readers therefore do not queue behind another
// reader's handler work.
func (s *DashboardServer) refreshForRead() error {
	if !s.localFilesNeedReload() {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.refreshLocalFiles()
}

func (s *DashboardServer) localFilesNeedReload() bool {
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()
	if s.files == nil {
		return true
	}
	for path := range s.localFiles() {
		if path == "" {
			continue
		}
		info, err := os.Stat(path)
		if err != nil && !os.IsNotExist(err) {
			return true
		}
		prior := s.files[path]
		if !prior.initialized || !sameDashboardFile(prior.info, info) {
			return true
		}
	}
	return false
}

func (s *DashboardServer) invalidateViews() {
	s.cacheMu.Lock()
	s.viewCache = nil
	s.generation++
	s.cacheMu.Unlock()
}
func (s *DashboardServer) serveCachedAPI(w http.ResponseWriter, r *http.Request, p []string) {
	if p[0] == "generation" {
		s.cacheMu.RLock()
		generation := s.generation
		s.cacheMu.RUnlock()
		dashboardJSON(w, 200, map[string]any{"generation": fmt.Sprintf("%d:%d", generation, time.Now().Unix()/60)})
		return
	}
	if p[0] == "performance" {
		dashboardJSON(w, 200, perfSnapshot())
		return
	}
	key := r.URL.RequestURI()
	// Action cards and diagnostics are always recomputed; permissions never come
	// from this cache. The short TTL also handles time-dependent display fields.
	cacheable := p[0] != "actions" && p[0] != "conversations" && p[0] != "diagnostics" && p[0] != "notifications" && p[0] != "health"
	s.cacheMu.RLock()
	entry, ok := s.viewCache[key]
	s.cacheMu.RUnlock()
	if cacheable && ok && time.Since(entry.at) < 15*time.Second {
		perfRecord("display.page_hit", time.Now(), 1)
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Write(entry.raw)
		return
	}
	capture := &dashboardResponseCapture{ResponseWriter: w}
	s.readAPI(capture, r, p)
	if cacheable && capture.status == http.StatusOK {
		s.cacheMu.Lock()
		if s.viewCache == nil || len(s.viewCache) > 64 {
			s.viewCache = map[string]dashboardViewEntry{}
		}
		s.viewCache[key] = dashboardViewEntry{raw: capture.raw, at: time.Now()}
		s.cacheMu.Unlock()
	}
}

type dashboardResponseCapture struct {
	http.ResponseWriter
	status int
	raw    []byte
}

func (w *dashboardResponseCapture) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}
func (w *dashboardResponseCapture) Write(raw []byte) (int, error) {
	w.raw = append(w.raw, raw...)
	return w.ResponseWriter.Write(raw)
}
