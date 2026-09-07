package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// DataCompleteness describes how much trusted vacancy data is available. It
// is deliberately independent from match status: a partial source is not a
// failed match.
type DataCompleteness string

const (
	DataCompletenessFull    DataCompleteness = "full"
	DataCompletenessPartial DataCompleteness = "partial"
	DataCompletenessMinimal DataCompleteness = "minimal"
)

type ReconciliationEvidence struct {
	Method       string    `json:"method"`
	Source       string    `json:"source"`
	Confidence   float64   `json:"confidence"`
	ReconciledAt time.Time `json:"reconciled_at"`
}

type ReconcileIssue struct {
	Relation string `json:"relation"`
	RecordID string `json:"record_id"`
	Reason   string `json:"reason"`
}

type ReconcileResult struct {
	DryRun                bool             `json:"dry_run"`
	LinksFound            int              `json:"links_found"`
	ApplicationsCreated   int              `json:"applications_created"`
	VacanciesEnriched     int              `json:"vacancies_enriched"`
	MatchesBackfilled     int              `json:"matches_backfilled"`
	EventsCreated         int              `json:"events_created"`
	UnresolvedRelations   []ReconcileIssue `json:"unresolved_relations,omitempty"`
	InsufficientMatchData []string         `json:"insufficient_match_data,omitempty"`
}

type CareerDataReconciler struct {
	Vacancies     VacancyRepository
	Applications  ApplicationRepository
	Conversations ConversationRepository
	Analyzer      interface {
		Analyze(Vacancy, any) MatchResult
	}
	Candidate any
}

func NewCareerDataReconciler(vacancies *VacancyStore, applications *ApplicationStore, conversations *ConversationStore, dependencies ...any) *CareerDataReconciler {
	analyzer := interface {
		Analyze(Vacancy, any) MatchResult
	}(NewVacancyAnalyzer())
	var candidate any
	for _, dependency := range dependencies {
		switch value := dependency.(type) {
		case interface {
			Analyze(Vacancy, any) MatchResult
		}:
			analyzer = value
		default:
			if candidate == nil {
				candidate = value
			}
		}
	}
	return NewCareerDataReconcilerWithRepositories(NewJSONVacancyRepository(vacancies), NewJSONApplicationRepository(applications), NewJSONConversationRepository(conversations), analyzer, candidate)
}

// NewCareerDataReconcilerWithRepositories is the typed construction path for
// application logic. The legacy constructor above remains for compatibility
// with the CLI and existing callers.
func NewCareerDataReconcilerWithRepositories(vacancies VacancyRepository, applications ApplicationRepository, conversations ConversationRepository, analyzer interface {
	Analyze(Vacancy, any) MatchResult
}, candidate any) *CareerDataReconciler {
	if analyzer == nil {
		analyzer = NewVacancyAnalyzer()
	}
	return &CareerDataReconciler{Vacancies: vacancies, Applications: applications, Conversations: conversations, Analyzer: analyzer, Candidate: candidate}
}

func (r *CareerDataReconciler) Reconcile(dryRun bool) (ReconcileResult, error) {
	return r.ReconcileContext(context.Background(), dryRun)
}

func (r *CareerDataReconciler) ReconcileContext(ctx context.Context, dryRun bool) (ReconcileResult, error) {
	if !dryRun {
		if career := r.postgresCareerStore(); career != nil {
			var result ReconcileResult
			var transactionErr error
			err := career.WithTx(ctx, func(tx CareerTx) error {
				transactional := *r
				transactional.Vacancies = tx.Vacancies()
				transactional.Applications = tx.Applications()
				transactional.Conversations = tx.Conversations()
				result, transactionErr = transactional.reconcileContext(ctx, false)
				return transactionErr
			})
			return result, err
		}
	}
	return r.reconcileContext(ctx, dryRun)
}

