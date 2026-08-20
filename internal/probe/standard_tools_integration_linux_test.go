//go:build integration

package probe

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

const (
	integrationFIOOutputLimit    = 4 << 20
	integrationStreamOutputLimit = 64 << 10
)

func TestIntegrationFIO(t *testing.T) {
	fioPath, err := exec.LookPath("fio")
	if err != nil {
		t.Fatalf("fio is required for integration tests: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	engine := detectFIOEngine(ctx, fioPath)
	if !engine.Detected {
		t.Fatalf("fio engine detection failed for %q (fallback=%q)", fioPath, engine.Name)
	}

	const size = int64(16 << 20)
	tempDir := t.TempDir()
	filename := filepath.Join(tempDir, "integration-fio.dat")
	plan := []fioJobSpec{{
		Name:      "integration_randrw",
		RW:        "randrw",
		BlockSize: "4k",
		IODepth:   1,
		NumJobs:   1,
		MixRead:   50,
		Runtime:   time.Second,
	}}
	args := fioArguments(filename, size, engine, plan)
	for _, want := range []string{
		"--name=integration_randrw",
		"--filename=" + filename,
		"--size=" + strconv.FormatInt(size, 10),
		"--direct=1",
		"--ioengine=" + engine.Name,
		"--runtime=1",
		"--time_based=1",
		"--rw=randrw",
		"--bs=4k",
		"--iodepth=1",
		"--numjobs=1",
		"--rwmixread=50",
	} {
		if !containsArgument(args, want) {
			t.Fatalf("fioArguments() omitted %q: %q", want, args)
		}
	}

	command := exec.CommandContext(ctx, fioPath, args...)
	command.Env = append(os.Environ(), "LC_ALL=C", "LANG=C", "NO_COLOR=1")
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	runErr := command.Run()
	if ctx.Err() != nil {
		t.Fatalf("fio exceeded integration context: %v", ctx.Err())
	}
	if stdout.Len() > integrationFIOOutputLimit {
		t.Fatalf("fio JSON size = %d, exceeds 4 MiB", stdout.Len())
	}
	if runErr != nil {
		t.Fatalf("fio failed: %v: %s", runErr, tailText(sanitizeCommandOutput(stderr.Bytes()), 800))
	}

	jobs, err := parseFIOJobs(stdout.Bytes())
	if err != nil {
		t.Fatalf("parseFIOJobs(): %v", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("fio returned %d jobs, want 1", len(jobs))
	}
	job, ok := jobs[plan[0].Name]
	if !ok {
		t.Fatalf("fio output omitted job %q", plan[0].Name)
	}
	if !fioJobHasEvidence(plan[0], job) {
		t.Fatalf("fio job lacks randrw evidence: error=%d read_bytes=%d write_bytes=%d read_iops=%f write_iops=%f",
			job.Error, job.Read.IOBytes, job.Write.IOBytes, job.Read.IOPS, job.Write.IOPS)
	}
	if job.Read.IOBytes == 0 || job.Write.IOBytes == 0 {
		t.Fatalf("fio randrw byte evidence is incomplete: read=%d write=%d", job.Read.IOBytes, job.Write.IOBytes)
	}

	rel, err := filepath.Rel(tempDir, filename)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		t.Fatalf("fio file escaped t.TempDir: dir=%q file=%q rel=%q err=%v", tempDir, filename, rel, err)
	}
	info, err := os.Stat(filename)
	if err != nil {
		t.Fatalf("stat fio test file: %v", err)
	}
	if !info.Mode().IsRegular() || info.Size() != size {
		t.Fatalf("fio test file mode/size = %s/%d, want regular/%d", info.Mode(), info.Size(), size)
	}
}

func TestIntegrationSysbench(t *testing.T) {
	path, err := exec.LookPath("sysbench")
	if err != nil {
		t.Fatalf("sysbench is required for integration tests: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	run, err := executeSysbenchCPU(ctx, path, 1, 1)
	if err != nil {
		t.Fatalf("executeSysbenchCPU(): %v", err)
	}
	if run.Rate <= 0 || run.Events == 0 || run.P95MS <= 0 {
		t.Fatalf("sysbench returned non-positive evidence: rate=%f events=%d p95=%f", run.Rate, run.Events, run.P95MS)
	}
	for _, marker := range []string{"events per second:", "total number of events:", "95th percentile:"} {
		if !strings.Contains(run.Output, marker) {
			t.Errorf("sysbench output omitted %q", marker)
		}
	}
	version := commandVersion(ctx, path)
	if version == "" || version == "unknown" || !strings.Contains(strings.ToLower(version), "sysbench") {
		t.Fatalf("commandVersion(sysbench) = %q", version)
	}
	if ctx.Err() != nil {
		t.Fatalf("sysbench integration context expired: %v", ctx.Err())
	}
}

func TestIntegrationIPerf3Loopback(t *testing.T) {
	path, err := exec.LookPath("iperf3")
	if err != nil {
		t.Fatalf("iperf3 is required for integration tests: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	var (
		forward iperfDirectionResult
		reverse iperfDirectionResult
		udp     udpResult
		lastErr error
	)
	for attempt := 1; attempt <= 5; attempt++ {
		forward, reverse, udp, lastErr = runIPerf3LoopbackAttempt(ctx, t.TempDir(), path)
		if lastErr == nil {
			break
		}
		if ctx.Err() != nil {
			break
		}
	}
	if lastErr != nil {
		t.Fatalf("iperf3 loopback failed after bounded retries: %v", lastErr)
	}
	if forward.Port <= 0 || reverse.Port != forward.Port {
		t.Errorf("iperf3 TCP ports = forward:%d reverse:%d, want one positive shared port", forward.Port, reverse.Port)
	}

	for name, sample := range map[string]iperfDirectionResult{"forward": forward, "reverse": reverse} {
		if sample.Error != "" {
			t.Errorf("iperf3 %s error: %s", name, sample.Error)
		}
		if sample.Mbps <= 0 || sample.Bytes <= 0 || sample.Seconds <= 0 {
			t.Errorf("iperf3 %s lacks positive TCP evidence: mbps=%f bytes=%d seconds=%f", name, sample.Mbps, sample.Bytes, sample.Seconds)
		}
		if sample.LocalHost != "127.0.0.1" || sample.RemoteHost != "127.0.0.1" {
			t.Errorf("iperf3 %s escaped IPv4 loopback: local=%q remote=%q", name, sample.LocalHost, sample.RemoteHost)
		}
		if len(sample.IntervalMbps) == 0 {
			t.Errorf("iperf3 %s returned no TCP interval evidence", name)
		}
		for index, interval := range sample.IntervalMbps {
			if !isPositiveFinite(interval) {
				t.Errorf("iperf3 %s interval %d is not positive finite: %f", name, index, interval)
			}
		}
		if !isPositiveFinite(sample.IntervalMin) || !isPositiveFinite(sample.IntervalMedian) {
			t.Errorf("iperf3 %s interval summary invalid: min=%f median=%f", name, sample.IntervalMin, sample.IntervalMedian)
		}
	}
	if !udp.Available || udp.Err != "" || udp.Mbps <= 0 || udp.Packets <= 0 || udp.JitterMS < 0 || udp.LostPercent < 0 || udp.LostPercent > 100 {
		t.Errorf("iperf3 UDP evidence invalid: %+v", udp)
	}
}

func TestIntegrationPingLoopback(t *testing.T) {
	if _, err := exec.LookPath(pingCommand); err != nil {
		t.Fatalf("iputils ping is required for integration tests: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	stats := runICMPPingFamily(ctx, "127.0.0.1", 2, time.Second, "4")
	if ctx.Err() != nil {
		t.Fatalf("ping loopback context expired: %v", ctx.Err())
	}
	if stats.Err != nil || !stats.Available {
		t.Fatalf("ping loopback unavailable: %+v", stats)
	}
	if !stats.LossKnown || stats.LossPercent != 0 {
		t.Errorf("ping loss = %f (known=%t), want zero", stats.LossPercent, stats.LossKnown)
	}
	if !stats.RTTKnown || !stats.StdDevKnown {
		t.Fatalf("ping did not return iputils min/avg/max/mdev statistics: %+v", stats)
	}
	if stats.MinMS < 0 || stats.AvgMS < stats.MinMS || stats.MaxMS < stats.AvgMS || stats.StdDevMS < 0 {
		t.Errorf("ping min/avg/max/mdev ordering is invalid: %.3f/%.3f/%.3f/%.3f",
			stats.MinMS, stats.AvgMS, stats.MaxMS, stats.StdDevMS)
	}
}

func TestIntegrationSTREAM(t *testing.T) {
	path, err := exec.LookPath("stream")
	if err != nil {
		t.Fatalf("official STREAM is required for integration tests: %v", err)
	}
	if !IsOfficialStreamBinary(path) {
		t.Fatalf("%q is not an official STREAM binary", path)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Second)
	defer cancel()
	run, err := executeStreamMemory(ctx, path, 1)
	if err != nil {
		t.Fatalf("executeStreamMemory(): %v", err)
	}
	if len(run.Output) > integrationStreamOutputLimit {
		t.Fatalf("STREAM output size = %d, exceeds 64 KiB", len(run.Output))
	}
	if !streamVersionPattern.MatchString(run.Output) {
		t.Fatalf("STREAM output omitted its version marker")
	}
	if run.Threads != 1 || run.Sample.RequestedThreads != 1 {
		t.Fatalf("STREAM thread evidence = run:%d output:%d, want 1/1", run.Threads, run.Sample.RequestedThreads)
	}
	if len(run.Sample.Samples) != len(streamKernels) {
		t.Fatalf("STREAM returned %d kernels, want %d", len(run.Sample.Samples), len(streamKernels))
	}
	for _, kernel := range streamKernels {
		sample, ok := run.Sample.Samples[kernel]
		if !ok {
			t.Errorf("STREAM omitted %s", kernel)
			continue
		}
		if sample.RateMiBS <= 0 || sample.MinTime <= 0 || sample.AvgTime < sample.MinTime || sample.MaxTime < sample.AvgTime {
			t.Errorf("STREAM %s evidence/order invalid: %+v", kernel, sample)
		}
	}
}

func containsArgument(args []string, want string) bool {
	for _, argument := range args {
		if argument == want {
			return true
		}
	}
	return false
}

type integrationIPerfServer struct {
	command    *exec.Cmd
	cancel     context.CancelFunc
	done       chan error
	stopped    bool
	stopErr    error
	stdout     *os.File
	stderr     *os.File
	stdoutPath string
	stderrPath string
}

func runIPerf3LoopbackAttempt(ctx context.Context, tempDir, path string) (iperfDirectionResult, iperfDirectionResult, udpResult, error) {
	server, port, err := startIntegrationIPerfServer(ctx, tempDir, path)
	if err != nil {
		return iperfDirectionResult{}, iperfDirectionResult{}, udpResult{}, err
	}
	defer func() { _ = server.stop() }()

	forward := executeIPerf(ctx, path, "127.0.0.1", port, "IPv4", false, 1, 1)
	if forward.Error != "" {
		stopErr := server.stop()
		return forward, iperfDirectionResult{}, udpResult{}, fmt.Errorf("forward TCP: %s%s", forward.Error, formatIPerfStopError(stopErr))
	}
	reverse := executeIPerf(ctx, path, "127.0.0.1", port, "IPv4", true, 1, 1)
	if reverse.Error != "" {
		stopErr := server.stop()
		return forward, reverse, udpResult{}, fmt.Errorf("reverse TCP: %s%s", reverse.Error, formatIPerfStopError(stopErr))
	}
	udp := runIPerfUDP(ctx, path, "127.0.0.1", port, "IPv4", "1M", 1)
	if udp.Err != "" || !udp.Available {
		stopErr := server.stop()
		return forward, reverse, udp, fmt.Errorf("UDP: %s%s", fallback(udp.Err, "unavailable"), formatIPerfStopError(stopErr))
	}
	if err := server.stop(); err != nil {
		return forward, reverse, udp, err
	}
	return forward, reverse, udp, nil
}

func startIntegrationIPerfServer(ctx context.Context, tempDir, path string) (*integrationIPerfServer, int, error) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, 0, fmt.Errorf("reserve loopback port: %w", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		return nil, 0, fmt.Errorf("release loopback port %d: %w", port, err)
	}

	stdoutPath := filepath.Join(tempDir, "iperf3-server.stdout")
	stderrPath := filepath.Join(tempDir, "iperf3-server.stderr")
	stdout, err := os.Create(stdoutPath)
	if err != nil {
		return nil, 0, fmt.Errorf("create iperf3 stdout log: %w", err)
	}
	stderr, err := os.Create(stderrPath)
	if err != nil {
		_ = stdout.Close()
		return nil, 0, fmt.Errorf("create iperf3 stderr log: %w", err)
	}

	serverCtx, cancel := context.WithCancel(ctx)
	command := exec.CommandContext(serverCtx, path, "-s", "-B", "127.0.0.1", "-p", strconv.Itoa(port))
	command.Env = append(os.Environ(), "LC_ALL=C", "LANG=C", "NO_COLOR=1")
	command.Stdout = stdout
	command.Stderr = stderr
	server := &integrationIPerfServer{
		command: command, cancel: cancel, done: make(chan error, 1),
		stdout: stdout, stderr: stderr, stdoutPath: stdoutPath, stderrPath: stderrPath,
	}
	if err := command.Start(); err != nil {
		cancel()
		_ = stdout.Close()
		_ = stderr.Close()
		return nil, 0, fmt.Errorf("start iperf3 server on 127.0.0.1:%d: %w", port, err)
	}
	go func() {
		server.done <- command.Wait()
	}()

	readyDeadline := time.NewTimer(5 * time.Second)
	defer readyDeadline.Stop()
	readyTicker := time.NewTicker(25 * time.Millisecond)
	defer readyTicker.Stop()
	for {
		select {
		case waitErr := <-server.done:
			server.closeLogs()
			return nil, 0, fmt.Errorf("iperf3 server exited before ready: %v%s", waitErr, server.diagnostics())
		case <-ctx.Done():
			_ = server.stop()
			return nil, 0, ctx.Err()
		case <-readyDeadline.C:
			stopErr := server.stop()
			return nil, 0, fmt.Errorf("iperf3 server readiness timeout%s", formatIPerfStopError(stopErr))
		case <-readyTicker.C:
			connection, dialErr := net.DialTimeout("tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), 100*time.Millisecond)
			if dialErr != nil {
				continue
			}
			_ = connection.Close()
			select {
			case waitErr := <-server.done:
				server.closeLogs()
				return nil, 0, fmt.Errorf("iperf3 server exited during readiness: %v%s", waitErr, server.diagnostics())
			default:
				return server, port, nil
			}
		}
	}
}

func (server *integrationIPerfServer) stop() error {
	if server.stopped {
		return server.stopErr
	}
	server.stopped = true
	server.stopErr = server.stopProcess()
	return server.stopErr
}

func (server *integrationIPerfServer) stopProcess() error {
	server.cancel()
	var waitErr error
	select {
	case waitErr = <-server.done:
	case <-time.After(5 * time.Second):
		if server.command.Process != nil {
			_ = server.command.Process.Kill()
		}
		select {
		case waitErr = <-server.done:
		case <-time.After(5 * time.Second):
			return fmt.Errorf("iperf3 server did not exit after kill")
		}
	}
	server.closeLogs()
	if waitErr != nil && !strings.Contains(waitErr.Error(), "signal: killed") {
		return fmt.Errorf("iperf3 server shutdown: %v%s", waitErr, server.diagnostics())
	}
	return nil
}

func (server *integrationIPerfServer) closeLogs() {
	if server.stdout != nil {
		_ = server.stdout.Close()
		server.stdout = nil
	}
	if server.stderr != nil {
		_ = server.stderr.Close()
		server.stderr = nil
	}
}

func (server *integrationIPerfServer) diagnostics() string {
	stdout, _ := os.ReadFile(server.stdoutPath)
	stderr, _ := os.ReadFile(server.stderrPath)
	text := strings.TrimSpace(sanitizeCommandOutput(append(stdout, stderr...)))
	if text == "" {
		return ""
	}
	return ": " + tailText(text, 800)
}

func formatIPerfStopError(err error) string {
	if err == nil {
		return ""
	}
	return "; " + err.Error()
}
