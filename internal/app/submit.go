package app

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"ecs/internal/i18n"
	"ecs/internal/probe"
	reporter "ecs/internal/report"
	"ecs/internal/score"
)

// submitCommand 从完整报告导出一份可公开入库的瘦身提交。
func submitCommand(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("ecs submit", flag.ContinueOnError)
	flags.SetOutput(stderr)
	input := flags.String("input", "", i18n.T("flag.submitInput"))
	output := flags.String("output", "", i18n.T("flag.submitOutput"))
	region := flags.String("region", "", i18n.T("flag.submitRegion"))
	provider := flags.String("provider", "", i18n.T("flag.submitProvider"))
	note := flags.String("note", "", i18n.T("flag.submitNote"))
	flags.Usage = func() {
		fmt.Fprintln(stderr, i18n.T("help.submitUsage"))
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 1
	}
	if flags.NArg() != 0 {
		fmt.Fprintf(stderr, "%s %s\n", i18n.T("help.extraArgs"), strings.Join(flags.Args(), " "))
		return 1
	}
	outputGiven := false
	flags.Visit(func(flag *flag.Flag) {
		if flag.Name == "output" {
			outputGiven = true
		}
	})
	if outputGiven {
		if err := preflightSubmissionOutput(*output); err != nil {
			fmt.Fprintf(stderr, "%s: %v\n", i18n.T("cli.error"), err)
			return 1
		}
	}
	if *input == "" {
		fmt.Fprintln(stderr, i18n.T("help.renderInputRequired"))
		return 1
	}
	data, err := reporter.LoadJSON(*input)
	if err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", i18n.T("cli.error"), err)
		return 1
	}
	submission, err := score.BuildSubmission(probe.BuiltinCatalog(), data, score.SubmissionOptions{
		Region: *region, Provider: *provider, Note: *note,
	})
	if err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", i18n.T("cli.error"), err)
		return 1
	}
	content, err := submission.Encode()
	if err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", i18n.T("cli.error"), err)
		return 1
	}
	target, err := resolveSubmissionTarget(*output, submission.FileName(), outputGiven)
	if err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", i18n.T("cli.error"), err)
		return 1
	}
	if err := writeSubmissionExclusive(target, content); err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", i18n.T("cli.error"), err)
		return 1
	}
	fmt.Fprintf(stdout, "%s %s\n", i18n.T("submit.written"), target)
	fmt.Fprintf(stdout, "%s\n", fmt.Sprintf(i18n.T("submit.summary"),
		submission.Host.VCPU, submission.Host.MemoryGiB, len(submission.Metrics)))
	fmt.Fprintf(stdout, "%s\n", i18n.T("submit.privacyNote"))
	return 0
}

// validateSubmissionPath rejects paths that cannot be represented safely in a
// shell-produced command or a filesystem error message. NUL cannot normally
// occur in a Go string received from argv, but checking it keeps this helper
// safe for direct callers and tests as well.
func validateSubmissionPath(path string) error {
	if path == "" {
		return fmt.Errorf("submission output path must not be empty")
	}
	if strings.IndexFunc(path, func(r rune) bool { return r == 0 || r < 0x20 || r == 0x7f }) >= 0 {
		return fmt.Errorf("submission output path contains a control character")
	}
	return nil
}

func validateSubmissionParent(path string) error {
	// Check every existing component, not just the final directory. Otherwise
	// `safe-link/submission.json` could still follow a symlink in `safe-link`
	// even though the final target itself is not one.
	for current := filepath.Clean(path); ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("submission output parent does not exist: %s", current)
			}
			return fmt.Errorf("inspect submission output parent %s: %w", current, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("submission output parent must not be a symlink: %s", current)
		}
		if !info.IsDir() {
			return fmt.Errorf("submission output parent is not a directory: %s", current)
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
	}
	return nil
}

type submissionPathState struct {
	exists bool
	info   os.FileInfo
}

