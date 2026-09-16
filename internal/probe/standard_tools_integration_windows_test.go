//go:build integration && windows

package probe

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"ecs/internal/buildinfo"
	"ecs/internal/config"
	"ecs/internal/model"
	"ecs/internal/report"
)

const integrationWindowsToolTimeout = 30 * time.Minute

var integrationWindowsToolNames = []string{"zstd", "npb-ep", "npb-ft", "openssl", "stream", "fio"}

// TestIntegrationWindowsFrozenTools is the Windows ECS-level gate for the
// six frozen benchmark tools. Every benchmark result below comes from the
// production probe, which resolves ECS_TOOL_BIN and owns command execution and
// parsing. The hostile PATH binaries only prove that the resolver cannot fall
// back to a host-installed tool.
func TestIntegrationWindowsFrozenTools(t *testing.T) {
	stageBin := requireFrozenWindowsStage(t)
	assertFrozenWindowsBundleReadOnly(t, stageBin)
	corpusPath := requireFrozenWindowsCorpus(t)
	beforeWorktree := snapshotIntegrationWorktree(t)
	defer func() {
		afterWorktree := snapshotIntegrationWorktree(t)
		if !sameIntegrationWorktree(beforeWorktree, afterWorktree) {
			t.Errorf("integration changed files below the probe worktree: before=%v after=%v", beforeWorktree, afterWorktree)
		}
	}()

	hostileDir := filepath.Join(t.TempDir(), "hostile PATH 工具")
	buildHostileWindowsTools(t, hostileDir)
	originalPath := os.Getenv("PATH")
	t.Setenv("PATH", hostileDir+string(os.PathListSeparator)+originalPath)
	assertHostileWindowsTools(t, hostileDir)
	for _, name := range integrationWindowsToolNames {
		path := requireFrozenWindowsTool(t, stageBin, name)
		if !sameWindowsPath(filepath.Dir(path), stageBin) {
			t.Fatalf("LookupTool(%q) resolved outside ECS_TOOL_BIN: %q (bin=%q)", name, path, stageBin)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), integrationWindowsToolTimeout)
	defer cancel()
	fioDirectory := filepath.Join(t.TempDir(), "fio integration path with spaces")
	if err := os.MkdirAll(fioDirectory, 0o700); err != nil {
		t.Fatalf("create fio sandbox: %v", err)
	}
	if !strings.Contains(fioDirectory, " ") || containsNonASCII(fioDirectory) {
		t.Fatalf("fio workload sandbox must be an existing ASCII path with spaces: %q", fioDirectory)
	}
	env := Environment{Config: config.Runtime{
		DiskPath:       fioDirectory,
		DiskMiB:        16,
		DiskMatrixMode: config.DiskMatrixTime,
	}}

	results := make([]model.Result, 0, 5)
	for _, testCase := range []struct {
		name string
		run  func(context.Context, Environment) model.Result
	}{
		{name: "zstd", run: (zstdProbe{}).Run},
		{name: "npb", run: (npbProbe{}).Run},
		{name: "openssl", run: (cryptoProbe{}).Run},
		{name: "stream", run: (memoryProbe{}).Run},
		{name: "fio", run: (diskProbe{}).Run},
	} {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			result := testCase.run(ctx, env)
			assertCompleteWindowsProbeResult(t, result)
			switch result.ID {
			case "zstd":
				assertWindowsZstdResult(t, result, corpusPath)
			case "npb":
				assertWindowsNPBResult(t, result)
			case "crypto":
				assertWindowsOpenSSLResult(t, result)
			case "memory":
				assertWindowsSTREAMResult(t, result)
			case "disk":
				assertWindowsFIOResult(t, result, fioDirectory)
				assertWindowsFIORawEvidence(t, ctx, fioDirectory)
			default:
				t.Fatalf("unexpected Windows integration result ID %q", result.ID)
			}
			results = append(results, result)
		})
	}
	if t.Failed() {
		return
	}
	assertWindowsCLIReport(t, results)
	if ctx.Err() != nil {
		t.Fatalf("Windows frozen-tool integration context expired: %v", ctx.Err())
	}
}

