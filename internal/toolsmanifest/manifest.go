// Package toolsmanifest parses and validates the machine-readable metadata
// shipped with an ecs-tools architecture package.
package toolsmanifest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"slices"
)

const SchemaVersion = "ecs-tools.manifest/v1"

var architectures = [...]string{
	"amd64",
	"arm64",
	"armv7",
	"386",
	"s390x",
	"riscv64",
	"ppc64le",
}

var toolNames = [...]string{
	"sysbench",
	"zstd",
	"npb-ep",
	"npb-ft",
	"openssl",
	"stream",
	"fio",
	"iperf3",
	"nexttrace-tiny",
	"ping",
}

// Manifest describes one Linux architecture's ecs-tools package.
type Manifest struct {
	SchemaVersion          string        `json:"schema_version"`
	Architecture           string        `json:"architecture"`
	SupportedArchitectures []string      `json:"supported_architectures,omitempty"`
	Build                  BuildMetadata `json:"build"`
	Tools                  []Tool        `json:"tools"`
}

// BuildMetadata records how the target binaries were built and how CI
// validated them. PerformanceValid is deliberately false for every CI build:
// benchmark values produced during smoke tests are not release measurements.
type BuildMetadata struct {
	ToolchainMode string             `json:"toolchain_mode"`
	BuildTriplet  string             `json:"build_triplet"`
	TargetTriplet string             `json:"target_triplet"`
	SmokeRunner   string             `json:"smoke_runner"`
	Validation    ValidationMetadata `json:"validation"`
}

// ValidationMetadata defines the scope and interpretation of CI validation.
type ValidationMetadata struct {
	Scope            string `json:"scope"`
	PerformanceValid bool   `json:"performance_valid"`
}

// Tool describes one executable in a package.  The feature and parameter
// fields intentionally remain strings/maps so the manifest can grow without
// changing the required metadata contract.
type Tool struct {
	Name             string         `json:"name"`
	Upstream         string         `json:"upstream"`
	Version          string         `json:"version"`
	TagOrCommit      string         `json:"tag_or_commit"`
	Source           string         `json:"source"`
	BuildFlags       []string       `json:"build_flags"`
	EnabledFeatures  []string       `json:"enabled_features"`
	DisabledFeatures []string       `json:"disabled_features"`
	Architecture     string         `json:"architecture"`
	License          string         `json:"license"`
	Parameters       map[string]any `json:"parameters,omitempty"`
	Fallback         string         `json:"fallback,omitempty"`
}

// Field lists mirror the json tags of the types below. They stay explicit
// rather than using encoding/json's DisallowUnknownFields because the standard
// decoder reports every nesting level as a bare `unknown field "x"`, which does
// not tell a CI operator which object of a three-level manifest is wrong.
// TestFieldListsMatchStructTags keeps them from drifting away from the structs.
var (
	manifestFields   = []string{"schema_version", "architecture", "supported_architectures", "build", "tools"}
	buildFields      = []string{"toolchain_mode", "build_triplet", "target_triplet", "smoke_runner", "validation"}
	validationFields = []string{"scope", "performance_valid"}
	toolFields       = []string{
		"name", "upstream", "version", "tag_or_commit", "source",
		"build_flags", "enabled_features", "disabled_features",
		"architecture", "license", "parameters", "fallback",
	}
)

