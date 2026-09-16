package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/http"
	"strconv"
	"strings"

	xhtml "golang.org/x/net/html"
	"hh-ai-responder/internal/usecase/hhwritepreflight"
)

// VacancyPreflight is the trusted, read-only state collected immediately
// before an application. Known flags are deliberately separate from bool
// values: false and "not observed" have different safety implications.
type VacancyPreflight struct {
	VacancyID                    int
	Available                    bool
	Archived                     bool
	ArchivedKnown                bool
	AlreadyResponded             bool
	AlreadyRespondedKnown        bool
	AlreadyRespondedEvidence     AlreadyRespondedEvidence
	TestPresent                  bool
	TestPresentKnown             bool
	LetterRequired               bool
	LetterRequiredKnown          bool
	Area                         string
	AreaKnown                    bool
	WorkSchedule                 string
	WorkScheduleKnown            bool
	WorkExperience               string
	WorkExperienceKnown          bool
	CanApply                     bool
	CanApplyKnown                bool
	ResponseURL                  string
	ResponseIdentifierPresent    bool
	NegotiationIdentifierPresent bool
}

// AlreadyRespondedValue is the bounded, provider-evidence-backed state used
// for the response decision. The legacy bool/known pair remains in the public
// compatibility projection, but it must not be used to manufacture YES.
type AlreadyRespondedValue string

const (
	AlreadyRespondedYes     AlreadyRespondedValue = "YES"
	AlreadyRespondedNo      AlreadyRespondedValue = "NO"
	AlreadyRespondedUnknown AlreadyRespondedValue = "UNKNOWN"
)

type AlreadyRespondedEvidenceCode string

const (
	EvidenceExplicitRespondedMarker AlreadyRespondedEvidenceCode = "EXPLICIT_RESPONDED_MARKER"
	EvidenceNegotiationIDFound      AlreadyRespondedEvidenceCode = "NEGOTIATION_ID_FOUND"
	EvidenceApplicationHistoryMatch AlreadyRespondedEvidenceCode = "APPLICATION_HISTORY_MATCH"
	EvidenceApplyActionAvailable    AlreadyRespondedEvidenceCode = "APPLY_ACTION_AVAILABLE"
	EvidenceExplicitNotResponded    AlreadyRespondedEvidenceCode = "EXPLICIT_NOT_RESPONDED"
	EvidenceAmbiguousPage           AlreadyRespondedEvidenceCode = "AMBIGUOUS_PAGE"
	EvidenceAuthFailure             AlreadyRespondedEvidenceCode = "AUTH_FAILURE"
)

// AlreadyRespondedEvidence is deliberately bounded: it records only the
// classification and the name of the provider signal, never raw HTML,
// cookies, or a private response body.
type AlreadyRespondedEvidence struct {
	Value        AlreadyRespondedValue        `json:"value"`
	EvidenceCode AlreadyRespondedEvidenceCode `json:"evidence_code"`
}

type VacancyPreflightResult struct {
	Type                         string                   `json:"type"`
	VacancyID                    int                      `json:"vacancy_id"`
	ResponseURL                  string                   `json:"response_url,omitempty"`
	Archived                     *bool                    `json:"archived"`
	ArchivedKnown                bool                     `json:"archived_known"`
	AlreadyResponded             *bool                    `json:"already_responded"`
	AlreadyRespondedKnown        bool                     `json:"already_responded_known"`
	AlreadyRespondedValue        string                   `json:"already_responded_value"`
	AlreadyRespondedEvidenceCode string                   `json:"already_responded_evidence_code"`
	AlreadyRespondedEvidence     AlreadyRespondedEvidence `json:"already_responded_evidence"`
	TestPresent                  *bool                    `json:"test_present"`
	TestPresentKnown             bool                     `json:"test_present_known"`
	LetterRequired               *bool                    `json:"letter_required"`
	LetterRequiredKnown          bool                     `json:"letter_required_known"`
	CanApply                     *bool                    `json:"can_apply"`
	CanApplyKnown                bool                     `json:"can_apply_known"`
	Area                         string                   `json:"area,omitempty"`
	WorkSchedule                 string                   `json:"work_schedule,omitempty"`
	WorkExperience               string                   `json:"work_experience,omitempty"`
}

