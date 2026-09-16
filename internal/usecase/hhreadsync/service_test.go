package hhreadsync

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"hh-ai-responder/internal/hhread"
	"hh-ai-responder/internal/ports"
	"hh-ai-responder/internal/vacancy"
)

type vacancyMemory struct{ values []vacancy.Vacancy }

type observingVacancyMemory struct {
	*vacancyMemory
	observed []struct {
		value vacancy.Vacancy
		at    time.Time
	}
}

func (m *observingVacancyMemory) ObserveVacancy(_ context.Context, value vacancy.Vacancy, at time.Time) (vacancy.ObservationResult, error) {
	m.observed = append(m.observed, struct {
		value vacancy.Vacancy
		at    time.Time
	}{value: value, at: at})
	if _, err := m.GetByExternalID(context.Background(), value.ExternalID); err == nil {
		return vacancy.ObservationResult{}, nil
	}
	if _, err := m.Create(context.Background(), value); err != nil {
		return vacancy.ObservationResult{}, err
	}
	return vacancy.ObservationResult{Created: true}, nil
}

var _ ports.VacancyObserver = (*observingVacancyMemory)(nil)

func (m *vacancyMemory) Get(_ context.Context, id int) (vacancy.Vacancy, error) {
	for _, value := range m.values {
		if value.ID == id {
			return value, nil
		}
	}
	return vacancy.Vacancy{}, vacancy.ErrVacancyNotFound
}
func (m *vacancyMemory) GetByExternalID(_ context.Context, id string) (vacancy.Vacancy, error) {
	for _, value := range m.values {
		if value.ExternalID == id {
			return value, nil
		}
	}
	return vacancy.Vacancy{}, vacancy.ErrVacancyNotFound
}
func (m *vacancyMemory) List(context.Context, ports.VacancyQuery) ([]vacancy.Vacancy, error) {
	return append([]vacancy.Vacancy{}, m.values...), nil
}
func (m *vacancyMemory) Create(_ context.Context, value vacancy.Vacancy) (vacancy.Vacancy, error) {
	if value.ID == 0 {
		value.ID = len(m.values) + 1
	}
	m.values = append(m.values, value)
	return value, nil
}
func (m *vacancyMemory) Update(_ context.Context, value vacancy.Vacancy) error {
	for i := range m.values {
		if m.values[i].ID == value.ID {
			m.values[i] = value
			return nil
		}
	}
	return vacancy.ErrVacancyNotFound
}
func (*vacancyMemory) Save(context.Context) error { return nil }

var _ ports.VacancyStore = (*vacancyMemory)(nil)

type readSource struct{ page hhread.VacancyPage }

func (r readSource) ReadVacancies(context.Context, string) (hhread.VacancyPage, error) {
	return r.page, nil
}
func (readSource) ReadApplications(context.Context, string) (hhread.ApplicationPage, error) {
	return hhread.ApplicationPage{}, nil
}
func (readSource) ReadConversations(context.Context, string) (hhread.ConversationPage, error) {
	return hhread.ConversationPage{}, nil
}

type vacancyDetailReadSource struct {
	readSource
	detail hhread.VacancyRecord
	err    error
}

func (s vacancyDetailReadSource) ReadVacancyDetail(context.Context, int) (hhread.VacancyRecord, error) {
	return s.detail, s.err
}

type boundedConversationSource struct {
	pages       map[string]hhread.ConversationPage
	cursors     []string
	detailReads int
	unbounded   int
	providerErr error
	blockUntil  <-chan struct{}
	entered     chan struct{}
}

