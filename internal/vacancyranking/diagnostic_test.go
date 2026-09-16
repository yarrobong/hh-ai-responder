package vacancyranking_test

import (
	"context"
	"fmt"
	"os"
	"sort"
	"testing"
	"time"

	jsonstorage "hh-ai-responder/internal/adapters/storage/json"
	postgresstorage "hh-ai-responder/internal/adapters/storage/postgres"
	app "hh-ai-responder/internal/runtime"
	"hh-ai-responder/internal/vacancyranking"
)

// TestCurrentDatasetDiagnostic is opt-in: it reads the checked-out local
// stores and prints only ranking metadata. It never writes a store or calls HH.
func TestCurrentDatasetDiagnostic(t *testing.T) {
	if os.Getenv("P1_2_DATASET_DIAGNOSTIC") != "1" {
		t.Skip("P1_2_DATASET_DIAGNOSTIC is not enabled")
	}
	ctx := context.Background()
	vacancies := jsonstorage.NewVacancyRepository("vacancies.json")
	if err := vacancies.Load(); err != nil {
		t.Fatal(err)
	}
	applications := jsonstorage.NewApplicationRepository("job_applications.json")
	if err := applications.Load(); err != nil {
		t.Fatal(err)
	}
	candidates := jsonstorage.NewCandidateRepository(jsonstorage.CandidateRepositoryConfig{ProfilePath: "candidate_profile.json", StoriesPath: "candidate_stories.json", CandidateID: "candidate-local"})
	readModel := vacancyranking.ReadModel{Vacancies: vacancies, Candidate: candidates, Applications: applications, Now: func() time.Time { return time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC) }}
	started := time.Now()
	values, err := readModel.ListRankedVacancies(ctx)
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	fitCounts := map[string]int{}
	confidenceCounts := map[string]int{}
	analysisCounts := map[string]int{}
	unknownCounts := make([]int, 0, len(values))
	sectionCounts := map[vacancyranking.QueueSection]int{}
	rankable, applicationExcluded := 0, 0
	for _, value := range values {
		counts[string(value.Eligibility)]++
		fitCounts[string(value.FitBand)]++
		confidenceCounts[string(value.Confidence)]++
		analysisCounts[string(value.AnalysisState)]++
		unknownCounts = append(unknownCounts, len(value.Unknowns))
		sectionCounts[vacancyranking.SectionFor(value)]++
		if value.Rankable {
			rankable++
		}
		if value.ExclusionCode == "already_application_linked" {
			applicationExcluded++
		}
	}
	averageUnknowns, medianUnknowns := unknownSummary(unknownCounts)
	t.Logf("dataset vacancies=%d elapsed=%s rankable=%d application_excluded=%d eligibility=%v fit=%v confidence=%v analysis=%v average_unknown_count=%.2f median_unknown_count=%d to_review=%d stretch_manual_review=%d", len(values), time.Since(started), rankable, applicationExcluded, counts, fitCounts, confidenceCounts, analysisCounts, averageUnknowns, medianUnknowns, sectionCounts[vacancyranking.SectionToReview], sectionCounts[vacancyranking.SectionStretchManual])
	top := 20
	if len(values) < top {
		top = len(values)
	}
	for i := 0; i < top; i++ {
		value := values[i]
		t.Logf("top[%02d] id=%d title=%q company=%q eligibility=%s fit=%s score=%d confidence=%s rankable=%t review=%s application_linked=%t reasons=%s concerns=%s unknowns=%d", i+1, value.Vacancy.ID, value.Vacancy.Title, value.Vacancy.Company.Name, value.Eligibility, value.FitBand, value.BaseRankScore, value.Confidence, value.Rankable, value.ReviewState, value.ApplicationLinked, reasonCodes(value.PositiveReasons), reasonCodes(value.Concerns), len(value.Unknowns))
	}
	return
}

