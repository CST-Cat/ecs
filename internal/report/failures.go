package report

import (
	"strings"

	"ecs/internal/i18n"
	"ecs/internal/model"
)

func failureCategoryLabel(category model.FailureCategory) string {
	key := "failure." + string(category)
	if i18n.Has(i18n.Current(), key) {
		return i18n.T(key)
	}
	if strings.TrimSpace(string(category)) == "" {
		return i18n.T("failure.unknown")
	}
	return string(category)
}

func failureRetryableLabel(retryable bool) string {
	if retryable {
		return i18n.T("failure.yes")
	}
	return i18n.T("failure.no")
}
