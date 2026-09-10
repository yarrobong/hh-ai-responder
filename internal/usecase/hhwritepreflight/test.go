package hhwritepreflight

import "fmt"

// CompareTestMetadata compares the exact task and option identity used by an
// already prepared atomic response. It never remaps an answer to a changed
// task or option.
func CompareTestMetadata(vacancyID int, expected, current TestMetadata) TestResult {
	result := TestResult{Status: StatusPassed, Operation: "vacancy_test_metadata", VacancyID: vacancyID}
	if expected.UIDPK != current.UIDPK || expected.GUID != current.GUID || expected.StartTime != current.StartTime || expected.Required != current.Required {
		result.Status = StatusStale
		result.Reasons = append(result.Reasons, "vacancy test identity changed")
	}
	if len(expected.Tasks) != len(current.Tasks) {
		result.Status = StatusStale
		result.Reasons = append(result.Reasons, "vacancy test task set changed")
		return result
	}
	for i := range expected.Tasks {
		want, got := expected.Tasks[i], current.Tasks[i]
		if want.ID != got.ID {
			result.Status = StatusStale
			result.Reasons = append(result.Reasons, fmt.Sprintf("vacancy test task changed at position %d", i))
			continue
		}
		if want.Open != got.Open || !sameStrings(want.ChoiceIDs, got.ChoiceIDs) {
			result.Status = StatusStale
			result.Reasons = append(result.Reasons, fmt.Sprintf("vacancy test options changed for task %d", want.ID))
		}
	}
	return result
}

func sameStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
