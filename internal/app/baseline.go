package app

import (
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"ecs/internal/model"
	"ecs/internal/score"
)

func baselineCommand(args []string, stdout, stderr io.Writer) int {
	return leaderboardCommandNamed("baseline", args, stdout, stderr)
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