func requireFrozenWindowsStage(t *testing.T) string {
	t.Helper()
	stageBin := strings.TrimSpace(os.Getenv(ToolBinEnv))
	if stageBin == "" {
		t.Fatalf("%s must point to the frozen staged bundle bin", ToolBinEnv)
	}
	absolute, err := filepath.Abs(stageBin)
	if err != nil {
		t.Fatalf("resolve %s=%q: %v", ToolBinEnv, stageBin, err)
	}
	info, err := os.Stat(absolute)
	if err != nil || !info.IsDir() {
		t.Fatalf("%s=%q is not a staged bin directory: %v", ToolBinEnv, absolute, err)
	}
	if !strings.EqualFold(filepath.Base(absolute), "bin") {
		t.Fatalf("%s=%q must name the staged bundle bin directory", ToolBinEnv, absolute)
	}
	if !strings.Contains(absolute, " ") {
		t.Fatalf("staged bundle path must contain a space: %q", absolute)
	}
	if !containsNonASCII(absolute) {
		t.Fatalf("staged bundle path must exercise a Unicode path: %q", absolute)
	}
	if pathIsUnderWindowsRoot(absolute, mustIntegrationWorkingDirectory(t)) {
		t.Fatalf("staged bundle escaped the test sandbox into the worktree: %q", absolute)
	}
	return absolute
}

func assertFrozenWindowsBundleReadOnly(t *testing.T, stageBin string) {
	t.Helper()
	bundleRoot := filepath.Dir(stageBin)
	manifestPath := filepath.Join(bundleRoot, "manifest.json")
	licensesRoot := filepath.Join(bundleRoot, "LICENSES")
	if info, err := os.Stat(manifestPath); err != nil || !info.Mode().IsRegular() {
		t.Fatalf("staged bundle manifest.json is not a regular file: %q (%v)", manifestPath, err)
	}
	if info, err := os.Stat(licensesRoot); err != nil || !info.IsDir() {
		t.Fatalf("staged bundle LICENSES directory is missing: %q (%v)", licensesRoot, err)
	}

	regularFiles := make([]string, 0, 16)
	err := filepath.WalkDir(bundleRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("staged bundle entry is not a regular file: %s", path)
		}
		regularFiles = append(regularFiles, path)
		return nil
	})
	if err != nil {
		t.Fatalf("walk staged bundle regular files %q: %v", bundleRoot, err)
	}
	if len(regularFiles) == 0 {
		t.Fatalf("staged bundle %q contains no regular files", bundleRoot)
	}
	licenseFiles := 0
	for _, path := range regularFiles {
		if pathIsUnderWindowsRoot(path, licensesRoot) {
			licenseFiles++
		}
		readOnlyProbe, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
		if err == nil {
			_ = readOnlyProbe.Close()
			t.Fatalf("staged bundle regular file is writable: %q", path)
		}
	}
	if licenseFiles == 0 {
		t.Fatalf("staged bundle LICENSES directory contains no regular files: %q", licensesRoot)
	}
}

func requireFrozenWindowsCorpus(t *testing.T) string {
	t.Helper()
	corpus := strings.TrimSpace(os.Getenv("ECS_ZSTD_CORPUS"))
	if corpus == "" {
		t.Fatalf("ECS_ZSTD_CORPUS must provide the fixed corpus in the test sandbox")
	}
	absolute, err := filepath.Abs(corpus)
	if err != nil {
		t.Fatalf("resolve ECS_ZSTD_CORPUS=%q: %v", corpus, err)
	}
	info, err := os.Stat(absolute)
	if err != nil || !info.Mode().IsRegular() {
		t.Fatalf("ECS_ZSTD_CORPUS=%q is not a regular file: %v", absolute, err)
	}
	if !strings.Contains(absolute, " ") || containsNonASCII(absolute) {
		t.Fatalf("fixed corpus path must be an existing ASCII path with spaces: %q", absolute)
	}
	if pathIsUnderWindowsRoot(absolute, mustIntegrationWorkingDirectory(t)) {
		t.Fatalf("fixed corpus escaped the test sandbox into the worktree: %q", absolute)
	}
	return absolute
}

func requireFrozenWindowsTool(t *testing.T, stageBin, name string) string {
	t.Helper()
	path, err := LookupTool(name)
	if err != nil {
		t.Fatalf("LookupTool(%q): %v", name, err)
	}
	if !sameWindowsPath(filepath.Dir(path), stageBin) {
		t.Fatalf("LookupTool(%q) returned %q outside %q", name, path, stageBin)
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
		t.Fatalf("staged %s is not a non-empty regular file: %q (%v)", name, path, err)
	}
	readOnlyProbe, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
	if err == nil {
		_ = readOnlyProbe.Close()
		t.Fatalf("staged %s is writable; the integration bundle must be read-only", name)
	}
	return path
}

