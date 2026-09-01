package app

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"ecs/internal/buildinfo"
	"ecs/internal/config"
	"ecs/internal/i18n"
	"ecs/internal/model"
	"ecs/internal/probe"
	reporter "ecs/internal/report"
	"ecs/internal/runner"
	"ecs/internal/score"
	"ecs/internal/termcolor"
	"ecs/internal/ui"
)

func runCommand(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	resolved, err := resolveRunConfig(args, stderr)
	if err != nil {
		var parseErr runFlagParseError
		if errors.As(err, &parseErr) {
			if errors.Is(err, flag.ErrHelp) {
				return 0
			}
			// flag.FlagSet already emitted the parse error to stderr.
			return 1
		}
		fmt.Fprintln(stderr, err)
		return 1
	}
	cfg := resolved.Runtime
	terminalColor, colorErr := resolveTerminalColor(resolved.Color, cfg.NoColor, stdout)
	if colorErr != nil {
		fmt.Fprintf(stderr, "%s: %v\n", i18n.T("cli.error"), colorErr)
		return 1
	}
	if resolved.Version {
		fmt.Fprintf(stdout, "%s %s\n", buildinfo.Name, buildinfo.Version)
		return 0
	}
	if resolved.Interactive && !resolved.Yes {
		wizardOK, wizardErr := runWizard(ctx, &cfg)
		if wizardErr != nil {
			if errors.Is(wizardErr, context.Canceled) {
				return 130
			}
			fmt.Fprintf(stderr, "%s: %v\n", i18n.T("cli.error"), wizardErr)
			return 1
		}
		if !wizardOK {
			return 0
		}
	}
	if err := config.Validate(cfg); err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", i18n.T("cli.error"), err)
		return 1
	}
	baseline := score.EmbeddedBaseline()
	if resolved.ScoreBaseline != "" {
		loaded, err := score.LoadBaseline(resolved.ScoreBaseline)
		if err != nil {
			fmt.Fprintf(stderr, "%s: %v\n", i18n.T("cli.error"), i18n.Errorf("err.baselineLoad", resolved.ScoreBaseline, err))
			return 1
		}
		baseline = loaded
	}

	terminal := ui.NewWithColor(stdout, terminalColor)
	terminal.Header(cfg, probe.EstimateFor(cfg))
	progress := terminal.BeginProgress(len(cfg.Modules))
	raw, runErr := func() (model.Report, error) {
		defer progress.Stop()
		return runner.Run(ctx, cfg, func(event runner.Progress) {
			if event.TitleKey != "" {
				if title := i18n.T(event.TitleKey); title != event.TitleKey {
					event.Title = title
				}
			}
			progress.Update(event)
		})
	}()
	if runErr != nil {
		fmt.Fprintf(stderr, "%s: %v\n", i18n.T("cli.error"), runErr)
		return 1
	}
	data := model.RedactedCopy(raw, cfg.Reveal)
	scored := score.Compute(data, baseline)

	files, writeErr := reporter.WriteFilesWithOptions(data, cfg.Output, resolved.Name, cfg.Formats,
		reporter.Options{Score: scored})
	if writeErr != nil {
		terminal.Error("%s: %v", i18n.T("term.writeFailed"), writeErr)
		return 1
	}
	terminal.FullReport(data, files, scored)
	if data.Run.Canceled {
		return 130
	}
	if resolved.Strict && (data.Summary.Errors > 0 || data.Summary.Warnings > 0) {
		return 2
	}
	return 0
}

func resolveTerminalColor(raw string, noColor bool, out io.Writer) (termcolor.Level, error) {
	switch normalized := strings.ToLower(strings.TrimSpace(raw)); normalized {
	case "":
		return termcolor.LevelNone, invalidTerminalColorError(raw)
	case "auto":
		if noColor {
			return termcolor.LevelNone, nil
		}
		return termcolor.Detect(writerIsTerminal(out)), nil
	case "always":
		if noColor {
			return termcolor.LevelNone, nil
		}
		level := termcolor.Detect(true)
		if level == termcolor.LevelNone {
			level = termcolor.LevelANSI256
		}
		return level, nil
	default:
		level, ok := termcolor.ParseLevel(raw)
		if !ok {
			return termcolor.LevelNone, invalidTerminalColorError(raw)
		}
		if noColor {
			return termcolor.LevelNone, nil
		}
		return level, nil
	}
}

func invalidTerminalColorError(raw string) error {
	return fmt.Errorf("invalid terminal color mode %q; choose auto, none, basic, 256, truecolor, or always", raw)
}

func writerIsTerminal(writer io.Writer) bool {
	file, ok := writer.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}
