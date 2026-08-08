// Package toolsmanifest parses and validates the machine-readable metadata
// shipped with an ecs-tools architecture package.
package toolsmanifest

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
)

const SchemaVersion = "ecs-tools.manifest/v1"

var Architectures = []string{
	"amd64",
	"arm64",
	"armv7",
	"386",
	"s390x",
	"riscv64",
	"ppc64le",
}

var ToolNames = []string{
	"sysbench",
	"stream",
	"fio",
	"iperf3",
	"nexttrace-tiny",
	"ping",
}

var sha256Pattern = regexp.MustCompile(`^[[:xdigit:]]{64}$`)

// Manifest describes one Linux architecture's ecs-tools package.
type Manifest struct {
	SchemaVersion          string   `json:"schema_version"`
	Architecture           string   `json:"architecture"`
	SupportedArchitectures []string `json:"supported_architectures,omitempty"`
	Tools                  []Tool   `json:"tools"`
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
	SHA256           string         `json:"sha256"`
	Parameters       map[string]any `json:"parameters,omitempty"`
	Fallback         string         `json:"fallback,omitempty"`
}

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
	for _, field := range []string{"schema_version", "architecture", "tools"} {
		if err := requireField(raw, field); err != nil {
			return Manifest{}, err
		}
	}

	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return Manifest{}, fmt.Errorf("decode manifest: %w", err)
	}
	for index, toolRaw := range rawTools(raw["tools"]) {
		for _, field := range []string{
			"name", "upstream", "version", "tag_or_commit", "source",
			"build_flags", "enabled_features", "disabled_features",
			"architecture", "license", "sha256", "parameters",
		} {
			if err := requireField(toolRaw, field); err != nil {
				return Manifest{}, fmt.Errorf("tool %d: %w", index, err)
			}
		}
	}
	if err := Validate(manifest); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
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

// Validate checks the stable manifest contract without consulting the
// filesystem or executing any tool.
func Validate(manifest Manifest) error {
	if manifest.SchemaVersion != SchemaVersion {
		return fmt.Errorf("schema_version must be %q, got %q", SchemaVersion, manifest.SchemaVersion)
	}
	if !contains(Architectures, manifest.Architecture) {
		return fmt.Errorf("unsupported architecture %q", manifest.Architecture)
	}
	if manifest.SupportedArchitectures != nil {
		if len(manifest.SupportedArchitectures) != len(Architectures) {
			return fmt.Errorf("supported_architectures must list all %d Linux architectures", len(Architectures))
		}
		seen := make(map[string]bool, len(manifest.SupportedArchitectures))
		for _, architecture := range manifest.SupportedArchitectures {
			if !contains(Architectures, architecture) || seen[architecture] {
				return fmt.Errorf("invalid supported architecture %q", architecture)
			}
			seen[architecture] = true
		}
		for _, architecture := range Architectures {
			if !seen[architecture] {
				return fmt.Errorf("supported_architectures omits %q", architecture)
			}
		}
	}
	if len(manifest.Tools) != len(ToolNames) {
		return fmt.Errorf("tools must contain exactly %d entries", len(ToolNames))
	}
	seenTools := make(map[string]bool, len(manifest.Tools))
	for index, tool := range manifest.Tools {
		if !contains(ToolNames, tool.Name) {
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
	for _, name := range ToolNames {
		if !seenTools[name] {
			return fmt.Errorf("tools is missing %q", name)
		}
	}
	return nil
}

func validateTool(manifestArchitecture string, tool Tool) error {
	for field, value := range map[string]string{
		"name":          tool.Name,
		"upstream":      tool.Upstream,
		"version":       tool.Version,
		"tag_or_commit": tool.TagOrCommit,
		"source":        tool.Source,
		"license":       tool.License,
	} {
		if value == "" {
			return fmt.Errorf("%s must be a non-empty string", field)
		}
	}
	for field, values := range map[string][]string{
		"build_flags":       tool.BuildFlags,
		"enabled_features":  tool.EnabledFeatures,
		"disabled_features": tool.DisabledFeatures,
	} {
		if values == nil {
			return fmt.Errorf("%s is required and must be an array", field)
		}
		for index, value := range values {
			if value == "" {
				return fmt.Errorf("%s[%d] must be a non-empty string", field, index)
			}
		}
	}
	if tool.Architecture != manifestArchitecture {
		return fmt.Errorf("architecture %q does not match manifest architecture %q", tool.Architecture, manifestArchitecture)
	}
	if !contains(Architectures, tool.Architecture) {
		return fmt.Errorf("unsupported architecture %q", tool.Architecture)
	}
	if !isSHA256(tool.SHA256) {
		return fmt.Errorf("sha256 must be 64 hexadecimal characters or unknown/unavailable, got %q", tool.SHA256)
	}
	if tool.Parameters == nil {
		return fmt.Errorf("parameters is required and must be an object")
	}
	return nil
}

func isSHA256(value string) bool {
	if value == "unknown" || value == "unavailable" {
		return true
	}
	if !sha256Pattern.MatchString(value) {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
