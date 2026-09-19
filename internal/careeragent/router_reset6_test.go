package careeragent

import (
	"strings"
	"testing"
)

func TestClassifyVacancyRoleRecognizesStrongPythonBackendTitle(t *testing.T) {
	evidence := ClassifyVacancyRole(VacancyInput{
		Title:          "Python-разработчик backend",
		RequiredSkills: []string{"Python", "Django", "PostgreSQL"},
		Description:    "Разработка backend-сервисов и REST API",
	}, nil)

	python := roleFamilyEvidence(evidence, RoleFamilyPythonBackend)
	if python == nil || python.Strength != RoleEvidenceStrong {
		t.Fatalf("Python/backend title was not strong evidence: %+v", evidence)
	}
	if len(python.TitleAnchors) == 0 {
		t.Fatalf("strong title anchor was not recorded: %+v", python)
	}
	if len(python.SpecificSignals) == 0 {
		t.Fatalf("specific supporting signals were not recorded: %+v", python)
	}
}

func TestClassifyVacancyRoleRecognizesTechnicalSupportTitle(t *testing.T) {
	evidence := ClassifyVacancyRole(VacancyInput{
		Title:          "Специалист технической поддержки",
		RequiredSkills: []string{"Диагностика неисправностей", "Linux"},
		Description:    "Первая линия поддержки пользователей и разбор обращений",
	}, nil)

	support := roleFamilyEvidence(evidence, RoleFamilyTechSupport)
	if support == nil || support.Strength != RoleEvidenceStrong {
		t.Fatalf("support title was not strong evidence: %+v", evidence)
	}
	if len(support.TitleAnchors) == 0 || len(support.SpecificSignals) == 0 {
		t.Fatalf("support evidence was not explainable: %+v", support)
	}
}

func TestClassifyVacancyRoleDoesNotTreatGenericWordsAsStrongAnchors(t *testing.T) {
	for _, title := range []string{"Инженер", "Специалист", "Разработчик"} {
		evidence := ClassifyVacancyRole(VacancyInput{Title: title}, nil)
		if len(evidence.StrongFamilies) != 0 {
			t.Fatalf("generic title %q created strong family evidence: %+v", title, evidence)
		}
	}
}

func TestClassifyVacancyRoleIgnoresCompanyTextForStrongEvidence(t *testing.T) {
	evidence := ClassifyVacancyRole(VacancyInput{
		Title:       "Инженер",
		Description: "Компания ООО Системный дом. Работа с клиентами и документами.",
	}, nil)
	if len(evidence.StrongFamilies) != 0 {
		t.Fatalf("generic company text created strong family evidence: %+v", evidence)
	}
}

func TestClassifyVacancyRoleRecognizesSysadminOnlyAsVacancyEvidence(t *testing.T) {
	evidence := ClassifyVacancyRole(VacancyInput{
		Title:          "Системный администратор Linux",
		RequiredSkills: []string{"Linux", "DNS", "Active Directory"},
		Description:    "Администрирование серверов и сетевой инфраструктуры",
	}, []ResumeProfile{{ID: "python", Title: "Python backend", Skills: []string{"Python"}, Enabled: true}})

	sysadmin := roleFamilyEvidence(evidence, RoleFamilySystemAdmin)
	if sysadmin == nil || sysadmin.Strength != RoleEvidenceStrong {
		t.Fatalf("unsupported sysadmin family was hidden by enabled resumes: %+v", evidence)
	}
	if !containsRoleFamily(evidence.StrongFamilies, RoleFamilySystemAdmin) {
		t.Fatalf("sysadmin family was not classified independently: %+v", evidence)
	}
}

