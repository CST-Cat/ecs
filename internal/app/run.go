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
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		fmt.Fprintln(stderr, err)
		return 1
	}
	if resolved.Version {
		fmt.Fprintf(stdout, "%s %s\n", buildinfo.Name, buildinfo.Version)
		return 0
	}
	cfg := resolved.Runtime
	if resolved.Interactive && !resolved.Yes {
		if !runWizard(&cfg, stdout) {
			return 0
		}
	}
	if err := config.Validate(cfg); err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", i18n.T("cli.error"), err)
		return 1
	}
	if planPath := os.Getenv("ECS_PLAN_FILE"); planPath != "" {
		if err := writeOneShotPlan(planPath, cfg); err != nil {
			fmt.Fprintf(stderr, "%s: %v\n", i18n.T("cli.error"), err)
			return 1
		}
		return 0
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

	terminalColor := resolveTerminalColor(resolved.Color, cfg.NoColor, stdout)
	terminal := ui.NewWithColor(stdout, terminalColor)
	terminal.Header(cfg, probe.EstimateFor(cfg))
	progress := terminal.BeginProgress(len(cfg.Modules))
	raw := func() model.Report {
		defer progress.EndProgress()
		return runner.Run(ctx, cfg, progress.Update)
	}()
	data := model.RedactedCopy(raw, cfg.Reveal)
	scored := score.Compute(data, baseline)

	files, writeErr := reporter.WriteFilesWithOptions(data, cfg.Output, resolved.Name, cfg.Formats,
		reporter.Options{Score: scored})
	if writeErr != nil {
		terminal.Error("%s: %v", i18n.T("term.writeFailed"), writeErr)
		return 1
	}
	terminal.FullReport(reporter.Localize(data), files, scored, terminalColor)
	if data.Run.Canceled {
		return 130
	}
	if resolved.Strict && (data.Summary.Errors > 0 || data.Summary.Warnings > 0) {
		return 2
	}
	return 0
}

func resolveTerminalColor(raw string, noColor bool, out io.Writer) termcolor.Level {
	if noColor {
		return termcolor.LevelNone
	}
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "auto":
		return termcolor.Detect(writerIsTerminal(out))
	case "always":
		level := termcolor.Detect(true)
		if level == termcolor.LevelNone {
			level = termcolor.LevelANSI256
		}
		return level
	}
	if level, ok := termcolor.ParseLevel(raw); ok {
		return level
	}
	return termcolor.LevelNone
}

func writerIsTerminal(writer io.Writer) bool {
	file, ok := writer.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func writeOneShotPlan(path string, cfg config.Runtime) error {
	content := cfg.Profile + "\n" + strings.Join(cfg.Modules, ",") + "\n"
	return os.WriteFile(path, []byte(content), 0o600)
}
