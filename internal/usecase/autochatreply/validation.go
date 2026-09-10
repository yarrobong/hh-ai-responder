package autochatreply

import (
	"fmt"
	"regexp"
	"strings"

	"hh-ai-responder/internal/usecase/candidatecontext"
	"hh-ai-responder/internal/usecase/conversationpolicy"
	"hh-ai-responder/internal/usecase/employerreply"
)

var salaryClaimRE = regexp.MustCompile(`(?i)(зарплат|salary|компенсац|руб|₽|тыс|\$|€).*\d|\d.*(зарплат|salary|компенсац|руб|₽|тыс|\$|€)`)

// Review returns a deterministic manual-review reason for generated text.
// The model is never allowed to review its own output.
func Review(input Input, generated string) string {
	for _, source := range []struct {
		name string
		text string
	}{
		{name: "last employer message", text: input.EmployerMessage},
		{name: "chat history", text: FormatHistory(input.History)},
		{name: "reply option", text: buttonText(input.Buttons)},
		{name: "generated reply", text: generated},
	} {
		if reason := conversationpolicy.HighRiskMessageReason(source.text); reason != "" {
			return fmt.Sprintf("high-risk %s in %s", reason, source.name)
		}
	}
	if strings.TrimSpace(generated) != "" {
		if reason := reviewCandidateFacts(generated, input.Candidate.Context); reason != "" {
			return reason
		}
		if len(input.Buttons) > 0 && !observedButton(generated, input.Buttons) {
			return "generated reply is not an observed button"
		}
		if len(input.Buttons) == 0 && runeCount(strings.TrimSpace(generated)) > 400 {
			return "generated reply is too long for HR chat"
		}
		if claimsExternalCompletion(generated) {
			return "generated reply claims an unverified external action"
		}
	}
	return ""
}

func buttonText(buttons []Button) string {
	values := make([]string, 0, len(buttons))
	for _, button := range buttons {
		values = append(values, button.Text)
	}
	return strings.Join(values, "\n")
}

func observedButton(reply string, buttons []Button) bool {
	value := strings.TrimSpace(reply)
	for _, button := range buttons {
		if value == strings.TrimSpace(button.Text) {
			return true
		}
	}
	return false
}

func reviewCandidateFacts(reply string, context candidatecontext.CandidateContext) string {
	if err := employerreply.ValidateDraft(reply, context); err != nil {
		return "generated reply failed Candidate safety: " + err.Error()
	}
	lower := strings.ToLower(reply)
	allFacts := append([]candidatecontext.ResolvedFact{}, context.ResolvedFacts...)
	allFacts = append(allFacts, context.PartiallyResolvedFacts...)
	allFacts = append(allFacts, context.UnknownAtomicFacts...)
	allFacts = append(allFacts, context.RestrictedFacts...)
	for _, fact := range allFacts {
		topic := strings.TrimSpace(fact.Topic)
		if topic == "" || !candidatecontext.Mentions(lower, topic) {
			continue
		}
		switch fact.Status {
		case candidatecontext.ResolvedFactUnknown:
			return fmt.Sprintf("generated reply mentions unknown Candidate fact %q", topic)
		case candidatecontext.ResolvedFactRestricted:
			return fmt.Sprintf("generated reply mentions restricted Candidate fact %q", topic)
		case candidatecontext.ResolvedFactPartiallyAnswerable:
			if unsupportedTechnicalQualifier(lower, fact, context) {
				return fmt.Sprintf("generated reply overstates partial Candidate fact %q", topic)
			}
		}
	}
	if unsupportedSalary(lower, context) {
		return "generated reply contains an unsupported salary claim"
	}
	if unsupportedBusinessTrips(lower, context) {
		return "generated reply contradicts canonical business-trips context"
	}
	if unsupportedExactExperience(lower, context) {
		return "generated reply contains an unsupported experience duration"
	}
	return ""
}

