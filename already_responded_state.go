package main

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type alreadyRespondedStateFile struct {
	VacancyIDs []int `json:"already_responded"`
}

func (r *HHAIResponder) loadAlreadyRespondedState() {
	r.alreadyRespondedMu.Lock()
	defer r.alreadyRespondedMu.Unlock()

	r.alreadyResponded = make(map[int]struct{})
	path := strings.TrimSpace(r.alreadyRespondedStatePath)
	if path == "" {
		return
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			logger.Warn("Could not load already-responded state: %v", err)
		}
		return
	}

	var state alreadyRespondedStateFile
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	if err := decoder.Decode(&state); err != nil {
		logger.Warn("Ignoring corrupt already-responded state: %v", err)
		return
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			logger.Warn("Ignoring corrupt already-responded state: trailing data")
		} else {
			logger.Warn("Ignoring corrupt already-responded state: %v", err)
		}
		return
	}

	for _, vacancyID := range state.VacancyIDs {
		if vacancyID <= 0 {
			logger.Warn("Ignoring corrupt already-responded state: invalid vacancy_id")
			r.alreadyResponded = make(map[int]struct{})
			return
		}
		r.alreadyResponded[vacancyID] = struct{}{}
	}
}

func (r *HHAIResponder) isAlreadyResponded(vacancyID int) bool {
	r.alreadyRespondedMu.Lock()
	defer r.alreadyRespondedMu.Unlock()
	_, ok := r.alreadyResponded[vacancyID]
	return ok
}

func (r *HHAIResponder) rememberConfirmedAlreadyResponded(vacancyID int) {
	if vacancyID <= 0 || strings.TrimSpace(r.alreadyRespondedStatePath) == "" {
		return
	}

	r.alreadyRespondedMu.Lock()
	defer r.alreadyRespondedMu.Unlock()
	if r.alreadyResponded == nil {
		r.alreadyResponded = make(map[int]struct{})
	}
	if _, exists := r.alreadyResponded[vacancyID]; exists {
		return
	}
	r.alreadyResponded[vacancyID] = struct{}{}

	ids := make([]int, 0, len(r.alreadyResponded))
	for id := range r.alreadyResponded {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	if err := writeAlreadyRespondedState(r.alreadyRespondedStatePath, alreadyRespondedStateFile{VacancyIDs: ids}); err != nil {
		logger.Warn("Could not save already-responded state: %v", err)
	}
}

func (r *HHAIResponder) rememberConfirmedPreflight(preflight VacancyPreflight) {
	if preflight.AlreadyRespondedKnown && preflight.AlreadyResponded {
		r.rememberConfirmedAlreadyResponded(preflight.VacancyID)
	}
}

func writeAlreadyRespondedState(path string, state alreadyRespondedStateFile) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	dir := filepath.Dir(path)
	if dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}

	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return nil
}