func buildHostileWindowsTools(t *testing.T, directory string) {
	t.Helper()
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatalf("create hostile PATH directory: %v", err)
	}
	source := filepath.Join(directory, "hostile.go")
	output := filepath.Join(directory, "hostile.exe")
	const program = "package main\n\nimport \"os\"\n\nfunc main() { os.Exit(97) }\n"
	if err := os.WriteFile(source, []byte(program), 0o600); err != nil {
		t.Fatalf("write hostile binary source: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, "go", "build", "-trimpath", "-o", output, source)
	command.Dir = directory
	command.Env = append(os.Environ(), "GO111MODULE=off", "GOTOOLCHAIN=local")
	combined, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("build hostile PATH binary: %v: %s", err, tailText(string(combined), 600))
	}
	binary, err := os.ReadFile(output)
	if err != nil {
		t.Fatalf("read hostile binary: %v", err)
	}
	for _, name := range []string{"fio", "openssl", "zstd"} {
		path := filepath.Join(directory, toolFilename(name))
		if err := os.WriteFile(path, binary, 0o700); err != nil {
			t.Fatalf("install hostile %s: %v", name, err)
		}
	}
}

func assertHostileWindowsTools(t *testing.T, directory string) {
	t.Helper()
	for _, name := range []string{"fio", "openssl", "zstd"} {
		path, err := exec.LookPath(toolFilename(name))
		if err != nil || !sameWindowsPath(filepath.Dir(path), directory) {
			t.Fatalf("hostile PATH did not resolve %s.exe first: path=%q err=%v", name, path, err)
		}
		command := exec.Command(path)
		err = command.Run()
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) || exitErr.ExitCode() != 97 {
			t.Fatalf("hostile %s.exe exit = %v, want 97", name, err)
		}
	}
}

func assertCompleteWindowsProbeResult(t *testing.T, result model.Result) {
	t.Helper()
	if result.Status != model.StatusOK {
		t.Fatalf("%s probe status=%s failures=%+v", result.ID, result.Status, result.Failures)
	}
	if result.Evidence == nil || result.Evidence.Expected <= 0 || result.Evidence.Valid != result.Evidence.Expected {
		t.Fatalf("%s probe evidence=%+v, want complete evidence", result.ID, result.Evidence)
	}
	if len(result.Failures) != 0 {
		t.Fatalf("%s probe returned failures: %+v", result.ID, result.Failures)
	}
}

func assertWindowsZstdResult(t *testing.T, result model.Result, corpusPath string) {
	t.Helper()
	if !strings.Contains(corpusPath, " ") || containsNonASCII(corpusPath) {
		t.Fatalf("zstd corpus workload path must be ASCII with spaces: %q", corpusPath)
	}
	if got := windowsResultField(result, "version"); !strings.Contains(got, zstdExpectedVersion) {
		t.Fatalf("zstd frozen version evidence=%q, want %s", got, zstdExpectedVersion)
	}
	if got := windowsResultField(result, "corpus"); got != zstdCorpusName {
		t.Fatalf("zstd corpus evidence=%q, want %s", got, zstdCorpusName)
	}
	if pathIsUnderWindowsRoot(corpusPath, mustIntegrationWorkingDirectory(t)) {
		t.Fatalf("zstd corpus path escaped the test worktree: %q", corpusPath)
	}
	assertWindowsPositiveMeasurements(t, result, "zstd_compress_1t_mb_s", "zstd_decompress_1t_mb_s")
	if !hasWindowsMeasurementPrefix(result, "zstd_compress_") || !hasWindowsMeasurementPrefix(result, "zstd_decompress_") {
		t.Fatalf("zstd result lacks both compression and decompression measurements: %+v", result.Measurements)
	}
}

