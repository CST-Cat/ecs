package app

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"ecs/internal/config"
	"ecs/internal/i18n"
)

func listCommand(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("ecs list", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.String("lang", string(i18n.Current()), i18n.T("flag.lang"))
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 1
	}
	if flags.NArg() != 0 {
		fmt.Fprintf(stderr, "%s: %s\n", i18n.T("cli.error"), i18n.T("help.extraArgs"))
		return 1
	}
	fmt.Fprintln(stdout, i18n.T("list.profilesHeader"))
	for _, profile := range []string{config.ProfileStandard, config.ProfileFull} {
		fmt.Fprintf(stdout, "  %-10s %s\n", profile, i18n.T("profile."+profile))
	}
	fmt.Fprintln(stdout, "\n"+i18n.T("list.modulesHeader"))
	for _, descriptor := range config.ModuleDescriptors() {
		descriptionKey := descriptor.DescriptionKey
		if descriptionKey == "" {
			descriptionKey = "module." + descriptor.ID + ".desc"
		}
		fmt.Fprintf(stdout, "  %-10s %-11s %s\n", descriptor.ID, descriptor.Exposure.String(), i18n.T(descriptionKey))
	}
	fmt.Fprintln(stdout, "\n"+i18n.T("list.exposureHeader"))
	for _, name := range config.ExposureNames() {
		fmt.Fprintf(stdout, "  %-11s %s\n", name, i18n.T("exposure."+name))
	}
	fmt.Fprintln(stdout, "  "+i18n.T("list.exposureNote"))
	fmt.Fprintln(stdout, "\n"+i18n.T("list.sourcesHeader"))
	fmt.Fprintf(stdout, "  %s\n", strings.Join(config.IPQualitySourceIDs(), ", "))
	fmt.Fprintln(stdout, "  "+i18n.T("list.sourcesNote"))
	return 0
}
