package app

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"

	"ecs/internal/buildinfo"
	"ecs/internal/config"
	"ecs/internal/i18n"
)

// executionPlan is deliberately language independent. It is consumed by the
// wrapper and other automation, so it contains stable keys and enum values,
// never localized labels or prose assembled for a human report.
type executionPlan struct {
	SchemaVersion    string          `json:"schema_version"`
	Tool             planTool        `json:"tool"`
	Profile          string          `json:"profile"`
	Exposure         string          `json:"exposure"`
	Reveal           bool            `json:"reveal"`
	IPVersion        string          `json:"ip_version"`
	Modules          []plannedModule `json:"modules"`
	RequiredTools    []string        `json:"required_tools"`
	NeedsEgressIP    bool            `json:"needs_egress_ip"`
	ExternalServices []string        `json:"external_services,omitempty"`
	Staging          planStaging     `json:"staging"`
}

type planTool struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type plannedModule struct {
	ID string `json:"id"`
}

type planStaging struct {
	Mode                  string   `json:"mode"`
	ToolArchiveRequired   bool     `json:"tool_archive_required"`
	ToolArchiveTools      []string `json:"tool_archive_tools,omitempty"`
	NextTraceTinyRequired bool     `json:"nexttrace_tiny_required"`
	NextTraceSource       string   `json:"nexttrace_source,omitempty"`
	OoklaPackageRequired  bool     `json:"ookla_package_required"`
	OoklaPackageSource    string   `json:"ookla_package_source,omitempty"`
	ZstdCorpusRequired    bool     `json:"zstd_corpus_required"`
}

func planCommand(args []string, stdout, stderr io.Writer) int {
	resolved, err := resolveRunConfig(args, stderr)
	if err != nil {
		var parseErr runFlagParseError
		if errors.As(err, &parseErr) {
			if errors.Is(err, flag.ErrHelp) {
				return 0
			}
			return 1
		}
		fmt.Fprintln(stderr, err)
		return 1
	}
	if resolved.Version {
		fmt.Fprintln(stderr, i18n.T("cli.error")+": "+i18n.T("err.planVersion"))
		return 1
	}
	runtime := resolved.Runtime
	if resolved.Interactive && !resolved.Yes {
		if !runWizard(&runtime, stderr) {
			return 0
		}
	}
	if err := config.Validate(runtime); err != nil {
		fmt.Fprintln(stderr, i18n.T("cli.error")+": "+err.Error())
		return 1
	}

	content, err := json.MarshalIndent(buildExecutionPlan(runtime), "", "  ")
	if err != nil {
		fmt.Fprintln(stderr, i18n.T("cli.error")+": "+err.Error())
		return 1
	}
	content = append(content, '\n')
	_, _ = stdout.Write(content)
	return 0
}

func buildExecutionPlan(runtime config.Runtime) executionPlan {
	plan := executionPlan{
		SchemaVersion: buildinfo.PlanSchemaVersion,
		Tool:          planTool{Name: buildinfo.Name, Version: buildinfo.Version},
		Profile:       runtime.Profile,
		Exposure:      runtime.Exposure.String(),
		Reveal:        runtime.Reveal,
		IPVersion:     runtime.IPVersion,
		Modules:       make([]plannedModule, 0, len(runtime.Modules)),
		Staging:       planStaging{Mode: "temporary-prefix"},
	}
	needsThirdPartyProvider := false
	needsOokla := false
	for _, id := range runtime.Modules {
		descriptor, ok := config.ModuleDescriptorFor(id)
		if !ok {
			continue
		}
		plan.Modules = append(plan.Modules, plannedModule{
			ID: descriptor.ID,
		})
		plan.NeedsEgressIP = plan.NeedsEgressIP || descriptor.NeedsEgressIP
		if descriptor.Exposure == config.ExposureThirdParty {
			needsThirdPartyProvider = true
		}
		if descriptor.ID == "ookla" {
			needsOokla = true
		}
		for _, tool := range descriptor.RequiredTools {
			if containsPlanValue(plan.RequiredTools, tool) {
				continue
			}
			plan.RequiredTools = append(plan.RequiredTools, tool)
			switch tool {
			case "speedtest":
				plan.Staging.OoklaPackageRequired = true
				plan.Staging.OoklaPackageSource = "official-signed-package-source"
			case "nexttrace-tiny":
				plan.Staging.NextTraceTinyRequired = true
				plan.Staging.NextTraceSource = "official-architecture-asset"
				plan.Staging.ToolArchiveRequired = true
				plan.Staging.ToolArchiveTools = append(plan.Staging.ToolArchiveTools, tool)
			case "zstd":
				plan.Staging.ZstdCorpusRequired = true
				plan.Staging.ToolArchiveRequired = true
				plan.Staging.ToolArchiveTools = append(plan.Staging.ToolArchiveTools, tool)
			default:
				plan.Staging.ToolArchiveRequired = true
				plan.Staging.ToolArchiveTools = append(plan.Staging.ToolArchiveTools, tool)
			}
		}
	}
	if plan.NeedsEgressIP {
		plan.ExternalServices = append(plan.ExternalServices, "egress-ip-discovery")
	}
	if needsThirdPartyProvider {
		plan.ExternalServices = append(plan.ExternalServices, "third-party-provider")
	}
	if needsOokla {
		plan.ExternalServices = append(plan.ExternalServices, "ookla")
	}
	return plan
}

func containsPlanValue(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