func assertWindowsNPBResult(t *testing.T, result model.Result) {
	t.Helper()
	if got := windowsResultField(result, "problem_class"); got != npbExpectedClass {
		t.Fatalf("NPB class evidence=%q, want %s", got, npbExpectedClass)
	}
	if !strings.Contains(windowsResultField(result, "benchmarks"), "EP") || !strings.Contains(windowsResultField(result, "benchmarks"), "FT") {
		t.Fatalf("NPB benchmark evidence is incomplete: %q", windowsResultField(result, "benchmarks"))
	}
	assertWindowsPositiveMeasurements(t, result, "npb_ep_1t_mops", "npb_ft_1t_mops")
	expectedCompileFlags, expectedLinkFlags := npbExpectedFlags(runtime.GOOS)
	seenEP, seenFT := false, false
	for _, block := range result.TextBlocks {
		if strings.Contains(block.Content, "- EP Benchmark") {
			seenEP = true
		}
		if strings.Contains(block.Content, "- FT Benchmark") {
			seenFT = true
		}
		if !strings.Contains(block.Content, "FFLAGS       = "+expectedCompileFlags) || !strings.Contains(block.Content, "FLINKFLAGS   = "+expectedLinkFlags) {
			t.Fatalf("NPB raw production evidence has unexpected compiler/linker flags: %q", block.Content)
		}
	}
	if !seenEP || !seenFT {
		t.Fatalf("NPB raw production evidence omitted EP or FT output: EP=%t FT=%t", seenEP, seenFT)
	}
	verified := map[string]bool{"EP": false, "FT": false}
	for _, table := range result.Tables {
		if table.Key != "benchmark.npb.results" {
			continue
		}
		for _, row := range table.Rows {
			if len(row) < 8 {
				continue
			}
			benchmark := strings.ToUpper(strings.TrimSpace(row[0].Text()))
			if _, wanted := verified[benchmark]; !wanted {
				continue
			}
			verification, ok := row[7].Key()
			if ok && verification == "probe.npb.verification.successful" {
				verified[benchmark] = true
			}
		}
	}
	for benchmark, ok := range verified {
		if !ok {
			t.Fatalf("NPB %s table has no successful production verification evidence", benchmark)
		}
	}
}

func assertWindowsOpenSSLResult(t *testing.T, result model.Result) {
	t.Helper()
	if !strings.Contains(windowsResultField(result, "algorithms"), "AES-256-GCM") ||
		!strings.Contains(windowsResultField(result, "algorithms"), "ChaCha20-Poly1305") ||
		!strings.Contains(windowsResultField(result, "algorithms"), "SHA-256") {
		t.Fatalf("OpenSSL algorithm evidence is incomplete: %q", windowsResultField(result, "algorithms"))
	}
	versionEvidence := windowsResultField(result, "version")
	if !strings.Contains(versionEvidence, "OpenSSL") || !strings.Contains(versionEvidence, openSSLExpectedVersion) {
		t.Fatalf("OpenSSL frozen version/banner evidence=%q, want OpenSSL %s", versionEvidence, openSSLExpectedVersion)
	}
	assertWindowsPositiveMeasurements(t,
		result,
		"openssl_aes_256_gcm_1w_mb_s",
		"openssl_chacha20_poly1305_1w_mb_s",
		"openssl_sha_256_1w_mb_s",
	)
	if len(result.TextBlocks) == 0 {
		t.Fatalf("OpenSSL result has no production raw sample evidence")
	}
	rawSpeedEvidence := false
	for _, block := range result.TextBlocks {
		if strings.Contains(block.Content, "+F:") {
			rawSpeedEvidence = true
			break
		}
	}
	if !rawSpeedEvidence {
		t.Fatalf("OpenSSL production raw stdout/stderr has no machine-speed evidence")
	}
	argumentEvidence := 0
	for _, field := range result.Fields {
		if !strings.HasPrefix(field.Key, "arguments_") {
			continue
		}
		arguments := field.Value.Text()
		if strings.Contains(arguments, "-multi") || !strings.Contains(arguments, "-seconds 5") || !strings.Contains(arguments, "-bytes 16384") || !strings.Contains(arguments, "-mr") {
			t.Fatalf("OpenSSL Windows production arguments violate the fixed speed contract: %q", arguments)
		}
		argumentEvidence++
	}
	if argumentEvidence < len(openSSLAlgorithmSpecs) {
		t.Fatalf("OpenSSL production result has %d fixed-argument evidence fields, want at least %d", argumentEvidence, len(openSSLAlgorithmSpecs))
	}
	var algorithmRows int
	for _, table := range result.Tables {
		if table.Key == "benchmark.openssl.results" {
			algorithmRows = len(table.Rows)
		}
	}
	if algorithmRows < len(openSSLAlgorithmSpecs) {
		t.Fatalf("OpenSSL production result has %d algorithm rows, want at least %d", algorithmRows, len(openSSLAlgorithmSpecs))
	}
}

