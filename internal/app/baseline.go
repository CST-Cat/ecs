package app

// baseline 子命令：从多份报告聚合评分基线。
//
// 内置基线只是单机快照，横向比较需要跨机器的样本。这个命令就是那条路径：
// 把多台机器的 JSON 报告喂进来，每个指标取中位数，写出一份可以用
// --score-baseline 传回去的基线文件。
//
// 刻意不做的事：不上传、不下载、不合并远端基线。样本从哪来、代表什么，
// 由使用者自己掌握——这也是分数能被解释的前提。

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"ecs/internal/i18n"
	"ecs/internal/model"
	reporter "ecs/internal/report"
	"ecs/internal/score"
)

// submitCommand 从完整报告导出一份可公开入库的瘦身提交。
func submitCommand(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("ecs submit", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.String("lang", string(i18n.Current()), i18n.T("flag.lang"))
	input := flags.String("input", "", i18n.T("flag.submitInput"))
	output := flags.String("output", "", i18n.T("flag.submitOutput"))
	region := flags.String("region", "", i18n.T("flag.submitRegion"))
	provider := flags.String("provider", "", i18n.T("flag.submitProvider"))
	note := flags.String("note", "", i18n.T("flag.submitNote"))
	flags.Usage = func() {
		fmt.Fprintln(stderr, i18n.T("help.submitUsage"))
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 1
	}
	outputGiven := false
	flags.Visit(func(flag *flag.Flag) {
		if flag.Name == "output" {
			outputGiven = true
		}
	})
	if outputGiven {
		if err := preflightSubmissionOutput(*output); err != nil {
			fmt.Fprintf(stderr, "%s: %v\n", i18n.T("cli.error"), err)
			return 1
		}
	}
	if *input == "" {
		fmt.Fprintln(stderr, i18n.T("help.renderInputRequired"))
		return 1
	}
	data, err := reporter.LoadJSON(*input)
	if err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", i18n.T("cli.error"), err)
		return 1
	}
	submission, err := score.BuildSubmission(data, score.SubmissionOptions{
		Region: *region, Provider: *provider, Note: *note,
	})
	if err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", i18n.T("cli.error"), err)
		return 1
	}
	if err := submission.Validate(); err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", i18n.T("cli.error"), err)
		return 1
	}
	content, err := submission.Encode()
	if err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", i18n.T("cli.error"), err)
		return 1
	}
	target, err := resolveSubmissionTarget(*output, submission.FileName(), outputGiven)
	if err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", i18n.T("cli.error"), err)
		return 1
	}
	if err := writeSubmissionExclusive(target, content); err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", i18n.T("cli.error"), err)
		return 1
	}
	fmt.Fprintf(stdout, "%s %s\n", i18n.T("submit.written"), target)
	fmt.Fprintf(stdout, "%s\n", fmt.Sprintf(i18n.T("submit.summary"),
		submission.Host.VCPU, submission.Host.MemoryGiB, len(submission.Metrics)))
	fmt.Fprintf(stdout, "%s\n", i18n.T("submit.privacyNote"))
	return 0
}

// validateSubmissionPath rejects paths that cannot be represented safely in a
// shell-produced command or a filesystem error message. NUL cannot normally
// occur in a Go string received from argv, but checking it keeps this helper
// safe for direct callers and tests as well.
func validateSubmissionPath(path string) error {
	if path == "" {
		return fmt.Errorf("submission output path must not be empty")
	}
	if strings.IndexFunc(path, func(r rune) bool { return r == 0 || r < 0x20 || r == 0x7f }) >= 0 {
		return fmt.Errorf("submission output path contains a control character")
	}
	return nil
}

func validateSubmissionParent(path string) error {
	// Check every existing component, not just the final directory. Otherwise
	// `safe-link/submission.json` could still follow a symlink in `safe-link`
	// even though the final target itself is not one.
	for current := filepath.Clean(path); ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("submission output parent does not exist: %s", current)
			}
			return fmt.Errorf("inspect submission output parent %s: %w", current, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("submission output parent must not be a symlink: %s", current)
		}
		if !info.IsDir() {
			return fmt.Errorf("submission output parent is not a directory: %s", current)
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
	}
	return nil
}