func (p VacancyPreflight) event() VacancyPreflightResult {
	evidence := p.alreadyRespondedEvidence()
	return VacancyPreflightResult{
		Type:                         "vacancy_preflight",
		VacancyID:                    p.VacancyID,
		ResponseURL:                  p.ResponseURL,
		Archived:                     knownBoolPointer(p.Archived, p.ArchivedKnown),
		ArchivedKnown:                p.ArchivedKnown,
		AlreadyResponded:             knownBoolPointer(evidence.Value == AlreadyRespondedYes, evidence.Value != AlreadyRespondedUnknown),
		AlreadyRespondedKnown:        evidence.Value != AlreadyRespondedUnknown,
		AlreadyRespondedValue:        string(evidence.Value),
		AlreadyRespondedEvidenceCode: string(evidence.EvidenceCode),
		AlreadyRespondedEvidence:     evidence,
		TestPresent:                  knownBoolPointer(p.TestPresent, p.TestPresentKnown),
		TestPresentKnown:             p.TestPresentKnown,
		LetterRequired:               knownBoolPointer(p.LetterRequired, p.LetterRequiredKnown),
		LetterRequiredKnown:          p.LetterRequiredKnown,
		CanApply:                     knownBoolPointer(p.CanApply, p.CanApplyKnown),
		CanApplyKnown:                p.CanApplyKnown,
		Area:                         p.Area,
		WorkSchedule:                 p.WorkSchedule,
		WorkExperience:               p.WorkExperience,
	}
}

func (p VacancyPreflight) alreadyRespondedEvidence() AlreadyRespondedEvidence {
	if (p.AlreadyRespondedEvidence.Value == AlreadyRespondedYes || p.AlreadyRespondedEvidence.Value == AlreadyRespondedNo || p.AlreadyRespondedEvidence.Value == AlreadyRespondedUnknown) && p.AlreadyRespondedEvidence.EvidenceCode != "" {
		return p.AlreadyRespondedEvidence
	}
	return AlreadyRespondedEvidence{Value: AlreadyRespondedUnknown, EvidenceCode: EvidenceAmbiguousPage}
}

func setAlreadyRespondedEvidence(preflight *VacancyPreflight, value AlreadyRespondedValue, code AlreadyRespondedEvidenceCode) {
	preflight.AlreadyRespondedEvidence = AlreadyRespondedEvidence{Value: value, EvidenceCode: code}
	preflight.AlreadyRespondedKnown = value != AlreadyRespondedUnknown
	preflight.AlreadyResponded = value == AlreadyRespondedYes
}

// applyApplicationHistoryEvidence is an independent read-only reconciliation
// step. It is intentionally separate from the vacancy response-page parser:
// a negotiation is positive only when its provider vacancy identity matches
// the requested vacancy and it carries a provider identifier.
func applyApplicationHistoryEvidence(preflight *VacancyPreflight, applications []HHApplicationRecord) {
	if preflight == nil || preflight.VacancyID <= 0 {
		return
	}
	for _, application := range applications {
		if application.VacancyID != preflight.VacancyID || strings.TrimSpace(application.ExternalID) == "" {
			continue
		}
		setAlreadyRespondedEvidence(preflight, AlreadyRespondedYes, EvidenceNegotiationIDFound)
		return
	}
}

func knownBoolPointer(value, known bool) *bool {
	if !known {
		return nil
	}
	return &value
}

func (r *HHAIResponder) GetVacancyPreflight(vacancy Vacancy) (VacancyPreflight, error) {
	return r.getVacancyPreflightContext(r.ctx, vacancy)
}

