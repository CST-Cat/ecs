package app

// leaderboard 子命令：从多份报告聚合评分基线。
//
// 横向比较需要跨机器的样本。这个命令就是那条路径：
// 把多台机器的 JSON 报告喂进来，每个指标取算术平均，写出一份可以用
// --score-baseline 传回去的基线文件。
//
// 刻意不做的事：不上传、不下载、不合并远端基线。样本从哪来、代表什么，
// 由使用者自己掌握——这也是分数能被解释的前提。

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"ecs/internal/buildinfo"
	"ecs/internal/i18n"
	"ecs/internal/model"
	reporter "ecs/internal/report"
	"ecs/internal/score"
)

func leaderboardCommand(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("ecs leaderboard", flag.ContinueOnError)
	flags.SetOutput(stderr)
	output := flags.String("output", "ecs-baseline.json", i18n.T("flag.baselineOutput"))
	source := flags.String("source", "", i18n.T("flag.baselineSource"))
	annotateFlag := flags.Bool("annotate", false, i18n.T("flag.baselineAnnotate"))
	verboseFlag := flags.Bool("verbose", false, i18n.T("flag.baselineVerbose"))
	strictFlag := flags.Bool("strict", false, i18n.T("flag.baselineStrict"))
	flags.Usage = func() {
		fmt.Fprintln(stderr, i18n.T("help.leaderboardUsage"))
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
		fmt.Fprintf(stderr, "%s: %s\n", i18n.T("cli.error"),
			fmt.Sprintf(i18n.T("baseline.strictFailure"), filepath.Base(path), issue))
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
	// 输入按位置参数给出，方便直接用 shell 展开：ecs leaderboard reports/*.json
	expanded := expandReportPathsDetailed(flags.Args())
	for _, issue := range expanded.issues {
		if inputIssue(issue.path, fmt.Errorf("traversal error: %w", issue.err)) {
			return 1
		}
	}
	for _, duplicate := range expanded.duplicates {
		issue := fmt.Errorf("duplicate input path %q (already included as %q)",
			filepath.Base(duplicate.path), filepath.Base(duplicate.previous))
		if inputIssue(duplicate.path, issue) {
			return 1
		}
	}
	paths := expanded.paths
	if len(paths) == 0 {
		fmt.Fprintln(stderr, i18n.T("help.baselineInputRequired"))
		return 1
	}

	// 两种输入都收：完整报告（本地跑完直接用）与瘦身提交（排行榜库里的）。
	// 提交会被转成最小报告，因此聚合只有一条代码路径。
	var reports []model.Report
	providerCounts := make(map[string]int)
	regionCounts := make(map[string]int)
	recordMetadata := func(provider, region string) {
		if strings.TrimSpace(provider) == "" {
			provider = "unknown"
		}
		if strings.TrimSpace(region) == "" {
			region = "unknown"
		}
		providerCounts[provider]++
		regionCounts[region]++
	}
	// Submission.ID identifies an artifact's contents; SampleID identifies the
	// benchmark run. Only the latter is used here so a full report and its
	// derived submission cannot become two leaderboard samples.
	seenSampleIDs := make(map[string]string)
	var outlierSamples []score.OutlierSample
	for _, path := range paths {
		schema, err := readLeaderboardSchema(path)
		if err != nil {
			if inputIssue(path, err) {
				return 1
			}
			continue
		}
		switch schema {
		case score.BaselineSchema:
			// 目录里通常就放着上一次生成的基线，它不是输入。
			if _, err := score.LoadBaseline(path); err != nil {
				if inputIssue(path, fmt.Errorf("baseline validation error: %w", err)) {
					return 1
				}
			}
			continue
		case score.SubmissionSchema:
			submission, err := score.LoadSubmission(path)
			if err != nil {
				if inputIssue(path, fmt.Errorf("submission validation error: %w", err)) {
					return 1
				}
				continue
			}
			if previous, duplicated := seenSampleIDs[submission.SampleID]; duplicated {
				issue := duplicateSampleIssue(submission.SampleID, previous)
				if inputIssue(path, issue) {
					return 1
				}
				continue
			}
			seenSampleIDs[submission.SampleID] = path
			outlierSamples = append(outlierSamples, submission.OutlierSample())
			recordMetadata(submission.Host.Provider, submission.Host.Region)
			reports = append(reports, submission.AsReport())
			continue
		case buildinfo.SchemaVersion:
			data, err := reporter.LoadJSON(path)
			if err != nil {
				// 一份坏输入默认不该让整批失败，但必须说出来是哪一份、为什么。
				if inputIssue(path, fmt.Errorf("report validation error: %w", err)) {
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
			sampleID, err := score.SampleIDForRunID(data.Run.ID)
			if err != nil {
				if inputIssue(path, err) {
					return 1
				}
				continue
			}
			if previous, duplicated := seenSampleIDs[sampleID]; duplicated {
				issue := duplicateSampleIssue(sampleID, previous)
				if inputIssue(path, issue) {
					return 1
				}
				continue
			}
			seenSampleIDs[sampleID] = path
			if sample, err := score.OutlierSampleFromReport(data); err != nil {
				if inputIssue(path, fmt.Errorf("outlier projection: %w", err)) {
					return 1
				}
			} else {
				outlierSamples = append(outlierSamples, sample)
			}
			provider, region := score.ExtractSubmissionMetadata(data)
			recordMetadata(provider, region)
			reports = append(reports, data)
		default:
			if inputIssue(path, fmt.Errorf("unsupported ECS artifact schema %q", schema)) {
				return 1
			}
		}
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
	printMetadataCounts(stdout, providerCounts, regionCounts)

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
	if len(outlierSamples) > 0 {
		outliers := score.DetectOutliers(outlierSamples)
		if len(outliers.Outliers) > 0 {
			fmt.Fprintf(stdout, "\n%s\n", i18n.T("baseline.outliersHeader"))
			for _, item := range outliers.Outliers {
				fmt.Fprintf(stdout, "  %s\n", item.Describe())
				if annotate {
					// GitHub Actions 注解：让离群在检查页面上直接可见。
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

func duplicateSampleIssue(sampleID, previous string) error {
	return fmt.Errorf("duplicate sample %q (already included as %q)", sampleID, filepath.Base(previous))
}

func printMetadataCounts(stdout io.Writer, providerCounts, regionCounts map[string]int) {
	fmt.Fprintf(stdout, "\n%s\n", i18n.T("baseline.metadataHeader"))
	for _, item := range sortedMetadataCounts(providerCounts) {
		fmt.Fprintf(stdout, "  %s\n", fmt.Sprintf(i18n.T("baseline.providerSamples"), item.value, item.count))
	}
	for _, item := range sortedMetadataCounts(regionCounts) {
		fmt.Fprintf(stdout, "  %s\n", fmt.Sprintf(i18n.T("baseline.regionSamples"), item.value, item.count))
	}
}

type metadataCount struct {
	value string
	count int
}

func sortedMetadataCounts(values map[string]int) []metadataCount {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	items := make([]metadataCount, 0, len(keys))
	for _, key := range keys {
		items = append(items, metadataCount{value: key, count: values[key]})
	}
	return items
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