func TestDeriveResumeIdentityBuildsRuntimeRoleFamilies(t *testing.T) {
	identity := DeriveResumeIdentity(ResumeProfile{
		Title:   "Backend-разработчик (Python/Django) / автоматизация и интеграции",
		Skills:  []string{"Python", "Django", "REST API", "Автоматизация процессов", "API-интеграции"},
		Enabled: true,
	})

	if !containsRoleFamily(identity.PrimaryRoleFamilies, RoleFamilyPythonBackend) {
		t.Fatalf("Python/backend family missing from identity: %+v", identity)
	}
	if !containsRoleFamily(identity.SecondaryRoleFamilies, RoleFamilyAutomationIntegrations) {
		t.Fatalf("automation/integration family missing from identity: %+v", identity)
	}
	if len(identity.StrongPositiveAnchors) == 0 || len(identity.CoreSkills) == 0 {
		t.Fatalf("routing profile lacks strong anchors/core skills: %+v", identity)
	}
}

func TestDeriveResumeIdentityKeepsGenericAndSpecificAnchorsSeparate(t *testing.T) {
	identity := DeriveResumeIdentity(ResumeProfile{
		Title:  "Разработчик",
		Skills: []string{"Python", "Git", "API", "Linux"},
	})

	joinedStrong := strings.ToLower(strings.Join(identity.StrongPositiveAnchors, " "))
	joinedGeneric := strings.ToLower(strings.Join(identity.GenericAnchors, " "))
	if !strings.Contains(joinedStrong, "python") {
		t.Fatalf("specific Python anchor was not retained: %+v", identity)
	}
	if strings.Contains(joinedStrong, "git") || strings.Contains(joinedStrong, "api") || strings.Contains(joinedStrong, "linux") {
		t.Fatalf("generic/supporting anchors were promoted to strong: %+v", identity)
	}
	if !strings.Contains(joinedGeneric, "git") && !strings.Contains(joinedGeneric, "api") && !strings.Contains(joinedGeneric, "linux") {
		t.Fatalf("generic anchors were not separated: %+v", identity)
	}
}

func TestDeriveResumeIdentityDerivesNegativeMismatchSignalsOnlyFromTrustedFields(t *testing.T) {
	identity := DeriveResumeIdentity(ResumeProfile{
		Title:           "Python backend developer",
		Skills:          []string{"Python", "Django"},
		ExcludeKeywords: []string{"Flutter", "Vue-only frontend"},
	})

	joined := strings.ToLower(strings.Join(identity.NegativeMismatchAnchors, " "))
	if !strings.Contains(joined, "flutter") || !strings.Contains(joined, "vue-only frontend") {
		t.Fatalf("trusted negative anchors were not retained: %+v", identity)
	}
}

func roleFamilyEvidence(evidence VacancyRoleEvidence, family RoleFamily) *RoleFamilyEvidence {
	for index := range evidence.Families {
		if evidence.Families[index].Family == family {
			return &evidence.Families[index]
		}
	}
	return nil
}

func TestRouteResumeSelectsPythonBackendFromStrongRoleEvidence(t *testing.T) {
	resumes := []ResumeProfile{
		{ID: "python", Title: "Backend-разработчик (Python/Django)", Skills: []string{"Python", "Django", "PostgreSQL"}, Enabled: true},
		{ID: "automation", Title: "Automation / Integration specialist", Skills: []string{"API", "SQL", "Linux"}, Enabled: true},
		{ID: "support", Title: "Technical support", Skills: []string{"SQL", "Linux"}, Enabled: true},
	}
	decision := RouteResume(VacancyInput{
		ID:             601,
		Title:          "Python-разработчик backend",
		RequiredSkills: []string{"Python", "Django"},
		Description:    "Разработка Python Django backend-сервисов",
	}, resumes)

	if decision.Status != RouteSelected || decision.SelectedResumeID != "python" || decision.ReasonCode != "ROUTE_SELECTED" {
		t.Fatalf("strong Python/backend evidence did not select Python resume: %+v", decision)
	}
	selected := resumeScoreByID(decision, "python")
	if selected == nil || !containsRoleFamily(selected.MatchedRoleFamilies, RoleFamilyPythonBackend) || selected.SpecificEvidenceCount == 0 {
		t.Fatalf("selected score lacks Python role/specific evidence: %+v", selected)
	}
}

