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

func containsRoleFamily(values []RoleFamily, want RoleFamily) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
