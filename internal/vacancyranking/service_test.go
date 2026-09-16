package vacancyranking

import (
	"context"
	"reflect"
	"testing"
	"time"

	"hh-ai-responder/internal/application"
	"hh-ai-responder/internal/candidate"
	"hh-ai-responder/internal/ports"
	"hh-ai-responder/internal/vacancy"
	"hh-ai-responder/internal/vacancyreview"
)

var rankingAt = time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)

func confirmedMeta() candidate.KnowledgeMetadata {
	return candidate.KnowledgeMetadata{TruthStatus: candidate.TruthStatusConfirmed, Sources: []candidate.KnowledgeSourceRecord{{Type: candidate.KnowledgeSourceUserConfirmed, Evidence: []string{"candidate confirmed"}}}}
}

func confirmedProfileFact(value string) candidate.ProfileStringFact {
	return candidate.ProfileStringFact{Value: value, ProfileFact: candidate.ProfileFact{Source: candidate.CandidateSourceUserConfirmed, Confirmed: true}}
}

func rankingCandidate() candidate.Candidate {
	months := 11
	return candidate.Candidate{
		ID:       "candidate-local",
		Identity: candidate.CanonicalCandidateIdentity{Location: "Екатеринбург", LocationMetadata: confirmedMeta()},
		Profile: candidate.CanonicalProfileSnapshot{
			TotalExperienceMonths: candidate.ProfileIntFact{Value: months, ProfileFact: candidate.ProfileFact{Source: candidate.CandidateSourceUserConfirmed, Confirmed: true}},
			WorkPreferences: candidate.WorkPreferences{
				PrimaryRoles: confirmedProfileFact("backend developer"), WorkMode: confirmedProfileFact("office Ekaterinburg or remote"),
				Relocation: confirmedProfileFact("no"), BusinessTrips: confirmedProfileFact("no"), SalaryMinimum: candidate.ProfileIntFact{Value: 40000, ProfileFact: candidate.ProfileFact{Source: candidate.CandidateSourceUserConfirmed, Confirmed: true}},
			},
		},
		Skills: []candidate.CanonicalCandidateSkill{
			{ID: "python", Name: "python", DisplayName: "Python", Level: candidate.SkillLevelWorking, State: candidate.CanonicalClaimActive, Metadata: confirmedMeta()},
			{ID: "postgres", Name: "postgresql", DisplayName: "PostgreSQL", Level: candidate.SkillLevelWorking, State: candidate.CanonicalClaimActive, Metadata: confirmedMeta()},
		},
	}
}

func evaluateFixture(t *testing.T, value vacancy.Vacancy) Result {
	t.Helper()
	ctx, err := NewCandidateContext(rankingCandidate())
	if err != nil {
		t.Fatal(err)
	}
	return (Evaluator{}).Evaluate(EvaluateInput{Vacancy: value, Candidate: ctx, Effective: vacancyreview.EffectiveState{VacancyID: value.ID, State: vacancyreview.StateUnseen}, Now: rankingAt})
}

func baseVacancy() vacancy.Vacancy {
	return vacancy.Vacancy{ID: 1, Name: "Python backend developer", Title: "Python backend developer", Description: "Python and PostgreSQL API development", WorkFormat: "remote", WorkExperience: "noExperience", Compensation: vacancy.Compensation{From: intPtr(60000), Currency: "RUR"}}
}

func intPtr(value int) *int { return &value }

func TestHardEligibilityLocationAndRemote(t *testing.T) {
	office := baseVacancy()
	office.WorkFormat, office.Area.Name = "office", "Москва"
	got := evaluateFixture(t, office)
	if got.Eligibility != EligibilityIneligible || got.ExclusionCode != "office_location_conflict" || got.FitBand != FitHardIncompatible {
		t.Fatalf("office conflict=%+v", got)
	}
	remote := office
	remote.WorkFormat = "remote"
	got = evaluateFixture(t, remote)
	if got.Eligibility == EligibilityIneligible || !hasCode(got.PositiveReasons, "remote_compatible") {
		t.Fatalf("remote=%+v", got)
	}
	unknown := baseVacancy()
	unknown.WorkFormat, unknown.WorkSchedule, unknown.Location, unknown.Area.Name = "", "", "", ""
	got = evaluateFixture(t, unknown)
	if got.Eligibility != EligibilityReviewRequired || !hasCode(got.Unknowns, "insufficient_location_evidence") {
		t.Fatalf("unknown location=%+v", got)
	}
	relocation := baseVacancy()
	relocation.Description, relocation.WorkFormat = "Обязательная релокация в Москву", "remote"
	got = evaluateFixture(t, relocation)
	if got.Eligibility != EligibilityIneligible || !hasCode(got.HardReasons, "relocation_required") {
		t.Fatalf("relocation=%+v", got)
	}
	travel := baseVacancy()
	travel.Description = "Готовность к командировкам обязательна"
	got = evaluateFixture(t, travel)
	if got.Eligibility != EligibilityIneligible || !hasCode(got.HardReasons, "business_travel_required") {
		t.Fatalf("travel=%+v", got)
	}
	archived := baseVacancy()
	archived.Archived = true
	got = evaluateFixture(t, archived)
	if got.Eligibility != EligibilityUnavailable || got.Rankable || got.FitBand != FitHardIncompatible {
		t.Fatalf("archived=%+v", got)
	}
}

