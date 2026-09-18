//go:build windows

package probe

import (
	"context"
	"time"

	"ecs/internal/model"
	"ecs/internal/tool"
)

const windowsUnsupportedMethodology = "unsupported on native Windows"

// applyPlatformDefinitions preserves the canonical module catalog while
// replacing modules whose declared requirements contain an unsupported runtime
// tool. RequiredTools stays in the descriptor as canonical metadata; the app
// plan and this probe projection both consume the same tool source facts.
func applyPlatformDefinitions(definitions []Definition) []Definition {
	for index := range definitions {
		if windowsModuleHasUnsupportedTool(definitions[index].Descriptor.RequiredTools) {
			definitions[index].Probe = newWindowsUnsupportedProbe(definitions[index].Descriptor.ID)
		}
	}
	return definitions
}

func windowsModuleHasUnsupportedTool(requiredTools []string) bool {
	for _, id := range requiredTools {
		if tool.PlatformToolSource(tool.PlatformWindows, id) == tool.ToolSourceUnsupported {
			return true
		}
	}
	return false
}

type windowsUnsupportedProbe struct {
	id string
}

func newWindowsUnsupportedProbe(id string) windowsUnsupportedProbe {
	return windowsUnsupportedProbe{id: id}
}

func (probe windowsUnsupportedProbe) ID() string { return probe.id }

func (probe windowsUnsupportedProbe) Run(context.Context, Environment) model.Result {
	start := time.Now()
	result := model.NewResult(probe.id, "")
	result.Status = model.StatusSkipped
	result.SummaryMessages = []model.Message{model.NewMessage("probe.platform.summary.unsupported")}
	result.Failures = []model.Failure{{
		Category: model.FailureUnsupported,
		Stage:    "platform",
		Target:   probe.id,
		Count:    1,
		Message:  windowsUnsupportedMethodology,
	}}
	result.Evidence = model.NewEvidence(0, 1, "run")
	result.Finish(start)
	return result
}