func assertWindowsSTREAMResult(t *testing.T, result model.Result) {
	t.Helper()
	if windowsResultField(result, "kernel_order") != "Copy / Scale / Add / Triad" {
		t.Fatalf("STREAM kernel order evidence=%q", windowsResultField(result, "kernel_order"))
	}
	if strings.TrimSpace(windowsResultField(result, "threads")) == "" ||
		!strings.Contains(windowsResultField(result, "thread_control"), "OMP_NUM_THREADS") {
		t.Fatalf("STREAM thread evidence is incomplete: fields=%+v", result.Fields)
	}
	assertWindowsPositiveMeasurements(t,
		result,
		"stream_copy_1t_mib_s",
		"stream_scale_1t_mib_s",
		"stream_add_1t_mib_s",
		"stream_triad_1t_mib_s",
	)
	if len(result.TextBlocks) == 0 {
		t.Fatalf("STREAM result has no production raw output evidence")
	}
	crlf := false
	for _, block := range result.TextBlocks {
		crlf = crlf || strings.Contains(block.Content, "\r\n")
	}
	if !crlf {
		t.Fatalf("STREAM production output did not contain Windows CRLF evidence")
	}
}

func assertWindowsFIOResult(t *testing.T, result model.Result, fioDirectory string) {
	t.Helper()
	if !strings.Contains(strings.ToLower(windowsResultField(result, "ioengine")), "windowsaio") {
		t.Fatalf("fio production ioengine evidence=%q, want windowsaio", windowsResultField(result, "ioengine"))
	}
	if result.Methodology.Parameters["ioengine"] != "windowsaio" {
		t.Fatalf("fio methodology ioengine=%q, want windowsaio", result.Methodology.Parameters["ioengine"])
	}
	assertWindowsPositiveMeasurements(t,
		result,
		"fio_random_read_4k_iops",
		"fio_random_write_4k_iops",
	)
	path := windowsResultField(result, "path")
	if !pathIsUnderWindowsRoot(path, fioDirectory) {
		t.Fatalf("fio result path escaped t.TempDir: path=%q dir=%q", path, fioDirectory)
	}
}

func assertWindowsFIORawEvidence(t *testing.T, ctx context.Context, fioDirectory string) {
	t.Helper()
	fioPath := requireFrozenWindowsTool(t, strings.TrimSpace(os.Getenv(ToolBinEnv)), "fio")
	engine := detectFIOEngine(ctx, fioPath)
	if !engine.Detected || engine.Name != "windowsaio" || !engine.AsyncQueue {
		t.Fatalf("production detectFIOEngine() = %+v, want detected async windowsaio", engine)
	}
	filename := filepath.Join(fioDirectory, "integration direct randrw.dat")
	plan := []fioJobSpec{{
		Name: "windows_integration_randrw", RW: "randrw", BlockSize: "4k", IODepth: 1,
		NumJobs: 1, MixRead: 50, Runtime: time.Second,
	}}
	args := fioArguments(filename, 16<<20, engine, plan)
	if !containsArgument(args, "--ioengine=windowsaio") || !containsArgument(args, "--filename="+filename) {
		t.Fatalf("production fioArguments() omitted Windows path/engine: %q", args)
	}
	command := newProbeCommand(ctx, fioPath, args...)
	command.Env = append(os.Environ(), "LC_ALL=C", "LANG=C", "NO_COLOR=1")
	command.Dir = fioDirectory
	run := command.RunSeparate()
	if run.Err != nil {
		t.Fatalf("production fio subprocess failed: %v: stdout=%q stderr=%q", run.Err, tailText(string(run.Stdout), 600), tailText(string(run.Stderr), 600))
	}
	_, jobs, err := parseFIOJobs(run.Stdout)
	if err != nil {
		t.Fatalf("production parseFIOJobs(): %v", err)
	}
	job, ok := jobs[plan[0].Name]
	if !ok || job.Error != 0 {
		t.Fatalf("fio parsed job=%+v, want successful %s job", job, plan[0].Name)
	}
	if job.Read.IOBytes == 0 || job.Write.IOBytes == 0 || !isPositiveFinite(job.Read.IOPS) || !isPositiveFinite(job.Write.IOPS) {
		t.Fatalf("fio parsed read/write evidence invalid: read bytes=%d iops=%f write bytes=%d iops=%f", job.Read.IOBytes, job.Read.IOPS, job.Write.IOBytes, job.Write.IOPS)
	}
	info, err := os.Stat(filename)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 {
		t.Fatalf("fio direct workload file=%q is invalid: info=%v err=%v", filename, info, err)
	}
	if !pathIsUnderWindowsRoot(filename, fioDirectory) {
		t.Fatalf("fio direct workload escaped t.TempDir: file=%q dir=%q", filename, fioDirectory)
	}
}