func TestRolePolicyAndExperienceBands(t *testing.T) {
	teacher := baseVacancy()
	teacher.Name, teacher.Title, teacher.Description = "Преподаватель математики", "Преподаватель математики", "Ищем преподавателя математики"
	teacher.WorkFormat = "remote"
	teacherResult := evaluateFixture(t, teacher)
	if teacherResult.FitBand != FitUnlikely || !hasCode(teacherResult.Concerns, "role_fit_uncertain") {
		t.Fatalf("unrelated teacher role=%+v", teacherResult)
	}
	oneC := baseVacancy()
	oneC.Name, oneC.Title, oneC.Description, oneC.WorkFormat = "1C специалист", "1C специалист", "Только 1С сопровождение", "remote"
	got := evaluateFixture(t, oneC)
	if got.Eligibility != EligibilityIneligible || !hasCode(got.HardReasons, "role_1c_only") {
		t.Fatalf("1C=%+v", got)
	}
	sales := baseVacancy()
	sales.Title, sales.Description = "Менеджер", "Холодные звонки и cold sales"
	got = evaluateFixture(t, sales)
	if got.Eligibility != EligibilityIneligible || !hasCode(got.HardReasons, "role_sales_only") {
		t.Fatalf("sales=%+v", got)
	}
	mixed := baseVacancy()
	mixed.Title, mixed.Description = "Специалист поддержки", "Поддержка клиентов и продажи"
	got = evaluateFixture(t, mixed)
	if got.Eligibility != EligibilityReviewRequired || !hasCode(got.Concerns, "mixed_role_ambiguous") {
		t.Fatalf("mixed=%+v", got)
	}
	stretch := baseVacancy()
	stretch.WorkExperience = "between1And3"
	got = evaluateFixture(t, stretch)
	if got.FitBand != FitStretch || !hasCode(got.Concerns, "experience_stretch") {
		t.Fatalf("stretch=%+v", got)
	}
	exact := baseVacancy()
	exact.WorkExperience, exact.Description = "", "Django developer, 3+ years commercial Django experience"
	got = evaluateFixture(t, exact)
	if got.Eligibility != EligibilityIneligible || got.FitBand != FitHardIncompatible || !hasCode(got.HardReasons, "experience_specialist_gap") {
		t.Fatalf("exact specialist gap=%+v", got)
	}
}

func TestSkillsSalaryUnknownAndSynonyms(t *testing.T) {
	value := baseVacancy()
	value.Skills = []string{"React.js", "Postgres", "REST API"}
	value.Description = "React.js, Postgres and REST API"
	got := evaluateFixture(t, value)
	if got.Components.SkillFit <= 0 || !hasCode(got.Unknowns, "skill_unknown") {
		t.Fatalf("skill synonym/unknown=%+v", got)
	}
	withoutSalary := value
	withoutSalary.Compensation = vacancy.Compensation{}
	got = evaluateFixture(t, withoutSalary)
	if !hasCode(got.Unknowns, "salary_unknown") || got.Components.SalaryFit != 5 {
		t.Fatalf("salary unknown=%+v", got)
	}
	compatibleSalary := value
	compatibleSalary.Compensation = vacancy.Compensation{From: intPtr(50000), Currency: "RUR"}
	got2 := evaluateFixture(t, compatibleSalary)
	if got2.Components.SalaryFit != 10 || got2.Components.SkillFit != got.Components.SkillFit {
		t.Fatalf("salary comparison got=%+v other=%+v", got, got2)
	}
}

