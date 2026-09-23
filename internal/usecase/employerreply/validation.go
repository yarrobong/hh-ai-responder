package employerreply

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"

	candidatecontext "hh-ai-responder/internal/usecase/candidatecontext"
)

func ParseDecision(raw string) (Decision, error) {
	var value Decision
	decoder := jsonDecoder(raw)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return Decision{}, errors.New("invalid structured AI decision JSON")
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); err != io.EOF {
		return Decision{}, errors.New("structured AI decision has trailing data")
	}
	if err := ValidateDecision(value); err != nil {
		return Decision{}, err
	}
	return value, nil
}

func ValidateDecision(value Decision) error {
	switch value.Action {
	case ActionDraftReply, ActionNeedCandidate, ActionNoReplyNeeded, ActionCourtesyReply, ActionManualReview:
	default:
		return errors.New("invalid AI decision action")
	}
	if value.ReplyRequirement != "" && value.ReplyRequirement != "REPLY_REQUIRED" && value.ReplyRequirement != "REPLY_OPTIONAL" && value.ReplyRequirement != "NO_REPLY_NEEDED" {
		return errors.New("invalid reply requirement")
	}
	if strings.TrimSpace(value.Reason) == "" {
		return errors.New("AI decision reason is required")
	}
	if value.Confidence < 0 || value.Confidence > 1 {
		return errors.New("AI decision confidence must be between 0 and 1")
	}
	if !value.ForbiddenClaimsChecked {
		return errors.New("AI decision must report forbidden claims check")
	}
	if (value.Action == ActionDraftReply || value.Action == ActionCourtesyReply) && strings.TrimSpace(value.Draft) == "" {
		return errors.New("reply action requires a draft")
	}
	if value.Action != ActionDraftReply && value.Action != ActionCourtesyReply && strings.TrimSpace(value.Draft) != "" {
		return errors.New("only reply actions may contain a draft")
	}
	if value.Action == ActionNeedCandidate && len(value.MissingInformation) == 0 {
		return errors.New("need_candidate_input requires missing information")
	}
	for _, missing := range value.MissingInformation {
		if strings.TrimSpace(missing.Topic) == "" || strings.TrimSpace(missing.Question) == "" {
			return errors.New("missing information requires topic and question")
		}
	}
	return nil
}

func ValidateUsedFacts(used []string, value candidatecontext.CandidateContext) error {
	allowedValues := append([]string{}, value.AllowedFacts...)
	allowedValues = append(allowedValues, value.RelevantSkills...)
	for _, fact := range value.ResolvedFacts {
		allowedValues = append(allowedValues, fact.AllowedClaims...)
		allowedValues = append(allowedValues, fact.Value)
	}
	allowed := strings.ToLower(strings.Join(allowedValues, "\n"))
	for _, fact := range used {
		fact = strings.TrimSpace(fact)
		if fact == "" || !candidatecontext.Mentions(allowed, fact) {
			return errors.New("used fact is not present in CandidateContext")
		}
	}
	return nil
}

// ValidateGeneratedReply is the single deterministic validation boundary for
// all employer-facing draft actions. Courtesy replies are not exempt: the
// text and every fact must satisfy the same evidence policy as a normal draft.
func ValidateGeneratedReply(decision Decision, value candidatecontext.CandidateContext) error {
	if decision.Action != ActionDraftReply && decision.Action != ActionCourtesyReply {
		return nil
	}
	if strings.TrimSpace(decision.Draft) == "" {
		return errors.New("reply action requires a draft")
	}
	if len([]rune(decision.Draft)) > 600 {
		return errors.New("reply draft exceeds maximum length")
	}
	if err := ValidateUsedFacts(decision.UsedFacts, value); err != nil {
		return fmt.Errorf("used facts: %w", err)
	}
	if err := ValidateDraft(decision.Draft, value); err != nil {
		return fmt.Errorf("draft: %w", err)
	}
	return nil
}

