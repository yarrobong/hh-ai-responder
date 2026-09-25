package runtime

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	hhapi "hh-ai-responder/internal/adapters/hh/api"
	web "hh-ai-responder/internal/adapters/hh/web"
	"hh-ai-responder/internal/hhwebsession"
	hhwrite "hh-ai-responder/internal/ports/hhwrite"
	applicationreconciliation "hh-ai-responder/internal/usecase/applicationreconciliation"
)

type controlledApplicationTransport interface {
	Prepare(context.Context, APIApplicationApproval) (controlledApplicationContext, error)
	Writer() hhwrite.VacancyResponseWriter
	EvidenceReader() applicationreconciliation.EvidenceReader
	PersistenceHealth() error
}

type controlledApplicationContext struct {
	ResumeID       string
	Preflight      VacancyPreflight
	ResponseURL    string
	RefererURL     string
	Writer         hhwrite.VacancyResponseWriter
	EvidenceReader applicationreconciliation.EvidenceReader
	Metadata       map[string]string
}

type apiControlledApplicationTransport struct {
	client *hhapi.APIHHClient
}

func (t *apiControlledApplicationTransport) Prepare(ctx context.Context, approval APIApplicationApproval) (controlledApplicationContext, error) {
	if t == nil || t.client == nil {
		return controlledApplicationContext{}, errors.New("API application transport is unavailable")
	}
	providerResumeID := approvalProviderResumeID(approval)
	preflight, err := apiVacancyPreflightWithSource(ctx, t.client, approval.VacancyID, providerResumeID)
	if err != nil {
		return controlledApplicationContext{}, fmt.Errorf("HH API apply GET-only preflight failed: %w", err)
	}
	return controlledApplicationContext{ResumeID: providerResumeID, Preflight: preflight, ResponseURL: preflight.ResponseURL, Writer: hhapi.NewAPIApplicationWriter(t.client), EvidenceReader: &apiApplicationEvidenceReader{client: t.client}, Metadata: map[string]string{"transport": "api"}}, nil
}

func (t *apiControlledApplicationTransport) Writer() hhwrite.VacancyResponseWriter {
	if t == nil || t.client == nil {
		return nil
	}
	return hhapi.NewAPIApplicationWriter(t.client)
}

func (t *apiControlledApplicationTransport) EvidenceReader() applicationreconciliation.EvidenceReader {
	if t == nil || t.client == nil {
		return nil
	}
	return &apiApplicationEvidenceReader{client: t.client}
}

func (*apiControlledApplicationTransport) PersistenceHealth() error { return nil }

type cookieControlledApplicationTransport struct {
	preflight *CookieWebApplicationPreflightService
	writer    hhwrite.VacancyResponseWriter
	evidence  applicationreconciliation.EvidenceReader
	session   *hhwebsession.Session
}

func (t *cookieControlledApplicationTransport) Prepare(ctx context.Context, approval APIApplicationApproval) (controlledApplicationContext, error) {
	if t == nil || t.preflight == nil || t.writer == nil || t.session == nil {
		return controlledApplicationContext{}, errors.New("cookie web application transport is unavailable")
	}
	if err := validateBrowserApprovalHash(approval); err != nil {
		return controlledApplicationContext{}, err
	}
	preflight, err := t.preflight.Preflight(ctx, approval.VacancyID, approval.BrowserResumeHash)
	if err != nil {
		return controlledApplicationContext{}, fmt.Errorf("cookie web application GET-only preflight failed: %w", err)
	}
	if err := preflight.ValidateForSend(approval.CoverLetter); err != nil {
		return controlledApplicationContext{}, fmt.Errorf("cookie web application preflight blocked: %w", err)
	}
	refererURL := preflight.ResponseURL
	if parsed, parseErr := url.Parse(refererURL); parseErr == nil {
		parsed.Path = fmt.Sprintf("/vacancy/%d", approval.VacancyID)
		parsed.RawQuery = ""
		refererURL = parsed.String()
	}
	return controlledApplicationContext{ResumeID: approval.BrowserResumeHash, Preflight: preflight.VacancyPreflight, ResponseURL: preflight.ResponseURL, RefererURL: refererURL, Writer: t.writer, EvidenceReader: t.evidence, Metadata: map[string]string{"transport": "browser"}}, nil
}

func (t *cookieControlledApplicationTransport) Writer() hhwrite.VacancyResponseWriter {
	if t == nil {
		return nil
	}
	return t.writer
}

func (t *cookieControlledApplicationTransport) EvidenceReader() applicationreconciliation.EvidenceReader {
	if t == nil {
		return nil
	}
	return t.evidence
}

func (t *cookieControlledApplicationTransport) PersistenceHealth() error {
	if t == nil || t.session == nil {
		return errors.New("cookie web session is unavailable")
	}
	return t.session.PersistenceError()
}

func newCookieControlledApplicationTransport(cfg Config, userAgent string) (controlledApplicationTransport, error) {
	base := &url.URL{Scheme: "https", Host: "hh.ru"}
	if raw := strings.TrimSpace(cfg.SearchURL); raw != "" {
		parsed, err := url.Parse(raw)
		if err != nil || parsed.Host == "" {
			return nil, errors.New("cookie web transport search URL is invalid")
		}
		base = parsed
	}
	session, err := hhwebsession.New(cfg.CookiesPath, hhwebsession.Options{BaseURL: base, AllowedHosts: []string{"hh.ru", base.Hostname()}, UserAgent: userAgent})
	if err != nil {
		return nil, err
	}
	reader, err := web.NewHTTPResumeMappingReader(session.ReadClient(), base)
	if err != nil {
		return nil, err
	}
	preflight, err := NewCookieWebApplicationPreflightService(session.ReadClient(), base, reader, session.PersistenceError)
	if err != nil {
		return nil, err
	}
	writer, err := web.NewCookieWebVacancyResponseWriter(base, session, userAgent)
	if err != nil {
		return nil, err
	}
	return &cookieControlledApplicationTransport{preflight: preflight, writer: writer, session: session}, nil
}
