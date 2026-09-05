package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/http"
	"strconv"
	"strings"

	xhtml "golang.org/x/net/html"
)

// VacancyPreflight is the trusted, read-only state collected immediately
// before an application. Known flags are deliberately separate from bool
// values: false and "not observed" have different safety implications.
type VacancyPreflight struct {
	VacancyID             int
	Available             bool
	Archived              bool
	ArchivedKnown         bool
	AlreadyResponded      bool
	AlreadyRespondedKnown bool
	TestPresent           bool
	TestPresentKnown      bool
	LetterRequired        bool
	LetterRequiredKnown   bool
	Area                  string
	AreaKnown             bool
	WorkSchedule          string
	WorkScheduleKnown     bool
	WorkExperience        string
	WorkExperienceKnown   bool
	CanApply              bool
	CanApplyKnown         bool
	ResponseURL           string
}

type VacancyPreflightResult struct {
	Type                  string `json:"type"`
	VacancyID             int    `json:"vacancy_id"`
	ResponseURL           string `json:"response_url,omitempty"`
	Archived              *bool  `json:"archived"`
	ArchivedKnown         bool   `json:"archived_known"`
	AlreadyResponded      *bool  `json:"already_responded"`
	AlreadyRespondedKnown bool   `json:"already_responded_known"`
	TestPresent           *bool  `json:"test_present"`
	TestPresentKnown      bool   `json:"test_present_known"`
	LetterRequired        *bool  `json:"letter_required"`
	LetterRequiredKnown   bool   `json:"letter_required_known"`
	CanApply              *bool  `json:"can_apply"`
	CanApplyKnown         bool   `json:"can_apply_known"`
	Area                  string `json:"area,omitempty"`
	WorkSchedule          string `json:"work_schedule,omitempty"`
	WorkExperience        string `json:"work_experience,omitempty"`
}

func (p VacancyPreflight) event() VacancyPreflightResult {
	return VacancyPreflightResult{
		Type:                  "vacancy_preflight",
		VacancyID:             p.VacancyID,
		ResponseURL:           p.ResponseURL,
		Archived:              knownBoolPointer(p.Archived, p.ArchivedKnown),
		ArchivedKnown:         p.ArchivedKnown,
		AlreadyResponded:      knownBoolPointer(p.AlreadyResponded, p.AlreadyRespondedKnown),
		AlreadyRespondedKnown: p.AlreadyRespondedKnown,
		TestPresent:           knownBoolPointer(p.TestPresent, p.TestPresentKnown),
		TestPresentKnown:      p.TestPresentKnown,
		LetterRequired:        knownBoolPointer(p.LetterRequired, p.LetterRequiredKnown),
		LetterRequiredKnown:   p.LetterRequiredKnown,
		CanApply:              knownBoolPointer(p.CanApply, p.CanApplyKnown),
		CanApplyKnown:         p.CanApplyKnown,
		Area:                  p.Area,
		WorkSchedule:          p.WorkSchedule,
		WorkExperience:        p.WorkExperience,
	}
}

func knownBoolPointer(value, known bool) *bool {
	if !known {
		return nil
	}
	return &value
}

func (r *HHAIResponder) GetVacancyPreflight(vacancy Vacancy) (VacancyPreflight, error) {
	if err := r.ctx.Err(); err != nil {
		return VacancyPreflight{}, err
	}
	responseURL := r.ResolveURL(fmt.Sprintf("/applicant/vacancy_response?vacancyId=%d&startedWithQuestion=false&hhtmFrom=vacancy", vacancy.ID))
	req, err := r.buildRequest(http.MethodGet, responseURL, nil, nil)
	if err != nil {
		return VacancyPreflight{}, err
	}
	resp, err := r.requester.Do(req)
	if err != nil {
		return VacancyPreflight{}, err
	}
	if resp.Status != http.StatusOK {
		return VacancyPreflight{}, unexpectedHTTPStatus(resp.Status)
	}

	preflight, err := parseVacancyPreflight(resp.Body, vacancy, responseURL)
	if err != nil {
		return VacancyPreflight{}, err
	}
	r.rememberVacancyPreflight(preflight)
	return preflight, nil
}

