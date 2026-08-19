package app

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"

	"ecs/internal/buildinfo"
	comparison "ecs/internal/compare"
	"ecs/internal/config"
	"ecs/internal/i18n"
	"ecs/internal/model"
	reporter "ecs/internal/report"
)

func compareCommand(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("ecs compare", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.String("lang", string(i18n.Current()), i18n.T("flag.lang"))
	formatsFlag := flags.String("format", "json,txt,md,html", i18n.T("flag.format"))
	outputFlag := flags.String("output", "./reports", i18n.T("compare.flag.output"))
	nameFlag := flags.String("name", "", i18n.T("flag.name"))
	referenceFlag := flags.Int("reference", 1, i18n.T("compare.flag.reference"))
	colorFlag := flags.String("color", "auto", i18n.T("flag.color"))
	noColorFlag := flags.Bool("no-color", false, i18n.T("flag.noColor"))
	flags.Usage = func() {
		fmt.Fprintln(stderr, i18n.T("compare.help.usage"))
		flags.PrintDefaults()
	}

	normalized, err := normalizeCompareArgs(args)
	if err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", i18n.T("cli.error"), err)
		return 1
	}
	if err := flags.Parse(normalized); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 1
	}
	paths := flags.Args()
	if len(paths) < 2 {
		fmt.Fprintln(stderr, i18n.T("compare.help.inputs"))
		return 1
	}
	if *referenceFlag < 1 || *referenceFlag > len(paths) {
		fmt.Fprintln(stderr, i18n.Errorf("compare.help.referenceRange", len(paths)))
		return 1
	}
	formats := config.ParseList(*formatsFlag)
	if err := validateComparisonFormats(formats); err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", i18n.T("cli.error"), err)
		return 1
	}

	reports := make([]model.Report, 0, len(paths))
	labels := make([]string, 0, len(paths))
	for _, path := range paths {
		data, loadErr := reporter.LoadJSONForComparison(path)
		if loadErr != nil {
			fmt.Fprintf(stderr, "%s: %s: %v\n", i18n.T("cli.error"), path, loadErr)
			return 1
		}
		// Comparison is a machine-data operation. Keep the canonical report
		// untouched here; localization belongs only to human-facing renderers.
		reports = append(reports, data)
		base := filepath.Base(path)
		labels = append(labels, strings.TrimSuffix(base, filepath.Ext(base)))
	}

	data, err := comparison.Build(reports, comparison.Options{
		Labels: labels,
		Tool: model.ToolInfo{
			Name: buildinfo.Name, Version: buildinfo.Version, Commit: buildinfo.Commit, BuildDate: buildinfo.BuildDate,
		},
		Reference: *referenceFlag - 1,
	})
	if err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", i18n.T("cli.error"), err)
		return 1
	}

	written, err := reporter.WriteComparisonFiles(data, *outputFlag, *nameFlag, formats, reporter.ComparisonOptions{
		TextColor: resolveFileTextColor(*colorFlag, *noColorFlag),
	})
	if err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", i18n.T("cli.error"), err)
		return 1
	}
	fmt.Fprint(stdout, reporter.ComparisonText(data, resolveTerminalColor(*colorFlag, *noColorFlag, stdout)))
	fmt.Fprintln(stdout)
	fmt.Fprintln(stdout, i18n.T("compare.written")+i18n.T("punct.colon"))
	keys := make([]string, 0, len(written))
	for key := range written {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		fmt.Fprintf(stdout, "%s %s\n", strings.ToUpper(key), written[key])
	}
	return 0
}

func validateComparisonFormats(formats []string) error {
	if len(formats) == 0 {
		return i18n.Errorf("err.noFormats")
	}
	allowed := map[string]bool{"json": true, "txt": true, "md": true, "html": true}
	for _, format := range formats {
		if !allowed[format] {
			return i18n.Errorf("err.unknownFormat", format)
		}
	}
	return nil
}

// normalizeCompareArgs lets users put report paths before or after flags. The
// standard flag package stops parsing at its first positional argument, while
// comparisons are naturally written as `ecs compare a.json b.json --output …`.
func normalizeCompareArgs(args []string) ([]string, error) {
	valueFlags := map[string]bool{
		"--lang": true, "-lang": true, "--format": true, "-format": true,
		"--output": true, "-output": true, "--name": true, "-name": true,
		"--reference": true, "-reference": true, "--color": true, "-color": true,
	}
	var flagArgs, paths []string
	positionalOnly := false
	for index := 0; index < len(args); index++ {
		argument := args[index]
		if positionalOnly {
			paths = append(paths, argument)
			continue
		}
		if argument == "--" {
			positionalOnly = true
			continue
		}
		name := argument
		if equal := strings.IndexByte(argument, '='); equal >= 0 {
			name = argument[:equal]
		}
		if valueFlags[name] {
			flagArgs = append(flagArgs, argument)
			if name == argument {
				if index+1 >= len(args) {
					return nil, fmt.Errorf("%s requires a value", argument)
				}
				index++
				flagArgs = append(flagArgs, args[index])
			}
			continue
		}
		if strings.HasPrefix(argument, "-") {
			flagArgs = append(flagArgs, argument)
			continue
		}
		paths = append(paths, argument)
	}
	return append(flagArgs, paths...), nil
}
