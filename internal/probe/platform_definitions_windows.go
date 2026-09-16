//go:build windows

package probe

import (
	"context"
	"time"

	"ecs/internal/model"
	"ecs/internal/module"
)

const windowsUnsupportedMethodology = "unsupported on native Windows"

// applyPlatformDefinitions preserves the canonical module catalog while
// replacing only methodologies whose native Windows gate has not passed in the
// current bundle. RequiredTools stays in the descriptor as canonical metadata;
// app's Windows resolver removes those tools from the wrapper plan.
func applyPlatformDefinitions(definitions []Definition) []Definition {
	for index := range definitions {
		switch definitions[index].Descriptor.ID {
		case "cpu", "speed", "ookla", "route", "backtrace":
			definitions[index].Probe = newWindowsUnsupportedProbe(definitions[index].Descriptor)
		}
	}
	return definitions
}

type windowsUnsupportedProbe struct {
	id          string
	title       string
	description string
	methodology model.Methodology
}

func newWindowsUnsupportedProbe(descriptor module.Descriptor) windowsUnsupportedProbe {
	methodology := descriptor.Methodology
	if descriptor.Methodology.Parameters != nil {
		methodology.Parameters = make(map[string]string, len(descriptor.Methodology.Parameters))
		for key, value := range descriptor.Methodology.Parameters {
			methodology.Parameters[key] = value
		}
	}
	return windowsUnsupportedProbe{
		id:          descriptor.ID,
		title:       descriptor.TitleKey,
		description: descriptor.DescriptionKey,
		methodology: methodology,
	}
}

func (probe windowsUnsupportedProbe) ID() string { return probe.id }

func (probe windowsUnsupportedProbe) Run(context.Context, Environment) model.Result {
	start := time.Now()
	result := model.NewResult(probe.id, probe.title)
	result.Description = probe.description
	result.Methodology = probe.methodology
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
