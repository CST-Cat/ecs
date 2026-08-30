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
	if err := model.ValidateReportIdentity(data); err != nil {
		return nil, err
	}
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
	return loadJSON(path)
}

func loadJSON(path string) (model.Report, error) {
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
	decoder.DisallowUnknownFields()
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
	if data.SchemaVersion != buildinfo.SchemaVersion {
		return data, i18n.Errorf("err.reportSchemaMismatch", data.SchemaVersion, buildinfo.SchemaVersion)
	}
	for index := range data.Results {
		if data.Results[index].Evidence != nil {
			data.Results[index].Evidence.Normalize()
		}
	}
	if err := model.ValidateReportIdentity(data); err != nil {
		return data, err
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
