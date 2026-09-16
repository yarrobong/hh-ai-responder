package vacancyranking

// GoldLabel is a privacy-safe evaluation record. It stores only an ID,
// broad human label, and a hard-ineligible marker; vacancy descriptions do
// not belong in committed evaluation fixtures.
type GoldLabel struct {
	VacancyID       int
	Relevant        bool
	ExpectedFitBand FitBand
	HardIneligible  bool
	Notes           string
}

type GoldMetrics struct {
	Labeled             int
	PrecisionAt5        float64
	PrecisionAt10       float64
	HardIneligibleTop10 int
}

// EvaluateGoldSet computes small offline diagnostics once labels exist. An
// unlabeled result is ignored; an excluded application is still eligible for
// hard-ineligible monitoring if it appears in the provided order.
func EvaluateGoldSet(ranked []Result, labels []GoldLabel) GoldMetrics {
	byID := make(map[int]GoldLabel, len(labels))
	for _, label := range labels {
		byID[label.VacancyID] = label
	}
	metrics := GoldMetrics{}
	metrics.Labeled = len(byID)
	metrics.PrecisionAt5 = precisionAt(ranked, byID, 5)
	metrics.PrecisionAt10 = precisionAt(ranked, byID, 10)
	for i, value := range ranked {
		if i >= 10 {
			break
		}
		if label, ok := byID[value.Vacancy.ID]; ok && label.HardIneligible {
			metrics.HardIneligibleTop10++
		}
	}
	return metrics
}

func precisionAt(ranked []Result, labels map[int]GoldLabel, limit int) float64 {
	if limit > len(ranked) {
		limit = len(ranked)
	}
	seen, relevant := 0, 0
	for _, value := range ranked[:limit] {
		label, ok := labels[value.Vacancy.ID]
		if !ok {
			continue
		}
		seen++
		if label.Relevant {
			relevant++
		}
	}
	if seen == 0 {
		return 0
	}
	return float64(relevant) / float64(seen)
}
