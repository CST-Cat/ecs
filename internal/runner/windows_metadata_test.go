//go:build windows

package runner_test

import (
	"context"
	"testing"

	"ecs/internal/config"
	"ecs/internal/probe"
	"ecs/internal/runner"
)

func TestWindowsUnsupportedProbeUsesCanonicalDescriptorMetadata(t *testing.T) {
	definitions := probe.BuiltinDefinitions()
	catalog, err := probe.CatalogFromDefinitions(definitions)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Defaults(catalog, config.ProfileStandard)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Modules = []string{"cpu"}

	report, err := runner.Run(context.Background(), definitions, catalog, cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Results) != 1 {
		t.Fatalf("Windows unsupported results = %d, want 1", len(report.Results))
	}
	descriptor, ok := catalog.Lookup("cpu")
	if !ok {
		t.Fatal("cpu descriptor missing")
	}
	result := report.Results[0]
	if result.Title != descriptor.TitleKey || result.Description != descriptor.DescriptionKey ||
		result.Methodology.Kind != descriptor.Methodology.Kind || result.Methodology.Label != descriptor.Methodology.Label ||
		result.Methodology.Engine != descriptor.Methodology.Engine || result.Methodology.Profile != descriptor.Methodology.Profile ||
		result.Methodology.ComparisonScope != descriptor.Methodology.ComparisonScope {
		t.Fatalf("Windows unsupported metadata = %+v, descriptor = %+v", result, descriptor)
	}
}