func assertWindowsPositiveMeasurements(t *testing.T, result model.Result, keys ...string) {
	t.Helper()
	measurements := make(map[string]model.Measurement, len(result.Measurements))
	for _, measurement := range result.Measurements {
		measurements[measurement.Key] = measurement
	}
	for _, key := range keys {
		measurement, ok := measurements[key]
		if !ok || !isPositiveFinite(measurement.Value) {
			t.Fatalf("%s measurement %q=%+v is not positive finite", result.ID, key, measurement)
		}
	}
}

func hasWindowsMeasurementPrefix(result model.Result, prefix string) bool {
	for _, measurement := range result.Measurements {
		if strings.HasPrefix(measurement.Key, prefix) && isPositiveFinite(measurement.Value) {
			return true
		}
	}
	return false
}

func windowsResultField(result model.Result, key string) string {
	for _, field := range result.Fields {
		if field.Key == key {
			return field.Value.Text()
		}
	}
	return ""
}

func assertWindowsCLIReport(t *testing.T, directResults []model.Result) {
	t.Helper()
	reportPath := strings.TrimSpace(os.Getenv("ECS_WINDOWS_CLI_REPORT"))
	if reportPath == "" {
		t.Fatal("ECS_WINDOWS_CLI_REPORT must point to the JSON written by the real ecs.exe CLI")
	}
	sandboxRoot := strings.TrimSpace(os.Getenv("ECS_WINDOWS_SANDBOX_ROOT"))
	if sandboxRoot == "" {
		t.Fatal("ECS_WINDOWS_SANDBOX_ROOT must identify the integration sandbox")
	}
	if !pathIsUnderWindowsRoot(reportPath, sandboxRoot) || pathIsUnderWindowsRoot(reportPath, mustIntegrationWorkingDirectory(t)) {
		t.Fatalf("CLI report escaped its sandbox: report=%q sandbox=%q worktree=%q", reportPath, sandboxRoot, mustIntegrationWorkingDirectory(t))
	}
	info, err := os.Stat(reportPath)
	if err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
		t.Fatalf("real ecs.exe did not write a regular JSON report: %q (%v)", reportPath, err)
	}
	outputEntries, err := os.ReadDir(filepath.Dir(reportPath))
	if err != nil {
		t.Fatalf("read CLI report directory: %v", err)
	}
	if len(outputEntries) != 1 || outputEntries[0].Name() != filepath.Base(reportPath) {
		t.Fatalf("CLI output directory contains unexpected files: %v", outputEntries)
	}
	parsed, err := report.LoadJSON(reportPath)
	if err != nil {
		t.Fatalf("production report.LoadJSON() failed for real CLI output: %v", err)
	}
	if parsed.SchemaVersion != buildinfo.SchemaVersion {
		t.Fatalf("real CLI report schema=%q, want %q", parsed.SchemaVersion, buildinfo.SchemaVersion)
	}
	if parsed.Summary.Status != model.StatusOK || parsed.Summary.OK != 5 || parsed.Summary.Warnings != 0 || parsed.Summary.Errors != 0 || parsed.Summary.Skipped != 0 {
		t.Fatalf("real CLI report summary=%+v, want five OK results", parsed.Summary)
	}
	wantIDs := []string{"zstd", "npb", "memory", "crypto", "disk"}
	if len(parsed.Results) != len(wantIDs) {
		t.Fatalf("real CLI report result count=%d, want %d", len(parsed.Results), len(wantIDs))
	}
	got := make(map[string]model.Result, len(parsed.Results))
	for _, result := range parsed.Results {
		got[result.ID] = result
	}
	if len(got) != len(wantIDs) {
		t.Fatalf("real CLI report result count=%d, want %d: %+v", len(got), len(wantIDs), parsed.Results)
	}
	directIDs := make(map[string]bool, len(directResults))
	for _, result := range directResults {
		directIDs[result.ID] = true
	}
	for _, id := range wantIDs {
		result, ok := got[id]
		if !ok {
			t.Fatalf("real CLI report omitted result %q", id)
		}
		if !directIDs[id] {
			t.Fatalf("real CLI report result %q is not covered by the direct production integration set", id)
		}
		if result.Status != model.StatusOK || result.Evidence == nil || result.Evidence.DerivedGrade() != model.EvidenceComplete {
			t.Fatalf("real CLI result %q status/evidence=%s/%+v, want OK/complete", id, result.Status, result.Evidence)
		}
		if result.Methodology.Kind == "" || len(result.Measurements) == 0 {
			t.Fatalf("real CLI result %q lacks methodology or measurements: %+v", id, result)
		}
	}
	if !containsString(parsed.Run.OutputFormats, "json") {
		t.Fatalf("real CLI report output formats=%v, want json", parsed.Run.OutputFormats)
	}
	if parsed.Run.Profile != config.ProfileStandard {
		t.Fatalf("real CLI report profile=%q, want %q", parsed.Run.Profile, config.ProfileStandard)
	}
	cliDiskPath := strings.TrimSpace(os.Getenv("ECS_WINDOWS_CLI_DISK_PATH"))
	if cliDiskPath == "" || !pathIsUnderWindowsRoot(cliDiskPath, sandboxRoot) || !strings.Contains(cliDiskPath, " ") {
		t.Fatalf("real CLI disk path=%q is not an existing sandbox path with spaces", cliDiskPath)
	}
	assertWindowsZstdResult(t, got["zstd"], mustWindowsCLIReportCorpus(t))
	assertWindowsNPBResult(t, got["npb"])
	assertWindowsOpenSSLResult(t, got["crypto"])
	assertWindowsSTREAMResult(t, got["memory"])
	assertWindowsFIOResult(t, got["disk"], cliDiskPath)
}

