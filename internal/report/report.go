package report

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"

	"ecs/internal/buildinfo"
	"ecs/internal/i18n"
	"ecs/internal/model"
	"ecs/internal/score"
)

// reportSchemaFamily 是报告 schema 标识的家族前缀。
//
// 比较路径放宽的是版本号，不是格式：不同 ecs.report/ 版本可以放在一起尽力比较，
// 但一个完全陌生的 schema 说明这份 JSON 根本不是 ecs 报告，仍然拒绝。
const reportSchemaFamily = "ecs.report/"

// Options 控制报告生成。
type Options struct {
	// Score 是可选的综合评分。
	Score *score.Report
}

func WriteFiles(data model.Report, directory, baseName string, formats []string) (map[string]string, error) {
	return WriteFilesWithOptions(data, directory, baseName, formats, Options{})
}

func WriteFilesWithOptions(data model.Report, directory, baseName string, formats []string, options Options) (map[string]string, error) {
	if directory == "" {
		directory = "./reports"
	}
	absolute, err := filepath.Abs(directory)
	if err != nil {
		return nil, i18n.Errorf("err.reportOutputDir", err)
	}
	if err := os.MkdirAll(absolute, 0o700); err != nil {
		return nil, i18n.Errorf("err.reportCreateDir", err)
	}
	if baseName == "" {
		baseName = "ecs-report-" + data.Run.StartedAt.Format("20060102-150405")
	}
	baseName = sanitizeBaseName(baseName)
	// Render every requested format before touching the filesystem. JSON is the
	// canonical machine artifact; human formats resolve stable keys directly
	// from the same input report. This keeps renderer failures from leaving a
	// partially generated set of new files.
	contents := make(map[string][]byte, len(formats))
	orderedFormats := make([]string, 0, len(formats))
	for _, format := range formats {
		if _, seen := contents[format]; seen {
			continue
		}
		var content []byte
		switch format {
		case "json":
			content, err = JSON(data)
		case "md":
			content = []byte(markdownReport(data, options.Score))
		case "html":
			content, err = htmlReport(data, options.Score)
		default:
			err = i18n.Errorf("err.reportUnknownFormat", format)
		}
		if err != nil {
			return nil, i18n.Errorf("err.reportGenerate", format, err)
		}
		contents[format] = content
		orderedFormats = append(orderedFormats, format)
	}
	written := make(map[string]string, len(orderedFormats))
	for _, format := range orderedFormats {
		content := contents[format]
		path := filepath.Join(absolute, baseName+"."+format)
		if err := atomicWrite(path, content, 0o600); err != nil {
			return written, i18n.Errorf("err.reportWrite", format, err)
		}
		written[format] = path
	}
	return written, nil
}

func JSON(data model.Report) ([]byte, error) {
	content, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(content, '\n'), nil
}

// LoadJSON 读取一份报告，并要求它的 schema 版本与本二进制完全一致。
//
// run 只生成当前 schema 的报告；render、submit、baseline 和 leaderboard 等读取报告的
// 命令走这条路，把输入当作当前 schema 的实例解释。版本不符就意味着字段语义可能已经
// 变了，继续下去只会得到看似合理的错误结论。
func LoadJSON(path string) (model.Report, error) {
	return loadJSON(path, true)
}

// LoadJSONForComparison 读取一份用于比较的报告，允许它的 schema 版本与本二进制
// 不同。除这一点外，检查与 LoadJSON 完全相同。
//
// 为什么比较可以放宽，而 run/submit 不行：真正防止"拿不可比的数字作比较"的是
// 指标签名（key + method + unit + direction + 逐个参数），不是版本号。本项目
// 约定工作负载语义变了就升 measurement.method，因此跨版本的语义变化必然表现为
// 签名不一致，会落进 MetricIssue 并由 Differences 逐分量说明——那比拒绝加载
// 信息量严格更大。
//
// 硬拒绝的代价是实打实的：schema 一旦升版，用户手里所有旧报告立刻永久不可比，
// 而"比较不同时期的报告"正是 compare 存在的理由。
//
// 仍然拒绝空版本和非 ecs.report 家族的输入：放宽的是版本，不是格式。签名覆盖
// 不到的字段（status 枚举、evidence 口径）跨版本仍可能改变含义，因此调用方要
// 把比较结果降级并显式提示，见 compare.Build。
func LoadJSONForComparison(path string) (model.Report, error) {
	return loadJSON(path, false)
}

func loadJSON(path string, requireExactSchema bool) (model.Report, error) {
	var data model.Report
	file, err := os.Open(path)
	if err != nil {
		return data, err
	}
	defer file.Close()
	if info, err := file.Stat(); err == nil && info.Size() > 32*1024*1024 {
		return data, i18n.Errorf("err.reportTooLarge")
	}
	decoder := json.NewDecoder(file)
	if err := decoder.Decode(&data); err != nil {
		return data, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return data, i18n.Errorf("err.reportSingleObject")
		}
		return data, i18n.Errorf("err.reportTrailing", err)
	}
	if data.SchemaVersion == "" {
		return data, i18n.Errorf("err.reportNoSchema")
	}
	if requireExactSchema {
		if data.SchemaVersion != buildinfo.SchemaVersion {
			return data, i18n.Errorf("err.reportSchemaMismatch", data.SchemaVersion, buildinfo.SchemaVersion)
		}
	} else if !strings.HasPrefix(data.SchemaVersion, reportSchemaFamily) {
		return data, i18n.Errorf("err.reportSchemaFamily", data.SchemaVersion, reportSchemaFamily)
	}
	for index := range data.Results {
		if data.Results[index].Evidence != nil {
			data.Results[index].Evidence.Normalize()
		}
	}
	return data, nil
}

func atomicWrite(path string, content []byte, mode os.FileMode) error {
	directory := filepath.Dir(path)
	temp, err := os.CreateTemp(directory, ".ecs-report-*")
	if err != nil {
		return err
	}
	tempName := temp.Name()
	cleanup := func() {
		_ = temp.Close()
		_ = os.Remove(tempName)
	}
	if err := temp.Chmod(mode); err != nil {
		cleanup()
		return err
	}
	if _, err := temp.Write(content); err != nil {
		cleanup()
		return err
	}
	if err := temp.Sync(); err != nil {
		cleanup()
		return err
	}
	if err := temp.Close(); err != nil {
		_ = os.Remove(tempName)
		return err
	}
	if err := os.Rename(tempName, path); err != nil {
		_ = os.Remove(tempName)
		return err
	}
	return nil
}

func sanitizeBaseName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "ecs-report"
	}
	var builder strings.Builder
	for _, runeValue := range value {
		switch {
		case runeValue >= 'a' && runeValue <= 'z':
			builder.WriteRune(runeValue)
		case runeValue >= 'A' && runeValue <= 'Z':
			builder.WriteRune(runeValue)
		case runeValue >= '0' && runeValue <= '9':
			builder.WriteRune(runeValue)
		case runeValue == '-', runeValue == '_', runeValue == '.':
			builder.WriteRune(runeValue)
		default:
			builder.WriteRune('-')
		}
	}
	result := strings.Trim(builder.String(), ".-")
	if result == "" {
		return "ecs-report"
	}
	return result
}