func (r *HHAIResponder) rememberVacancyPreflight(preflight VacancyPreflight) {
	r.preflightMu.Lock()
	defer r.preflightMu.Unlock()
	if r.preflightCache == nil {
		r.preflightCache = make(map[int]VacancyPreflight)
	}
	r.preflightCache[preflight.VacancyID] = preflight
}

func (r *HHAIResponder) clearVacancyPreflightCache() {
	r.preflightMu.Lock()
	defer r.preflightMu.Unlock()
	r.preflightCache = nil
}

func (r *HHAIResponder) cachedVacancyPreflight(vacancyID int) (VacancyPreflight, bool) {
	r.preflightMu.Lock()
	defer r.preflightMu.Unlock()
	preflight, ok := r.preflightCache[vacancyID]
	return preflight, ok
}

func (r *HHAIResponder) requireLiveApplicationPreflight(vacancyID int) error {
	preflight, ok := r.cachedVacancyPreflight(vacancyID)
	if !ok {
		var err error
		preflight, err = r.GetVacancyPreflight(Vacancy{ID: vacancyID})
		if err != nil {
			return fmt.Errorf("vacancy preflight failed: %w", err)
		}
	}
	decision, reason := vacancyPreflightDecision(preflight)
	if decision != VacancyMatch {
		return fmt.Errorf("vacancy preflight blocked live application: %s", reason)
	}
	return nil
}

func parseVacancyPreflight(data []byte, vacancy Vacancy, responseURL string) (VacancyPreflight, error) {
	preflight := VacancyPreflight{VacancyID: vacancy.ID, ResponseURL: responseURL}
	state, stateErr := embeddedVacancyState(data)
	if stateErr == nil {
		populateVacancyPreflightFromState(&preflight, state, vacancy.ID)
	}
	populateVacancyPreflightFromHTML(&preflight, data)

	// Search-card structured fields are a safe fallback for descriptive fields
	// only. Critical response state is never inferred from the search card.
	if !preflight.AreaKnown && strings.TrimSpace(vacancy.Area.Name) != "" {
		preflight.Area = strings.TrimSpace(vacancy.Area.Name)
		preflight.AreaKnown = true
	}
	if !preflight.WorkScheduleKnown && strings.TrimSpace(vacancy.WorkSchedule) != "" {
		preflight.WorkSchedule = strings.TrimSpace(vacancy.WorkSchedule)
		preflight.WorkScheduleKnown = true
	}
	if !preflight.WorkExperienceKnown && strings.TrimSpace(vacancy.WorkExperience) != "" {
		preflight.WorkExperience = strings.TrimSpace(vacancy.WorkExperience)
		preflight.WorkExperienceKnown = true
	}
	if !preflight.ArchivedKnown && vacancy.Archived {
		preflight.Archived = true
		preflight.ArchivedKnown = true
	}
	if !preflight.TestPresentKnown && vacancy.UserTestPresent {
		preflight.TestPresent = true
		preflight.TestPresentKnown = true
	}
	if !preflight.LetterRequiredKnown && vacancy.ResponseLetterRequired {
		preflight.LetterRequired = true
		preflight.LetterRequiredKnown = true
	}

	if stateErr != nil && !preflight.hasAnyReliableState() {
		return VacancyPreflight{}, fmt.Errorf("parse vacancy preflight state: %w", stateErr)
	}
	return preflight, nil
}

func (p VacancyPreflight) hasAnyReliableState() bool {
	return p.ArchivedKnown || p.AlreadyRespondedKnown || p.TestPresentKnown ||
		p.LetterRequiredKnown || p.CanApplyKnown || p.AreaKnown ||
		p.WorkScheduleKnown || p.WorkExperienceKnown
}