func mustWindowsCLIReportCorpus(t *testing.T) string {
	t.Helper()
	corpus := strings.TrimSpace(os.Getenv("ECS_ZSTD_CORPUS"))
	if corpus == "" {
		t.Fatal("ECS_ZSTD_CORPUS must remain set while validating the real CLI report")
	}
	return corpus
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

type integrationFileFact struct {
	Digest [sha256.Size]byte
}

func snapshotIntegrationWorktree(t *testing.T) map[string]integrationFileFact {
	t.Helper()
	root := mustIntegrationWorkingDirectory(t)
	facts := make(map[string]integrationFileFact)
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		facts[relative] = integrationFileFact{Digest: sha256.Sum256(content)}
		return nil
	})
	if err != nil {
		t.Fatalf("snapshot integration worktree %q: %v", root, err)
	}
	return facts
}

func sameIntegrationWorktree(before, after map[string]integrationFileFact) bool {
	if len(before) != len(after) {
		return false
	}
	for path, beforeFact := range before {
		if afterFact, ok := after[path]; !ok || afterFact != beforeFact {
			return false
		}
	}
	return true
}

func mustIntegrationWorkingDirectory(t *testing.T) string {
	t.Helper()
	workingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatalf("get integration working directory: %v", err)
	}
	return workingDirectory
}

func pathIsUnderWindowsRoot(path, root string) bool {
	path, pathErr := filepath.Abs(path)
	root, rootErr := filepath.Abs(root)
	if pathErr != nil || rootErr != nil {
		return false
	}
	path = filepath.Clean(path)
	root = filepath.Clean(root)
	if sameWindowsPath(path, root) {
		return true
	}
	separator := string(filepath.Separator)
	return strings.HasPrefix(strings.ToLower(path), strings.ToLower(root)+separator)
}

func sameWindowsPath(left, right string) bool {
	leftAbs, leftErr := filepath.Abs(left)
	rightAbs, rightErr := filepath.Abs(right)
	return leftErr == nil && rightErr == nil && strings.EqualFold(filepath.Clean(leftAbs), filepath.Clean(rightAbs))
}

func containsNonASCII(value string) bool {
	return !utf8.ValidString(value) || strings.IndexFunc(value, func(r rune) bool { return r > 127 }) >= 0
}

func containsArgument(args []string, want string) bool {
	for _, argument := range args {
		if argument == want {
			return true
		}
	}
	return false
}
