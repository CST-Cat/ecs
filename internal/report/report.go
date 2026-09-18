package report

import (
	"bytes"
	"encoding/json"
	"fmt"
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

func WriteFilesWithOptions(data model.Report, directory, baseName string, formats []string, options Options) (map[string]string, error) {
	if directory == "" {
		directory = "./reports"
	}
	absolute, err := filepath.Abs(directory)
	if err != nil {
		return nil, i18n.Errorf("err.reportOutputDir", err)
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
	if err := os.MkdirAll(absolute, 0o700); err != nil {
		return nil, i18n.Errorf("err.reportCreateDir", err)
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

// JSON 序列化一份报告。身份契约由上游 owner 保证：runner.Run 校验自己生成的
// 报告，LoadJSON 校验外部读入的报告，这里不再重复检查。
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
	var data model.Report
	file, err := os.Open(path)
	if err != nil {
		return data, err
	}
	defer file.Close()
	if info, err := file.Stat(); err == nil && info.Size() > 32*1024*1024 {
		return data, i18n.Errorf("err.reportTooLarge")
	}
	content, err := io.ReadAll(io.LimitReader(file, 32*1024*1024+1))
	if err != nil {
		return data, err
	}
	return ParseJSON(content)
}

// ParseJSON validates one complete report from cached bytes. Callers that
// already read an artifact can use this to avoid reopening the same path.
func ParseJSON(content []byte) (model.Report, error) {
	var data model.Report
	if int64(len(content)) > 32*1024*1024 {
		return data, i18n.Errorf("err.reportTooLarge")
	}
	decoder := json.NewDecoder(bytes.NewReader(content))
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
	if err := validateReportJSONPresence(content); err != nil {
		return data, err
	}
	if err := validateReportSchemaValues(data); err != nil {
		return data, err
	}
	if err := validateReportSummary(data); err != nil {
		return data, err
	}
	if err := model.ValidateReportIdentity(data); err != nil {
		return data, err
	}
	return data, nil
}

// validateReportJSONPresence checks the fields which form the current report
// contract before typed decoding's zero values can make a missing field look
// like a valid false, zero, empty string, or nil slice. It deliberately does
// not duplicate the model schema: DisallowUnknownFields and the model
// UnmarshalJSON implementations remain responsible for the full type and
// nested-object contract.
func validateReportJSONPresence(content []byte) error {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(content, &root); err != nil {
		return err
	}
	if root == nil {
		return fmt.Errorf("report must be a JSON object")
	}
	if _, err := requiredJSONString(root, "report", "schema_version"); err != nil {
		return err
	}
	tool, err := requiredJSONObject(root, "report", "tool")
	if err != nil {
		return err
	}
	if _, err := requiredJSONString(tool, "tool", "name"); err != nil {
		return err
	}
	if _, err := requiredJSONString(tool, "tool", "version"); err != nil {
		return err
	}

	run, err := requiredJSONObject(root, "report", "run")
	if err != nil {
		return err
	}
	for _, field := range []string{"id", "profile", "started_at", "completed_at", "exposure"} {
		if _, err := requiredJSONString(run, "run", field); err != nil {
			return err
		}
	}
	if _, err := requiredJSONInt(run, "run", "duration_ms"); err != nil {
		return err
	}
	if _, err := requiredJSONBool(run, "run", "redacted"); err != nil {
		return err
	}
	if _, err := requiredJSONStringArray(run, "run", "requested_modules"); err != nil {
		return err
	}
	if _, err := requiredJSONStringArray(run, "run", "output_formats"); err != nil {
		return err
	}

	results, err := requiredJSONArray(root, "report", "results")
	if err != nil {
		return err
	}
	summary, err := requiredJSONObject(root, "report", "summary")
	if err != nil {
		return err
	}
	if _, err := requiredJSONString(summary, "summary", "status"); err != nil {
		return err
	}
	for _, field := range []string{"ok", "warnings", "skipped", "errors"} {
		if _, err := requiredJSONInt(summary, "summary", field); err != nil {
			return err
		}
	}

	for index, rawResult := range results {
		resultPath := fmt.Sprintf("results[%d]", index)
		result, err := jsonObjectAt(rawResult, resultPath)
		if err != nil {
			return err
		}
		for _, field := range []string{"id", "title", "started_at"} {
			if _, err := requiredJSONString(result, resultPath, field); err != nil {
				return err
			}
		}
		if _, err := requiredJSONString(result, resultPath, "status"); err != nil {
			return err
		}
		if _, err := requiredJSONInt(result, resultPath, "duration_ms"); err != nil {
			return err
		}
		methodology, err := requiredJSONObject(result, resultPath, "methodology")
		if err != nil {
			return err
		}
		if _, err := requiredJSONString(methodology, resultPath+".methodology", "kind"); err != nil {
			return err
		}
		if rawEvidence, ok := result["evidence"]; ok {
			evidence, err := jsonObjectAt(rawEvidence, resultPath+".evidence")
			if err != nil {
				return err
			}
			if _, err := requiredJSONInt(evidence, resultPath+".evidence", "valid"); err != nil {
				return err
			}
			if _, err := requiredJSONInt(evidence, resultPath+".evidence", "expected"); err != nil {
				return err
			}
			if rawUnit, ok := evidence["unit"]; ok {
				if _, err := jsonStringAt(rawUnit, resultPath+".evidence.unit"); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func requiredRawField(object map[string]json.RawMessage, path, field string) (json.RawMessage, error) {
	raw, ok := object[field]
	if !ok {
		return nil, fmt.Errorf("missing required field %s.%s", path, field)
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, fmt.Errorf("%s.%s must not be null", path, field)
	}
	return raw, nil
}

func requiredJSONString(object map[string]json.RawMessage, path, field string) (string, error) {
	raw, err := requiredRawField(object, path, field)
	if err != nil {
		return "", err
	}
	return jsonStringAt(raw, path+"."+field)
}

func jsonStringAt(raw json.RawMessage, path string) (string, error) {
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return "", fmt.Errorf("%s must be a string", path)
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", fmt.Errorf("%s must be a string: %w", path, err)
	}
	return value, nil
}

func requiredJSONBool(object map[string]json.RawMessage, path, field string) (bool, error) {
	raw, err := requiredRawField(object, path, field)
	if err != nil {
		return false, err
	}
	var value bool
	if err := json.Unmarshal(raw, &value); err != nil {
		return false, fmt.Errorf("%s.%s must be a boolean: %w", path, field, err)
	}
	return value, nil
}

func requiredJSONInt(object map[string]json.RawMessage, path, field string) (int64, error) {
	raw, err := requiredRawField(object, path, field)
	if err != nil {
		return 0, err
	}
	var value int64
	if err := json.Unmarshal(raw, &value); err != nil {
		return 0, fmt.Errorf("%s.%s must be an integer: %w", path, field, err)
	}
	return value, nil
}

func requiredJSONObject(object map[string]json.RawMessage, path, field string) (map[string]json.RawMessage, error) {
	raw, err := requiredRawField(object, path, field)
	if err != nil {
		return nil, err
	}
	return jsonObjectAt(raw, path+"."+field)
}

func jsonObjectAt(raw json.RawMessage, path string) (map[string]json.RawMessage, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return nil, fmt.Errorf("%s must be an object", path)
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil {
		return nil, fmt.Errorf("%s must be an object: %w", path, err)
	}
	if object == nil {
		return nil, fmt.Errorf("%s must be an object", path)
	}
	return object, nil
}

func requiredJSONArray(object map[string]json.RawMessage, path, field string) ([]json.RawMessage, error) {
	raw, err := requiredRawField(object, path, field)
	if err != nil {
		return nil, err
	}
	return jsonArrayAt(raw, path+"."+field)
}

func jsonArrayAt(raw json.RawMessage, path string) ([]json.RawMessage, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '[' {
		return nil, fmt.Errorf("%s must be an array", path)
	}
	var values []json.RawMessage
	if err := json.Unmarshal(raw, &values); err != nil {
		return nil, fmt.Errorf("%s must be an array: %w", path, err)
	}
	if values == nil {
		return nil, fmt.Errorf("%s must be an array", path)
	}
	return values, nil
}

func requiredJSONStringArray(object map[string]json.RawMessage, path, field string) ([]string, error) {
	raw, err := requiredRawField(object, path, field)
	if err != nil {
		return nil, err
	}
	values, err := jsonArrayAt(raw, path+"."+field)
	if err != nil {
		return nil, err
	}
	result := make([]string, len(values))
	for index, value := range values {
		result[index], err = jsonStringAt(value, fmt.Sprintf("%s.%s[%d]", path, field, index))
		if err != nil {
			return nil, err
		}
	}
	return result, nil
}

func validateReportSchemaValues(data model.Report) error {
	if strings.TrimSpace(data.Tool.Name) == "" {
		return fmt.Errorf("tool.name must not be empty")
	}
	if strings.TrimSpace(data.Tool.Version) == "" {
		return fmt.Errorf("tool.version must not be empty")
	}
	if strings.TrimSpace(data.Run.ID) == "" {
		return fmt.Errorf("run.id must not be empty")
	}
	if strings.TrimSpace(data.Run.Profile) == "" {
		return fmt.Errorf("run.profile must not be empty")
	}
	if data.Run.StartedAt.IsZero() {
		return fmt.Errorf("run.started_at must not be zero")
	}
	if data.Run.CompletedAt.IsZero() {
		return fmt.Errorf("run.completed_at must not be zero")
	}
	if data.Run.CompletedAt.Before(data.Run.StartedAt) {
		return fmt.Errorf("run.completed_at must not be before run.started_at")
	}
	if data.Run.DurationMS < 0 {
		return fmt.Errorf("run.duration_ms must not be negative")
	}
	if strings.TrimSpace(data.Run.Exposure) == "" || !validReportExposure(data.Run.Exposure) {
		return fmt.Errorf("unsupported run.exposure %q", data.Run.Exposure)
	}
	if status := data.Summary.Status; strings.TrimSpace(string(status)) == "" {
		return fmt.Errorf("summary.status must not be empty")
	} else if !validReportStatus(status) {
		return fmt.Errorf("unsupported summary.status %q", status)
	}
	for _, item := range []struct {
		name  string
		value int
	}{
		{name: "ok", value: data.Summary.OK},
		{name: "warnings", value: data.Summary.Warnings},
		{name: "skipped", value: data.Summary.Skipped},
		{name: "errors", value: data.Summary.Errors},
	} {
		if item.value < 0 {
			return fmt.Errorf("summary.%s must not be negative", item.name)
		}
	}
	for resultIndex, result := range data.Results {
		prefix := fmt.Sprintf("results[%d]", resultIndex)
		if strings.TrimSpace(result.ID) == "" {
			return fmt.Errorf("%s.id must not be empty", prefix)
		}
		if strings.TrimSpace(result.Title) == "" {
			return fmt.Errorf("%s.title must not be empty", prefix)
		}
		if result.StartedAt.IsZero() {
			return fmt.Errorf("%s.started_at must not be zero", prefix)
		}
		if result.DurationMS < 0 {
			return fmt.Errorf("%s.duration_ms must not be negative", prefix)
		}
		if status := result.Status; strings.TrimSpace(string(status)) == "" {
			return fmt.Errorf("%s.status must not be empty", prefix)
		} else if !validReportStatus(status) {
			return fmt.Errorf("unsupported %s.status %q", prefix, status)
		}
		if strings.TrimSpace(result.Methodology.Kind) == "" {
			return fmt.Errorf("%s.methodology.kind must not be empty", prefix)
		}
		if kind := result.Methodology.Kind; !validMethodologyKind(kind) {
			return fmt.Errorf("unsupported %s.methodology.kind %q", prefix, kind)
		}
		if err := validateEvidence(prefix+".evidence", result.Evidence); err != nil {
			return err
		}
		for failureIndex, failure := range result.Failures {
			if category := failure.Category; strings.TrimSpace(string(category)) == "" || !validFailureCategory(category) {
				return fmt.Errorf("unsupported %s.failures[%d].category %q", prefix, failureIndex, category)
			}
		}
	}
	return nil
}

func validateEvidence(path string, evidence *model.Evidence) error {
	if evidence == nil {
		return nil
	}
	if evidence.Valid < 0 {
		return fmt.Errorf("%s.valid must not be negative", path)
	}
	if evidence.Expected < 0 {
		return fmt.Errorf("%s.expected must not be negative", path)
	}
	if evidence.Valid > evidence.Expected {
		return fmt.Errorf("%s.valid must not exceed %s.expected", path, path)
	}
	return validateEvidenceUnit(path, evidence)
}

func validateReportSummary(data model.Report) error {
	total := data.Summary.OK + data.Summary.Warnings + data.Summary.Skipped + data.Summary.Errors
	if total != len(data.Results) {
		return fmt.Errorf("summary counts total %d does not match results length %d", total, len(data.Results))
	}
	expected := data
	model.Summarize(&expected)
	if data.Summary.OK != expected.Summary.OK || data.Summary.Warnings != expected.Summary.Warnings ||
		data.Summary.Skipped != expected.Summary.Skipped || data.Summary.Errors != expected.Summary.Errors {
		return fmt.Errorf("summary counts do not match result statuses")
	}
	if data.Summary.Status != expected.Summary.Status {
		return fmt.Errorf("summary.status %q contradicts result statuses; want %q", data.Summary.Status, expected.Summary.Status)
	}
	return nil
}

func validReportStatus(status model.Status) bool {
	switch status {
	case model.StatusOK, model.StatusWarning, model.StatusSkipped, model.StatusError:
		return true
	default:
		return false
	}
}

func validReportExposure(exposure string) bool {
	switch exposure {
	case "local", "public", "thirdparty", "any":
		return true
	default:
		return false
	}
}

func validMethodologyKind(kind string) bool {
	switch kind {
	case "standard-benchmark", "protocol-measurement", "provider-assessment", "heuristic", "inventory":
		return true
	default:
		return false
	}
}

func validateEvidenceUnit(path string, evidence *model.Evidence) error {
	if evidence == nil || evidence.Unit == "" || validEvidenceUnit(evidence.Unit) {
		return nil
	}
	return fmt.Errorf("unsupported %s.unit %q", path, evidence.Unit)
}

// validEvidenceUnit mirrors the evidence.unit(s).* catalog entries. Every value
// here has a translation; admitting one without a key would render the key
// itself once i18n.Has stopped covering for it.
func validEvidenceUnit(unit string) bool {
	switch unit {
	case "module", "run", "job", "sample", "query", "target", "operation", "source":
		return true
	default:
		return false
	}
}

func validFailureCategory(category model.FailureCategory) bool {
	switch category {
	case model.FailureTimeout, model.FailureDNS, model.FailureConnectionRefused,
		model.FailureNetworkUnreachable, model.FailureRateLimited, model.FailureHTTPRejected,
		model.FailureTLS, model.FailureParse, model.FailureToolMissing, model.FailurePermissionDenied,
		model.FailureUnsupported, model.FailureCanceled, model.FailureUnknown:
		return true
	default:
		return false
	}
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