func TestReviewApplicationAndFreshnessSemantics(t *testing.T) {
	value := baseVacancy()
	result := evaluateFixture(t, value)
	if !result.Rankable || result.ReviewState != vacancyreview.StateUnseen {
		t.Fatalf("unseen=%+v", result)
	}
	changed := true
	result = (Evaluator{}).Evaluate(EvaluateInput{Vacancy: value, Candidate: mustCandidateContext(t), Effective: vacancyreview.EffectiveState{VacancyID: 1, State: vacancyreview.StateDismissed, ChangedSinceReview: &changed}, Now: rankingAt})
	if !result.Rankable || !hasCode(result.PositiveReasons, "material_change_reconsideration") {
		t.Fatalf("dismissed changed=%+v", result)
	}
	result = (Evaluator{}).Evaluate(EvaluateInput{Vacancy: value, Candidate: mustCandidateContext(t), Effective: vacancyreview.EffectiveState{VacancyID: 1, State: vacancyreview.StateDismissed}, Now: rankingAt})
	if result.Rankable || result.ExclusionCode != "dismissed" {
		t.Fatalf("dismissed=%+v", result)
	}
	result = (Evaluator{}).Evaluate(EvaluateInput{Vacancy: value, Candidate: mustCandidateContext(t), Effective: vacancyreview.EffectiveState{VacancyID: 1, ApplicationLinked: true}, Now: rankingAt})
	if result.Rankable || result.ExclusionCode != "already_application_linked" || result.Eligibility == EligibilityUnavailable {
		t.Fatalf("application-linked=%+v", result)
	}
	first := evaluateFixture(t, value)
	second := evaluateFixture(t, value)
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("evaluation is not deterministic:\nfirst=%+v\nsecond=%+v", first, second)
	}
}

func mustCandidateContext(t *testing.T) CandidateContext {
	t.Helper()
	value, err := NewCandidateContext(rankingCandidate())
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func hasCode(values []Reason, code string) bool {
	for _, value := range values {
		if value.Code == code {
			return true
		}
	}
	return false
}

type countingVacancyReader struct {
	values []vacancy.Vacancy
	calls  int
}

func (r *countingVacancyReader) List(context.Context, ports.VacancyQuery) ([]vacancy.Vacancy, error) {
	r.calls++
	return append([]vacancy.Vacancy{}, r.values...), nil
}
func (r *countingVacancyReader) Get(context.Context, int) (vacancy.Vacancy, error) {
	return vacancy.Vacancy{}, nil
}
func (r *countingVacancyReader) GetByExternalID(context.Context, string) (vacancy.Vacancy, error) {
	return vacancy.Vacancy{}, nil
}

type countingCandidateReader struct {
	value candidate.Candidate
	calls int
}

func (r *countingCandidateReader) CurrentCandidate(context.Context) (candidate.Candidate, error) {
	r.calls++
	return r.value, nil
}

type countingApplicationReader struct{ calls int }

func (r *countingApplicationReader) List(context.Context) ([]application.JobApplication, error) {
	r.calls++
	return nil, nil
}
func (r *countingApplicationReader) Get(context.Context, string) (application.JobApplication, error) {
	return application.JobApplication{}, nil
}
func (r *countingApplicationReader) GetByExternalID(context.Context, string) (application.JobApplication, error) {
	return application.JobApplication{}, nil
}
func (r *countingApplicationReader) Timeline(context.Context, string) ([]application.Event, error) {
	return nil, nil
}

type countingReviewStore struct{ freshness, states int }

func (r *countingReviewStore) GetFreshness(context.Context, int) (vacancy.Freshness, error) {
	return vacancy.Freshness{}, nil
}
func (r *countingReviewStore) ListFreshness(_ context.Context, ids []int) (map[int]vacancy.Freshness, error) {
	r.freshness++
	return map[int]vacancy.Freshness{}, nil
}
func (r *countingReviewStore) GetReviewState(context.Context, int) (vacancyreview.ReviewState, error) {
	return vacancyreview.ReviewState{}, vacancyreview.ErrReviewStateNotFound
}
func (r *countingReviewStore) ListReviewStates(_ context.Context, ids []int) (map[int]vacancyreview.ReviewState, error) {
	r.states++
	return map[int]vacancyreview.ReviewState{}, nil
}
func (r *countingReviewStore) RecordReviewAction(context.Context, vacancyreview.Action) error {
	return nil
}
func (r *countingReviewStore) ListReviewEvents(context.Context, int) ([]vacancyreview.Event, error) {
	return nil, nil
}

func TestReadModelUsesBoundedReadGroups(t *testing.T) {
	vacancies := &countingVacancyReader{values: make([]vacancy.Vacancy, 10)}
	for i := range vacancies.values {
		vacancies.values[i] = baseVacancy()
		vacancies.values[i].ID = i + 1
	}
	candidateReader := &countingCandidateReader{value: rankingCandidate()}
	applications := &countingApplicationReader{}
	reviews := &countingReviewStore{}
	readModel := ReadModel{Vacancies: vacancies, Candidate: candidateReader, Applications: applications, Reviews: reviews, Now: func() time.Time { return rankingAt }}
	results, err := readModel.ListRankedVacancies(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 10 || vacancies.calls != 1 || candidateReader.calls != 1 || applications.calls != 1 || reviews.freshness != 1 || reviews.states != 1 {
		t.Fatalf("bounded reads results=%d vacancy=%d candidate=%d apps=%d freshness=%d states=%d", len(results), vacancies.calls, candidateReader.calls, applications.calls, reviews.freshness, reviews.states)
	}
}