// lstatSubmissionPath centralizes the filesystem inspection shared by the
// early output check, target resolution, and final write boundary. Callers
// decide whether an existing path is acceptable; this helper only preserves
// the missing/error distinction and never follows a final symlink.
func lstatSubmissionPath(path string) (submissionPathState, error) {
	info, err := os.Lstat(path)
	switch {
	case err == nil:
		return submissionPathState{exists: true, info: info}, nil
	case errors.Is(err, os.ErrNotExist):
		return submissionPathState{}, nil
	default:
		return submissionPathState{}, fmt.Errorf("inspect submission output %s: %w", path, err)
	}
}

// inspectSubmissionPath validates an output boundary and rejects a symlink
// when the path is supplied as an output destination. The final write uses
// lstatSubmissionPath directly so its historical existing-target error stays
// uniform for files, directories, and symlinks.
func inspectSubmissionPath(path string) (submissionPathState, error) {
	if err := validateSubmissionPath(path); err != nil {
		return submissionPathState{}, err
	}
	state, err := lstatSubmissionPath(path)
	if err != nil {
		return submissionPathState{}, err
	}
	if state.exists && state.info.Mode()&os.ModeSymlink != 0 {
		return submissionPathState{}, fmt.Errorf("submission output must not be a symlink: %s", path)
	}
	return state, nil
}

// preflightSubmissionOutput rejects an explicitly supplied target before the
// report is loaded. Existing files are never overwritten; directories are
// accepted as destinations but are never created by ecs submit.
func preflightSubmissionOutput(path string) error {
	state, err := inspectSubmissionPath(path)
	if err != nil {
		return err
	}
	if !state.exists {
		return validateSubmissionParent(filepath.Dir(path))
	}
	if state.info.IsDir() {
		return validateSubmissionParent(path)
	}
	return fmt.Errorf("submission output already exists: %s", path)
}

func resolveSubmissionTarget(output, fileName string, explicit bool) (string, error) {
	if explicit {
		state, err := inspectSubmissionPath(output)
		if err != nil {
			return "", err
		}
		if state.exists {
			if state.info.IsDir() {
				return filepath.Join(output, fileName), nil
			}
			return "", fmt.Errorf("submission output already exists: %s", output)
		}
		return output, nil
	}

	if err := validateSubmissionParent(os.TempDir()); err != nil {
		return "", err
	}
	return filepath.Join(os.TempDir(), fileName), nil
}

// writeSubmissionExclusive writes complete content to a new path without
// following or replacing an existing file. A fully synced temporary inode is
// hard-linked into place; link(2) fails atomically when the target already
// exists (including a symlink or hardlink), unlike rename which would replace
// it.
func writeSubmissionExclusive(path string, content []byte) error {
	if err := validateSubmissionPath(path); err != nil {
		return err
	}
	parent := filepath.Dir(path)
	if err := validateSubmissionParent(parent); err != nil {
		return err
	}
	state, err := lstatSubmissionPath(path)
	if err != nil {
		return err
	}
	if state.exists {
		return fmt.Errorf("submission output already exists: %s", path)
	}

	temporary, err := os.CreateTemp(parent, ".ecs-submit-*")
	if err != nil {
		return fmt.Errorf("create temporary submission: %w", err)
	}
	temporaryName := temporary.Name()
	removeTemporary := func() {
		_ = temporary.Close()
		_ = os.Remove(temporaryName)
	}
	defer func() { _ = os.Remove(temporaryName) }()
	if err := temporary.Chmod(0o600); err != nil {
		removeTemporary()
		return fmt.Errorf("set temporary submission permissions: %w", err)
	}
	if _, err := temporary.Write(content); err != nil {
		removeTemporary()
		return fmt.Errorf("write temporary submission: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		removeTemporary()
		return fmt.Errorf("sync temporary submission: %w", err)
	}
	if err := temporary.Close(); err != nil {
		_ = os.Remove(temporaryName)
		return fmt.Errorf("close temporary submission: %w", err)
	}
	if err := os.Link(temporaryName, path); err != nil {
		_ = os.Remove(temporaryName)
		return fmt.Errorf("create submission without overwriting target: %w", err)
	}
	if directory, err := os.Open(parent); err == nil {
		_ = directory.Sync()
		_ = directory.Close()
	}
	if err := os.Remove(temporaryName); err != nil {
		return fmt.Errorf("remove temporary submission link: %w", err)
	}
	return nil
}
