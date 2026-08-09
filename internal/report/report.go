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
	"ecs/internal/termcolor"
)

// Options 控制报告生成。
type Options struct {
	// TextColor 是纯文本报告的颜色档位。写入文件默认无色：文件会被 diff、
	// 贴进不解析转义序列的地方，转义码在那里就是可见垃圾。需要彩色文件时
	// 由调用方显式指定。
	TextColor termcolor.Level
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
	// 选了哪种语言，全部输出就用哪种语言，JSON 也不例外。
	// 机器标识符（模块 id、measurement.key、method、unit、status、
	// methodology.kind）本来就不参与翻译，下游按这些字段解析不受影响。
	localized := Localize(data)
	written := make(map[string]string)
	for _, format := range formats {
		var content []byte
		switch format {
		case "json":
			content, err = JSON(localized)
		case "md":
			content = []byte(Markdown(localized, options.Score))
		case "html":
			content, err = HTML(localized, options.Score)
		case "txt":
			content = []byte(Text(localized, TextOptions{Color: options.TextColor, Score: options.Score}))
		default:
			err = i18n.Errorf("err.reportUnknownFormat", format)
		}
		if err != nil {
			return written, i18n.Errorf("err.reportGenerate", format, err)
		}
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

func LoadJSON(path string) (model.Report, error) {
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
	if data.SchemaVersion != buildinfo.SchemaVersion {
		return data, i18n.Errorf("err.reportSchemaMismatch", data.SchemaVersion, buildinfo.SchemaVersion)
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
