package runtime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"hh-ai-responder/internal/hhwebsession"
	applicationreconciliation "hh-ai-responder/internal/usecase/applicationreconciliation"
)

type cookieWebApplicationEvidenceReader struct {
	client  hhwebsession.RequestDoer
	baseURL *url.URL
}

var _ applicationreconciliation.EvidenceReader = (*cookieWebApplicationEvidenceReader)(nil)

func newCookieWebApplicationEvidenceReader(client hhwebsession.RequestDoer, baseURL *url.URL) *cookieWebApplicationEvidenceReader {
	return &cookieWebApplicationEvidenceReader{client: client, baseURL: baseURL}
}

func (r *cookieWebApplicationEvidenceReader) ReadVacancyResponseEvidence(ctx context.Context, target applicationreconciliation.Target) (applicationreconciliation.EvidenceSnapshot, error) {
	snapshot := applicationreconciliation.EvidenceSnapshot{VacancyID: target.VacancyID, ObservedAt: time.Now().UTC()}
	if r == nil || r.client == nil || r.baseURL == nil || target.VacancyID <= 0 {
		return snapshot, errors.New("cookie web reconciliation target is invalid")
	}
	responseURL := r.baseURL.ResolveReference(&url.URL{Path: "/applicant/vacancy_response", RawQuery: url.Values{"vacancyId": {strconv.Itoa(target.VacancyID)}, "startedWithQuestion": {"false"}, "hhtmFrom": {"vacancy"}}.Encode()})
	responsePage, err := r.get(ctx, responseURL)
	if err != nil {
		snapshot.PreflightError = "cookie web vacancy response evidence unavailable"
		return snapshot, err
	}
	preflight, err := parseVacancyPreflight(responsePage.Body, Vacancy{ID: target.VacancyID}, responseURL.String())
	if err != nil {
		snapshot.PreflightError = "cookie web vacancy response state was not recognized"
		return snapshot, err
	}
	evidence := preflight.alreadyRespondedEvidence()
	snapshot.PreflightAvailable = true
	snapshot.Preflight = applicationreconciliation.PreflightEvidence{Available: preflight.Available, AlreadyResponded: evidence.Value == AlreadyRespondedYes, AlreadyRespondedKnown: evidence.Value != AlreadyRespondedUnknown, CanApply: preflight.CanApply, CanApplyKnown: preflight.CanApplyKnown}
	negotiationsURL := r.baseURL.ResolveReference(&url.URL{Path: "/applicant/negotiations", RawQuery: url.Values{"page": {"0"}}.Encode()})
	negotiationPage, err := r.get(ctx, negotiationsURL)
	if err != nil {
		snapshot.ApplicationsError = "cookie web vacancy negotiations evidence unavailable"
		return snapshot, err
	}
	applications, _, recognized, parseErr := parseHHNegotiations(negotiationPage.Body)
	if parseErr != nil {
		snapshot.ApplicationsError = "cookie web vacancy negotiations state was not recognized"
		return snapshot, parseErr
	}
	if !recognized {
		// A response-page positive marker is still valid vacancy-scoped evidence;
		// an unrecognized negotiations page is otherwise insufficient, not a
		// reason to infer a negative result.
		snapshot.ApplicationsAvailable = true
		return snapshot, nil
	}
	snapshot.ApplicationsAvailable = true
	for _, application := range applications {
		if application.VacancyID != target.VacancyID || strings.TrimSpace(application.ExternalID) == "" {
			continue
		}
		snapshot.Applications = append(snapshot.Applications, applicationreconciliation.ProviderResponse{VacancyID: target.VacancyID, ApplicationID: strings.TrimSpace(application.ExternalID), NegotiationID: strings.TrimSpace(application.ExternalID), ConversationID: strings.TrimSpace(application.ConversationExternal), Source: "cookie_web_vacancy_scoped_negotiations", ResponseByApplicant: application.Metadata["delivery_confirmed"] == "true"})
	}
	return snapshot, nil
}

type cookieWebEvidencePage struct {
	Status int
	Body   []byte
}

func (r *cookieWebApplicationEvidenceReader) get(ctx context.Context, target *url.URL) (cookieWebEvidencePage, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return cookieWebEvidencePage{}, err
	}
	response, err := r.client.Do(request)
	if err != nil {
		return cookieWebEvidencePage{}, err
	}
	defer response.Body.Close()
	body, readErr := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if readErr != nil {
		return cookieWebEvidencePage{}, readErr
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return cookieWebEvidencePage{}, fmt.Errorf("cookie web evidence GET returned status %d", response.StatusCode)
	}
	return cookieWebEvidencePage{Status: response.StatusCode, Body: body}, nil
}
