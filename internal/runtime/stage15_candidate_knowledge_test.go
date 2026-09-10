package runtime

import (
	"strings"
	"testing"
	"time"
)

func stage15Profile() CandidateProfile {
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	confirmed := confirmedProfileFact(CandidateSourceUserConfirmed, now)
	profile := NewCandidateProfile(now)
	profile.Skills = []CandidateSkill{{Name: "Django", Level: SkillLevelAdvanced, ProfileFact: confirmed}, {Name: "Docker", Level: SkillLevelWorking, ProfileFact: confirmed}}
	profile.Projects = []ProjectFact{{Name: "BizonVR", Role: "developer", Description: "Backend service", Technologies: []string{"Python", "Django"}, ProfileFact: confirmed}}
	profile.TotalExperienceMonths = ProfileIntFact{Value: 11, ProfileFact: confirmed}
	profile.Education = []EducationFact{{Level: "СПО", Specialty: "Информационные системы", Details: "подтверждено", ProfileFact: confirmed}}
	profile.Languages = []LanguageFact{{Name: "English", Level: "B2", ProfileFact: confirmed}}
	profile.WorkPreferences.WorkMode = ProfileStringFact{Value: "Удалёнка по России и миру, офис Екатеринбург", ProfileFact: confirmed}
	profile.WorkPreferences.Relocation = ProfileStringFact{Value: "Не готов", ProfileFact: confirmed}
	profile.WorkPreferences.BusinessTrips = ProfileStringFact{Value: "Не готов", ProfileFact: confirmed}
	profile.WorkPreferences.PrimaryRoles = ProfileStringFact{Value: "Backend Developer", ProfileFact: confirmed}
	profile.WorkPreferences.SalaryMinimum = ProfileIntFact{Value: 40000, ProfileFact: confirmed}
	profile.EmployerCommunicationPreferences.Salary = ProfileStringFact{Value: "Минимум 40-50 тыс., цель 50-100 тыс.", ProfileFact: confirmed}
	return profile
}

func TestStage15ResolverUsesLegacyProfileAndProjectEvidence(t *testing.T) {
	kb := NewCandidateKnowledgeBase("")
	kb.Profile = stage15Profile()
	kb.Projects = []CandidateProject{{ID: "bizon", Name: "BizonVR", Technologies: []string{"Django"}, Tasks: []string{"Backend development"}, KnowledgeMetadata: knowledgeTestMetadata(KnowledgeSourceUserConfirmed, TruthStatusConfirmed)}}
	resolver := NewCandidateContextResolver(kb)

	answer, err := resolver.ResolveForEmployerMessage("Есть опыт Django?", nil)
	requireKnowledgeOK(t, err)
	if answer.MessageIntent != EmployerMessageIntentFactualQuestion || len(answer.MissingInformation) != 0 || len(answer.ResolvedFacts) == 0 || answer.ResolvedFacts[0].Status != ResolvedFactAnswerable {
		t.Fatalf("Django fact was not resolved from profile: %+v", answer)
	}

	detail, err := resolver.ResolveForEmployerMessage("Что делали с Django?", nil)
	requireKnowledgeOK(t, err)
	if len(detail.MissingInformation) != 0 || len(detail.RelevantProjects) != 1 || len(detail.ResolvedFacts) == 0 || detail.ResolvedFacts[0].Status != ResolvedFactAnswerable {
		t.Fatalf("Django project evidence was not used: %+v", detail)
	}

	partial, err := resolver.ResolveForEmployerMessage("Сколько лет Docker в production?", nil)
	requireKnowledgeOK(t, err)
	if len(partial.PartiallyResolvedFacts) == 0 || partial.PartiallyResolvedFacts[0].Status != ResolvedFactPartiallyAnswerable || len(partial.UnknownAtomicFacts) != 0 {
		t.Fatalf("Docker qualifier was not separated from base skill: %+v", partial)
	}
}

func TestStage15StructuredProfileFactsAndIntent(t *testing.T) {
	kb := NewCandidateKnowledgeBase("")
	kb.Profile = stage15Profile()
	resolver := NewCandidateContextResolver(kb)
	for _, query := range []string{"Какая зарплата?", "Готовы к релокации?", "Какой формат работы?", "Какое образование и английский?", "Сколько месяцев общего опыта?"} {
		context, err := resolver.ResolveForEmployerMessage(query, nil)
		requireKnowledgeOK(t, err)
		if len(context.MissingInformation) != 0 {
			t.Fatalf("structured profile fact became a clarification for %q: %+v", query, context.MissingInformation)
		}
	}
	for _, query := range []string{"Спасибо, будем на связи", "Приглашаем на собеседование завтра", "К сожалению, выбрали другого кандидата"} {
		context, err := resolver.ResolveForEmployerMessage(query, nil)
		requireKnowledgeOK(t, err)
		if len(context.MissingInformation) != 0 || len(context.UnknownAtomicFacts) != 0 {
			t.Fatalf("non-question intent created candidate input for %q: %+v", query, context)
		}
	}
	instruction, err := resolver.ResolveForEmployerMessage("Вы внимательно ознакомились с вакансией и ее условиями? Если ознакомились - напишите именно \"Да\"", nil)
	requireKnowledgeOK(t, err)
	if !instruction.UserConfirmationRequired || len(instruction.MissingInformation) != 1 || len(instruction.UnknownAtomicFacts) != 0 {
		t.Fatalf("instruction did not require explicit candidate confirmation: %+v", instruction)
	}
	unknown, err := resolver.ResolveForEmployerMessage("Есть ли опыт BGP?", nil)
	requireKnowledgeOK(t, err)
	if unknown.UnsupportedIntent || len(unknown.UnknownAtomicFacts) != 1 || len(unknown.MissingInformation) != 1 {
		t.Fatalf("explicit unsupported topic should be one atomic clarification, not generic fallback: %+v", unknown)
	}
}

func TestStage15ProfileSyncIsIdempotent(t *testing.T) {
	kb := NewCandidateKnowledgeBase("")
	kb.Profile = stage15Profile()
	first, err := kb.SyncProfileKnowledge()
	requireKnowledgeOK(t, err)
	if first.MigratedSkills != 2 || first.MigratedProjects != 1 || len(kb.Events) != 3 {
		t.Fatalf("first sync did not migrate confirmed profile data: %+v events=%d", first, len(kb.Events))
	}
	second, err := kb.SyncProfileKnowledge()
	requireKnowledgeOK(t, err)
	if second.MigratedSkills != 0 || second.MigratedProjects != 0 || len(kb.Skills) != 2 || len(kb.Projects) != 1 || len(kb.Events) != 3 {
		t.Fatalf("second sync was not idempotent: %+v skills=%d projects=%d events=%d", second, len(kb.Skills), len(kb.Projects), len(kb.Events))
	}
	for _, skill := range kb.Skills {
		if !strings.Contains(skill.Name, "Django") && !strings.Contains(skill.Name, "Docker") {
			t.Fatalf("unexpected migrated skill: %+v", skill)
		}
	}
}
