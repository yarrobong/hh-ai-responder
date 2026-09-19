package vacancyanalysis

import "strings"

type WorkModeClassification string

const (
	WorkModeOfficeRequired     WorkModeClassification = "OFFICE_REQUIRED"
	WorkModeRelocationRequired WorkModeClassification = "RELOCATION_REQUIRED"
	WorkModeLocationRequired   WorkModeClassification = "LOCATION_REQUIRED"
	WorkModeOfficeAvailable    WorkModeClassification = "OFFICE_AVAILABLE"
	WorkModeRemoteAvailable    WorkModeClassification = "REMOTE_AVAILABLE"
	WorkModeOption             WorkModeClassification = "WORK_MODE_OPTION"
)

func classifyWorkMode(text string) WorkModeClassification {
	normalized := normalizeEvidenceText(text)
	if normalized == "" {
		return WorkModeOption
	}
	if containsAnyText(normalized, "релокац", "переезд") && containsAnyText(normalized, "обязатель", "требуется", "готовность", "must", "required") {
		return WorkModeRelocationRequired
	}
	if containsAnyText(normalized, "офис", "office", "onsite", "on-site") {
		if containsAnyText(normalized, "есть офис", "можно в офис", "доступен офис", "если не люб") && containsAnyText(normalized, "удален", "удалён", "remote", "дистанцион") {
			return WorkModeOfficeAvailable
		}
		if containsAnyText(normalized, "только", "обязатель", "требуется", "must", "required") {
			return WorkModeOfficeRequired
		}
	}
	if containsAnyText(normalized, "удален", "удалён", "remote", "дистанцион") && containsAnyText(normalized, "доступ", "можно", "available", "option", "предлага") {
		return WorkModeRemoteAvailable
	}
	if containsAnyText(normalized, "локац", "город", "место работы", "location") {
		return WorkModeLocationRequired
	}
	return WorkModeOption
}

func workModeIsNonBlocking(text string) bool {
	switch classifyWorkMode(text) {
	case WorkModeOfficeAvailable, WorkModeRemoteAvailable, WorkModeOption:
		return true
	default:
		return false
	}
}

func workModeText(value ...string) string {
	return strings.Join(value, " ")
}
