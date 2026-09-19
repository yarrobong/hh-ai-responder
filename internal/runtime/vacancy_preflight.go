package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	xhtml "golang.org/x/net/html"
	"hh-ai-responder/internal/hhread"
	hhreadport "hh-ai-responder/internal/ports/hhread"
	"hh-ai-responder/internal/usecase/hhwritepreflight"
)

// VacancyPreflight is the trusted, read-only state collected immediately
// before an application. Known flags are deliberately separate from bool
// values: false and "not observed" have different safety implications.
type VacancyPreflight struct {
	VacancyID                        int
	Available                        bool
	Archived                         bool
	ArchivedKnown                    bool
	AlreadyResponded                 bool
	AlreadyRespondedKnown            bool
	AlreadyRespondedEvidence         AlreadyRespondedEvidence
	TestPresent                      bool
	TestPresentKnown                 bool
	LetterRequired                   bool
	LetterRequiredKnown              bool
	LetterAllowed                    bool
	LetterAllowedKnown               bool
	Area                             string
	AreaKnown                        bool
	WorkSchedule                     string
	WorkScheduleKnown                bool
	WorkExperience                   string
	WorkExperienceKnown              bool
	CanApply                         bool
	CanApplyKnown                    bool
	ResponseURL                      string
	ResponseIdentifierPresent        bool
	NegotiationIdentifierPresent     bool
	NegotiationID                    string `json:"-"`
	NegotiationResumeID              string `json:"-"`
	GotResponseRelation              bool
	ExistingNegotiation              bool
	ExistingNegotiationKnown         bool
	SelectedResumeSuitable           bool
	SelectedResumeSuitableKnown      bool
	NegotiationsURLPresent           bool
	SuitableResumesURLPresent        bool
	NegotiationCollectionsDiscovered int
	NegotiationCollectionsChecked    int
	NegotiationPagesChecked          int
	MatchingNegotiation              bool
	NegotiationScanComplete          bool
	ActiveState                      VacancyActiveState
	ActiveEvidence                   []VacancyActiveEvidenceCode
}

type VacancyActiveState string

const (
	VacancyActiveStateActive   VacancyActiveState = "ACTIVE"
	VacancyActiveStateInactive VacancyActiveState = "INACTIVE"
	VacancyActiveStateUnknown  VacancyActiveState = "UNKNOWN"
)

type VacancyActiveEvidenceCode string

const (
	ActiveEvidenceNone                        VacancyActiveEvidenceCode = "NO_ACTIVE_EVIDENCE"
	ActiveEvidenceArchiveMarker               VacancyActiveEvidenceCode = "EXPLICIT_ARCHIVE_MARKER"
	ActiveEvidenceProviderArchivedTrue        VacancyActiveEvidenceCode = "PROVIDER_ARCHIVED_TRUE"
	ActiveEvidenceProviderArchivedFalse       VacancyActiveEvidenceCode = "PROVIDER_ARCHIVED_FALSE"
	ActiveEvidenceVacancyApplyAction          VacancyActiveEvidenceCode = "VACANCY_SCOPED_APPLY_ACTION"
	ActiveEvidenceResponseLink                VacancyActiveEvidenceCode = "VACANCY_SCOPED_RESPONSE_LINK"
	ActiveEvidenceApplicationForm             VacancyActiveEvidenceCode = "VACANCY_SCOPED_APPLICATION_FORM"
	ActiveEvidenceProviderCanApply            VacancyActiveEvidenceCode = "PROVIDER_CAN_APPLY"
	ActiveEvidenceProviderClosedForApplicants VacancyActiveEvidenceCode = "PROVIDER_CLOSED_FOR_APPLICANTS"
	ActiveEvidenceAuthFailure                 VacancyActiveEvidenceCode = "AUTH_FAILURE"
	ActiveEvidenceContradiction               VacancyActiveEvidenceCode = "ACTIVE_INACTIVE_CONTRADICTION"
)

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
	Type                             string                   `json:"type"`
	VacancyID                        int                      `json:"vacancy_id"`
	ResponseURL                      string                   `json:"response_url,omitempty"`
	Archived                         *bool                    `json:"archived"`
	ArchivedKnown                    bool                     `json:"archived_known"`
	AlreadyResponded                 *bool                    `json:"already_responded"`
	AlreadyRespondedKnown            bool                     `json:"already_responded_known"`
	AlreadyRespondedValue            string                   `json:"already_responded_value"`
	AlreadyRespondedEvidenceCode     string                   `json:"already_responded_evidence_code"`
	AlreadyRespondedEvidence         AlreadyRespondedEvidence `json:"already_responded_evidence"`
	TestPresent                      *bool                    `json:"test_present"`
	TestPresentKnown                 bool                     `json:"test_present_known"`
	LetterRequired                   *bool                    `json:"letter_required"`
	LetterRequiredKnown              bool                     `json:"letter_required_known"`
	LetterAllowed                    *bool                    `json:"letter_allowed"`
	LetterAllowedKnown               bool                     `json:"letter_allowed_known"`
	CanApply                         *bool                    `json:"can_apply"`
	CanApplyKnown                    bool                     `json:"can_apply_known"`
	Area                             string                   `json:"area,omitempty"`
	WorkSchedule                     string                   `json:"work_schedule,omitempty"`
	WorkExperience                   string                   `json:"work_experience,omitempty"`
	Active                           string                   `json:"active"`
	ActiveEvidence                   []string                 `json:"active_evidence,omitempty"`
	ExistingNegotiation              *bool                    `json:"existing_negotiation"`
	ExistingNegotiationKnown         bool                     `json:"existing_negotiation_known"`
	SelectedResumeSuitable           *bool                    `json:"selected_resume_suitable"`
	SelectedResumeSuitableKnown      bool                     `json:"selected_resume_suitable_known"`
	NegotiationIDPresent             bool                     `json:"negotiation_id_present"`
	NegotiationResumeIDPresent       bool                     `json:"negotiation_resume_id_present"`
	GotResponseRelation              bool                     `json:"got_response_relation"`
	NegotiationsURLPresent           bool                     `json:"negotiations_url_present"`
	SuitableResumesURLPresent        bool                     `json:"suitable_resumes_url_present"`
	NegotiationCollectionsDiscovered int                      `json:"negotiation_collections_discovered"`
	NegotiationCollectionsChecked    int                      `json:"negotiation_collections_checked"`
	NegotiationPagesChecked          int                      `json:"negotiation_pages_checked"`
	MatchingNegotiation              bool                     `json:"matching_negotiation"`
	NegotiationScanComplete          bool                     `json:"negotiation_scan_complete"`
}