// preflightSubmissionOutput rejects an explicitly supplied target before the
// report is loaded. Existing files are never overwritten; directories are
// accepted as destinations but are never created by ecs submit.
func preflightSubmissionOutput(path string) error {
	if err := validateSubmissionPath(path); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	switch {
	case err == nil:
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("submission output must not be a symlink: %s", path)
		}
		if info.IsDir() {
			return validateSubmissionParent(path)
		}
		return fmt.Errorf("submission output already exists: %s", path)
	case errors.Is(err, os.ErrNotExist):
		return validateSubmissionParent(filepath.Dir(path))
	default:
		return fmt.Errorf("inspect submission output %s: %w", path, err)
	}
}

func resolveSubmissionTarget(output, fileName string, explicit bool) (string, error) {
	if explicit {
		if err := validateSubmissionPath(output); err != nil {
			return "", err
		}
		info, err := os.Lstat(output)
		if err == nil {
			if info.Mode()&os.ModeSymlink != 0 {
				return "", fmt.Errorf("submission output must not be a symlink: %s", output)
			}
			if info.IsDir() {
				return filepath.Join(output, fileName), nil
			}
			return "", fmt.Errorf("submission output already exists: %s", output)
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("inspect submission output %s: %w", output, err)
		}
		return output, nil
	}

	if err := validateSubmissionParent(os.TempDir()); err != nil {
		return "", err
	}
	return filepath.Join(os.TempDir(), fileName), nil
}

