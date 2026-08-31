package app

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"

	"ecs/internal/config"
	"ecs/internal/i18n"
	reporter "ecs/internal/report"
	"ecs/internal/score"
)

func renderCommand(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("ecs render", flag.ContinueOnError)
	flags.SetOutput(stderr)
	input := flags.String("input", "", i18n.T("flag.renderInput"))
	formats := flags.String("format", "json,md,html", i18n.T("flag.format"))
	output := flags.String("output", "", i18n.T("flag.renderOutput"))
	name := flags.String("name", "", i18n.T("flag.name"))
	renderBaseline := flags.String("score-baseline", "", i18n.T("flag.scoreBaseline"))
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 1
	}
	if flags.NArg() != 0 {
		fmt.Fprintf(stderr, "%s %s\n", i18n.T("help.extraArgs"), strings.Join(flags.Args(), " "))
		return 1
	}
	if *input == "" {
		fmt.Fprintln(stderr, i18n.T("help.renderInputRequired"))
		return 1
	}
	outputFormats := config.ParseList(*formats)
	if err := config.ValidateFormats(outputFormats); err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", i18n.T("cli.error"), err)
		return 1
	}
	data, err := reporter.LoadJSON(*input)
	if err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", i18n.T("cli.error"), err)
		return 1
	}
	if *output == "" {
		*output = filepath.Dir(*input)
	}
	if *name == "" {
		base := filepath.Base(*input)
		*name = strings.TrimSuffix(base, filepath.Ext(base))
	}
	baseline := score.EmbeddedBaseline()
	if *renderBaseline != "" {
		loaded, loadErr := score.LoadBaseline(*renderBaseline)
		if loadErr != nil {
			fmt.Fprintf(stderr, "%s: %v\n", i18n.T("cli.error"), i18n.Errorf("err.baselineLoad", *renderBaseline, loadErr))
			return 1
		}
		baseline = loaded
	}
	written, err := reporter.WriteFilesWithOptions(data, *output, *name, outputFormats,
		reporter.Options{Score: score.Compute(data, baseline)})
	if err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", i18n.T("cli.error"), err)
		return 1
	}
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