func embeddedVacancyState(data []byte) (map[string]any, error) {
	text := html.UnescapeString(string(data))
	for _, marker := range []string{`{"redirectConfig":`, `{"vacancyView":`, `{"vacancyResponse":`, `{"vacancyTests":`, `{"vacancy":`, `{"response":`} {
		idx := strings.Index(text, marker)
		if idx < 0 {
			continue
		}
		var state map[string]any
		decoder := json.NewDecoder(strings.NewReader(text[idx:]))
		if err := decoder.Decode(&state); err == nil {
			return state, nil
		}
	}
	trimmed := strings.TrimSpace(text)
	if strings.HasPrefix(trimmed, "{") {
		var state map[string]any
		if err := json.Unmarshal([]byte(trimmed), &state); err == nil {
			return state, nil
		}
	}
	return nil, errors.New("embedded vacancy state not found")
}

func populateVacancyPreflightFromState(preflight *VacancyPreflight, state map[string]any, vacancyID int) {
	if value, ok := findStateValue(state, "archived", "isArchived"); ok {
		if parsed, parsedOK := stateBool(value); parsedOK {
			preflight.Archived, preflight.ArchivedKnown = parsed, true
		}
	}
	if value, ok := findStateValue(state, "alreadyResponded", "responseAlreadySent", "hasResponse", "responseExists", "responded"); ok {
		if parsed, parsedOK := stateBool(value); parsedOK {
			preflight.AlreadyResponded, preflight.AlreadyRespondedKnown = parsed, true
		}
	}
	if value, ok := findStateValue(state, "canApply", "canRespond", "responseAllowed", "isResponseAllowed", "applyAvailable"); ok {
		if parsed, parsedOK := stateBool(value); parsedOK {
			preflight.CanApply, preflight.CanApplyKnown = parsed, true
			preflight.Available = parsed
		}
	}
	if value, ok := findStateValue(state, "available", "isAvailable"); ok {
		if parsed, parsedOK := stateBool(value); parsedOK {
			preflight.Available = parsed
			if !preflight.CanApplyKnown {
				preflight.CanApply, preflight.CanApplyKnown = parsed, true
			}
		}
	}
	if value, ok := findStateValue(state, "responseLetterRequired", "@responseLetterRequired", "letterRequired", "isResponseLetterRequired"); ok {
		if parsed, parsedOK := stateBool(value); parsedOK {
			preflight.LetterRequired, preflight.LetterRequiredKnown = parsed, true
		}
	}
	if value, ok := findStateValue(state, "userTestPresent", "testPresent", "hasTest", "testRequired"); ok {
		if parsed, parsedOK := stateBool(value); parsedOK {
			preflight.TestPresent, preflight.TestPresentKnown = parsed, true
		}
	}
	if value, ok := findStateValue(state, "vacancyTests"); ok {
		preflight.TestPresent, preflight.TestPresentKnown = stateHasVacancyTest(value, vacancyID), true
	}

	preflight.Area, preflight.AreaKnown = stateStringField(state, "area", "areaName", "location")
	preflight.WorkSchedule, preflight.WorkScheduleKnown = stateStringField(state, "workSchedule", "@workSchedule", "workFormat", "workFormats")
	preflight.WorkExperience, preflight.WorkExperienceKnown = stateStringField(state, "workExperience", "experience", "@workExperience")
}

func populateVacancyPreflightFromHTML(preflight *VacancyPreflight, data []byte) {
	document, err := xhtml.Parse(bytes.NewReader(data))
	if err != nil {
		return
	}
	for _, node := range findHTMLNodes(document, func(node *xhtml.Node) bool {
		dataQA := strings.ToLower(htmlAttr(node, "data-qa"))
		return strings.Contains(dataQA, "response") && (node.Data == "button" || node.Data == "a")
	}) {
		ariaDisabled := strings.EqualFold(strings.TrimSpace(htmlAttr(node, "aria-disabled")), "true")
		if hasHTMLAttr(node, "disabled") || ariaDisabled {
			preflight.CanApply = false
			preflight.CanApplyKnown = true
			preflight.Available = false
			break
		}
	}
	text := strings.ToLower(normalizeHTMLText(htmlNodeText(document)))
	if !preflight.AlreadyRespondedKnown && containsAny(text, "вы уже откликались", "отклик уже отправлен", "отклик отправлен") {
		preflight.AlreadyResponded, preflight.AlreadyRespondedKnown = true, true
	}
	if !preflight.ArchivedKnown && containsAny(text, "вакансия в архиве", "вакансия закрыта") {
		preflight.Archived, preflight.ArchivedKnown = true, true
	}
	if !preflight.CanApplyKnown && containsAny(text, "откликнуться", "отправить отклик") {
		preflight.CanApply, preflight.CanApplyKnown = true, true
		preflight.Available = true
	}
}