func (r *boundedConversationSource) ReadVacancies(context.Context, string) (hhread.VacancyPage, error) {
	return hhread.VacancyPage{}, nil
}
func (r *boundedConversationSource) ReadApplications(context.Context, string) (hhread.ApplicationPage, error) {
	return hhread.ApplicationPage{}, nil
}
func (r *boundedConversationSource) ReadConversations(_ context.Context, cursor string) (hhread.ConversationPage, error) {
	r.unbounded++
	return r.pages[cursor], nil
}
func (r *boundedConversationSource) ReadConversationsBounded(ctx context.Context, cursor string, limit int) (hhread.ConversationPage, error) {
	r.cursors = append(r.cursors, cursor)
	if r.entered != nil {
		close(r.entered)
	}
	if r.blockUntil != nil {
		select {
		case <-r.blockUntil:
		case <-ctx.Done():
			return hhread.ConversationPage{}, ctx.Err()
		}
	}
	if r.providerErr != nil {
		return hhread.ConversationPage{}, r.providerErr
	}
	page := r.pages[cursor]
	if len(page.Items) > limit {
		page.Items = page.Items[:limit]
	}
	r.detailReads += len(page.Items)
	return page, nil
}

func conversationRecords(count int, prefix string) []hhread.ConversationRecord {
	values := make([]hhread.ConversationRecord, count)
	for i := range values {
		values[i].ExternalID = fmt.Sprintf("%s-%d", prefix, i+1)
	}
	return values
}

func TestServicePreservesResponseCountKnowledgeAndLocalVacancyState(t *testing.T) {
	at := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	local := vacancy.Vacancy{ID: 7, ExternalID: "hh-7", Name: "old", CreatedAt: at, UpdatedAt: at,
		MatchResult: &vacancy.MatchResult{Score: 80}, ApplicationRecommendation: &vacancy.ApplicationRecommendation{Decision: vacancy.RecommendationMaybe},
		ReconciliationEvidence: []vacancy.ReconciliationEvidence{{Method: "test", Source: "local", Confidence: 1, ReconciledAt: at}}, TotalResponsesCount: 4, TotalResponsesCountKnown: true}
	store := &vacancyMemory{values: []vacancy.Vacancy{local}}
	service := NewService(Dependencies{Vacancies: store})
	result, err := service.ImportBatch(context.Background(), Batch{Vacancies: []hhread.VacancyRecord{{ExternalID: "hh-7", ID: 7, Title: "new", Description: "detail", Requirements: []string{"Python"}, Location: "remote"}}}, ImportOptions{})
	if err != nil || result.Updated != 1 {
		t.Fatalf("import result=%+v err=%v", result, err)
	}
	if store.values[0].TotalResponsesCountKnown || store.values[0].MatchResult == nil || store.values[0].ApplicationRecommendation == nil || len(store.values[0].ReconciliationEvidence) != 1 {
		t.Fatalf("local merge or unknown response count lost: %+v", store.values[0])
	}
}

func TestServicePassesOneBatchObservationTimestampToFreshnessObserver(t *testing.T) {
	at := time.Date(2026, 9, 11, 10, 20, 30, 123456789, time.UTC)
	store := &observingVacancyMemory{vacancyMemory: &vacancyMemory{}}
	service := NewService(Dependencies{Vacancies: store})
	result, err := service.ImportBatch(context.Background(), Batch{Vacancies: []hhread.VacancyRecord{{ID: 7, ExternalID: "hh-7", Title: "Support"}}}, ImportOptions{ObservedAt: at})
	if err != nil || result.Created != 1 || len(store.observed) != 1 || !store.observed[0].at.Equal(at) {
		t.Fatalf("result=%+v observations=%+v err=%v", result, store.observed, err)
	}
}

