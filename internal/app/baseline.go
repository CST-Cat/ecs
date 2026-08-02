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
	"os"
	"path/filepath"
	"sort"
	"strings"

	"ecs/internal/i18n"
	"ecs/internal/model"
	reporter "ecs/internal/report"
	"ecs/internal/score"
)

func baselineCommand(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("ecs baseline", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.String("lang", string(i18n.Current()), i18n.T("flag.lang"))
	output := flags.String("output", "ecs-baseline.json", i18n.T("flag.baselineOutput"))
	source := flags.String("source", "", i18n.T("flag.baselineSource"))
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

	// 输入按位置参数给出，方便直接用 shell 展开：ecs baseline reports/*.json
	paths := expandReportPaths(flags.Args())
	if len(paths) == 0 {
		fmt.Fprintln(stderr, i18n.T("help.baselineInputRequired"))
		return 1
	}

	var reports []model.Report
	var skipped []string
	for _, path := range paths {
		data, err := reporter.LoadJSON(path)
		if err != nil {
			// 一份坏报告不该让整批失败，但必须说出来是哪一份、为什么。
			skipped = append(skipped, fmt.Sprintf("%s: %v", filepath.Base(path), err))
			continue
		}
		reports = append(reports, data)
	}
	for _, item := range skipped {
		fmt.Fprintf(stderr, "%s %s\n", i18n.T("baseline.skipped"), item)
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
	return 0
}

// expandReportPaths 展开位置参数，目录按其中的 .json 文件收集。
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
		entries, err := os.ReadDir(arg)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
				continue
			}
			paths = append(paths, filepath.Join(arg, entry.Name()))
		}
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