// TestPostgresCurrentDatasetDiagnostic is the acceptance counterpart for the
// canonical configured dataset. It is opt-in and read-only apart from the
// idempotent migration runner.
func TestPostgresCurrentDatasetDiagnostic(t *testing.T) {
	if os.Getenv("P1_2_POSTGRES_DIAGNOSTIC") != "1" {
		t.Skip("P1_2_POSTGRES_DIAGNOSTIC is not enabled")
	}
	dsn := os.Getenv("POSTGRES_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("POSTGRES_TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	pool, err := app.OpenPostgres(ctx, app.PostgresConfig{DatabaseURL: dsn})
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err := app.ApplyPostgresMigrations(ctx, pool); err != nil {
		t.Fatal(err)
	}
	readModel := vacancyranking.ReadModel{
		Vacancies: postgresstorage.NewVacancyRepository(pool), Candidate: postgresstorage.NewCandidateRepositoryForID(pool, "candidate-local"),
		Applications: postgresstorage.NewApplicationRepository(pool), Reviews: postgresstorage.NewVacancyReviewRepository(pool),
		Now: func() time.Time { return time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC) },
	}
	started := time.Now()
	values, err := readModel.ListRankedVacancies(ctx)
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	fitCounts := map[string]int{}
	confidenceCounts := map[string]int{}
	analysisCounts := map[string]int{}
	unknownCounts := make([]int, 0, len(values))
	sectionCounts := map[vacancyranking.QueueSection]int{}
	rankable, applicationExcluded := 0, 0
	for _, value := range values {
		counts[string(value.Eligibility)]++
		fitCounts[string(value.FitBand)]++
		confidenceCounts[string(value.Confidence)]++
		analysisCounts[string(value.AnalysisState)]++
		unknownCounts = append(unknownCounts, len(value.Unknowns))
		sectionCounts[vacancyranking.SectionFor(value)]++
		if value.Rankable {
			rankable++
		}
		if value.ExclusionCode == "already_application_linked" {
			applicationExcluded++
		}
	}
	averageUnknowns, medianUnknowns := unknownSummary(unknownCounts)
	t.Logf("postgres dataset vacancies=%d elapsed=%s rankable=%d application_excluded=%d eligibility=%v fit=%v confidence=%v analysis=%v average_unknown_count=%.2f median_unknown_count=%d to_review=%d stretch_manual_review=%d", len(values), time.Since(started), rankable, applicationExcluded, counts, fitCounts, confidenceCounts, analysisCounts, averageUnknowns, medianUnknowns, sectionCounts[vacancyranking.SectionToReview], sectionCounts[vacancyranking.SectionStretchManual])
	top := 20
	if len(values) < top {
		top = len(values)
	}
	for i := 0; i < top; i++ {
		value := values[i]
		t.Logf("postgres top[%02d] id=%d title=%q company=%q eligibility=%s fit=%s score=%d confidence=%s rankable=%t review=%s application_linked=%t reasons=%s concerns=%s unknowns=%d", i+1, value.Vacancy.ID, value.Vacancy.Title, value.Vacancy.Company.Name, value.Eligibility, value.FitBand, value.BaseRankScore, value.Confidence, value.Rankable, value.ReviewState, value.ApplicationLinked, reasonCodes(value.PositiveReasons), reasonCodes(value.Concerns), len(value.Unknowns))
	}
}

func unknownSummary(values []int) (float64, int) {
	if len(values) == 0 {
		return 0, 0
	}
	sorted := append([]int(nil), values...)
	sort.Ints(sorted)
	total := 0
	for _, value := range sorted {
		total += value
	}
	median := sorted[len(sorted)/2]
	if len(sorted)%2 == 0 {
		median = (sorted[len(sorted)/2-1] + sorted[len(sorted)/2]) / 2
	}
	return float64(total) / float64(len(sorted)), median
}

func TestPostgresQueueDiagnostic(t *testing.T) {
	if os.Getenv("P1_3_QUEUE_DIAGNOSTIC") != "1" {
		t.Skip("P1_3_QUEUE_DIAGNOSTIC is not enabled")
	}
	dsn := os.Getenv("POSTGRES_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("POSTGRES_TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	pool, err := app.OpenPostgres(ctx, app.PostgresConfig{DatabaseURL: dsn})
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err := app.ApplyPostgresMigrations(ctx, pool); err != nil {
		t.Fatal(err)
	}
	queue := vacancyranking.QueueService{ReadModel: vacancyranking.ReadModel{
		Vacancies: postgresstorage.NewVacancyRepository(pool), Candidate: postgresstorage.NewCandidateRepositoryForID(pool, "candidate-local"),
		Applications: postgresstorage.NewApplicationRepository(pool), Reviews: postgresstorage.NewVacancyReviewRepository(pool),
		Now: func() time.Time { return time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC) },
	}, DefaultPageSize: 25, MaxPageSize: 100}
	page, err := queue.List(ctx, vacancyranking.QueueFilter{Limit: 25})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("queue total=%d first_page=%d counts=%+v payload_items=%d algorithm=%s", page.Total, len(page.Items), page.Counts, len(page.Items), page.Algorithm)
}

func reasonCodes(values []vacancyranking.Reason) string {
	result := ""
	for i, value := range values {
		if i > 0 {
			result += ","
		}
		result += fmt.Sprintf("%s:%s", value.Code, value.Source)
	}
	return result
}
