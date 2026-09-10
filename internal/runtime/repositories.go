package runtime

import (
	"context"
	"errors"
	"time"

	jsonstorage "hh-ai-responder/internal/adapters/storage/json"
	domaincandidate "hh-ai-responder/internal/candidate"
	"hh-ai-responder/internal/ports"
)

// These aliases preserve the root composition API while making the contracts
// importable by orchestration packages.
type VacancyQuery = ports.VacancyQuery
type VacancyReader = ports.VacancyReader
type VacancyWriter = ports.VacancyWriter
type VacancyRepository = ports.VacancyStore
type ApplicationReader = ports.ApplicationReader
type ApplicationWriter = ports.ApplicationWriter
type ApplicationRepository = ports.ApplicationStore
type ConversationReader = ports.ConversationReader
type ConversationWriter = ports.ConversationWriter
type ConversationRepository = ports.ConversationStore
type CandidateRepository = ports.CandidateReader

// JSONVacancyRepository and its constructor retain the root composition API.
// The implementation is owned by the JSON adapter; this is an alias only.
type JSONVacancyRepository = jsonstorage.VacancyRepository

func NewJSONVacancyRepository(store *VacancyStore) *JSONVacancyRepository {
	if store == nil {
		return nil
	}
	return store.repository
}

// JSONApplicationRepository is a root compatibility wrapper around the real
// JSON adapter. It intentionally contains no persistence state or I/O.
type JSONApplicationRepository struct {
	*jsonstorage.ApplicationRepository
	store *ApplicationStore
}

func NewJSONApplicationRepository(store *ApplicationStore) *JSONApplicationRepository {
	if store == nil {
		return &JSONApplicationRepository{}
	}
	return &JSONApplicationRepository{ApplicationRepository: store.repository, store: store}
}

func (r *JSONApplicationRepository) prepare() error {
	if r == nil || r.ApplicationRepository == nil {
		return errors.New("application repository is not configured")
	}
	if r.store != nil {
		return r.store.syncCompatibilityState()
	}
	return nil
}

func (r *JSONApplicationRepository) prepareWithContext(ctx context.Context) error {
	if err := repositoryContextErr(ctx); err != nil {
		return err
	}
	return r.prepare()
}

func (r *JSONApplicationRepository) finish(err error) error {
	if err != nil || r == nil || r.store == nil {
		return err
	}
	return r.store.refreshCompatibilityMirror()
}

func (r *JSONApplicationRepository) Get(ctx context.Context, id string) (JobApplication, error) {
	if err := r.prepareWithContext(ctx); err != nil {
		return JobApplication{}, err
	}
	return r.ApplicationRepository.Get(ctx, id)
}

func (r *JSONApplicationRepository) GetByExternalID(ctx context.Context, id string) (JobApplication, error) {
	if err := r.prepareWithContext(ctx); err != nil {
		return JobApplication{}, err
	}
	return r.ApplicationRepository.GetByExternalID(ctx, id)
}

func (r *JSONApplicationRepository) List(ctx context.Context) ([]JobApplication, error) {
	if err := r.prepareWithContext(ctx); err != nil {
		return nil, err
	}
	return r.ApplicationRepository.List(ctx)
}

func (r *JSONApplicationRepository) Timeline(ctx context.Context, id string) ([]ApplicationEvent, error) {
	if err := r.prepareWithContext(ctx); err != nil {
		return nil, err
	}
	return r.ApplicationRepository.Timeline(ctx, id)
}

func (r *JSONApplicationRepository) Create(ctx context.Context, value JobApplication) (JobApplication, error) {
	if err := r.prepareWithContext(ctx); err != nil {
		return JobApplication{}, err
	}
	created, err := r.ApplicationRepository.Create(ctx, value)
	return created, r.finish(err)
}

func (r *JSONApplicationRepository) Update(ctx context.Context, value JobApplication) error {
	if err := r.prepareWithContext(ctx); err != nil {
		return err
	}
	return r.finish(r.ApplicationRepository.Update(ctx, value))
}

func (r *JSONApplicationRepository) UpsertImported(ctx context.Context, value JobApplication) (JobApplication, bool, error) {
	if err := r.prepareWithContext(ctx); err != nil {
		return JobApplication{}, false, err
	}
	result, created, err := r.ApplicationRepository.UpsertImported(ctx, value)
	return result, created, r.finish(err)
}