func hasHTMLAttr(node *xhtml.Node, key string) bool {
	for _, attr := range node.Attr {
		if attr.Key == key {
			return true
		}
	}
	return false
}

func containsAny(text string, values ...string) bool {
	for _, value := range values {
		if strings.Contains(text, value) {
			return true
		}
	}
	return false
}

func findStateValue(value any, keys ...string) (any, bool) {
	wanted := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		wanted[strings.ToLower(key)] = struct{}{}
	}
	var visit func(any) (any, bool)
	visit = func(current any) (any, bool) {
		switch typed := current.(type) {
		case map[string]any:
			for key, item := range typed {
				if _, ok := wanted[strings.ToLower(key)]; ok {
					return item, true
				}
			}
			for _, item := range typed {
				if found, ok := visit(item); ok {
					return found, true
				}
			}
		case []any:
			for _, item := range typed {
				if found, ok := visit(item); ok {
					return found, true
				}
			}
		}
		return nil, false
	}
	return visit(value)
}

func stateBool(value any) (bool, bool) {
	switch typed := value.(type) {
	case bool:
		return typed, true
	case string:
		switch strings.ToLower(strings.TrimSpace(typed)) {
		case "true", "1", "yes", "да":
			return true, true
		case "false", "0", "no", "нет":
			return false, true
		}
	}
	return false, false
}

func stateStringField(state map[string]any, keys ...string) (string, bool) {
	value, ok := findStateValue(state, keys...)
	if !ok {
		return "", false
	}
	if text, ok := value.(string); ok && strings.TrimSpace(text) != "" {
		return strings.TrimSpace(text), true
	}
	if nested, ok := value.(map[string]any); ok {
		for _, key := range []string{"name", "title", "value", "text"} {
			if text, ok := nested[key].(string); ok && strings.TrimSpace(text) != "" {
				return strings.TrimSpace(text), true
			}
		}
	}
	if values, ok := value.([]any); ok {
		var parts []string
		for _, item := range values {
			if text, ok := item.(string); ok && strings.TrimSpace(text) != "" {
				parts = append(parts, strings.TrimSpace(text))
			}
		}
		if len(parts) > 0 {
			return strings.Join(parts, ", "), true
		}
	}
	return "", false
}

func stateHasVacancyTest(value any, vacancyID int) bool {
	switch typed := value.(type) {
	case map[string]any:
		_, exists := typed[strconv.Itoa(vacancyID)]
		return exists
	case []any:
		return len(typed) > 0
	case bool:
		return typed
	default:
		return value != nil
	}
}