func (r *HHAIResponder) getVacancyPreflightContext(ctx context.Context, vacancy Vacancy) (VacancyPreflight, error) {
	if ctx == nil {
		return VacancyPreflight{}, errors.New("vacancy preflight context is nil")
	}
	if err := ctx.Err(); err != nil {
		return VacancyPreflight{}, err
	}
	responseURL := r.ResolveURL(fmt.Sprintf("/applicant/vacancy_response?vacancyId=%d&startedWithQuestion=false&hhtmFrom=vacancy", vacancy.ID))
	req, err := r.buildRequest(http.MethodGet, responseURL, nil, nil)
	if err != nil {
		return VacancyPreflight{}, err
	}
	resp, err := r.requester.Do(req.WithContext(ctx))
	if err != nil {
		return VacancyPreflight{}, err
	}
	if resp.Status != http.StatusOK {
		if resp.Status == http.StatusUnauthorized || resp.Status == http.StatusForbidden {
			return VacancyPreflight{VacancyID: vacancy.ID, ResponseURL: responseURL, AlreadyRespondedEvidence: AlreadyRespondedEvidence{Value: AlreadyRespondedUnknown, EvidenceCode: EvidenceAuthFailure}}, nil
		}
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
	preflight := VacancyPreflight{VacancyID: vacancy.ID, ResponseURL: responseURL, ResponseIdentifierPresent: strings.TrimSpace(responseURL) != ""}
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
	// UserTestPresent from a search/detail projection is not sufficient for
	// this fresh web preflight. Only a vacancy-scoped response/form marker may
	// establish a mandatory test.
	if !preflight.LetterRequiredKnown && vacancy.ResponseLetterRequired {
		preflight.LetterRequired = true
		preflight.LetterRequiredKnown = true
	}

	if preflight.AlreadyRespondedEvidence.EvidenceCode == "" {
		if looksLikeAuthFailure(data) {
			setAlreadyRespondedEvidence(&preflight, AlreadyRespondedUnknown, EvidenceAuthFailure)
		} else {
			setAlreadyRespondedEvidence(&preflight, AlreadyRespondedUnknown, EvidenceAmbiguousPage)
		}
	}
	if preflight.AlreadyRespondedEvidence.EvidenceCode == "" {
		setAlreadyRespondedEvidence(&preflight, AlreadyRespondedUnknown, EvidenceAmbiguousPage)
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
	responseState, responseStateOK := directResponseState(state, vacancyID)
	archivedValue, archivedOK := directStateValue(state, "archived", "isArchived")
	if !archivedOK {
		archivedValue, archivedOK = directResponseStateValue(responseState, responseStateOK, "archived", "isArchived")
	}
	if value, ok := archivedValue, archivedOK; ok {
		if parsed, parsedOK := stateBool(value); parsedOK {
			preflight.Archived, preflight.ArchivedKnown = parsed, true
		}
	}
	// Only explicit response-state fields from the provider's response-state
	// projection are authoritative. Do not recursively interpret generic
	// response-shaped fields from unrelated page models.
	if value, ok := directResponseStateValue(responseState, responseStateOK, "alreadyResponded", "responseAlreadySent"); ok {
		if parsed, parsedOK := stateBool(value); parsedOK {
			if parsed {
				setAlreadyRespondedEvidence(preflight, AlreadyRespondedYes, EvidenceExplicitRespondedMarker)
			} else {
				setAlreadyRespondedEvidence(preflight, AlreadyRespondedNo, EvidenceExplicitNotResponded)
			}
		}
	}
	if _, ok := directResponseStateValue(responseState, responseStateOK, "responseId", "response_id", "negotiationId", "negotiation_id"); ok {
		preflight.NegotiationIdentifierPresent = true
	}
	if value, ok := directResponseStateValue(responseState, responseStateOK, "canApply", "canRespond", "responseAllowed", "isResponseAllowed", "applyAvailable"); ok {
		if parsed, parsedOK := stateBool(value); parsedOK {
			preflight.CanApply, preflight.CanApplyKnown = parsed, true
			preflight.Available = parsed
		}
	}
	if value, ok := directResponseStateValue(responseState, responseStateOK, "available", "isAvailable"); ok {
		if parsed, parsedOK := stateBool(value); parsedOK {
			preflight.Available = parsed
			if !preflight.CanApplyKnown {
				preflight.CanApply, preflight.CanApplyKnown = parsed, true
			}
		}
	}
	if value, ok := directResponseStateValue(responseState, responseStateOK, "responseLetterRequired", "@responseLetterRequired", "letterRequired", "isResponseLetterRequired"); ok {
		if parsed, parsedOK := stateBool(value); parsedOK {
			preflight.LetterRequired, preflight.LetterRequiredKnown = parsed, true
		}
	}
	if value, ok := directResponseStateValue(responseState, responseStateOK, "userTestPresent", "testPresent", "hasTest", "testRequired"); ok {
		if parsed, parsedOK := stateBool(value); parsedOK {
			preflight.TestPresent, preflight.TestPresentKnown = parsed, true
		}
	}
	if value, ok := directResponseStateValue(responseState, responseStateOK, "vacancyTests"); ok {
		preflight.TestPresent, preflight.TestPresentKnown = stateHasVacancyTest(value, vacancyID)
	}

	preflight.Area, preflight.AreaKnown = directStateStringField(responseState, responseStateOK, "area", "areaName", "location")
	preflight.WorkSchedule, preflight.WorkScheduleKnown = directStateStringField(responseState, responseStateOK, "workSchedule", "@workSchedule", "workFormat", "workFormats")
	preflight.WorkExperience, preflight.WorkExperienceKnown = directStateStringField(responseState, responseStateOK, "workExperience", "experience", "@workExperience")
	if preflight.AlreadyRespondedEvidence.EvidenceCode == "" && preflight.CanApplyKnown && preflight.CanApply {
		setAlreadyRespondedEvidence(preflight, AlreadyRespondedNo, EvidenceApplyActionAvailable)
	}
}

func populateVacancyPreflightFromHTML(preflight *VacancyPreflight, data []byte) {
	inspection := inspectHHWebPage(data, preflight.VacancyID, webTraceResponse)
	if inspection.DisabledApplyAction && preflight.CanApplyKnown {
		// A disabled control is not proof of a provider-level NO. In
		// particular, it can be a disabled generic component on a response
		// page, so it invalidates a contradictory positive signal instead.
		preflight.CanApply, preflight.CanApplyKnown, preflight.Available = false, false, false
		if preflight.AlreadyRespondedEvidence.EvidenceCode == EvidenceApplyActionAvailable {
			setAlreadyRespondedEvidence(preflight, AlreadyRespondedUnknown, EvidenceAmbiguousPage)
		}
	}
	if preflight.AlreadyRespondedEvidence.EvidenceCode == "" && inspection.ExplicitRespondedMarker {
		setAlreadyRespondedEvidence(preflight, AlreadyRespondedYes, EvidenceExplicitRespondedMarker)
	}
	if !preflight.ArchivedKnown && inspection.Class == WebPageVacancyArchived {
		preflight.Archived, preflight.ArchivedKnown = true, true
	}
	if inspection.ApplicationFormPresent && inspection.FormVacancyIDMatches {
		preflight.CanApply, preflight.CanApplyKnown = true, true
		preflight.Available = true
		setAlreadyRespondedEvidence(preflight, AlreadyRespondedNo, EvidenceApplyActionAvailable)
	} else if inspection.ExplicitApplyAction {
		preflight.CanApply, preflight.CanApplyKnown = true, true
		preflight.Available = true
		if preflight.AlreadyRespondedEvidence.EvidenceCode == "" {
			setAlreadyRespondedEvidence(preflight, AlreadyRespondedNo, EvidenceApplyActionAvailable)
		}
	}
}

func directResponseState(state map[string]any, vacancyID int) (map[string]any, bool) {
	for _, key := range []string{"redirectConfig", "vacancyResponse", "response"} {
		value, ok := directStateValue(state, key)
		if !ok {
			continue
		}
		object, ok := value.(map[string]any)
		if !ok || !responseStateMatchesVacancy(object, vacancyID) {
			continue
		}
		return object, true
	}
	// A bare JSON object is not a response-state container. Treating its
	// arbitrary nested component state as provider response state was the
	// source of the old generic-recursion false positives.
	return nil, false
}

func directResponseStateValue(state map[string]any, stateOK bool, keys ...string) (any, bool) {
	if !stateOK {
		return nil, false
	}
	return directStateValue(state, keys...)
}

func directStateStringField(state map[string]any, stateOK bool, keys ...string) (string, bool) {
	if !stateOK {
		return "", false
	}
	value, ok := directStateValue(state, keys...)
	if !ok {
		return "", false
	}
	return stateStringValue(value)
}

func responseStateMatchesVacancy(value any, vacancyID int) bool {
	object, ok := value.(map[string]any)
	if !ok || vacancyID <= 0 {
		return true
	}
	for _, key := range []string{"vacancyId", "vacancy_id"} {
		if raw, found := directStateValue(object, key); found {
			if text, ok := raw.(string); ok {
				return parseInt64(text) == int64(vacancyID)
			}
			if number, ok := raw.(float64); ok {
				return int(number) == vacancyID
			}
		}
	}
	return true
}

func directStateValue(state map[string]any, keys ...string) (any, bool) {
	for _, wanted := range keys {
		for key, value := range state {
			if strings.EqualFold(key, wanted) {
				return value, true
			}
		}
	}
	return nil, false
}

func looksLikeAuthFailure(data []byte) bool {
	text := strings.ToLower(string(data))
	return containsAny(text, "/account/login", "supernova-login-wrapper", "forbiddenpage", "captcha-container", "cf-chl-", "cloudflare challenge", "ddos-guard challenge", "access denied")
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

func stateStringValue(value any) (string, bool) {
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

func stateHasVacancyTest(value any, vacancyID int) (bool, bool) {
	switch typed := value.(type) {
	case map[string]any:
		if item, exists := typed[strconv.Itoa(vacancyID)]; exists {
			return item != nil, true
		}
		return false, false
	case []any:
		for _, item := range typed {
			if object, ok := item.(map[string]any); ok && responseStateMatchesVacancy(object, vacancyID) {
				return true, true
			}
		}
		return false, false
	case bool:
		return typed, true
	default:
		return false, false
	}
}

func localStructuredHardRequirements(preflight VacancyPreflight, candidate LegacyCandidateContext) []HardRequirementEvaluation {
	var result []HardRequirementEvaluation
	// HH's structured WorkExperience is factual context and a ranking signal,
	// not a hard requirement. Explicit duration requirements from the vacancy
	// description are derived separately from AI extraction.

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
	// “Гибрид/можно удалённо” has a remote path and therefore does not create
	// a mandatory city blocker. Hybrid without a remote option remains review.
	return containsAny(text, "удалён", "удален", "remote", "дистанцион") && !containsAny(text, "офис", "office", "onsite", "on-site", "на месте")
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
	evidence := preflight.alreadyRespondedEvidence()
	reader := vacancyPreflightStateReader{state: hhwritepreflight.VacancyResponseState{
		VacancyID: preflight.VacancyID, Archived: preflight.Archived, ArchivedKnown: preflight.ArchivedKnown,
		AlreadyResponded: evidence.Value == AlreadyRespondedYes, AlreadyRespondedKnown: evidence.Value != AlreadyRespondedUnknown,
		CanApply: preflight.CanApply, CanApplyKnown: preflight.CanApplyKnown,
		TestPresent: preflight.TestPresent, TestPresentKnown: preflight.TestPresentKnown,
		LetterRequired: preflight.LetterRequired, LetterRequiredKnown: preflight.LetterRequiredKnown,
		ResponseURL: preflight.ResponseURL,
	}}
	result := hhwritepreflight.NewService(hhwritepreflight.Dependencies{Vacancies: reader}).PreflightVacancyResponse(context.Background(), hhwritepreflight.VacancyInput{VacancyID: preflight.VacancyID})
	if result.Passed() {
		return VacancyMatch, ""
	}
	if result.Status == hhwritepreflight.StatusBlocked {
		return VacancyReject, firstPreflightReason(result.Reasons, "vacancy response is blocked")
	}
	return VacancyReviewRequired, firstPreflightReason(result.Reasons, "vacancy response preflight requires review")
}

type vacancyPreflightStateReader struct {
	state hhwritepreflight.VacancyResponseState
}

func (r vacancyPreflightStateReader) ReadVacancyResponseState(context.Context, int) (hhwritepreflight.VacancyResponseState, error) {
	return r.state, nil
}

func firstPreflightReason(values []string, fallback string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return fallback
}
