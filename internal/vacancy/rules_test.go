package vacancy_test

import (
	"testing"

	"hh-ai-responder/internal/vacancy"
)

func TestDeterministicRejectReason(t *testing.T) {
	amount := 90000
	value := vacancy.Vacancy{
		Name:         "Python backend developer",
		Area:         vacancy.NamedObject{Name: "Екатеринбург"},
		Company:      vacancy.Company{Name: "Example"},
		Compensation: vacancy.Compensation{To: &amount, Currency: "RUR"},
	}
	if got := vacancy.DeterministicRejectReason(value, "", 100000, "RUR", nil); got == "" {
		t.Fatal("expected salary rejection")
	}
	if got := vacancy.DeterministicRejectReason(value, "Работа с PHP", 0, "RUR", []string{" php "}); got != "exclude keyword: php" {
		t.Fatalf("keyword rejection: %q", got)
	}
	if got := vacancy.DeterministicRejectReason(value, "Python", 100000, "USD", nil); got != "" {
		t.Fatalf("different currency must not reject: %q", got)
	}
}
