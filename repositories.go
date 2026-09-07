package main

import (
	"context"
	"errors"
	"strings"
	"time"
)

// Repository boundaries are deliberately expressed in terms of the existing
// domain values. The JSON stores remain the compatibility implementation; a
// future SQL implementation can replace these adapters without introducing
// persistence DTOs into application code.

type VacancyQuery struct {
	ID         *int
	ExternalID string
}

type VacancyRepository interface {
	Get(context.Context, int) (Vacancy, error)
	GetByExternalID(context.Context, string) (Vacancy, error)
	List(context.Context, VacancyQuery) ([]Vacancy, error)
	Create(context.Context, Vacancy) (Vacancy, error)
	Update(context.Context, Vacancy) error
	Save(context.Context) error
}

type ApplicationRepository interface {
	Get(context.Context, string) (JobApplication, error)
	GetByExternalID(context.Context, string) (JobApplication, error)
	List(context.Context) ([]JobApplication, error)
	Create(context.Context, JobApplication) (JobApplication, error)
	Update(context.Context, JobApplication) error
	UpsertImported(context.Context, JobApplication) (JobApplication, bool, error)
	AttachImportedConversation(context.Context, string, string) error
	AttachConversation(context.Context, string, string) error
	UpdateStatus(context.Context, string, ApplicationStatus) error
	SetFollowUpState(context.Context, string, ConversationFollowUpState) error
	SaveMatchResult(context.Context, string, MatchResult) error
	AppendEvent(context.Context, string, time.Time, ApplicationEventType, string) error
	Timeline(context.Context, string) ([]ApplicationEvent, error)
	Save(context.Context) error
}

type ConversationRepository interface {
	Get(context.Context, string) (EmployerConversation, error)
	GetByHHConversationID(context.Context, string) (EmployerConversation, error)
	GetByVacancyID(context.Context, int) ([]EmployerConversation, error)
	List(context.Context) ([]EmployerConversation, error)
	Upsert(context.Context, EmployerConversation) (EmployerConversation, error)
	AppendMessage(context.Context, string, ConversationMessage) (ConversationMessage, error)
	UpdateState(context.Context, string, ConversationState) error
	UpdateSummary(context.Context, string, ConversationSummary) error
	AppendCandidateClaim(context.Context, string, CandidateConversationClaim) error
	Timeline(context.Context, string) ([]ConversationMessage, error)
	Save(context.Context) error
}

// CandidateRepository is intentionally read-only. Candidate is the canonical
// runtime model; callers must not fall back to CandidateProfile or KB values.
type CandidateRepository interface {
	CurrentCandidate(context.Context) (Candidate, error)
}

type JSONVacancyRepository struct{ store *VacancyStore }

func NewJSONVacancyRepository(store *VacancyStore) *JSONVacancyRepository {
	return &JSONVacancyRepository{store: store}
}

func (r *JSONVacancyRepository) Get(ctx context.Context, id int) (Vacancy, error) {
	if err := repositoryContextErr(ctx); err != nil {
		return Vacancy{}, err
	}
	if r == nil || r.store == nil {
		return Vacancy{}, ErrVacancyNotFound
	}
	return r.store.Get(id)
}

func (r *JSONVacancyRepository) GetByExternalID(ctx context.Context, id string) (Vacancy, error) {
	if err := repositoryContextErr(ctx); err != nil {
		return Vacancy{}, err
	}
	if r == nil || r.store == nil {
		return Vacancy{}, ErrVacancyNotFound
	}
	return r.store.GetByExternalID(id)
}

func (r *JSONVacancyRepository) List(ctx context.Context, query VacancyQuery) ([]Vacancy, error) {
	if err := repositoryContextErr(ctx); err != nil {
		return nil, err
	}
	if r == nil || r.store == nil {
		return []Vacancy{}, nil
	}
	values, err := r.store.List()
	if err != nil {
		return nil, err
	}
	filtered := make([]Vacancy, 0, len(values))
	for _, value := range values {
		if query.ID != nil && value.ID != *query.ID {
			continue
		}
		if strings.TrimSpace(query.ExternalID) != "" && value.ExternalID != query.ExternalID {
			continue
		}
		filtered = append(filtered, value)
	}
	return filtered, nil
}