// Parse decodes and validates one manifest.  Required JSON keys are checked
// before unmarshalling so an omitted array cannot be confused with an
// intentionally empty array.
func Parse(data []byte) (Manifest, error) {
	var raw map[string]json.RawMessage
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(&raw); err != nil {
		return Manifest{}, fmt.Errorf("parse manifest: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return Manifest{}, fmt.Errorf("parse manifest: more than one JSON value")
		}
		return Manifest{}, fmt.Errorf("parse manifest: trailing data: %w", err)
	}
	if raw == nil {
		return Manifest{}, fmt.Errorf("manifest must be a JSON object")
	}
	if err := rejectUnknownFields(raw, manifestFields...); err != nil {
		return Manifest{}, err
	}
	for _, field := range []string{"schema_version", "architecture", "build", "tools"} {
		if err := requireField(raw, field); err != nil {
			return Manifest{}, err
		}
	}
	if value, ok := raw["supported_architectures"]; ok && bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
		return Manifest{}, fmt.Errorf("supported_architectures must be an array when present")
	}
	buildRaw, err := rawObject(raw["build"], "build")
	if err != nil {
		return Manifest{}, err
	}
	if err := rejectUnknownFields(buildRaw, buildFields...); err != nil {
		return Manifest{}, fmt.Errorf("build: %w", err)
	}
	for _, field := range []string{"toolchain_mode", "build_triplet", "target_triplet", "smoke_runner", "validation"} {
		if err := requireField(buildRaw, field); err != nil {
			return Manifest{}, fmt.Errorf("build: %w", err)
		}
	}
	validationRaw, err := rawObject(buildRaw["validation"], "build.validation")
	if err != nil {
		return Manifest{}, err
	}
	if err := rejectUnknownFields(validationRaw, validationFields...); err != nil {
		return Manifest{}, fmt.Errorf("build.validation: %w", err)
	}
	for _, field := range []string{"scope", "performance_valid"} {
		if err := requireField(validationRaw, field); err != nil {
			return Manifest{}, fmt.Errorf("build.validation: %w", err)
		}
	}

	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return Manifest{}, fmt.Errorf("decode manifest: %w", err)
	}
	for index, toolRaw := range rawTools(raw["tools"]) {
		if err := rejectUnknownFields(toolRaw, toolFields...); err != nil {
			return Manifest{}, fmt.Errorf("tool %d: %w", index, err)
		}
		for _, field := range []string{
			"name", "upstream", "version", "tag_or_commit", "source",
			"build_flags", "enabled_features", "disabled_features",
			"architecture", "license", "parameters",
		} {
			if err := requireField(toolRaw, field); err != nil {
				return Manifest{}, fmt.Errorf("tool %d: %w", index, err)
			}
		}
		if value, ok := toolRaw["fallback"]; ok {
			var fallback string
			if err := json.Unmarshal(value, &fallback); err != nil || fallback == "" {
				return Manifest{}, fmt.Errorf("tool %d: fallback must be a non-empty string when present", index)
			}
		}
	}
	if err := Validate(manifest); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func rawObject(raw json.RawMessage, name string) (map[string]json.RawMessage, error) {
	var value map[string]json.RawMessage
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, fmt.Errorf("%s must be a JSON object: %w", name, err)
	}
	if value == nil {
		return nil, fmt.Errorf("%s must be a JSON object", name)
	}
	return value, nil
}

func rawTools(raw json.RawMessage) []map[string]json.RawMessage {
	var values []map[string]json.RawMessage
	if json.Unmarshal(raw, &values) != nil {
		return nil
	}
	return values
}

func requireField(fields map[string]json.RawMessage, field string) error {
	value, ok := fields[field]
	if !ok || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
		return fmt.Errorf("missing required field %q", field)
	}
	return nil
}

func rejectUnknownFields(fields map[string]json.RawMessage, allowed ...string) error {
	for field := range fields {
		if !slices.Contains(allowed, field) {
			return fmt.Errorf("unknown field %q", field)
		}
	}
	return nil
}

