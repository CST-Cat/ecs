package app

import (
	"fmt"
	"io"
	"strings"

	"ecs/internal/config"
	"ecs/internal/i18n"
)

func listCommand(args []string, stdout, stderr io.Writer) int {
	format := ""
	for index := 0; index < len(args); index++ {
		switch {
		case args[index] == "--machine":
			format = "machine"
		case args[index] == "--format" && index+1 < len(args):
			format = strings.ToLower(strings.TrimSpace(args[index+1]))
			index++
		case strings.HasPrefix(args[index], "--format="):
			format = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(args[index], "--format=")))
		case args[index] == "--lang" && index+1 < len(args):
			index++
		case strings.HasPrefix(args[index], "--lang="):
		default:
			fmt.Fprintf(stderr, "%s: %s\n", i18n.T("cli.error"), i18n.T("help.extraArgs"))
			return 1
		}
	}
	if format == "machine" || format == "manifest" {
		return writeModuleManifest(stdout)
	}
	if format != "" {
		fmt.Fprintf(stderr, "%s: unsupported list format %q\n", i18n.T("cli.error"), format)
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
	fmt.Fprintln(stdout, "  maxmind, ipinfo, ipregistry, ipapi, ip2location, abuseipdb,")
	fmt.Fprintln(stdout, "  scamalytics, ipqs, dbip, ipdata, ipwhois, ipapicom, ipsb")
	fmt.Fprintln(stdout, "  "+i18n.T("list.sourcesNote"))
	return 0
}

func writeModuleManifest(stdout io.Writer) int {
	fmt.Fprintln(stdout, "ecs-module-manifest\t1")
	for _, profile := range []string{config.ProfileStandard, config.ProfileFull} {
		fmt.Fprintf(stdout, "profile\t%s\t%s\n", profile, strings.Join(config.ModulesForProfile(profile), ","))
	}
	for _, descriptor := range config.ModuleDescriptors() {
		fmt.Fprintf(stdout, "module\t%s\t%s\t%s\n", descriptor.ID, descriptor.Exposure.String(), strings.Join(descriptor.RequiredTools, ","))
	}
	return 0
}