func (r *JSONVacancyRepository) Create(ctx context.Context, value Vacancy) (Vacancy, error) {
	if err := repositoryContextErr(ctx); err != nil {
		return Vacancy{}, err
	}
	if r == nil || r.store == nil {
		return Vacancy{}, errors.New("vacancy repository is not configured")
	}
	return r.store.Create(value)
}

func (r *JSONVacancyRepository) Update(ctx context.Context, value Vacancy) error {
	if err := repositoryContextErr(ctx); err != nil {
		return err
	}
	if r == nil || r.store == nil {
		return errors.New("vacancy repository is not configured")
	}
	return r.store.Update(value)
}

func (r *JSONVacancyRepository) Save(ctx context.Context) error {
	if err := repositoryContextErr(ctx); err != nil {
		return err
	}
	if r == nil || r.store == nil {
		return errors.New("vacancy repository is not configured")
	}
	return r.store.Save()
}

type JSONApplicationRepository struct{ store *ApplicationStore }

func NewJSONApplicationRepository(store *ApplicationStore) *JSONApplicationRepository {
	return &JSONApplicationRepository{store: store}
}

func (r *JSONApplicationRepository) Get(ctx context.Context, id string) (JobApplication, error) {
	if err := repositoryContextErr(ctx); err != nil {
		return JobApplication{}, err
	}
	if r == nil || r.store == nil {
		return JobApplication{}, ErrApplicationNotFound
	}
	return r.store.GetApplication(id)
}

func (r *JSONApplicationRepository) GetByExternalID(ctx context.Context, id string) (JobApplication, error) {
	if err := repositoryContextErr(ctx); err != nil {
		return JobApplication{}, err
	}
	if r == nil || r.store == nil {
		return JobApplication{}, ErrApplicationNotFound
	}
	return r.store.GetByExternalID(id)
}

func (r *JSONApplicationRepository) List(ctx context.Context) ([]JobApplication, error) {
	if err := repositoryContextErr(ctx); err != nil {
		return nil, err
	}
	if r == nil || r.store == nil {
		return []JobApplication{}, nil
	}
	return r.store.ListApplications()
}

func (r *JSONApplicationRepository) Create(ctx context.Context, value JobApplication) (JobApplication, error) {
	if err := repositoryContextErr(ctx); err != nil {
		return JobApplication{}, err
	}
	if r == nil || r.store == nil {
		return JobApplication{}, errors.New("application repository is not configured")
	}
	return r.store.CreateApplication(value)
}

func (r *JSONApplicationRepository) Update(ctx context.Context, value JobApplication) error {
	if err := repositoryContextErr(ctx); err != nil {
		return err
	}
	if r == nil || r.store == nil {
		return errors.New("application repository is not configured")
	}
	return r.store.UpdateApplication(value)
}

func (r *JSONApplicationRepository) UpsertImported(ctx context.Context, value JobApplication) (JobApplication, bool, error) {
	if err := repositoryContextErr(ctx); err != nil {
		return JobApplication{}, false, err
	}
	if r == nil || r.store == nil {
		return JobApplication{}, false, errors.New("application repository is not configured")
	}
	return r.store.UpsertImportedApplication(value)
}

func (r *JSONApplicationRepository) AttachImportedConversation(ctx context.Context, externalID, conversationID string) error {
	if err := repositoryContextErr(ctx); err != nil {
		return err
	}
	if r == nil || r.store == nil {
		return errors.New("application repository is not configured")
	}
	return r.store.AttachImportedConversation(externalID, conversationID)
}

func (r *JSONApplicationRepository) AttachConversation(ctx context.Context, applicationID, conversationID string) error {
	if err := repositoryContextErr(ctx); err != nil {
		return err
	}
	if r == nil || r.store == nil {
		return errors.New("application repository is not configured")
	}
	return r.store.AttachConversation(applicationID, conversationID)
}

func (r *JSONApplicationRepository) UpdateStatus(ctx context.Context, id string, status ApplicationStatus) error {
	if err := repositoryContextErr(ctx); err != nil {
		return err
	}
	if r == nil || r.store == nil {
		return errors.New("application repository is not configured")
	}
	return r.store.UpdateStatus(id, status)
}

