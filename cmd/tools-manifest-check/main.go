package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"ecs/internal/toolsmanifest"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	flags.SetOutput(stderr)
	expectedArchitecture := flags.String("architecture", "", "require this manifest architecture")
	expectedToolchainMode := flags.String("toolchain-mode", "", "require this build toolchain mode")
	expectedSmokeRunner := flags.String("smoke-runner", "", "require this build smoke runner")
	expectedNPBSmokeClass := flags.String("npb-smoke-class", "", "require this NPB CI smoke class")
	flags.Usage = func() {
		fmt.Fprintf(stderr, "usage: %s [expectation flags] MANIFEST [MANIFEST ...]\n", flags.Name())
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if flags.NArg() == 0 {
		flags.Usage()
		return 2
	}

	failed := false
	for _, path := range flags.Args() {
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
			fmt.Fprintf(stderr, "%s: invalid manifest: %v\n", path, err)
			failed = true
			continue
		}
		fmt.Fprintf(stdout, "%s: valid (%s)\n", path, manifest.Architecture)
	}
	if failed {
		return 1
	}
	return 0
}
