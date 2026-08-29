package app

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"ecs/internal/model"
	"ecs/internal/score"
)

const leaderboardEnvelopeLimit int64 = 32 * 1024 * 1024

type leaderboardInputEnvelope struct {
	Schema        string `json:"schema"`
	SchemaVersion string `json:"schema_version"`
}

// readLeaderboardSchema reads the top-level discriminant before a loader
// validates the complete input. The size bound keeps malformed or untrusted
// paths from turning identification into an unbounded read; the selected
// loader retains its own, tighter contract.
func readLeaderboardSchema(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	if info, err := file.Stat(); err == nil && info.Size() > leaderboardEnvelopeLimit {
		return "", fmt.Errorf("ECS artifact envelope exceeds the 32 MiB safety limit")
	}
	var envelope leaderboardInputEnvelope
	decoder := json.NewDecoder(io.LimitReader(file, leaderboardEnvelopeLimit+1))
	if err := decoder.Decode(&envelope); err != nil {
		return "", fmt.Errorf("read ECS artifact schema envelope: %w", err)
	}
	if envelope.Schema != "" && envelope.SchemaVersion != "" && envelope.Schema != envelope.SchemaVersion {
		return "", fmt.Errorf("unsupported ECS artifact schema: conflicting schema %q and schema_version %q", envelope.Schema, envelope.SchemaVersion)
	}
	if envelope.Schema != "" {
		return envelope.Schema, nil
	}
	return envelope.SchemaVersion, nil
}

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

type reportPathIssue struct {
	path string
	err  error
}

type reportPathExpansion struct {
	paths      []string
	duplicates []duplicateReportPath
	issues     []reportPathIssue
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
		walkErr := filepath.WalkDir(arg, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				// 单个不可读的子目录不该中断整次收集。
				expanded.issues = append(expanded.issues, reportPathIssue{path: path, err: err})
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
		if walkErr != nil {
			expanded.issues = append(expanded.issues, reportPathIssue{path: arg, err: walkErr})
		}
	}
	sort.Strings(expanded.paths)
	sort.SliceStable(expanded.duplicates, func(left, right int) bool {
		if expanded.duplicates[left].path == expanded.duplicates[right].path {
			return expanded.duplicates[left].previous < expanded.duplicates[right].previous
		}
		return expanded.duplicates[left].path < expanded.duplicates[right].path
	})
	sort.SliceStable(expanded.issues, func(left, right int) bool {
		if expanded.issues[left].path == expanded.issues[right].path {
			return expanded.issues[left].err.Error() < expanded.issues[right].err.Error()
		}
		return expanded.issues[left].path < expanded.issues[right].path
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