func (r *JSONApplicationRepository) SetFollowUpState(ctx context.Context, id string, state ConversationFollowUpState) error {
	if err := repositoryContextErr(ctx); err != nil {
		return err
	}
	if r == nil || r.store == nil {
		return errors.New("application repository is not configured")
	}
	return r.store.SetFollowUpState(id, state)
}

func (r *JSONApplicationRepository) SaveMatchResult(ctx context.Context, id string, result MatchResult) error {
	if err := repositoryContextErr(ctx); err != nil {
		return err
	}
	if r == nil || r.store == nil {
		return errors.New("application repository is not configured")
	}
	return r.store.SaveMatchResult(id, result)
}

func (r *JSONApplicationRepository) AppendEvent(ctx context.Context, id string, at time.Time, eventType ApplicationEventType, description string) error {
	if err := repositoryContextErr(ctx); err != nil {
		return err
	}
	if r == nil || r.store == nil {
		return errors.New("application repository is not configured")
	}
	return r.store.RecordEvent(id, at, eventType, description)
}

func (r *JSONApplicationRepository) Timeline(ctx context.Context, id string) ([]ApplicationEvent, error) {
	if err := repositoryContextErr(ctx); err != nil {
		return nil, err
	}
	if r == nil || r.store == nil {
		return nil, ErrApplicationNotFound
	}
	return r.store.GetApplicationTimeline(id)
}

func (r *JSONApplicationRepository) Save(ctx context.Context) error {
	if err := repositoryContextErr(ctx); err != nil {
		return err
	}
	if r == nil || r.store == nil {
		return errors.New("application repository is not configured")
	}
	return r.store.Save()
}

type JSONConversationRepository struct{ store *ConversationStore }

func NewJSONConversationRepository(store *ConversationStore) *JSONConversationRepository {
	return &JSONConversationRepository{store: store}
}

func (r *JSONConversationRepository) Get(ctx context.Context, id string) (EmployerConversation, error) {
	if err := repositoryContextErr(ctx); err != nil {
		return EmployerConversation{}, err
	}
	if r == nil || r.store == nil {
		return EmployerConversation{}, ErrConversationNotFound
	}
	return r.store.GetConversation(id)
}

func (r *JSONConversationRepository) GetByHHConversationID(ctx context.Context, id string) (EmployerConversation, error) {
	if err := repositoryContextErr(ctx); err != nil {
		return EmployerConversation{}, err
	}
	if r == nil || r.store == nil {
		return EmployerConversation{}, ErrConversationNotFound
	}
	return r.store.GetByHHConversationID(id)
}

func (r *JSONConversationRepository) GetByVacancyID(ctx context.Context, id int) ([]EmployerConversation, error) {
	if err := repositoryContextErr(ctx); err != nil {
		return nil, err
	}
	if r == nil || r.store == nil {
		return []EmployerConversation{}, nil
	}
	return r.store.GetByVacancyID(id)
}

func (r *JSONConversationRepository) List(ctx context.Context) ([]EmployerConversation, error) {
	if err := repositoryContextErr(ctx); err != nil {
		return nil, err
	}
	if r == nil || r.store == nil {
		return []EmployerConversation{}, nil
	}
	return r.store.ListConversations()
}

func (r *JSONConversationRepository) Upsert(ctx context.Context, value EmployerConversation) (EmployerConversation, error) {
	if err := repositoryContextErr(ctx); err != nil {
		return EmployerConversation{}, err
	}
	if r == nil || r.store == nil {
		return EmployerConversation{}, errors.New("conversation repository is not configured")
	}
	return r.store.UpsertConversation(value)
}

func (r *JSONConversationRepository) AppendMessage(ctx context.Context, id string, value ConversationMessage) (ConversationMessage, error) {
	if err := repositoryContextErr(ctx); err != nil {
		return ConversationMessage{}, err
	}
	if r == nil || r.store == nil {
		return ConversationMessage{}, errors.New("conversation repository is not configured")
	}
	return r.store.AppendMessage(id, value)
}

func (r *JSONConversationRepository) UpdateState(ctx context.Context, id string, state ConversationState) error {
	if err := repositoryContextErr(ctx); err != nil {
		return err
	}
	if r == nil || r.store == nil {
		return errors.New("conversation repository is not configured")
	}
	return r.store.UpdateConversationState(id, state)
}