func TestReadBatchEnrichesVacancyWithBoundedDetailRead(t *testing.T) {
	source := vacancyDetailReadSource{
		readSource: readSource{page: hhread.VacancyPage{Items: []hhread.VacancyRecord{{ID: 7, ExternalID: "7", Title: "Partial"}}}},
		detail:     hhread.VacancyRecord{ID: 7, ExternalID: "7", Description: "full detail", KeySkills: []string{"Python"}, AreaName: "Екатеринбург", WorkFormat: "remote", Experience: "between1And3"},
	}
	batch, result, err := NewService(Dependencies{Source: source}).ReadBatch(context.Background(), TargetVacancies, ReadOptions{MaxVacancyDetails: 1})
	if err != nil || len(batch.Vacancies) != 1 || result.DetailRequested != 1 || result.DetailSucceeded != 1 || result.DetailFieldsEnriched != 5 {
		t.Fatalf("batch=%+v result=%+v err=%v", batch, result, err)
	}
	if batch.Vacancies[0].Description != "full detail" || batch.Vacancies[0].WorkFormat != "remote" || batch.Vacancies[0].AreaName != "Екатеринбург" {
		t.Fatalf("detail was not merged: %+v", batch.Vacancies[0])
	}
}

func TestReadBatchKeepsSearchResultWhenDetailReadFails(t *testing.T) {
	source := vacancyDetailReadSource{readSource: readSource{page: hhread.VacancyPage{Items: []hhread.VacancyRecord{{ID: 7, ExternalID: "7", Title: "Partial"}}}}, err: errors.New("provider 503")}
	batch, result, err := NewService(Dependencies{Source: source}).ReadBatch(context.Background(), TargetVacancies, ReadOptions{MaxVacancyDetails: 1})
	if err != nil || len(batch.Vacancies) != 1 || result.DetailFailed != 1 || len(result.Warnings) != 1 {
		t.Fatalf("batch=%+v result=%+v err=%v", batch, result, err)
	}
}

func TestMergeVacancyPreservesRichFieldsFromPartialSearch(t *testing.T) {
	old := vacancy.Vacancy{ExternalID: "7", Description: "full", Skills: []string{"Python"}, Area: vacancy.NamedObject{Name: "Екатеринбург"}, WorkFormat: "remote", WorkExperience: "between1And3", ProfessionalRoles: []string{"96"}}
	got := MergeVacancy(old, vacancy.Vacancy{ExternalID: "7", Title: "Updated title"})
	if got.Description != old.Description || len(got.Skills) != 1 || got.Area.Name != old.Area.Name || got.WorkFormat != old.WorkFormat || got.WorkExperience != old.WorkExperience || len(got.ProfessionalRoles) != 1 {
		t.Fatalf("rich fields were downgraded: %+v", got)
	}
}

func TestServiceSyncProgressesPagesWithoutWritesBeyondPorts(t *testing.T) {
	store := &vacancyMemory{}
	source := readSource{page: hhread.VacancyPage{Items: []hhread.VacancyRecord{{ExternalID: "hh-1", ID: 1, Title: "Support"}}, NextCursor: ""}}
	result, err := NewService(Dependencies{Source: source, Vacancies: store}).Sync(context.Background(), TargetVacancies)
	if err != nil || result.Fetched != 1 || result.Created != 1 || len(store.values) != 1 {
		t.Fatalf("sync result=%+v values=%d err=%v", result, len(store.values), err)
	}
}

func TestMapMessagesUsesNormalizedSenderAndPreservesServiceMessages(t *testing.T) {
	at := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	values, warnings := MapMessages([]hhread.MessageRecord{
		{ExternalID: "system", Sender: "system", SystemEvent: true, Timestamp: at},
		{ExternalID: "candidate", Sender: "candidate", Text: "hello", Timestamp: at.Add(time.Minute)},
		{ExternalID: "bad", Sender: "", Text: "ignored", Timestamp: at},
	})
	if len(warnings) != 1 || len(values) != 2 || !values[0].HHSystemEvent || string(values[1].Sender) != "candidate" {
		t.Fatalf("mapping=%+v warnings=%v", values, warnings)
	}
}

func TestServiceCancellationFailsBeforeRead(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := NewService(Dependencies{Source: readSource{}}).Sync(ctx, TargetVacancies)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want cancellation", err)
	}
}