func (p VacancyPreflight) event() VacancyPreflightResult {
	evidence := p.alreadyRespondedEvidence()
	activeEvidence := make([]string, 0, len(p.ActiveEvidence))
	for _, code := range p.ActiveEvidence {
		activeEvidence = append(activeEvidence, string(code))
	}
	return VacancyPreflightResult{
		Type:                             "vacancy_preflight",
		VacancyID:                        p.VacancyID,
		ResponseURL:                      p.ResponseURL,
		Archived:                         knownBoolPointer(p.Archived, p.ArchivedKnown),
		ArchivedKnown:                    p.ArchivedKnown,
		AlreadyResponded:                 knownBoolPointer(evidence.Value == AlreadyRespondedYes, evidence.Value != AlreadyRespondedUnknown),
		AlreadyRespondedKnown:            evidence.Value != AlreadyRespondedUnknown,
		AlreadyRespondedValue:            string(evidence.Value),
		AlreadyRespondedEvidenceCode:     string(evidence.EvidenceCode),
		AlreadyRespondedEvidence:         evidence,
		TestPresent:                      knownBoolPointer(p.TestPresent, p.TestPresentKnown),
		TestPresentKnown:                 p.TestPresentKnown,
		LetterRequired:                   knownBoolPointer(p.LetterRequired, p.LetterRequiredKnown),
		LetterRequiredKnown:              p.LetterRequiredKnown,
		LetterAllowed:                    knownBoolPointer(p.LetterAllowed, p.LetterAllowedKnown),
		LetterAllowedKnown:               p.LetterAllowedKnown,
		CanApply:                         knownBoolPointer(p.CanApply, p.CanApplyKnown),
		CanApplyKnown:                    p.CanApplyKnown,
		Area:                             p.Area,
		WorkSchedule:                     p.WorkSchedule,
		WorkExperience:                   p.WorkExperience,
		Active:                           string(p.activeState()),
		ActiveEvidence:                   activeEvidence,
		ExistingNegotiation:              knownBoolPointer(p.ExistingNegotiation, p.ExistingNegotiationKnown),
		ExistingNegotiationKnown:         p.ExistingNegotiationKnown,
		SelectedResumeSuitable:           knownBoolPointer(p.SelectedResumeSuitable, p.SelectedResumeSuitableKnown),
		SelectedResumeSuitableKnown:      p.SelectedResumeSuitableKnown,
		NegotiationIDPresent:             strings.TrimSpace(p.NegotiationID) != "",
		NegotiationResumeIDPresent:       strings.TrimSpace(p.NegotiationResumeID) != "",
		GotResponseRelation:              p.GotResponseRelation,
		NegotiationsURLPresent:           p.NegotiationsURLPresent,
		SuitableResumesURLPresent:        p.SuitableResumesURLPresent,
		NegotiationCollectionsDiscovered: p.NegotiationCollectionsDiscovered,
		NegotiationCollectionsChecked:    p.NegotiationCollectionsChecked,
		NegotiationPagesChecked:          p.NegotiationPagesChecked,
		MatchingNegotiation:              p.MatchingNegotiation,
		NegotiationScanComplete:          p.NegotiationScanComplete,
	}
}

func (p VacancyPreflight) activeState() VacancyActiveState {
	if p.ActiveState == VacancyActiveStateActive || p.ActiveState == VacancyActiveStateInactive {
		return p.ActiveState
	}
	return VacancyActiveStateUnknown
}

func appendActiveEvidence(preflight *VacancyPreflight, code VacancyActiveEvidenceCode) {
	if preflight == nil || code == "" {
		return
	}
	for _, existing := range preflight.ActiveEvidence {
		if existing == code {
			return
		}
	}
	preflight.ActiveEvidence = append(preflight.ActiveEvidence, code)
}

