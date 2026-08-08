package main

import (
	"flag"
	"fmt"
	"os"

	"ecs/internal/toolsmanifest"
)

func main() {
	expectedArchitecture := flag.String("architecture", "", "require this manifest architecture")
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "usage: %s [--architecture ARCH] MANIFEST [MANIFEST ...]\n", os.Args[0])
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