func (r *CareerDataReconciler) postgresCareerStore() *PostgresCareerStore {
	if r == nil {
		return nil
	}
	vacancies, vacanciesOK := r.Vacancies.(*PostgresVacancyRepository)
	applications, applicationsOK := r.Applications.(*PostgresApplicationRepository)
	conversations, conversationsOK := r.Conversations.(*PostgresConversationRepository)
	if !vacanciesOK || !applicationsOK || !conversationsOK || vacancies == nil || applications == nil || conversations == nil || vacancies.pool == nil || vacancies.pool != applications.pool || vacancies.pool != conversations.pool {
		return nil
	}
	return NewPostgresCareerStore(vacancies.pool)
}

func (r *CareerDataReconciler) reconcileContext(ctx context.Context, dryRun bool) (ReconcileResult, error) {
	result := ReconcileResult{DryRun: dryRun, UnresolvedRelations: []ReconcileIssue{}, InsufficientMatchData: []string{}}
	if r == nil || r.Vacancies == nil || r.Applications == nil || r.Conversations == nil {
		return result, errors.New("reconciler requires all local stores")
	}
	now := time.Now().UTC()
	applications, err := r.Applications.List(ctx)
	if err != nil {
		return result, err
	}
	conversations, err := r.Conversations.List(ctx)
	if err != nil {
		return result, err
	}
	// Normalize completeness for every locally known vacancy, including
	// vacancies that currently have no application relation.
	for _, vacancy := range mustListVacancies(ctx, r.Vacancies) {
		if vacancy.DataCompleteness != "" {
			continue
		}
		if !dryRun {
			vacancy.DataCompleteness = inferVacancyCompleteness(vacancy)
			if err := r.Vacancies.Update(ctx, vacancy); err != nil {
				return result, err
			}
		}
	}
	// First use strong metadata. This also handles a conversation imported
	// before the corresponding negotiation page.
	for _, conversation := range conversations {
		if conversationHasApplication(applications, conversation) {
			continue
		}
		candidates := applicationsForConversation(applications, conversation)
		if len(candidates) == 1 {
			if !dryRun {
				if err := r.linkConversation(ctx, candidates[0], conversation, now); err != nil {
					return result, err
				}
			}
			result.LinksFound++
			continue
		}
		if len(candidates) == 0 {
			if externalID := conversationNegotiationID(conversation); externalID != "" {
				if !dryRun {
					created, err := r.createPartialApplication(ctx, conversation, externalID, now)
					if err != nil {
						return result, err
					}
					applications = append(applications, created)
				}
				result.ApplicationsCreated++
				result.LinksFound++
				continue
			}
		}
		result.UnresolvedRelations = append(result.UnresolvedRelations, ReconcileIssue{Relation: "conversation_application", RecordID: conversation.ID, Reason: "no unique strong HH negotiation/application relation"})
	}

	applications, err = r.Applications.List(ctx)
	if err != nil {
		return result, err
	}
	for _, application := range applications {
		vacancy, vacancyErr := r.Vacancies.Get(ctx, application.VacancyID)
		if vacancyErr != nil {
			if !errors.Is(vacancyErr, ErrVacancyNotFound) {
				return result, vacancyErr
			}
			if application.Source == ApplicationSourceHH && application.VacancyID > 0 {
				result.UnresolvedRelations = append(result.UnresolvedRelations, ReconcileIssue{Relation: "application_vacancy", RecordID: application.ID, Reason: "vacancy ID is known but no local HH vacancy record exists"})
			}
			if application.MatchResult == nil {
				result.InsufficientMatchData = append(result.InsufficientMatchData, application.ID)
			}
			continue
		}
		completenessWasMissing := vacancy.DataCompleteness == ""
		if completenessWasMissing {
			vacancy.DataCompleteness = inferVacancyCompleteness(vacancy)
		}
		updated := vacancy
		changed := completenessWasMissing
		if enrichVacancyFromApplication(&updated, application) {
			updated.DataCompleteness = inferVacancyCompleteness(updated)
			changed = true
		}
		if changed {
			result.VacanciesEnriched++
			if !dryRun {
				appendEvidence(&updated.ReconciliationEvidence, "application_vacancy_id", "local", 1, now)
				if err := r.Vacancies.Update(ctx, updated); err != nil {
					return result, err
				}
				if err := r.addEvent(ctx, application.ID, ApplicationEventVacancyEnriched, "vacancy data enriched from trusted application relation", now); err != nil {
					return result, err
				}
				result.EventsCreated++
			}
		}
		if application.DataCompleteness == "" || application.Partial != (vacancy.DataCompleteness != DataCompletenessFull) {
			application.DataCompleteness = vacancy.DataCompleteness
			application.Partial = vacancy.DataCompleteness != DataCompletenessFull
			if !dryRun {
				appendEvidence(&application.ReconciliationEvidence, "application_vacancy_id", "local", 1, now)
				if err := r.Applications.Update(ctx, application); err != nil {
					return result, err
				}
				if err := r.addEvent(ctx, application.ID, ApplicationEventLinkedVacancy, "application linked to local vacancy by HH vacancy ID", now); err != nil {
					return result, err
				}
				result.EventsCreated++
			}
		}
		if !dryRun {
			if refreshed, err := r.Applications.Get(ctx, application.ID); err == nil {
				application = refreshed
			}
		}
		if vacancy.DataCompleteness == DataCompletenessFull && application.MatchResult == nil {
			result.MatchesBackfilled++
			if !dryRun {
				if err := r.backfillMatch(ctx, application, vacancy, now); err != nil {
					return result, err
				}
				result.EventsCreated++
			}
		} else if application.MatchResult == nil {
			result.InsufficientMatchData = append(result.InsufficientMatchData, application.ID)
		}
	}

	if !dryRun {
		if err := r.Applications.Save(ctx); err != nil {
			return result, err
		}
		if err := r.Vacancies.Save(ctx); err != nil {
			return result, err
		}
	}
	return result, nil
}