// Validate checks the stable manifest contract without consulting the
// filesystem or executing any tool.
func Validate(manifest Manifest) error {
	if manifest.SchemaVersion != SchemaVersion {
		return fmt.Errorf("schema_version must be %q, got %q", SchemaVersion, manifest.SchemaVersion)
	}
	if !slices.Contains(architectures[:], manifest.Architecture) {
		return fmt.Errorf("unsupported architecture %q", manifest.Architecture)
	}
	if manifest.Build.ToolchainMode != "native" && manifest.Build.ToolchainMode != "cross" {
		return fmt.Errorf("build.toolchain_mode must be %q or %q, got %q", "native", "cross", manifest.Build.ToolchainMode)
	}
	// A slice, not a map: Go randomises map iteration, so two empty fields would
	// name a different one on each run and make the failure hard to reproduce.
	for _, required := range []struct{ field, value string }{
		{"build_triplet", manifest.Build.BuildTriplet},
		{"target_triplet", manifest.Build.TargetTriplet},
		{"smoke_runner", manifest.Build.SmokeRunner},
	} {
		if required.value == "" {
			return fmt.Errorf("build.%s must be a non-empty string", required.field)
		}
	}
	if manifest.Build.Validation.Scope != "functional" {
		return fmt.Errorf("build.validation.scope must be %q, got %q", "functional", manifest.Build.Validation.Scope)
	}
	if manifest.Build.Validation.PerformanceValid {
		return fmt.Errorf("build.validation.performance_valid must be false for CI smoke validation")
	}
	if manifest.SupportedArchitectures != nil {
		if len(manifest.SupportedArchitectures) != len(architectures) {
			return fmt.Errorf("supported_architectures must list all %d Linux architectures", len(architectures))
		}
		seen := make(map[string]bool, len(manifest.SupportedArchitectures))
		for _, architecture := range manifest.SupportedArchitectures {
			if !slices.Contains(architectures[:], architecture) || seen[architecture] {
				return fmt.Errorf("invalid supported architecture %q", architecture)
			}
			seen[architecture] = true
		}
		for _, architecture := range architectures {
			if !seen[architecture] {
				return fmt.Errorf("supported_architectures omits %q", architecture)
			}
		}
	}
	if len(manifest.Tools) != len(toolNames) {
		return fmt.Errorf("tools must contain exactly %d entries", len(toolNames))
	}
	seenTools := make(map[string]bool, len(manifest.Tools))
	for index, tool := range manifest.Tools {
		if !slices.Contains(toolNames[:], tool.Name) {
			return fmt.Errorf("tool %d has unsupported name %q", index, tool.Name)
		}
		if seenTools[tool.Name] {
			return fmt.Errorf("tools contains duplicate %q", tool.Name)
		}
		seenTools[tool.Name] = true
		if err := validateTool(manifest.Architecture, tool); err != nil {
			return fmt.Errorf("tool %q: %w", tool.Name, err)
		}
	}
	// 不再逐个检查缺失：上面已经确认数量与 ToolNames 相等、每个 name 都在
	// 白名单内、且互不重复，三者合起来在数学上就蕴含了全覆盖。
	return nil
}

func validateTool(manifestArchitecture string, tool Tool) error {
	for _, required := range []struct{ field, value string }{
		{"name", tool.Name},
		{"upstream", tool.Upstream},
		{"version", tool.Version},
		{"tag_or_commit", tool.TagOrCommit},
		{"source", tool.Source},
		{"license", tool.License},
	} {
		if required.value == "" {
			return fmt.Errorf("%s must be a non-empty string", required.field)
		}
	}
	for _, required := range []struct {
		field  string
		values []string
	}{
		{"build_flags", tool.BuildFlags},
		{"enabled_features", tool.EnabledFeatures},
		{"disabled_features", tool.DisabledFeatures},
	} {
		if required.values == nil {
			return fmt.Errorf("%s is required and must be an array", required.field)
		}
		for index, value := range required.values {
			if value == "" {
				return fmt.Errorf("%s[%d] must be a non-empty string", required.field, index)
			}
		}
	}
	// manifest.Architecture 已由 Validate 确认在白名单内，这里只需确认工具声明
	// 与 manifest 一致；再查一次白名单不可能发现新问题。
	if tool.Architecture != manifestArchitecture {
		return fmt.Errorf("architecture %q does not match manifest architecture %q", tool.Architecture, manifestArchitecture)
	}
	if tool.Parameters == nil {
		return fmt.Errorf("parameters is required and must be an object")
	}
	return nil
}