// ValidateDraft is deterministic and conservative. It only accepts claims
// that are represented by the already-resolved employer-safe context.
func ValidateDraft(draft string, value candidatecontext.CandidateContext) error {
	if strings.TrimSpace(draft) == "" {
		return errors.New("AI draft is empty")
	}
	lower := strings.ToLower(draft)
	for _, forbidden := range value.ForbiddenClaims {
		if strings.TrimSpace(forbidden) != "" && candidatecontext.Mentions(lower, forbidden) {
			return errors.New("draft contains a forbidden claim")
		}
	}
	for _, fact := range value.RestrictedFacts {
		if candidatecontext.Mentions(lower, fact.Topic) || candidatecontext.Mentions(lower, fact.Value) {
			return errors.New("draft mentions a restricted fact")
		}
	}
	for _, phrase := range []string{"senior developer", "senior разработчик", "kubernetes production", "kubernetes в production", "kafka production", "celery production", "ml engineer", "highload expert", "highload эксперт"} {
		if strings.Contains(lower, phrase) {
			return errors.New("draft contains an explicitly forbidden claim")
		}
	}
	allowedValues := append([]string{}, value.AllowedFacts...)
	allowedValues = append(allowedValues, value.RelevantSkills...)
	for _, fact := range value.ResolvedFacts {
		allowedValues = append(allowedValues, fact.AllowedClaims...)
		allowedValues = append(allowedValues, fact.Value)
	}
	allowed := strings.ToLower(strings.Join(allowedValues, "\n"))
	for _, technology := range candidatecontext.TechnologyNames() {
		if !candidatecontext.Mentions(lower, technology) {
			continue
		}
		if !candidatecontext.Mentions(allowed, technology) {
			return fmt.Errorf("draft mentions unsupported technology %q", technology)
		}
	}
	if unsupportedDuration(lower, value) {
		return errors.New("draft contains an unsupported experience duration")
	}
	if contradictoryRelocation(lower, value) {
		return errors.New("draft contradicts canonical relocation context")
	}
	if inventedSalary(lower, value) {
		return errors.New("draft contains an unsupported salary claim")
	}
	return nil
}

var durationRE = regexp.MustCompile(`(?i)(\d+)\s*(месяц\w*|month\w*|год\w*|year\w*)`)

func unsupportedDuration(text string, value candidatecontext.CandidateContext) bool {
	if !durationRE.MatchString(text) {
		return false
	}
	knownMonths, known := resolvedTotalMonths(value)
	if known {
		for _, match := range durationRE.FindAllStringSubmatch(text, -1) {
			count, err := strconv.Atoi(match[1])
			if err != nil {
				return true
			}
			months := count
			if strings.HasPrefix(strings.ToLower(match[2]), "год") || strings.HasPrefix(strings.ToLower(match[2]), "year") {
				months *= 12
			}
			if months != knownMonths {
				return true
			}
		}
	}
	// Preserve the existing fail-closed rule for unqualified generated
	// durations: a bare number is not evidence merely because the model wrote it.
	return !strings.Contains(text, "подтвержден") && !strings.Contains(text, "confirmed")
}

func resolvedTotalMonths(value candidatecontext.CandidateContext) (int, bool) {
	for _, fact := range append(append([]candidatecontext.ResolvedFact{}, value.ResolvedFacts...), value.PartiallyResolvedFacts...) {
		if fact.Topic != "total_experience" || fact.Status != candidatecontext.ResolvedFactAnswerable {
			continue
		}
		match := durationRE.FindStringSubmatch(fact.Value)
		if len(match) != 3 {
			continue
		}
		count, err := strconv.Atoi(match[1])
		if err != nil {
			continue
		}
		if strings.HasPrefix(strings.ToLower(match[2]), "год") || strings.HasPrefix(strings.ToLower(match[2]), "year") {
			count *= 12
		}
		return count, true
	}
	return 0, false
}

func contradictoryRelocation(text string, value candidatecontext.CandidateContext) bool {
	for _, fact := range append(append([]candidatecontext.ResolvedFact{}, value.ResolvedFacts...), value.PartiallyResolvedFacts...) {
		if fact.Topic != "relocation" || fact.Status != candidatecontext.ResolvedFactAnswerable {
			continue
		}
		canonical := strings.ToLower(fact.Value)
		if (strings.Contains(canonical, "не готов") || strings.Contains(canonical, "не готова") || strings.Contains(canonical, "нет")) &&
			(strings.Contains(text, "готов к релокац") || strings.Contains(text, "готова к релокац") || strings.Contains(text, "готов переех") || strings.Contains(text, "готова переех")) {
			return true
		}
	}
	return false
}

func inventedSalary(text string, value candidatecontext.CandidateContext) bool {
	answerable := false
	claims := []string{}
	for _, fact := range append(append([]candidatecontext.ResolvedFact{}, value.ResolvedFacts...), value.PartiallyResolvedFacts...) {
		if fact.Topic == "salary" && fact.Status == candidatecontext.ResolvedFactAnswerable {
			answerable = true
			claims = append(claims, fact.Value)
			claims = append(claims, fact.AllowedClaims...)
		}
	}
	if !answerable || !regexp.MustCompile(`(?i)(зарплат|salary|руб|₽|тыс|\$|€)`).MatchString(text) || !regexp.MustCompile(`\d`).MatchString(text) {
		return false
	}
	for _, claim := range claims {
		if strings.TrimSpace(claim) != "" && candidatecontext.Mentions(text, claim) {
			return false
		}
	}
	// A profile may expose the salary only through AllowedFacts. A matching
	// canonical value is safe; an unrelated numeric offer is not.
	for _, fact := range value.AllowedFacts {
		if regexp.MustCompile(`\d`).MatchString(fact) && candidatecontext.Mentions(text, fact) {
			return false
		}
	}
	return true
}

func jsonDecoder(raw string) *json.Decoder {
	return json.NewDecoder(strings.NewReader(raw))
}
