package app

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

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
	ID                  string   `json:"id"`
	TitleKey            string   `json:"title_key"`
	DescriptionKey      string   `json:"description_key"`
	Exposure            string   `json:"exposure"`
	NeedsEgressIP       bool     `json:"needs_egress_ip"`
	Concurrency         string   `json:"concurrency"`
	RetryOnInterference bool     `json:"retry_on_interference"`
	RequiredTools       []string `json:"required_tools,omitempty"`
	EstimateSeconds     int64    `json:"estimate_seconds"`
	EstimateMode        string   `json:"estimate_mode"`
	PrivacyNoticeKey    string   `json:"privacy_notice_key,omitempty"`
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
	filtered := make([]string, 0, len(args))
	jsonRequested := false
	for _, arg := range args {
		switch arg {
		case "--json":
			jsonRequested = true
		default:
			if strings.HasPrefix(arg, "--json=") {
				if strings.EqualFold(strings.TrimPrefix(arg, "--json="), "true") {
					jsonRequested = true
					continue
				}
				fmt.Fprintln(stderr, i18n.T("cli.error")+": "+i18n.T("err.planJSONRequired"))
				return 1
			}
			filtered = append(filtered, arg)
		}
	}
	if !jsonRequested {
		fmt.Fprintln(stderr, i18n.T("cli.error")+": "+i18n.T("err.planJSONRequired"))
		fmt.Fprintln(stderr, i18n.T("help.planUsage"))
		return 1
	}

	resolved, err := resolveRunConfig(filtered, stderr)
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
		fmt.Fprintln(stderr, i18n.T("err.planJSONRequired"))
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
	for _, id := range runtime.Modules {
		descriptor, ok := config.ModuleDescriptorFor(id)
		if !ok {
			continue
		}
		tools := append([]string(nil), descriptor.RequiredTools...)
		plan.Modules = append(plan.Modules, plannedModule{
			ID:                  descriptor.ID,
			TitleKey:            descriptor.TitleKey,
			DescriptionKey:      descriptor.DescriptionKey,
			Exposure:            descriptor.Exposure.String(),
			NeedsEgressIP:       descriptor.NeedsEgressIP,
			Concurrency:         string(descriptor.Concurrency),
			RetryOnInterference: descriptor.RetryOnInterference,
			RequiredTools:       tools,
			EstimateSeconds:     int64(descriptor.Estimate / time.Second),
			EstimateMode:        string(descriptor.EstimateMode),
			PrivacyNoticeKey:    descriptor.PrivacyNoticeKey,
		})
		plan.NeedsEgressIP = plan.NeedsEgressIP || descriptor.NeedsEgressIP
		for _, tool := range tools {
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
	for _, module := range plan.Modules {
		if module.Exposure == config.ExposureNameThirdParty && !containsPlanValue(plan.ExternalServices, "third-party-provider") {
			plan.ExternalServices = append(plan.ExternalServices, "third-party-provider")
		}
		if module.ID == "ookla" && !containsPlanValue(plan.ExternalServices, "ookla") {
			plan.ExternalServices = append(plan.ExternalServices, "ookla")
		}
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