func (r *JSONApplicationRepository) AttachImportedConversation(ctx context.Context, externalID, conversationID string) error {
	if err := r.prepareWithContext(ctx); err != nil {
		return err
	}
	return r.finish(r.ApplicationRepository.AttachImportedConversation(ctx, externalID, conversationID))
}

func (r *JSONApplicationRepository) AttachConversation(ctx context.Context, applicationID, conversationID string) error {
	if err := r.prepareWithContext(ctx); err != nil {
		return err
	}
	return r.finish(r.ApplicationRepository.AttachConversation(ctx, applicationID, conversationID))
}

func (r *JSONApplicationRepository) UpdateStatus(ctx context.Context, id string, status ApplicationStatus) error {
	if err := r.prepareWithContext(ctx); err != nil {
		return err
	}
	return r.finish(r.ApplicationRepository.UpdateStatus(ctx, id, status))
}

func (r *JSONApplicationRepository) SetFollowUpState(ctx context.Context, id string, state ConversationFollowUpState) error {
	if err := r.prepareWithContext(ctx); err != nil {
		return err
	}
	return r.finish(r.ApplicationRepository.SetFollowUpState(ctx, id, state))
}

func (r *JSONApplicationRepository) SaveMatchResult(ctx context.Context, id string, result MatchResult) error {
	if err := r.prepareWithContext(ctx); err != nil {
		return err
	}
	return r.finish(r.ApplicationRepository.SaveMatchResult(ctx, id, result))
}

func (r *JSONApplicationRepository) AppendEvent(ctx context.Context, id string, at time.Time, eventType ApplicationEventType, description string) error {
	if err := r.prepareWithContext(ctx); err != nil {
		return err
	}
	return r.finish(r.ApplicationRepository.AppendEvent(ctx, id, at, eventType, description))
}

func (r *JSONApplicationRepository) Save(ctx context.Context) error {
	if err := r.prepareWithContext(ctx); err != nil {
		return err
	}
	return r.ApplicationRepository.Save(ctx)
}

// JSONConversationRepository is a root compatibility wrapper. The adapter
// owns all JSON state and behavior; store is retained only to synchronize the
// deprecated root mirror used by older workflows.
type JSONConversationRepository struct {
	repository *jsonstorage.ConversationRepository
	store      *ConversationStore
}

func NewJSONConversationRepository(store *ConversationStore) *JSONConversationRepository {
	if store == nil {
		return &JSONConversationRepository{}
	}
	return &JSONConversationRepository{repository: store.repository, store: store}
}

func (r *JSONConversationRepository) currentRepository() *jsonstorage.ConversationRepository {
	if r == nil {
		return nil
	}
	if r.store != nil {
		return r.store.repository
	}
	return r.repository
}

func (r *JSONConversationRepository) prepare(ctx context.Context) error {
	if err := repositoryContextErr(ctx); err != nil {
		return err
	}
	if r.currentRepository() == nil {
		return errors.New("conversation repository is not configured")
	}
	if r.store != nil {
		return r.store.syncCompatibilityState()
	}
	return nil
}

func (r *JSONConversationRepository) finish(err error) error {
	if err != nil || r == nil || r.store == nil {
		return err
	}
	return r.store.refreshCompatibilityMirror()
}

func (r *JSONConversationRepository) Get(ctx context.Context, id string) (EmployerConversation, error) {
	if err := r.prepare(ctx); err != nil {
		return EmployerConversation{}, err
	}
	return r.currentRepository().Get(ctx, id)
}

func (r *JSONConversationRepository) GetByHHConversationID(ctx context.Context, id string) (EmployerConversation, error) {
	if err := r.prepare(ctx); err != nil {
		return EmployerConversation{}, err
	}
	return r.currentRepository().GetByHHConversationID(ctx, id)
}

func (r *JSONConversationRepository) GetByVacancyID(ctx context.Context, id int) ([]EmployerConversation, error) {
	if err := r.prepare(ctx); err != nil {
		return nil, err
	}
	return r.currentRepository().GetByVacancyID(ctx, id)
}

func (r *JSONConversationRepository) List(ctx context.Context) ([]EmployerConversation, error) {
	if err := r.prepare(ctx); err != nil {
		return nil, err
	}
	return r.currentRepository().List(ctx)
}

