package runtime

import "testing"

func TestCareerAgentAccountingRequiresExactlyOneTerminalOutcomePerUniqueVacancy(t *testing.T) {
	unique := []int{1, 2, 3}
	outcomes := map[int]CareerAgentVacancyResult{
		1: {VacancyID: 1, TerminalOutcome: TerminalDeterministicReject},
		2: {VacancyID: 2, TerminalOutcome: TerminalAIReject},
		3: {VacancyID: 3, TerminalOutcome: TerminalReviewRequired},
	}
	if err := validateCareerAgentAccounting(unique, outcomes); err != nil {
		t.Fatalf("valid accounting rejected: %v", err)
	}
	for name, broken := range map[string]map[int]CareerAgentVacancyResult{
		"missing": {1: outcomes[1], 2: outcomes[2]},
		"extra":   {1: outcomes[1], 2: outcomes[2], 3: outcomes[3], 4: {VacancyID: 4, TerminalOutcome: TerminalError}},
		"empty":   {1: outcomes[1], 2: outcomes[2], 3: {VacancyID: 3}},
	} {
		t.Run(name, func(t *testing.T) {
			if err := validateCareerAgentAccounting(unique, broken); err == nil {
				t.Fatal("broken accounting was accepted")
			}
		})
	}
}

func TestCareerAgentAccountingRejectsDuplicateUniqueIDs(t *testing.T) {
	outcomes := map[int]CareerAgentVacancyResult{1: {VacancyID: 1, TerminalOutcome: TerminalError}}
	if err := validateCareerAgentAccounting([]int{1, 1}, outcomes); err == nil {
		t.Fatal("duplicate unique vacancy was accepted")
	}
}

func TestCareerAgentModeCapsCanaryAndOverridesShadow(t *testing.T) {
	canary := Config{HHWriteEnabled: true, HHMaxWritesPerRun: 9, HHMaxWritesPerDay: 9, MaxApplicationsPerRun: 8}
	if err := configureCareerAgentMode(&canary, "canary"); err != nil {
		t.Fatal(err)
	}
	if canary.HHMaxWritesPerRun != 1 || canary.HHMaxWritesPerDay != 3 || canary.MaxApplicationsPerRun != 1 || canary.AutoChat || canary.AutoTouch || canary.AutoJobStatus {
		t.Fatalf("canary caps or write isolation are wrong: %+v", canary)
	}
	shadow := Config{DryRun: false, HHWriteEnabled: true, AutoChat: true, AutoTouch: true, AutoJobStatus: true}
	if err := configureCareerAgentMode(&shadow, "shadow"); err != nil {
		t.Fatal(err)
	}
	if !shadow.DryRun || shadow.HHWriteEnabled || shadow.AutoChat || shadow.AutoTouch || shadow.AutoJobStatus || shadow.AutoApplyMode != "off" {
		t.Fatalf("shadow did not override conflicting flags: %+v", shadow)
	}
}
