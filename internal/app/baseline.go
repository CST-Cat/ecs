package app

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"ecs/internal/model"
	"ecs/internal/score"
)

// validateBaselineReport rejects a syntactically valid full report that has
// no scoreable measurements. BuildBaseline is the single source of truth for
// scoreability, so this check cannot drift from the eventual aggregation.
func validateBaselineReport(report model.Report) error {
	_, err := score.BuildBaseline([]model.Report{report}, "")
	return err
}

type duplicateReportPath struct {
	path     string
	previous string
}

type reportPathExpansion struct {
	paths      []string
	duplicates []duplicateReportPath
}

// expandReportPaths 展开位置参数，目录递归收集其中的 .json 文件。
//
// 递归而不是只看直接子文件：提交库按月份分子目录存放，只扫一层会什么都找不到。
func expandReportPaths(args []string) []string {
	return expandReportPathsDetailed(args).paths
}

func expandReportPathsDetailed(args []string) reportPathExpansion {
	var expanded reportPathExpansion
	seen := make(map[string]string)
	appendPath := func(path string) {
		canonical := canonicalReportPath(path)
		if previous, ok := seen[canonical]; ok {
			expanded.duplicates = append(expanded.duplicates, duplicateReportPath{
				path: path, previous: previous,
			})
			return
		}
		seen[canonical] = path
		expanded.paths = append(expanded.paths, path)
	}
	for _, arg := range args {
		info, err := os.Stat(arg)
		if err != nil {
			continue
		}
		if !info.IsDir() {
			appendPath(arg)
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
				appendPath(path)
			}
			return nil
		})
	}
	sort.Strings(expanded.paths)
	sort.SliceStable(expanded.duplicates, func(left, right int) bool {
		if expanded.duplicates[left].path == expanded.duplicates[right].path {
			return expanded.duplicates[left].previous < expanded.duplicates[right].previous
		}
		return expanded.duplicates[left].path < expanded.duplicates[right].path
	})
	return expanded
}

func canonicalReportPath(path string) string {
	cleaned := filepath.Clean(path)
	absolute, err := filepath.Abs(cleaned)
	if err != nil {
		return cleaned
	}
	if resolved, err := filepath.EvalSymlinks(absolute); err == nil {
		return filepath.Clean(resolved)
	}
	return filepath.Clean(absolute)
}