func localStructuredHardRequirements(preflight VacancyPreflight, candidate CandidateContext) []HardRequirementEvaluation {
	var result []HardRequirementEvaluation
	workExperienceKnown := preflight.WorkExperienceKnown || strings.TrimSpace(preflight.WorkExperience) != ""
	if workExperienceKnown && strings.TrimSpace(preflight.WorkExperience) != "" && !vacancyDoesNotRequireExperience(preflight.WorkExperience) {
		result = append(result, HardRequirementEvaluation{
			Requirement:     preflight.WorkExperience,
			Category:        hardRequirementCategoryExperienceYears,
			Status:          hardRequirementStatusUnknown,
			VacancyEvidence: preflight.WorkExperience,
		})
	}

	workScheduleKnown := preflight.WorkScheduleKnown || strings.TrimSpace(preflight.WorkSchedule) != ""
	if !workScheduleKnown || isRemoteWorkSchedule(preflight.WorkSchedule) {
		return result
	}
	if !isOnsiteWorkSchedule(preflight.WorkSchedule) && !isHybridWorkSchedule(preflight.WorkSchedule) {
		return result
	}

	requirement := strings.TrimSpace(preflight.Area)
	if requirement == "" {
		requirement = strings.TrimSpace(preflight.WorkSchedule)
	}
	if requirement == "" {
		requirement = "Место работы"
	}
	location := HardRequirementEvaluation{
		Requirement:     requirement,
		Category:        hardRequirementCategoryLocation,
		Status:          hardRequirementStatusUnknown,
		VacancyEvidence: strings.TrimSpace(strings.Join([]string{preflight.WorkSchedule, preflight.Area}, ": ")),
	}
	if location.VacancyEvidence == "" {
		location.VacancyEvidence = requirement
	}
	if isOnsiteWorkSchedule(preflight.WorkSchedule) && strings.TrimSpace(candidate.Location) != "" && strings.TrimSpace(preflight.Area) != "" && locationsMatch(candidate.Location, preflight.Area) {
		location.Status = hardRequirementStatusMet
		location.CandidateEvidence = "Candidate location: " + candidate.Location
	}
	result = append(result, location)
	return result
}

func isRemoteWorkSchedule(value string) bool {
	text := strings.ToLower(strings.TrimSpace(value))
	return containsAny(text, "удалён", "удален", "remote", "дистанцион") && !isHybridWorkSchedule(text)
}

func isOnsiteWorkSchedule(value string) bool {
	text := strings.ToLower(strings.TrimSpace(value))
	return containsAny(text, "офис", "на месте", "в помещении", "onsite", "on-site", "office")
}

func isHybridWorkSchedule(value string) bool {
	text := strings.ToLower(strings.TrimSpace(value))
	return containsAny(text, "гибрид", "hybrid")
}

func mergeHardRequirements(local, ai []HardRequirementEvaluation) []HardRequirementEvaluation {
	result := make([]HardRequirementEvaluation, 0, len(local)+len(ai))
	result = append(result, local...)
	for _, candidate := range ai {
		duplicate := false
		for _, existing := range local {
			if existing.Category == candidate.Category && (containsNormalizedText(existing.Requirement, candidate.Requirement) || containsNormalizedText(existing.VacancyEvidence, candidate.VacancyEvidence)) {
				duplicate = true
				break
			}
		}
		if !duplicate {
			for _, existing := range result {
				if existing.Category == candidate.Category && normalizeEvidenceText(existing.Requirement) == normalizeEvidenceText(candidate.Requirement) {
					duplicate = true
					break
				}
			}
		}
		if !duplicate {
			result = append(result, candidate)
		}
	}
	return result
}

func vacancyPreflightDecision(preflight VacancyPreflight) (VacancyDecision, string) {
	if preflight.ArchivedKnown && preflight.Archived {
		return VacancyReject, "vacancy is archived"
	}
	if !preflight.ArchivedKnown {
		return VacancyReviewRequired, "archived state is unknown"
	}
	if preflight.AlreadyRespondedKnown && preflight.AlreadyResponded {
		return VacancyReject, "already responded according to vacancy detail"
	}
	if !preflight.AlreadyRespondedKnown {
		return VacancyReviewRequired, "already-responded state is unknown"
	}
	if preflight.TestPresentKnown && preflight.TestPresent {
		return VacancyReviewRequired, "vacancy has a test; safe live test flow is not enabled"
	}
	if !preflight.TestPresentKnown {
		return VacancyReviewRequired, "test state is unknown"
	}
	if !preflight.LetterRequiredKnown {
		return VacancyReviewRequired, "cover-letter requirement is unknown"
	}
	if !preflight.CanApplyKnown {
		return VacancyReviewRequired, "application availability is unknown"
	}
	if !preflight.CanApply {
		return VacancyReject, "vacancy does not allow an application"
	}
	return VacancyMatch, ""
}