// writeSubmissionExclusive writes complete content to a new path without
// following or replacing an existing file. A fully synced temporary inode is
// hard-linked into place; link(2) fails atomically when the target already
// exists (including a symlink or hardlink), unlike rename which would replace
// it.
func writeSubmissionExclusive(path string, content []byte) error {
	if err := validateSubmissionPath(path); err != nil {
		return err
	}
	parent := filepath.Dir(path)
	if err := validateSubmissionParent(parent); err != nil {
		return err
	}
	if _, err := os.Lstat(path); err == nil {
		return fmt.Errorf("submission output already exists: %s", path)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect submission output %s: %w", path, err)
	}

	temporary, err := os.CreateTemp(parent, ".ecs-submit-*")
	if err != nil {
		return fmt.Errorf("create temporary submission: %w", err)
	}
	temporaryName := temporary.Name()
	removeTemporary := func() {
		_ = temporary.Close()
		_ = os.Remove(temporaryName)
	}
	defer func() { _ = os.Remove(temporaryName) }()
	if err := temporary.Chmod(0o600); err != nil {
		removeTemporary()
		return fmt.Errorf("set temporary submission permissions: %w", err)
	}
	if _, err := temporary.Write(content); err != nil {
		removeTemporary()
		return fmt.Errorf("write temporary submission: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		removeTemporary()
		return fmt.Errorf("sync temporary submission: %w", err)
	}
	if err := temporary.Close(); err != nil {
		_ = os.Remove(temporaryName)
		return fmt.Errorf("close temporary submission: %w", err)
	}
	if err := os.Link(temporaryName, path); err != nil {
		_ = os.Remove(temporaryName)
		return fmt.Errorf("create submission without overwriting target: %w", err)
	}
	if directory, err := os.Open(parent); err == nil {
		_ = directory.Sync()
		_ = directory.Close()
	}
	if err := os.Remove(temporaryName); err != nil {
		return fmt.Errorf("remove temporary submission link: %w", err)
	}
	return nil
}

func baselineCommand(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("ecs baseline", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.String("lang", string(i18n.Current()), i18n.T("flag.lang"))
	output := flags.String("output", "ecs-baseline.json", i18n.T("flag.baselineOutput"))
	source := flags.String("source", "", i18n.T("flag.baselineSource"))
	annotateFlag := flags.Bool("annotate", false, i18n.T("flag.baselineAnnotate"))
	verboseFlag := flags.Bool("verbose", false, i18n.T("flag.baselineVerbose"))
	strictFlag := flags.Bool("strict", false, i18n.T("flag.baselineStrict"))
	flags.Usage = func() {
		fmt.Fprintln(stderr, i18n.T("help.baselineUsage"))
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 1
	}

	annotate, verbose, strict := *annotateFlag, *verboseFlag, *strictFlag
	inputIssue := func(path string, issue error) bool {
		if !strict {
			skipped := fmt.Sprintf("%s: %v", filepath.Base(path), issue)
			fmt.Fprintf(stderr, "%s %s\n", i18n.T("baseline.skipped"), skipped)
			return false
		}
		fmt.Fprintln(stderr, fmt.Sprintf(i18n.T("baseline.strictFailure"), filepath.Base(path), issue))
		return true
	}
	if strict {
		// expandReportPaths deliberately ignores missing paths for the default
		// batch workflow.  Strict mode must make those mistakes visible before
		// any aggregation or output write can occur.
		for _, path := range flags.Args() {
			if _, err := os.Stat(path); err != nil {
				inputIssue(path, err)
				return 1
			}
		}
	}
	// 输入按位置参数给出，方便直接用 shell 展开：ecs baseline reports/*.json
	paths := expandReportPaths(flags.Args())
	if len(paths) == 0 {
		fmt.Fprintln(stderr, i18n.T("help.baselineInputRequired"))
		return 1
	}

	// 两种输入都收：完整报告（本地跑完直接用）与瘦身提交（排行榜库里的）。
	// 提交会被转成最小报告，因此聚合只有一条代码路径。
	var reports []model.Report
	var submissions []score.Submission
	seen := make(map[string]string)
	for _, path := range paths {
		// 目录里通常就放着上一次生成的基线，它不是输入。
		if _, err := score.LoadBaseline(path); err == nil {
			continue
		}
		if submission, err := score.LoadSubmission(path); err == nil {
			if previous, duplicated := seen[submission.ID]; duplicated {
				issue := fmt.Errorf("%s (%s)", i18n.T("baseline.duplicate"), filepath.Base(previous))
				if inputIssue(path, issue) {
					return 1
				}
				continue
			}
			seen[submission.ID] = path
			submissions = append(submissions, submission)
			reports = append(reports, submission.AsReport())
			continue
		}
		data, err := reporter.LoadJSON(path)
		if err != nil {
			// 一份坏输入默认不该让整批失败，但必须说出来是哪一份、为什么。
			if inputIssue(path, err) {
				return 1
			}
			continue
		}
		if err := validateBaselineReport(data); err != nil {
			if inputIssue(path, err) {
				return 1
			}
			continue
		}
		reports = append(reports, data)
	}
	if len(reports) == 0 {
		fmt.Fprintf(stderr, "%s: %v\n", i18n.T("cli.error"), i18n.Errorf("err.baselineNoReports"))
		return 1
	}

	baseline, err := score.BuildBaseline(reports, *source)
	if err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", i18n.T("cli.error"), err)
		return 1
	}
	content, err := baseline.Encode()
	if err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", i18n.T("cli.error"), err)
		return 1
	}
	if err := os.WriteFile(*output, content, 0o600); err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", i18n.T("cli.error"), err)
		return 1
	}

	fmt.Fprintf(stdout, "%s %s\n", i18n.T("baseline.written"), *output)
	fmt.Fprintf(stdout, "%s\n", fmt.Sprintf(i18n.T("baseline.summary"), len(reports), len(baseline.Metrics)))

	// 逐项列出样本数：某个指标只有一两台机器测到时，它对基线的代表性远低于
	// 其他项，使用者应当看得到这件事。
	counts := score.MetricSampleCounts(reports)
	keys := make([]string, 0, len(baseline.Metrics))
	for key := range baseline.Metrics {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		fmt.Fprintf(stdout, "  %-22s %14.2f  %s\n", key, baseline.Metrics[key],
			fmt.Sprintf(i18n.T("baseline.sampleCount"), counts[key]))
	}
	if missing := missingMetrics(baseline); len(missing) > 0 {
		fmt.Fprintf(stdout, "%s %s\n", i18n.T("baseline.missingMetrics"), strings.Join(missing, ", "))
	}

	// 分档情况：样本够的档位才会被评分实际采用，这里如实列出。
	if len(baseline.Tiers) > 0 {
		fmt.Fprintf(stdout, "\n%s\n", i18n.T("baseline.tiersHeader"))
		for _, tier := range baseline.Tiers {
			status := i18n.T("baseline.tierActive")
			if tier.SampleCount < score.MinTierSamples() {
				status = fmt.Sprintf(i18n.T("baseline.tierFallback"), score.MinTierSamples())
			}
			fmt.Fprintf(stdout, "  %-14s %s  %s\n",
				score.TierLabel(tier.VCPUMin),
				fmt.Sprintf(i18n.T("baseline.sampleCount"), tier.SampleCount),
				status)
		}
	}

	// 离群只标记不阻断：可能是新硬件或特殊配置，由维护者判断是否收录。
	if len(submissions) > 0 {
		outliers := score.DetectOutliers(submissions)
		if len(outliers.Outliers) > 0 {
			fmt.Fprintf(stdout, "\n%s\n", i18n.T("baseline.outliersHeader"))
			for _, item := range outliers.Outliers {
				fmt.Fprintf(stdout, "  %s\n", item.Describe())
				if annotate {
					// GitHub Actions 注解：让离群在 PR 页面上直接可见。
					fmt.Fprintf(stdout, "::warning::%s\n", item.Describe())
				}
			}
			fmt.Fprintf(stdout, "  %s\n", i18n.T("baseline.outlierNote"))
		}
		if len(outliers.Undecidable) > 0 && verbose {
			fmt.Fprintf(stdout, "\n%s\n", i18n.T("baseline.undecidableHeader"))
			for _, item := range outliers.Undecidable {
				fmt.Fprintf(stdout, "  %s\n", item)
			}
		}
	}
	return 0
}