func mustListVacancies(ctx context.Context, store VacancyRepository) []Vacancy {
	if store == nil {
		return nil
	}
	values, _ := store.List(ctx, VacancyQuery{})
	return values
}

func inferVacancyCompleteness(v Vacancy) DataCompleteness {
	if strings.TrimSpace(v.Description) != "" && (len(v.Requirements) > 0 || len(v.Skills) > 0 || len(v.Description) >= 120) {
		return DataCompletenessFull
	}
	if strings.TrimSpace(v.Description) != "" || len(v.Requirements) > 0 || len(v.Skills) > 0 || strings.TrimSpace(v.Location) != "" {
		return DataCompletenessPartial
	}
	return DataCompletenessMinimal
}

func appendEvidence(values *[]ReconciliationEvidence, method, source string, confidence float64, now time.Time) bool {
	for _, value := range *values {
		if value.Method == method && value.Source == source {
			return false
		}
	}
	*values = append(*values, ReconciliationEvidence{Method: method, Source: source, Confidence: confidence, ReconciledAt: now})
	return true
}

func conversationHasApplication(applications []JobApplication, conversation EmployerConversation) bool {
	for _, application := range applications {
		if application.ConversationID == conversation.ID || application.HHMetadata["conversation_external_id"] == conversation.HHConversationID && conversation.HHConversationID != "" {
			return true
		}
	}
	return false
}

func applicationsForConversation(applications []JobApplication, conversation EmployerConversation) []JobApplication {
	result := []JobApplication{}
	for _, application := range applications {
		if application.ConversationID == conversation.ID || conversation.HHConversationID != "" && application.HHMetadata["conversation_external_id"] == conversation.HHConversationID {
			result = append(result, application)
			continue
		}
		for _, key := range []string{"conversation_id", "topic_id"} {
			if value := strings.TrimSpace(conversation.HHMetadata[key]); value != "" && (value == application.ExternalID || value == application.HHMetadata[key]) {
				result = append(result, application)
				break
			}
		}
	}
	return result
}

func conversationNegotiationID(c EmployerConversation) string {
	for _, key := range []string{"negotiation_id", "application_id", "hh_application_id", "applicant_negotiation_id", "topic_id"} {
		if value := strings.TrimSpace(c.HHMetadata[key]); value != "" {
			return value
		}
	}
	return ""
}

