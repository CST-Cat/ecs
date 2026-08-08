package report

import (
	"ecs/internal/i18n"
	"ecs/internal/model"
)

// resultTitle gives benchmark sections a stable, explicit name in every
// renderer. The presentation makes the benchmark/inventory distinction clear.
func resultTitle(result model.Result) string {
	switch result.ID {
	case "memory":
		return i18n.T("report.memoryBenchmark")
	case "disk":
		return i18n.T("report.diskBenchmark")
	default:
		return result.Title
	}
}