func (r *JSONConversationRepository) Upsert(ctx context.Context, value EmployerConversation) (EmployerConversation, error) {
	if err := r.prepare(ctx); err != nil {
		return EmployerConversation{}, err
	}
	result, err := r.currentRepository().Upsert(ctx, value)
	return result, r.finish(err)
}

func (r *JSONConversationRepository) AppendMessage(ctx context.Context, id string, value ConversationMessage) (ConversationMessage, error) {
	if err := r.prepare(ctx); err != nil {
		return ConversationMessage{}, err
	}
	result, err := r.currentRepository().AppendMessage(ctx, id, value)
	return result, r.finish(err)
}

func (r *JSONConversationRepository) UpdateState(ctx context.Context, id string, state ConversationState) error {
	if err := r.prepare(ctx); err != nil {
		return err
	}
	return r.finish(r.currentRepository().UpdateState(ctx, id, state))
}

func (r *JSONConversationRepository) UpdateSummary(ctx context.Context, id string, summary ConversationSummary) error {
	if err := r.prepare(ctx); err != nil {
		return err
	}
	return r.finish(r.currentRepository().UpdateSummary(ctx, id, summary))
}

func (r *JSONConversationRepository) AppendCandidateClaim(ctx context.Context, id string, claim CandidateConversationClaim) error {
	if err := r.prepare(ctx); err != nil {
		return err
	}
	return r.finish(r.currentRepository().AppendCandidateClaim(ctx, id, claim))
}

func (r *JSONConversationRepository) Timeline(ctx context.Context, id string) ([]ConversationMessage, error) {
	if err := r.prepare(ctx); err != nil {
		return nil, err
	}
	return r.currentRepository().Timeline(ctx, id)
}

func (r *JSONConversationRepository) Save(ctx context.Context) error {
	if err := r.prepare(ctx); err != nil {
		return err
	}
	return r.currentRepository().Save(ctx)
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
type JSONCandidateRepository = jsonstorage.CandidateRepository

func NewJSONCandidateRepository(config CandidateRepositoryConfig) *JSONCandidateRepository {
	return jsonstorage.NewCandidateRepository(jsonstorage.CandidateRepositoryConfig{
		ProfilePath: config.ProfilePath, StoriesPath: config.StoriesPath, CandidateID: config.CandidateID,
		Contacts: config.Contacts, GitHubURL: config.GitHubURL,
	})
}

func NewJSONCandidateRepositoryFromLegacy(kb *CandidateKnowledgeBase, stories []CandidateStory, contacts, githubURL string) *JSONCandidateRepository {
	input := domaincandidate.CanonicalCandidateInput{Contacts: contacts, GitHubURL: githubURL}
	if kb != nil {
		input.Profile = kb.Profile
		input.Knowledge = domaincandidate.KnowledgeSnapshot{Skills: kb.Skills, Projects: kb.Projects, Achievements: kb.Achievements, Unknowns: kb.Unknowns, Proposals: kb.Proposals, Events: kb.Events}
	}
	input.Stories = append([]CandidateStory{}, stories...)
	return jsonstorage.NewCandidateRepositoryFromInput(input)
}

// NewJSONCandidateRepositoryFromInput is the transitional adapter for a live
// per-run snapshot (for example, HH resume facts merged into a local profile).
// It still uses the same canonical mapper and keeps the repository read-only.
func NewJSONCandidateRepositoryFromInput(input CanonicalCandidateInput) *JSONCandidateRepository {
	domainInput := domaincandidate.CanonicalCandidateInput{
		Profile:   input.Profile,
		Knowledge: domaincandidate.KnowledgeSnapshot{Skills: input.Knowledge.Skills, Projects: input.Knowledge.Projects, Achievements: input.Knowledge.Achievements, Unknowns: input.Knowledge.Unknowns, Proposals: input.Knowledge.Proposals, Events: input.Knowledge.Events},
		Stories:   input.Stories, ResumeFacts: input.ResumeFacts, CandidateID: input.CandidateID,
		Contacts: input.Contacts, GitHubURL: input.GitHubURL,
	}
	return jsonstorage.NewCandidateRepositoryFromInput(domainInput)
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