// validateBaselineReport rejects a syntactically valid full report that has
// no scoreable measurements. BuildBaseline is the single source of truth for
// scoreability, so this check cannot drift from the eventual aggregation.
func validateBaselineReport(report model.Report) error {
	_, err := score.BuildBaseline([]model.Report{report}, "")
	return err
}

// expandReportPaths 展开位置参数，目录递归收集其中的 .json 文件。
//
// 递归而不是只看直接子文件：提交库按月份分子目录存放，只扫一层会什么都找不到。
func expandReportPaths(args []string) []string {
	var paths []string
	for _, arg := range args {
		info, err := os.Stat(arg)
		if err != nil {
			continue
		}
		if !info.IsDir() {
			paths = append(paths, arg)
			continue
		}
		_ = filepath.WalkDir(arg, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				// 单个不可读的子目录不该中断整次收集。
				return nil
			}
			if entry.IsDir() {
				// 隐藏目录（.git 之类）不参与。
				if name := entry.Name(); name != "." && strings.HasPrefix(name, ".") {
					return fs.SkipDir
				}
				return nil
			}
			if strings.HasSuffix(entry.Name(), ".json") {
				paths = append(paths, path)
			}
			return nil
		})
	}
	sort.Strings(paths)
	return paths
}

// missingMetrics 列出定义了但这批报告里没测到的指标。
func missingMetrics(baseline score.Baseline) []string {
	var missing []string
	for _, dimension := range score.Dimensions() {
		for _, metric := range dimension.Metrics {
			if _, ok := baseline.Metrics[metric.Key]; !ok {
				missing = append(missing, metric.Key)
			}
		}
	}
	return missing
}
