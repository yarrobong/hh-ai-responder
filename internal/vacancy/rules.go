package vacancy

import (
	"fmt"
	"strings"
)

// DeterministicRejectReason applies vacancy-only filters. Thresholds and
// keywords are supplied by the caller so this package remains configuration-
// independent and does not decide whether an application may be sent.
func DeterministicRejectReason(value Vacancy, description string, minSalary int, minSalaryCurrency string, excludeKeywords []string) string {
	text := strings.Join([]string{value.Name, value.Company.Name, value.Area.Name, description}, "\n")
	lowerText := strings.ToLower(text)
	matched := make([]string, 0, len(excludeKeywords))
	for _, keyword := range excludeKeywords {
		trimmed := strings.TrimSpace(keyword)
		if trimmed != "" && strings.Contains(lowerText, strings.ToLower(trimmed)) {
			matched = append(matched, trimmed)
		}
	}
	if len(matched) > 0 {
		return "exclude keyword: " + strings.Join(matched, ", ")
	}

	if minSalary <= 0 {
		return ""
	}
	currency := strings.ToUpper(strings.TrimSpace(value.Compensation.Currency))
	configuredCurrency := strings.ToUpper(strings.TrimSpace(minSalaryCurrency))
	if configuredCurrency == "" {
		configuredCurrency = "RUR"
	}
	if currency == "" || currency != configuredCurrency {
		return ""
	}

	ceiling, known := 0, false
	if value.Compensation.To != nil {
		ceiling, known = *value.Compensation.To, true
	} else if value.Compensation.From != nil {
		ceiling = *value.Compensation.From
	}
	if known && ceiling < minSalary {
		return fmt.Sprintf("salary ceiling %d %s is below minimum %d %s", ceiling, currency, minSalary, configuredCurrency)
	}
	return ""
}