func TestServiceBoundedConversationReadStopsBeforeNextPage(t *testing.T) {
	source := &boundedConversationSource{pages: map[string]hhread.ConversationPage{
		"":            {Items: conversationRecords(20, "chat"), NextCursor: "opaque-next"},
		"opaque-next": {Items: conversationRecords(20, "later")},
	}}
	_, result, err := NewService(Dependencies{Source: source}).ReadBatch(context.Background(), TargetInbox, ReadOptions{MaxConversations: 3})
	if err != nil {
		t.Fatal(err)
	}
	if result.Fetched != 3 || len(result.SelectedConversationIDs) != 3 || source.detailReads != 3 || len(source.cursors) != 1 || source.cursors[0] != "" {
		t.Fatalf("bounded page-1 read fetched=%d details=%d cursors=%v", result.Fetched, source.detailReads, source.cursors)
	}
}

func TestServiceBoundedConversationReadCrossesPageBoundaryWithOpaqueCursor(t *testing.T) {
	first := conversationRecords(2, "first")
	second := conversationRecords(2, "second")
	source := &boundedConversationSource{pages: map[string]hhread.ConversationPage{
		"":            {Items: first, NextCursor: "opaque-next"},
		"opaque-next": {Items: second, NextCursor: "should-not-read"},
	}}
	_, result, err := NewService(Dependencies{Source: source}).ReadBatch(context.Background(), TargetInbox, ReadOptions{MaxConversations: 3})
	if err != nil {
		t.Fatal(err)
	}
	if result.Fetched != 3 || len(result.SelectedConversationIDs) != 3 || source.detailReads != 3 || len(source.cursors) != 2 || source.cursors[1] != "opaque-next" {
		t.Fatalf("bounded cross-page read fetched=%d details=%d cursors=%v", result.Fetched, source.detailReads, source.cursors)
	}
}

func TestServiceUnboundedConversationReadPreservesFullScope(t *testing.T) {
	source := &boundedConversationSource{pages: map[string]hhread.ConversationPage{
		"":            {Items: conversationRecords(2, "first"), NextCursor: "opaque-next"},
		"opaque-next": {Items: conversationRecords(2, "second")},
	}}
	_, result, err := NewService(Dependencies{Source: source}).ReadBatch(context.Background(), TargetInbox, ReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Fetched != 4 || source.unbounded != 2 || len(source.cursors) != 0 {
		t.Fatalf("unbounded read unexpectedly used bounded source: result=%+v unbounded=%d cursors=%v", result, source.unbounded, source.cursors)
	}
}

func TestServiceBoundedConversationReadCancellationStopsBeforeNextPage(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	block := make(chan struct{})
	source := &boundedConversationSource{blockUntil: block, entered: make(chan struct{})}
	done := make(chan struct{})
	var result Result
	go func() {
		_, result, _ = NewService(Dependencies{Source: source}).ReadBatch(ctx, TargetInbox, ReadOptions{MaxConversations: 3})
		close(done)
	}()
	<-source.entered
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("bounded read did not stop after cancellation")
	}
	if len(source.cursors) != 1 || len(result.Errors) == 0 || source.detailReads != 0 {
		t.Fatalf("cancellation result=%+v cursors=%v detailReads=%d", result, source.cursors, source.detailReads)
	}
}

func TestServiceBoundedConversationProviderErrorRemainsError(t *testing.T) {
	providerErr := errors.New("provider 503")
	source := &boundedConversationSource{providerErr: providerErr}
	_, result, err := NewService(Dependencies{Source: source}).ReadBatch(context.Background(), TargetInbox, ReadOptions{MaxConversations: 3})
	if err != nil {
		t.Fatal("provider errors are reported in the result for partial reads", err)
	}
	if len(result.Errors) != 1 || result.Errors[0] != providerErr.Error() {
		t.Fatalf("provider error was not retained: %+v", result)
	}
}