func containsActiveEvidence(preflight *VacancyPreflight, code VacancyActiveEvidenceCode) bool {
	if preflight == nil {
		return false
	}
	for _, existing := range preflight.ActiveEvidence {
		if existing == code {
			return true
		}
	}
	return false
}

func setActiveEvidence(preflight *VacancyPreflight, state VacancyActiveState, code VacancyActiveEvidenceCode) {
	if preflight == nil {
		return
	}
	if preflight.ActiveState == "" {
		preflight.ActiveState = VacancyActiveStateUnknown
	}
	if state == VacancyActiveStateUnknown {
		appendActiveEvidence(preflight, code)
		return
	}
	if len(preflight.ActiveEvidence) > 0 {
		filtered := preflight.ActiveEvidence[:0]
		for _, existing := range preflight.ActiveEvidence {
			if existing != ActiveEvidenceNone {
				filtered = append(filtered, existing)
			}
		}
		preflight.ActiveEvidence = filtered
	}
	if preflight.ActiveState == VacancyActiveStateUnknown {
		if containsActiveEvidence(preflight, ActiveEvidenceContradiction) {
			return
		}
		preflight.ActiveState = state
	} else if preflight.ActiveState != state {
		preflight.ActiveState = VacancyActiveStateUnknown
		appendActiveEvidence(preflight, ActiveEvidenceContradiction)
		appendActiveEvidence(preflight, code)
		return
	}
	appendActiveEvidence(preflight, code)
	if state == VacancyActiveStateActive {
		if preflight.ArchivedKnown && preflight.Archived {
			preflight.ActiveState = VacancyActiveStateUnknown
			appendActiveEvidence(preflight, ActiveEvidenceContradiction)
			return
		}
		preflight.Archived, preflight.ArchivedKnown = false, true
	} else if state == VacancyActiveStateInactive {
		if preflight.ArchivedKnown && !preflight.Archived {
			preflight.ActiveState = VacancyActiveStateUnknown
			appendActiveEvidence(preflight, ActiveEvidenceContradiction)
			return
		}
		preflight.Archived, preflight.ArchivedKnown = true, true
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
	if r != nil && r.transport == transportAPI {
		return r.getAPIVacancyPreflightContext(ctx, vacancy)
	}
	responseURL := r.ResolveURL(fmt.Sprintf("/applicant/vacancy_response?vacancyId=%d&startedWithQuestion=false&hhtmFrom=vacancy", vacancy.ID))
	if r.browserSource != nil && r.baseURL != nil && isHHHost(r.baseURL.Hostname()) {
		vacancyURL := r.ResolveURL(fmt.Sprintf("/vacancy/%d", vacancy.ID))
		vacancyState, browserErr := r.browserSource.GetPage(ctx, vacancyURL)
		if browserErr != nil {
			return VacancyPreflight{}, browserErr
		}
		vacancyFinalURL := strings.ToLower(vacancyState.FinalURL)
		if vacancyState.Challenge || strings.Contains(vacancyFinalURL, "/account/captcha") || strings.Contains(vacancyFinalURL, "/account/login") || !vacancyState.Authenticated {
			return VacancyPreflight{VacancyID: vacancy.ID, ResponseURL: responseURL, ActiveState: VacancyActiveStateUnknown, ActiveEvidence: []VacancyActiveEvidenceCode{ActiveEvidenceAuthFailure}, AlreadyRespondedEvidence: AlreadyRespondedEvidence{Value: AlreadyRespondedUnknown, EvidenceCode: EvidenceAuthFailure}}, nil
		}
		state, browserErr := r.browserSource.GetPage(ctx, responseURL)
		if browserErr != nil {
			return VacancyPreflight{}, browserErr
		}
		finalURL := strings.ToLower(state.FinalURL)
		if state.Challenge || strings.Contains(finalURL, "/account/captcha") || strings.Contains(finalURL, "/account/login") || !state.Authenticated {
			return VacancyPreflight{VacancyID: vacancy.ID, ResponseURL: responseURL, ActiveState: VacancyActiveStateUnknown, ActiveEvidence: []VacancyActiveEvidenceCode{ActiveEvidenceAuthFailure}, AlreadyRespondedEvidence: AlreadyRespondedEvidence{Value: AlreadyRespondedUnknown, EvidenceCode: EvidenceAuthFailure}}, nil
		}
		preflight, parseErr := parseVacancyPreflight([]byte(state.HTML), vacancy, responseURL)
		if parseErr != nil {
			return VacancyPreflight{}, parseErr
		}
		activeState, activeEvidence := parseVacancyActiveState([]byte(vacancyState.HTML), vacancy.ID, webTraceVacancy)
		mergeVacancyActiveEvidence(&preflight, activeState, activeEvidence)
		r.rememberVacancyPreflight(preflight)
		return preflight, nil
	}
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

type apiApplicationPreflightSource interface {
	hhreadport.VacancyDetailSource
	ReadSuitableResumeIDs(context.Context, string) ([]string, error)
	ReadNegotiationCollections(context.Context, string) (hhread.NegotiationCollectionIndex, error)
	ReadNegotiationCollection(context.Context, string) (hhread.NegotiationPage, error)
}

func (r *HHAIResponder) getAPIVacancyPreflightContext(ctx context.Context, vacancy Vacancy) (VacancyPreflight, error) {
	if r == nil || r.readSource == nil {
		return VacancyPreflight{}, &TransportError{Code: transportNotImplemented, Reason: "API application preflight read capability is unavailable"}
	}
	source, ok := r.readSource.(apiApplicationPreflightSource)
	if !ok {
		return VacancyPreflight{}, &TransportError{Code: transportNotImplemented, Reason: "API application preflight read capability is unavailable"}
	}
	selectedResumeID := strings.TrimSpace(r.resumeIdentifier)
	if selectedResumeID == "" {
		if current := r.GetCurrentResume(); current != nil {
			selectedResumeID = r.resumeIdentifierForValue(*current)
		}
	}
	preflight, err := apiVacancyPreflightWithSource(ctx, source, vacancy.ID, selectedResumeID)
	if err != nil {
		return VacancyPreflight{}, err
	}
	r.rememberVacancyPreflight(preflight)
	return preflight, nil
}

func apiVacancyPreflightWithSource(ctx context.Context, source apiApplicationPreflightSource, vacancyID int, selectedResumeID string) (VacancyPreflight, error) {
	if source == nil || vacancyID <= 0 {
		return VacancyPreflight{}, errors.New("API application preflight input is invalid")
	}
	record, err := source.ReadVacancyDetail(ctx, vacancyID)
	if err != nil {
		return VacancyPreflight{}, err
	}
	selectedResumeID = strings.TrimSpace(selectedResumeID)
	preflight := apiVacancyPreflight(record, vacancyID)
	if strings.TrimSpace(record.NegotiationsURL) != "" {
		preflight.NegotiationsURLPresent = true
		applyAPINegotiationScan(&preflight, scanAPINegotiations(ctx, source, record.NegotiationsURL, vacancyID, selectedResumeID))
	}
	if strings.TrimSpace(record.SuitableResumesURL) != "" {
		preflight.SuitableResumesURLPresent = true
		if selectedResumeID != "" {
			suitableIDs, suitableErr := source.ReadSuitableResumeIDs(ctx, record.SuitableResumesURL)
			if suitableErr == nil {
				preflight.SelectedResumeSuitableKnown = true
				for _, suitableID := range suitableIDs {
					if strings.TrimSpace(suitableID) == selectedResumeID {
						preflight.SelectedResumeSuitable = true
						break
					}
				}
			}
		}
	}
	return preflight, nil
}

type apiNegotiationScan struct {
	collectionsDiscovered int
	collectionsChecked    int
	pagesChecked          int
	sameVacancy           *hhread.ApplicationRecord
	candidate             *hhread.ApplicationRecord
	matching              *hhread.ApplicationRecord
	complete              bool
}

func scanAPINegotiations(ctx context.Context, source apiApplicationPreflightSource, endpoint string, vacancyID int, selectedResumeID string) apiNegotiationScan {
	result := apiNegotiationScan{}
	index, err := source.ReadNegotiationCollections(ctx, endpoint)
	if err != nil {
		return result
	}
	if index.DirectPage != nil {
		page := *index.DirectPage
		expectedPage := 0
		itemsSeen := 0
		found := 0
		foundKnown := false
		for {
			if page.Page != expectedPage || page.Page < 0 || (page.PagesKnown && (page.Pages < 1 || page.Page >= page.Pages)) {
				return result
			}
			result.pagesChecked++
			if page.FoundKnown {
				if foundKnown && page.Found != found {
					return result
				}
				found, foundKnown = page.Found, true
			}
			itemsSeen += len(page.Items)
			if !inspectAPINegotiationItems(&result, page.Items, vacancyID, selectedResumeID) {
				return result
			}
			if page.NextURL != "" {
				page, err = source.ReadNegotiationCollection(ctx, page.NextURL)
				if err != nil {
					return result
				}
				expectedPage++
				continue
			}
			if !page.Complete {
				return result
			}
			break
		}
		if foundKnown && itemsSeen != found {
			return result
		}
		result.complete = strings.TrimSpace(selectedResumeID) != "" && result.candidate == nil
		return result
	}
	collections := flattenNegotiationCollections(index)
	result.collectionsDiscovered = len(collections)
	if len(collections) == 0 {
		result.complete = strings.TrimSpace(selectedResumeID) != ""
		return result
	}
	for _, collection := range collections {
		if !negotiationCollectionURLForVacancy(collection.URL, vacancyID) {
			return result
		}
		collectionItems := 0
		collectionFound := 0
		collectionFoundKnown := false
		pageEndpoint := collection.URL
		expectedPage := 0
		for {
			page, pageErr := source.ReadNegotiationCollection(ctx, pageEndpoint)
			if pageErr != nil || page.Page != expectedPage || page.Page < 0 || (page.PagesKnown && (page.Pages < 1 || page.Page >= page.Pages)) {
				return result
			}
			result.pagesChecked++
			if page.FoundKnown {
				if collectionFoundKnown && page.Found != collectionFound {
					return result
				}
				collectionFound, collectionFoundKnown = page.Found, true
			}
			collectionItems += len(page.Items)
			if !inspectAPINegotiationItems(&result, page.Items, vacancyID, selectedResumeID) {
				return result
			}
			if page.NextURL != "" {
				pageEndpoint = page.NextURL
				expectedPage++
				continue
			}
			if !page.Complete {
				return result
			}
			break
		}
		if (collection.TotalKnown && collectionItems != collection.Total) || (collectionFoundKnown && collectionItems != collectionFound) {
			return result
		}
		result.collectionsChecked++
	}
	result.complete = strings.TrimSpace(selectedResumeID) != "" && result.candidate == nil
	return result
}

func inspectAPINegotiationItems(result *apiNegotiationScan, items []hhread.ApplicationRecord, vacancyID int, selectedResumeID string) bool {
	if result == nil {
		return false
	}
	for index := range items {
		item := items[index]
		if item.VacancyID != 0 && item.VacancyID != vacancyID {
			continue
		}
		if strings.TrimSpace(item.ExternalID) == "" {
			return false
		}
		if result.sameVacancy == nil {
			sameVacancy := item
			result.sameVacancy = &sameVacancy
		}
		if strings.TrimSpace(selectedResumeID) == "" || strings.TrimSpace(item.ResumeID) == "" {
			if result.candidate == nil {
				candidate := item
				result.candidate = &candidate
			}
			continue
		}
		if strings.TrimSpace(item.ResumeID) == strings.TrimSpace(selectedResumeID) {
			matched := item
			result.matching = &matched
		}
	}
	return true
}

func negotiationCollectionURLForVacancy(endpoint string, vacancyID int) bool {
	parsed, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil || parsed == nil || strings.TrimSpace(parsed.Query().Get("vacancy_id")) == "" {
		return false
	}
	return strings.TrimSpace(parsed.Query().Get("vacancy_id")) == strconv.Itoa(vacancyID)
}

func flattenNegotiationCollections(index hhread.NegotiationCollectionIndex) []hhread.NegotiationCollection {
	result := make([]hhread.NegotiationCollection, 0, len(index.Collections)+len(index.GeneratedCollections))
	seenURLs := make(map[string]struct{})
	var visit func([]hhread.NegotiationCollection)
	visit = func(values []hhread.NegotiationCollection) {
		for _, value := range values {
			url := strings.TrimSpace(value.URL)
			if url == "" {
				result = append(result, value)
			} else if _, seen := seenURLs[url]; !seen {
				seenURLs[url] = struct{}{}
				result = append(result, value)
			}
			visit(value.SubCollections)
		}
	}
	visit(index.Collections)
	visit(index.GeneratedCollections)
	return result
}

func applyAPINegotiationScan(preflight *VacancyPreflight, scan apiNegotiationScan) {
	if preflight == nil {
		return
	}
	preflight.NegotiationCollectionsDiscovered = scan.collectionsDiscovered
	preflight.NegotiationCollectionsChecked = scan.collectionsChecked
	preflight.NegotiationPagesChecked = scan.pagesChecked
	preflight.NegotiationScanComplete = scan.complete
	if scan.sameVacancy != nil {
		preflight.ExistingNegotiation, preflight.ExistingNegotiationKnown = true, true
		preflight.NegotiationID = strings.TrimSpace(scan.sameVacancy.ExternalID)
		preflight.NegotiationResumeID = strings.TrimSpace(scan.sameVacancy.ResumeID)
		preflight.NegotiationIdentifierPresent = preflight.NegotiationID != ""
	}
	if scan.matching == nil {
		if scan.complete && preflight.alreadyRespondedEvidence().Value != AlreadyRespondedYes {
			setAlreadyRespondedEvidence(preflight, AlreadyRespondedNo, EvidenceExplicitNotResponded)
			if scan.sameVacancy == nil {
				preflight.ExistingNegotiation, preflight.ExistingNegotiationKnown = false, true
			}
		}
		return
	}
	preflight.MatchingNegotiation = true
	preflight.ExistingNegotiation, preflight.ExistingNegotiationKnown = true, true
	preflight.NegotiationID = strings.TrimSpace(scan.matching.ExternalID)
	preflight.NegotiationResumeID = strings.TrimSpace(scan.matching.ResumeID)
	preflight.NegotiationIdentifierPresent = preflight.NegotiationID != ""
	if preflight.alreadyRespondedEvidence().Value != AlreadyRespondedYes {
		setAlreadyRespondedEvidence(preflight, AlreadyRespondedYes, EvidenceNegotiationIDFound)
	}
}

func apiVacancyPreflight(record hhread.VacancyRecord, vacancyID int) VacancyPreflight {
	preflight := VacancyPreflight{
		VacancyID: vacancyID, Archived: record.Archived, ArchivedKnown: record.ArchivedKnown,
		TestPresent: record.UserTestPresent, TestPresentKnown: record.UserTestPresentKnown,
		LetterRequired: record.ResponseLetterRequired, LetterRequiredKnown: record.ResponseLetterRequiredKnown,
		Area: record.AreaName, AreaKnown: strings.TrimSpace(record.AreaName) != "",
		WorkSchedule: record.Schedule, WorkScheduleKnown: strings.TrimSpace(record.Schedule) != "",
		WorkExperience: record.Experience, WorkExperienceKnown: strings.TrimSpace(record.Experience) != "",
		ResponseURL: record.ResponseURL, ResponseIdentifierPresent: strings.TrimSpace(record.ResponseURL) != "",
		ActiveState: VacancyActiveStateUnknown,
	}
	if record.AlreadyResponded != nil {
		if *record.AlreadyResponded {
			setAlreadyRespondedEvidence(&preflight, AlreadyRespondedYes, EvidenceExplicitRespondedMarker)
		} else {
			// Applicant duplicate NO is established only by an exhaustive
			// vacancy-scoped negotiation scan below. A false relation alone is
			// not enough to prove absence of a selected-resume negotiation.
			setAlreadyRespondedEvidence(&preflight, AlreadyRespondedUnknown, EvidenceAmbiguousPage)
		}
	} else {
		setAlreadyRespondedEvidence(&preflight, AlreadyRespondedUnknown, EvidenceAmbiguousPage)
	}
	if record.AlreadyRespondedEvidence == "vacancy.relations.got_response" {
		setAlreadyRespondedEvidence(&preflight, AlreadyRespondedYes, EvidenceExplicitRespondedMarker)
		preflight.GotResponseRelation = true
	}
	for _, relation := range record.Relations {
		if strings.TrimSpace(relation) == "got_response" {
			setAlreadyRespondedEvidence(&preflight, AlreadyRespondedYes, EvidenceExplicitRespondedMarker)
			preflight.GotResponseRelation = true
			break
		}
	}
	if record.ClosedForApplicantsKnown && record.ClosedForApplicants {
		preflight.CanApply, preflight.CanApplyKnown = false, true
		setActiveEvidence(&preflight, VacancyActiveStateInactive, ActiveEvidenceProviderClosedForApplicants)
	} else if record.ArchivedKnown && record.Archived {
		setActiveEvidence(&preflight, VacancyActiveStateInactive, ActiveEvidenceProviderArchivedTrue)
	} else if record.QuickResponsesAllowedKnown && record.QuickResponsesAllowed {
		preflight.CanApply, preflight.CanApplyKnown = true, true
		setActiveEvidence(&preflight, VacancyActiveStateActive, ActiveEvidenceProviderCanApply)
	} else if record.ArchivedKnown && !record.Archived {
		setActiveEvidence(&preflight, VacancyActiveStateActive, ActiveEvidenceProviderArchivedFalse)
	}
	preflight.Available = preflight.activeState() == VacancyActiveStateActive
	return preflight
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
	preflight := VacancyPreflight{VacancyID: vacancy.ID, ResponseURL: responseURL, ResponseIdentifierPresent: strings.TrimSpace(responseURL) != "", ActiveState: VacancyActiveStateUnknown}
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
	if len(preflight.ActiveEvidence) == 0 {
		appendActiveEvidence(&preflight, ActiveEvidenceNone)
	}
	return preflight, nil
}

func parseVacancyActiveState(data []byte, vacancyID int, requestKind string) (VacancyActiveState, []VacancyActiveEvidenceCode) {
	preflight := VacancyPreflight{VacancyID: vacancyID, ActiveState: VacancyActiveStateUnknown}
	if looksLikeAuthFailure(data) {
		setActiveEvidence(&preflight, VacancyActiveStateUnknown, ActiveEvidenceAuthFailure)
		return preflight.activeState(), preflight.ActiveEvidence
	}
	inspection := inspectHHWebPage(data, vacancyID, requestKind)
	if inspection.ArchivedMarkerVacancyScoped || inspection.Class == WebPageVacancyArchived {
		setActiveEvidence(&preflight, VacancyActiveStateInactive, ActiveEvidenceArchiveMarker)
	}
	if inspection.ApplicationFormPresent && inspection.FormVacancyIDMatches {
		setActiveEvidence(&preflight, VacancyActiveStateActive, ActiveEvidenceApplicationForm)
	}
	if inspection.ExplicitApplyAction {
		code := ActiveEvidenceResponseLink
		if requestKind == webTraceVacancy {
			code = ActiveEvidenceVacancyApplyAction
		}
		setActiveEvidence(&preflight, VacancyActiveStateActive, code)
	}
	if inspection.StateCanApplyPresent && inspection.StateCanApply {
		setActiveEvidence(&preflight, VacancyActiveStateActive, ActiveEvidenceProviderCanApply)
	}
	if archived, known := providerArchivedValue(data, vacancyID); known {
		if archived {
			setActiveEvidence(&preflight, VacancyActiveStateInactive, ActiveEvidenceProviderArchivedTrue)
		} else {
			setActiveEvidence(&preflight, VacancyActiveStateActive, ActiveEvidenceProviderArchivedFalse)
		}
	}
	if len(preflight.ActiveEvidence) == 0 {
		appendActiveEvidence(&preflight, ActiveEvidenceNone)
	}
	return preflight.activeState(), preflight.ActiveEvidence
}

func providerArchivedValue(data []byte, vacancyID int) (bool, bool) {
	state, err := embeddedVacancyState(data)
	if err != nil {
		return false, false
	}
	candidates := []map[string]any{state}
	for _, key := range []string{"redirectConfig", "vacancyView", "vacancyResponse", "response", "vacancy"} {
		value, ok := directStateValue(state, key)
		if !ok {
			continue
		}
		object, ok := value.(map[string]any)
		if !ok || !responseStateMatchesVacancy(object, vacancyID) {
			continue
		}
		candidates = append(candidates, object)
	}
	for _, candidate := range candidates {
		value, ok := directStateValue(candidate, "archived", "isArchived")
		if !ok {
			continue
		}
		return stateBool(value)
	}
	return false, false
}

func mergeVacancyActiveEvidence(preflight *VacancyPreflight, state VacancyActiveState, evidence []VacancyActiveEvidenceCode) {
	if preflight == nil || state == VacancyActiveStateUnknown && len(evidence) == 0 {
		return
	}
	for _, code := range evidence {
		if code == ActiveEvidenceNone {
			continue
		}
		setActiveEvidence(preflight, state, code)
		if state == VacancyActiveStateActive && (code == ActiveEvidenceVacancyApplyAction || code == ActiveEvidenceResponseLink || code == ActiveEvidenceApplicationForm || code == ActiveEvidenceProviderCanApply) {
			preflight.CanApply, preflight.CanApplyKnown, preflight.Available = true, true, true
			if preflight.AlreadyRespondedEvidence.EvidenceCode == "" || preflight.AlreadyRespondedEvidence.Value == AlreadyRespondedUnknown {
				setAlreadyRespondedEvidence(preflight, AlreadyRespondedNo, EvidenceApplyActionAvailable)
			}
		}
	}
}

func (p VacancyPreflight) hasAnyReliableState() bool {
	return p.ArchivedKnown || p.AlreadyRespondedKnown || p.TestPresentKnown ||
		p.LetterRequiredKnown || p.CanApplyKnown || p.AreaKnown ||
		p.WorkScheduleKnown || p.WorkExperienceKnown
}

func embeddedVacancyState(data []byte) (map[string]any, error) {
	text := html.UnescapeString(string(data))
	for _, marker := range []string{`{"redirectConfig":`, `{"vacancyView":`, `{"vacancyResponse":`, `{"vacancyTests":`, `{"vacancy":`, `{"response":`, `{"applicantVacancyResponseStatuses":`, `{"vacancyResponsePopup":`} {
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
	// Some pages place the response-state key after unrelated root fields, so
	// the exact {"key": marker is absent. Use the nearest containing object
	// only as a fallback after the stable root markers above were exhausted.
	for _, key := range []string{`"applicantVacancyResponseStatuses":`, `"vacancyResponsePopup":`} {
		keyIndex := strings.Index(text, key)
		if keyIndex < 0 {
			continue
		}
		start := strings.LastIndex(text[:keyIndex], "{")
		if start < 0 {
			continue
		}
		var state map[string]any
		decoder := json.NewDecoder(strings.NewReader(text[start:]))
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
			if parsed {
				setActiveEvidence(preflight, VacancyActiveStateInactive, ActiveEvidenceProviderArchivedTrue)
			} else {
				setActiveEvidence(preflight, VacancyActiveStateActive, ActiveEvidenceProviderArchivedFalse)
			}
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
			if parsed {
				setActiveEvidence(preflight, VacancyActiveStateActive, ActiveEvidenceProviderCanApply)
			}
		}
	}
	if value, ok := directResponseStateValue(responseState, responseStateOK, "available", "isAvailable"); ok {
		if parsed, parsedOK := stateBool(value); parsedOK {
			preflight.Available = parsed
			if !preflight.CanApplyKnown {
				preflight.CanApply, preflight.CanApplyKnown = parsed, true
			}
			if parsed {
				setActiveEvidence(preflight, VacancyActiveStateActive, ActiveEvidenceProviderCanApply)
			}
		}
	}
	if value, ok := directResponseStateValue(responseState, responseStateOK, "responseImpossible"); ok {
		if impossible, known := stateBool(value); known {
			preflight.CanApply, preflight.CanApplyKnown, preflight.Available = !impossible, true, !impossible
			if !impossible {
				setActiveEvidence(preflight, VacancyActiveStateActive, ActiveEvidenceProviderCanApply)
			}
		}
	}
	if value, ok := directResponseStateValue(responseState, responseStateOK, "alreadyApplied"); ok {
		if applied, known := stateBool(value); known {
			if applied {
				setAlreadyRespondedEvidence(preflight, AlreadyRespondedYes, EvidenceExplicitRespondedMarker)
			} else {
				setAlreadyRespondedEvidence(preflight, AlreadyRespondedNo, EvidenceExplicitNotResponded)
			}
		}
	}
	if value, ok := directResponseStateValue(responseState, responseStateOK, "closedForApplicants"); ok {
		if closed, known := stateBool(value); known {
			if closed {
				setActiveEvidence(preflight, VacancyActiveStateInactive, ActiveEvidenceProviderClosedForApplicants)
			} else {
				setActiveEvidence(preflight, VacancyActiveStateActive, ActiveEvidenceProviderArchivedFalse)
			}
		}
	}
	if value, ok := directResponseStateValue(responseState, responseStateOK, "letterMaxLength"); ok {
		if maxLength, known := stateNumber(value); known {
			preflight.LetterAllowed, preflight.LetterAllowedKnown = maxLength > 0, true
		}
	}
	if value, ok := directResponseStateValue(responseState, responseStateOK, "test"); ok {
		if testObject, objectOK := value.(map[string]any); objectOK {
			if hasTests, known := stateBool(testObject["hasTests"]); known {
				preflight.TestPresent, preflight.TestPresentKnown = hasTests, true
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
	if inspection.ArchivedMarkerVacancyScoped || inspection.Class == WebPageVacancyArchived {
		setActiveEvidence(preflight, VacancyActiveStateInactive, ActiveEvidenceArchiveMarker)
	}
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
	if !preflight.ArchivedKnown && (inspection.ArchivedMarkerVacancyScoped || inspection.Class == WebPageVacancyArchived) {
		preflight.Archived, preflight.ArchivedKnown = true, true
	}
	if inspection.ApplicationFormPresent && inspection.FormVacancyIDMatches {
		preflight.CanApply, preflight.CanApplyKnown = true, true
		preflight.Available = true
		setActiveEvidence(preflight, VacancyActiveStateActive, ActiveEvidenceApplicationForm)
		setAlreadyRespondedEvidence(preflight, AlreadyRespondedNo, EvidenceApplyActionAvailable)
	} else if inspection.ExplicitApplyAction {
		preflight.CanApply, preflight.CanApplyKnown = true, true
		preflight.Available = true
		setActiveEvidence(preflight, VacancyActiveStateActive, ActiveEvidenceResponseLink)
		if preflight.AlreadyRespondedEvidence.EvidenceCode == "" {
			setAlreadyRespondedEvidence(preflight, AlreadyRespondedNo, EvidenceApplyActionAvailable)
		}
	}
}

func directResponseState(state map[string]any, vacancyID int) (map[string]any, bool) {
	if statuses, ok := directStateValue(state, "applicantVacancyResponseStatuses"); ok {
		if statusMap, ok := statuses.(map[string]any); ok {
			if object, ok := statusMap[strconv.Itoa(vacancyID)].(map[string]any); ok {
				return object, true
			}
		}
	}
	if popup, ok := directStateValue(state, "vacancyResponsePopup"); ok {
		if popupObject, ok := popup.(map[string]any); ok {
			if vacancy, ok := directStateValue(popupObject, "vacancy"); ok {
				if object, ok := vacancy.(map[string]any); ok && responseStateMatchesVacancy(object, vacancyID) {
					return object, true
				}
			}
		}
	}
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
	if value, ok := directStateValue(state, keys...); ok {
		return value, true
	}
	if shortVacancy, ok := directStateValue(state, "shortVacancy"); ok {
		if object, ok := shortVacancy.(map[string]any); ok {
			return directStateValue(object, keys...)
		}
	}
	return nil, false
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
	document, err := xhtml.Parse(strings.NewReader(string(data)))
	if err != nil {
		return false
	}
	if visibleHTMLAuthMarker(document) {
		return true
	}
	text := strings.ToLower(normalizeVisibleHTMLText(document))
	return containsAny(text, "/account/login", "cloudflare challenge", "ddos-guard challenge", "access denied")
}

func visibleHTMLAuthMarker(root *xhtml.Node) bool {
	if root == nil {
		return false
	}
	if root.Type == xhtml.ElementNode {
		switch strings.ToLower(root.Data) {
		case "script", "style", "noscript", "template":
			return false
		}
		attributes := make([]string, 0, len(root.Attr)*2)
		for _, attribute := range root.Attr {
			attributes = append(attributes, strings.ToLower(attribute.Key), strings.ToLower(attribute.Val))
		}
		if containsAny(strings.Join(attributes, " "), " /account/login", "/account/login", "supernova-login-wrapper", "forbiddenpage", "captcha-container", "cf-chl-", "cloudflare challenge", "ddos-guard challenge") {
			return true
		}
	}
	for child := root.FirstChild; child != nil; child = child.NextSibling {
		if visibleHTMLAuthMarker(child) {
			return true
		}
	}
	return false
}

func normalizeVisibleHTMLText(root *xhtml.Node) string {
	if root == nil {
		return ""
	}
	var builder strings.Builder
	var visit func(*xhtml.Node)
	visit = func(node *xhtml.Node) {
		if node == nil {
			return
		}
		if node.Type == xhtml.ElementNode {
			switch strings.ToLower(node.Data) {
			case "script", "style", "noscript", "template":
				return
			}
		}
		if node.Type == xhtml.TextNode {
			builder.WriteString(node.Data)
			builder.WriteByte(' ')
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			visit(child)
		}
	}
	visit(root)
	return normalizeHTMLText(builder.String())
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

func stateNumber(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case json.Number:
		parsed, err := typed.Float64()
		return parsed, err == nil
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		return parsed, err == nil
	default:
		return 0, false
	}
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