func unsupportedTechnicalQualifier(text string, fact candidatecontext.ResolvedFact, context candidatecontext.CandidateContext) bool {
	if !candidatecontext.ContainsAny(text, "production", "продакш", "проде", "commercial", "коммерческ", "expert", "эксперт", "senior", "ведущий", "highload", "высоконагруз") {
		return false
	}
	for _, claim := range append([]string{fact.Value}, fact.AllowedClaims...) {
		if claim != "" && candidatecontext.Mentions(text, claim) && candidatecontext.ContainsAny(strings.ToLower(claim), "production", "продакш", "проде", "commercial", "коммерческ", "expert", "эксперт", "senior", "ведущий", "highload", "высоконагруз") {
			return false
		}
	}
	for _, allowed := range context.AllowedFacts {
		if candidatecontext.Mentions(text, allowed) && candidatecontext.ContainsAny(strings.ToLower(allowed), "production", "продакш", "проде", "commercial", "коммерческ", "expert", "эксперт", "senior", "ведущий", "highload", "высоконагруз") {
			return false
		}
	}
	return true
}

func unsupportedSalary(text string, context candidatecontext.CandidateContext) bool {
	if !salaryClaimRE.MatchString(text) {
		return false
	}
	for _, fact := range append(append([]candidatecontext.ResolvedFact{}, context.ResolvedFacts...), context.PartiallyResolvedFacts...) {
		if fact.Topic != "salary" || fact.Status != candidatecontext.ResolvedFactAnswerable {
			continue
		}
		for _, claim := range append([]string{fact.Value}, fact.AllowedClaims...) {
			if claim != "" && candidatecontext.Mentions(text, claim) {
				return false
			}
		}
	}
	for _, allowed := range context.AllowedFacts {
		if candidatecontext.Mentions(text, allowed) && salaryClaimRE.MatchString(allowed) {
			return false
		}
	}
	return true
}

func unsupportedBusinessTrips(text string, context candidatecontext.CandidateContext) bool {
	for _, fact := range append(append([]candidatecontext.ResolvedFact{}, context.ResolvedFacts...), context.PartiallyResolvedFacts...) {
		if fact.Topic != "business_trips" || fact.Status != candidatecontext.ResolvedFactAnswerable {
			continue
		}
		if candidatecontext.ContainsAny(strings.ToLower(fact.Value), "не готов", "нет", "не могу") && candidatecontext.ContainsAny(text, "готов к командиров", "готова к командиров", "готов ездить", "готова ездить") {
			return true
		}
	}
	return false
}

func unsupportedExactExperience(text string, context candidatecontext.CandidateContext) bool {
	months, ok := exactTotalMonths(context)
	if !ok {
		return false
	}
	if months != 12 && candidatecontext.ContainsAny(text, "год", "года", "лет", "year") && !candidatecontext.ContainsAny(text, "месяц", "months", "month") {
		return true
	}
	return false
}

func exactTotalMonths(context candidatecontext.CandidateContext) (int, bool) {
	for _, fact := range append(append([]candidatecontext.ResolvedFact{}, context.ResolvedFacts...), context.PartiallyResolvedFacts...) {
		if fact.Topic != "total_experience" || fact.Status != candidatecontext.ResolvedFactAnswerable {
			continue
		}
		fields := strings.Fields(strings.ToLower(fact.Value))
		for index, field := range fields {
			var count int
			if _, err := fmt.Sscanf(field, "%d", &count); err != nil || count < 0 {
				continue
			}
			if index+1 < len(fields) && strings.HasPrefix(fields[index+1], "месяц") || index+1 < len(fields) && strings.HasPrefix(fields[index+1], "month") {
				return count, true
			}
			if index+1 < len(fields) && strings.HasPrefix(fields[index+1], "год") || index+1 < len(fields) && strings.HasPrefix(fields[index+1], "year") {
				return count * 12, true
			}
		}
	}
	return 0, false
}

func claimsExternalCompletion(text string) bool {
	return candidatecontext.ContainsAny(text,
		"тест выполнен", "тестовое задание выполнено", "заполнил форму", "заполнила форму", "анкету заполнил", "анкету заполнила", "документ отправлен", "документы отправлены", "интервью забронировано", "интервью назначено", "собеседование назначено", "записался на интервью", "записалась на интервью", "form submitted", "test completed", "interview booked", "interview scheduled")
}

func runeCount(value string) int {
	count := 0
	for range value {
		count++
	}
	return count
}
