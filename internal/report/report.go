package report

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"ecs/internal/buildinfo"
	"ecs/internal/model"
)

func WriteFiles(data model.Report, directory, baseName string, formats []string) (map[string]string, error) {
	if directory == "" {
		directory = "./reports"
	}
	absolute, err := filepath.Abs(directory)
	if err != nil {
		return nil, fmt.Errorf("解析输出目录: %w", err)
	}
	if err := os.MkdirAll(absolute, 0o700); err != nil {
		return nil, fmt.Errorf("创建输出目录: %w", err)
	}
	if baseName == "" {
		baseName = "ecs-report-" + data.Run.StartedAt.Format("20060102-150405")
	}
	baseName = sanitizeBaseName(baseName)
	written := make(map[string]string)
	for _, format := range formats {
		var content []byte
		switch format {
		case "json":
			content, err = JSON(data)
		case "md":
			content = []byte(Markdown(data))
		case "html":
			content, err = HTML(data)
		default:
			err = fmt.Errorf("未知报告格式 %q", format)
		}
		if err != nil {
			return written, fmt.Errorf("生成 %s 报告: %w", format, err)
		}
		path := filepath.Join(absolute, baseName+"."+format)
		if err := atomicWrite(path, content, 0o600); err != nil {
			return written, fmt.Errorf("写入 %s 报告: %w", format, err)
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
		return data, fmt.Errorf("报告文件超过 32 MiB 安全上限")
	}
	decoder := json.NewDecoder(file)
	if err := decoder.Decode(&data); err != nil {
		return data, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return data, fmt.Errorf("报告文件只能包含一个 JSON 对象")
		}
		return data, fmt.Errorf("报告文件尾部存在无效内容: %w", err)
	}
	if data.SchemaVersion == "" {
		return data, fmt.Errorf("缺少 schema_version")
	}
	if data.SchemaVersion != buildinfo.SchemaVersion {
		return data, fmt.Errorf("不支持 schema_version %q，当前渲染器支持 %q", data.SchemaVersion, buildinfo.SchemaVersion)
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