func (r *JSONConversationRepository) UpdateSummary(ctx context.Context, id string, summary ConversationSummary) error {
	if err := repositoryContextErr(ctx); err != nil {
		return err
	}
	if r == nil || r.store == nil {
		return errors.New("conversation repository is not configured")
	}
	return r.store.UpdateSummary(id, summary)
}

func (r *JSONConversationRepository) AppendCandidateClaim(ctx context.Context, id string, claim CandidateConversationClaim) error {
	if err := repositoryContextErr(ctx); err != nil {
		return err
	}
	if r == nil || r.store == nil {
		return errors.New("conversation repository is not configured")
	}
	return r.store.RecordCandidateClaim(id, claim)
}

func (r *JSONConversationRepository) Timeline(ctx context.Context, id string) ([]ConversationMessage, error) {
	if err := repositoryContextErr(ctx); err != nil {
		return nil, err
	}
	if r == nil || r.store == nil {
		return nil, ErrConversationNotFound
	}
	return r.store.GetConversationTimeline(id)
}

func (r *JSONConversationRepository) Save(ctx context.Context) error {
	if err := repositoryContextErr(ctx); err != nil {
		return err
	}
	if r == nil || r.store == nil {
		return errors.New("conversation repository is not configured")
	}
	return r.store.Save()
}

type CandidateRepositoryConfig struct {
	ProfilePath string
	StoriesPath string
	CandidateID string
	Contacts    string
	GitHubURL   string
}

// JSONCandidateRepository reads all legacy candidate inputs into one logical
// snapshot before mapping. It never exposes CandidateProfile or the KB.
type JSONCandidateRepository struct {
	config  CandidateRepositoryConfig
	kb      *CandidateKnowledgeBase
	stories []CandidateStory
	input   *CanonicalCandidateInput
}

func NewJSONCandidateRepository(config CandidateRepositoryConfig) *JSONCandidateRepository {
	return &JSONCandidateRepository{config: config}
}

func NewJSONCandidateRepositoryFromLegacy(kb *CandidateKnowledgeBase, stories []CandidateStory, contacts, githubURL string) *JSONCandidateRepository {
	return &JSONCandidateRepository{kb: kb, stories: append([]CandidateStory{}, stories...), config: CandidateRepositoryConfig{Contacts: contacts, GitHubURL: githubURL}}
}

// NewJSONCandidateRepositoryFromInput is the transitional adapter for a live
// per-run snapshot (for example, HH resume facts merged into a local profile).
// It still uses the same canonical mapper and keeps the repository read-only.
func NewJSONCandidateRepositoryFromInput(input CanonicalCandidateInput) *JSONCandidateRepository {
	return &JSONCandidateRepository{input: &input}
}

func (r *JSONCandidateRepository) CurrentCandidate(ctx context.Context) (Candidate, error) {
	candidate, _, err := r.CurrentCandidateWithDiagnostics(ctx)
	return candidate, err
}

func (r *JSONCandidateRepository) CurrentCandidateWithDiagnostics(ctx context.Context) (Candidate, CanonicalCandidateDiagnostics, error) {
	if err := repositoryContextErr(ctx); err != nil {
		return Candidate{}, CanonicalCandidateDiagnostics{}, err
	}
	if r == nil {
		return Candidate{}, CanonicalCandidateDiagnostics{}, errors.New("candidate repository is not configured")
	}
	var input CanonicalCandidateInput
	if r.input != nil {
		input = *r.input
	} else if r.kb != nil {
		input.Profile = r.kb.Profile
		input.Knowledge = *r.kb
		input.Stories = append([]CandidateStory{}, r.stories...)
	} else {
		kb := NewCandidateKnowledgeBase(r.config.ProfilePath)
		if err := kb.Load(); err != nil {
			return Candidate{}, CanonicalCandidateDiagnostics{}, err
		}
		input.Profile = kb.Profile
		input.Knowledge = *kb
		stories, err := LoadCandidateStories(r.config.StoriesPath)
		if err != nil {
			return Candidate{}, CanonicalCandidateDiagnostics{}, err
		}
		input.Stories = stories.Stories
	}
	input.CandidateID, input.Contacts, input.GitHubURL = r.config.CandidateID, r.config.Contacts, r.config.GitHubURL
	return BuildCanonicalCandidate(input)
}

func repositoryContextErr(ctx context.Context) error {
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