func (r *CareerDataReconciler) linkConversation(ctx context.Context, application JobApplication, conversation EmployerConversation, now time.Time) error {
	if application.ConversationID != "" && application.ConversationID != conversation.ID {
		return fmt.Errorf("application %s already has a different conversation", application.ID)
	}
	application.ConversationID = conversation.ID
	appendEvidence(&application.ReconciliationEvidence, "hh_negotiation_conversation_id", "hh", 1, now)
	if err := r.Applications.Update(ctx, application); err != nil {
		return err
	}
	return r.addEvent(ctx, application.ID, ApplicationEventLinkedConversation, "application linked to HH conversation by strong identifier", now)
}

func (r *CareerDataReconciler) createPartialApplication(ctx context.Context, c EmployerConversation, externalID string, now time.Time) (JobApplication, error) {
	metadata := copyStringMap(c.HHMetadata)
	metadata["conversation_external_id"] = c.HHConversationID
	metadata["reconciliation_method"] = "hh_negotiation_id"
	created := c.CreatedAt
	if created.IsZero() {
		created = now
	}
	updated := c.UpdatedAt
	if updated.IsZero() || updated.Before(created) {
		updated = created
	}
	value, _, err := r.Applications.UpsertImported(ctx, JobApplication{ExternalID: externalID, VacancyID: c.VacancyID, CompanyName: c.CompanyName, VacancyTitle: c.VacancyTitle, Source: ApplicationSourceHH, Status: ApplicationUnknown, CreatedAt: created, UpdatedAt: updated, ConversationID: c.ID, RawStatus: c.RawStatus, HHMetadata: metadata, Partial: true, DataCompleteness: DataCompletenessPartial, ReconciliationEvidence: []ReconciliationEvidence{{Method: "hh_negotiation_id", Source: "hh", Confidence: 1, ReconciledAt: now}}})
	if err != nil {
		return JobApplication{}, err
	}
	if err := r.addEvent(ctx, value.ID, ApplicationEventPartialCreated, "partial application created from HH negotiation evidence", now); err != nil {
		return JobApplication{}, err
	}
	return value, nil
}

func enrichVacancyFromApplication(v *Vacancy, a JobApplication) bool {
	changed := false
	if strings.TrimSpace(v.Name) == "" && strings.TrimSpace(a.VacancyTitle) != "" {
		v.Name, changed = a.VacancyTitle, true
	}
	if strings.TrimSpace(v.Title) == "" && strings.TrimSpace(a.VacancyTitle) != "" {
		v.Title, changed = a.VacancyTitle, true
	}
	if strings.TrimSpace(v.Source) == "" && a.Source == ApplicationSourceHH {
		v.Source, changed = "hh", true
	}
	return changed
}

func (r *CareerDataReconciler) backfillMatch(ctx context.Context, application JobApplication, vacancy Vacancy, now time.Time) error {
	if r.Analyzer == nil {
		return errors.New("vacancy analyzer is not configured")
	}
	result := r.Analyzer.Analyze(vacancy, r.Candidate)
	result.normalize()
	if err := result.validate(); err != nil {
		return err
	}
	application.MatchResult = &result
	application.DataCompleteness = vacancy.DataCompleteness
	application.Partial = vacancy.DataCompleteness != DataCompletenessFull
	application.UpdatedAt = now
	if application.UpdatedAt.Before(application.CreatedAt) {
		application.UpdatedAt = application.CreatedAt
	}
	if err := r.Applications.Update(ctx, application); err != nil {
		return err
	}
	return r.addEvent(ctx, application.ID, ApplicationEventMatchBackfilled, "match result backfilled from full vacancy data", now)
}

func (r *CareerDataReconciler) addEvent(ctx context.Context, applicationID string, eventType ApplicationEventType, description string, at time.Time) error {
	events, err := r.Applications.Timeline(ctx, applicationID)
	if err != nil {
		return err
	}
	for _, event := range events {
		if event.ApplicationID == applicationID && event.Type == eventType {
			return nil
		}
	}
	return r.Applications.AppendEvent(ctx, applicationID, at, eventType, description)
}
