package main

import (
	"flag"
	"fmt"
	"os"

	"ecs/internal/toolsmanifest"
)

func main() {
	expectedArchitecture := flag.String("architecture", "", "require this manifest architecture")
	expectedToolchainMode := flag.String("toolchain-mode", "", "require this build toolchain mode")
	expectedSmokeRunner := flag.String("smoke-runner", "", "require this build smoke runner")
	expectedNPBSmokeClass := flag.String("npb-smoke-class", "", "require this NPB CI smoke class")
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "usage: %s [expectation flags] MANIFEST [MANIFEST ...]\n", os.Args[0])
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() == 0 {
		flag.Usage()
		os.Exit(2)
	}

	failed := false
	for _, path := range flag.Args() {
		data, err := os.ReadFile(path)
		var manifest toolsmanifest.Manifest
		if err == nil {
			manifest, err = toolsmanifest.Parse(data)
			if err == nil && *expectedArchitecture != "" && manifest.Architecture != *expectedArchitecture {
				err = fmt.Errorf("architecture %q does not match expected %q", manifest.Architecture, *expectedArchitecture)
			}
			if err == nil && *expectedToolchainMode != "" && manifest.Build.ToolchainMode != *expectedToolchainMode {
				err = fmt.Errorf("build.toolchain_mode %q does not match expected %q", manifest.Build.ToolchainMode, *expectedToolchainMode)
			}
			if err == nil && *expectedSmokeRunner != "" && manifest.Build.SmokeRunner != *expectedSmokeRunner {
				err = fmt.Errorf("build.smoke_runner %q does not match expected %q", manifest.Build.SmokeRunner, *expectedSmokeRunner)
			}
			if err == nil && *expectedNPBSmokeClass != "" {
				for _, tool := range manifest.Tools {
					if tool.Name != "npb-ep" && tool.Name != "npb-ft" {
						continue
					}
					class, ok := tool.Parameters["ci_smoke_class"].(string)
					if !ok || class != *expectedNPBSmokeClass {
						err = fmt.Errorf("tool %q ci_smoke_class %q does not match expected %q", tool.Name, class, *expectedNPBSmokeClass)
						break
					}
				}
			}
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: invalid manifest: %v\n", path, err)
			failed = true
			continue
		}
		fmt.Printf("%s: valid (%s)\n", path, manifest.Architecture)
	}
	if failed {
		os.Exit(1)
	}
}
