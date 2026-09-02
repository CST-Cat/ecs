package config

import (
	"time"

	"ecs/internal/model"
	"ecs/internal/module"
)

// testModuleCatalog is deliberately a small explicit fixture for config unit
// tests. Built-in descriptor facts belong to probe.BuiltinDefinitions; these
// tests only need to exercise config's profile/selection/exposure adapters.
func testModuleCatalog() module.Catalog {
	catalog, err := module.NewCatalog([]module.Descriptor{
		testModuleDescriptor("system", true, module.ExposureLocal, false, []string{"sysinfo"}),
		testModuleDescriptor("network", false, module.ExposureThirdParty, true, nil),
		testModuleDescriptor("dns", true, module.ExposurePublic, false, nil),
	})
	if err != nil {
		panic(err)
	}
	return catalog
}

func testModuleDescriptor(id string, standard bool, exposure module.Exposure, needsEgress bool, tools []string) module.Descriptor {
	return module.Descriptor{
		ID: id, ProfileStandard: standard, Exposure: exposure, NeedsEgressIP: needsEgress,
		Concurrency: module.ConcurrencyProbe, RequiredTools: tools,
		TitleKey: "module." + id + ".title", DescriptionKey: "module." + id + ".desc",
		Estimate: time.Second, EstimateMode: module.EstimateModeFixed,
		Methodology: model.Methodology{Kind: "fixture", Label: "fixture.label", Engine: "fixture.engine", Profile: "fixture.profile", ComparisonScope: "fixture.scope"},
	}
}