func TestRouteResumeSelectsSupportWithoutGenericCollision(t *testing.T) {
	resumes := []ResumeProfile{
		{ID: "python", Title: "Python backend developer", Skills: []string{"Python", "Django", "SQL", "Linux", "API"}, Enabled: true},
		{ID: "support", Title: "Technical support", Skills: []string{"Technical Support", "Diagnostics", "SQL", "Linux"}, Enabled: true},
	}
	decision := RouteResume(VacancyInput{
		ID:             602,
		Title:          "Специалист технической поддержки",
		RequiredSkills: []string{"Диагностика неисправностей", "Linux", "SQL"},
		Description:    "Первая линия поддержки пользователей и обработка обращений",
	}, resumes)

	if decision.Status != RouteSelected || decision.SelectedResumeID != "support" {
		t.Fatalf("support evidence was displaced by generic collision: %+v", decision)
	}
	support := resumeScoreByID(decision, "support")
	if support == nil || !containsRoleFamily(support.MatchedRoleFamilies, RoleFamilyTechSupport) || support.GenericEvidenceRatio >= 0.5 {
		t.Fatalf("support score is not evidence-aware: %+v", support)
	}
}

func TestRouteResumeSelectsSysadminWhenMatchingResumeIsEnabled(t *testing.T) {
	resumes := []ResumeProfile{
		{ID: "sysadmin", Title: "Linux administrator", Skills: []string{"Linux", "DNS", "Network"}, Enabled: true},
		{ID: "python", Title: "Python backend", Skills: []string{"Python", "REST API", "Linux"}, Enabled: true},
	}
	decision := RouteResume(VacancyInput{
		ID:             603,
		Title:          "Системный администратор Linux",
		RequiredSkills: []string{"Linux", "DNS"},
		Description:    "Администрирование серверов и сетевой инфраструктуры",
	}, resumes)

	if decision.Status != RouteSelected || decision.SelectedResumeID != "sysadmin" {
		t.Fatalf("matching sysadmin resume was not selected: %+v", decision)
	}
}

func TestRouteResumeDistinguishesOutOfScopeFromLowEvidence(t *testing.T) {
	resumes := []ResumeProfile{
		{ID: "python", Title: "Python backend", Skills: []string{"Python", "Django"}, Enabled: true},
		{ID: "support", Title: "Technical support", Skills: []string{"Technical Support"}, Enabled: true},
	}

	unsupported := RouteResume(VacancyInput{ID: 604, Title: "Системный администратор Linux", Description: "Linux DNS network administration"}, resumes)
	if unsupported.Status != RouteReviewRequired || unsupported.ReasonCode != "ROLE_OUT_OF_SCOPE" {
		t.Fatalf("unsupported role was not separated from low evidence: %+v", unsupported)
	}
	unclear := RouteResume(VacancyInput{ID: 605, Title: "IT specialist", Description: "Работа с задачами и документами"}, resumes)
	if unclear.Status != RouteReviewRequired || unclear.ReasonCode != "ROUTE_LOW_EVIDENCE" {
		t.Fatalf("unclear role was not classified as low evidence: %+v", unclear)
	}
}

func TestRouteResumeKeepsStrongMixedRoleAmbiguous(t *testing.T) {
	resumes := []ResumeProfile{
		{ID: "python", Title: "Python backend developer", Skills: []string{"Python", "Django", "REST API"}, Enabled: true},
		{ID: "support", Title: "Technical support", Skills: []string{"Technical Support", "Diagnostics", "SQL"}, Enabled: true},
	}
	decision := RouteResume(VacancyInput{
		ID:          606,
		Title:       "Python developer / technical support engineer",
		Description: "Разработка Python backend-сервисов, первая линия поддержки и диагностика обращений",
		KeySkills:   []string{"Python", "Django", "Technical Support", "Diagnostics"},
	}, resumes)

	if decision.Status != RouteReviewRequired || decision.ReasonCode != "ROUTE_AMBIGUOUS" || decision.SelectedResumeID != "" {
		t.Fatalf("strong mixed role was incorrectly forced to one resume: %+v", decision)
	}
}

func TestRouteResumeStrongRoleAnchorNeedsSpecificSupportingEvidence(t *testing.T) {
	decision := RouteResume(VacancyInput{ID: 607, Title: "Python developer"}, []ResumeProfile{
		{ID: "python", Title: "Python developer", Skills: []string{"Git", "Linux", "API"}, Enabled: true},
	})
	if decision.Status != RouteReviewRequired || decision.ReasonCode != "NO_SUITABLE_RESUME" {
		t.Fatalf("strong title without specific support was selected: %+v", decision)
	}
}

func TestRouteResumeGenericSkillsAloneNeverSelect(t *testing.T) {
	decision := RouteResume(VacancyInput{ID: 608, Title: "IT specialist", Description: "Git Linux SQL API"}, []ResumeProfile{
		{ID: "python", Title: "Python backend", Skills: []string{"Git", "Linux", "SQL", "API"}, Enabled: true},
		{ID: "support", Title: "Technical support", Skills: []string{"Git", "Linux", "SQL", "API"}, Enabled: true},
	})
	if decision.Status == RouteSelected || decision.ReasonCode != "ROUTE_LOW_EVIDENCE" {
		t.Fatalf("generic skills forced a selection: %+v", decision)
	}
}

func TestRouteResumeSupportedFamilyWithoutEvidenceFloorIsNoSuitable(t *testing.T) {
	decision := RouteResume(VacancyInput{ID: 609, Title: "Python backend"}, []ResumeProfile{
		{ID: "python", Title: "Python backend", Skills: []string{"Git", "Linux"}, Enabled: true},
	})
	if decision.Status != RouteReviewRequired || decision.ReasonCode != "NO_SUITABLE_RESUME" {
		t.Fatalf("supported family without evidence floor was not blocked: %+v", decision)
	}
}

func TestRouteResumeRejectsVueOnlyAndFlutterOnlyAsPythonBackendTargets(t *testing.T) {
	for _, fixture := range []struct {
		name  string
		title string
		desc  string
	}{
		{name: "vue", title: "Vue.js frontend developer", desc: "Vue.js Git API Linux frontend"},
		{name: "flutter", title: "Flutter developer", desc: "Flutter Dart Git API mobile application"},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			decision := RouteResume(VacancyInput{ID: 610, Title: fixture.title, Description: fixture.desc}, []ResumeProfile{
				{ID: "python", Title: "Python backend", Skills: []string{"Python", "Django", "Git", "API", "Linux"}, Enabled: true},
			})
			if decision.Status == RouteSelected || (decision.ReasonCode != "ROLE_OUT_OF_SCOPE" && decision.ReasonCode != "NO_SUITABLE_RESUME") {
				t.Fatalf("%s-only vacancy selected Python backend: %+v", fixture.name, decision)
			}
		})
	}
}

func TestRouteResumePreservesMarginsAndFullCandidateTelemetry(t *testing.T) {
	decision := RouteResume(VacancyInput{ID: 611, Title: "Python backend developer", Description: "Python Django PostgreSQL REST API"}, []ResumeProfile{
		{ID: "python", Title: "Python backend", Skills: []string{"Python", "Django", "PostgreSQL", "REST API"}, Enabled: true},
		{ID: "support", Title: "Technical support", Skills: []string{"SQL", "Linux"}, Enabled: true},
	})
	if len(decision.AlternativeScores) != 2 || decision.TopRawScore == 0 || decision.TopNormalizedScore == 0 {
		t.Fatalf("top candidate telemetry is incomplete: %+v", decision)
	}
	if decision.AbsoluteMargin <= 0 || decision.RelativeMargin <= 0 {
		t.Fatalf("margins were not preserved: %+v", decision)
	}
	if !decision.RoleEvidence.EvidenceAvailable {
		t.Fatalf("role evidence telemetry is missing: %+v", decision)
	}
	for _, candidate := range decision.AlternativeScores {
		if candidate.MatchedRoleFamilies == nil {
			t.Fatalf("candidate telemetry is incomplete: %+v", candidate)
		}
	}
}

func resumeScoreByID(decision RouteDecision, id string) *ResumeScore {
	for index := range decision.AlternativeScores {
		if decision.AlternativeScores[index].ResumeID == id {
			return &decision.AlternativeScores[index]
		}
	}
	return nil
}
