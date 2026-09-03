package app

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"ecs/internal/config"
	"ecs/internal/module"
)

func TestPrompterChooseAcceptsSelection(t *testing.T) {
	var output strings.Builder
	prompt := &prompter{
		in:  bufio.NewReader(strings.NewReader("2\n")),
		out: &output,
	}
	choice, err := prompt.choose("choose", []string{"first", "second"}, 0)
	if err != nil || choice != 1 {
		t.Fatalf("choice = %d, want second option", choice)
	}
}

type wizardTestTTY struct {
	input      []byte
	output     bytes.Buffer
	readStart  chan struct{}
	closed     chan struct{}
	readOnce   sync.Once
	closeOnce  sync.Once
	blockAfter bool
}

func newWizardTestTTY(input string, blockAfter bool) *wizardTestTTY {
	return &wizardTestTTY{
		input:      []byte(input),
		readStart:  make(chan struct{}),
		closed:     make(chan struct{}),
		blockAfter: blockAfter,
	}
}

func (tty *wizardTestTTY) Read(target []byte) (int, error) {
	if len(tty.input) > 0 {
		count := copy(target, tty.input)
		tty.input = tty.input[count:]
		return count, nil
	}
	if !tty.blockAfter {
		return 0, io.EOF
	}
	tty.readOnce.Do(func() { close(tty.readStart) })
	<-tty.closed
	return 0, io.ErrClosedPipe
}

func (tty *wizardTestTTY) Write(source []byte) (int, error) {
	return tty.output.Write(source)
}

func (tty *wizardTestTTY) Close() error {
	tty.closeOnce.Do(func() { close(tty.closed) })
	return nil
}

func withWizardTTY(t *testing.T, tty *wizardTestTTY) {
	t.Helper()
	original := openWizardTTY
	openWizardTTY = func() (io.ReadWriteCloser, error) { return tty, nil }
	t.Cleanup(func() { openWizardTTY = original })
}

func TestPrompterUsesOpenedTTYForInputAndOutput(t *testing.T) {
	tty := newWizardTestTTY("answer\n", false)
	withWizardTTY(t, tty)
	prompt, ok := newPrompter(false)
	if !ok {
		t.Fatal("test terminal was not opened")
	}
	answer, err := prompt.ask("question: ")
	prompt.Close()
	if err != nil || answer != "answer" {
		t.Fatalf("ask = %q, %v", answer, err)
	}
	if tty.output.String() != "question: " {
		t.Fatalf("prompt output = %q, want output on the opened tty", tty.output.String())
	}
}

func TestWizardProfileSwitchOnlyChangesProfileAndModules(t *testing.T) {
	runtime, err := config.Defaults(newApplication().modules, config.ProfileStandard)
	if err != nil {
		t.Fatal(err)
	}
	runtime.Exposure = module.ExposureConsent
	runtime.Reveal = true
	runtime.IPVersion = config.IPVersion6
	runtime.IPQualitySources = []string{"ipinfo"}
	runtime.Formats = []string{"md"}
	runtime.Output = "/tmp/fixture-reports"
	runtime.NoColor = true
	runtime.CPUTime = 2 * time.Second
	runtime.DiskMiB = 64
	runtime.DiskPath = "/tmp/fixture-disk"
	runtime.DiskMulti = true
	runtime.DiskMatrixMode = config.DiskMatrixFixed
	runtime.IPerfDuration = 3 * time.Second
	runtime.IPerfTargets = []config.IPerfEndpoint{{Name: "fixture", Host: "example.com", PortStart: 5201, PortEnd: 5201}}
	runtime.HTTPTimeout = 20 * time.Second
	runtime.DNSAttempts = 3
	runtime.LatencyAttempts = 4
	runtime.SpeedThreads = 2
	runtime.DNSResolvers = []config.Endpoint{{Name: "dns", Address: "1.1.1.1:53"}}
	runtime.LatencyTargets = []config.Endpoint{{Name: "latency", Address: "example.com:443"}}
	runtime.RouteTargets = []config.Endpoint{{Name: "route", Address: "1.1.1.1"}}
	runtime.BacktraceTargets = []config.Endpoint{{Name: "trace", Address: "1.1.1.1", Kind: config.BacktraceCarrierTelecom}}
	runtime.STUNServers = []config.Endpoint{{Name: "stun", Address: "stun.example.com:3478"}}
	runtime.MediaRegions = []string{"jp"}
	runtime.OoklaServers = []config.OoklaServer{{Carrier: config.OoklaCarrierTelecom, ID: 1}}
	want := runtime
	want.Profile = config.ProfileFull
	want.Modules = config.ModulesForProfile(newApplication().modules, config.ProfileFull)

	tty := newWizardTestTTY("2\n", false)
	withWizardTTY(t, tty)
	ok, err := runWizard(context.Background(), newApplication().modules, &runtime)
	if err != nil || !ok {
		t.Fatalf("runWizard = %v, %v", ok, err)
	}
	if !reflect.DeepEqual(runtime, want) {
		t.Fatalf("profile switch changed fields beyond Profile/Modules:\n got:  %#v\n want: %#v", runtime, want)
	}
}

func TestWizardCancellationClosesTTYAndDiffersFromEOF(t *testing.T) {
	eofPrompt := &prompter{in: bufio.NewReader(strings.NewReader("")), out: &strings.Builder{}}
	if answer, err := eofPrompt.ask("eof: "); answer != "" || err != nil {
		t.Fatalf("EOF ask = %q, %v, want empty default without error", answer, err)
	}

	tty := newWizardTestTTY("2\n", true)
	withWizardTTY(t, tty)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan struct {
		ok  bool
		err error
	}, 1)
	var runtime config.Runtime
	go func() {
		ok, err := runWizard(ctx, newApplication().modules, &runtime)
		result <- struct {
			ok  bool
			err error
		}{ok: ok, err: err}
	}()
	select {
	case <-tty.readStart:
		cancel()
	case <-time.After(time.Second):
		t.Fatal("wizard did not reach the owned tty read")
	}
	select {
	case got := <-result:
		if got.ok || !errors.Is(got.err, context.Canceled) {
			t.Fatalf("canceled wizard = %v, %v, want context.Canceled", got.ok, got.err)
		}
	case <-time.After(time.Second):
		t.Fatal("wizard cancellation did not unblock the owned tty read")
	}
}

// 没有可用终端时向导必须放行，否则 cron 与 CI 会永远卡在等输入。
func TestWizardWithoutTerminalDoesNotBlock(t *testing.T) {
	if prompt, ok := newPrompter(false); ok {
		prompt.Close()
		t.Skip("当前环境有可用终端，跳过无终端路径测试")
	}
	var cfg config.Runtime
	if ok, err := runWizard(context.Background(), newApplication().modules, &cfg); err != nil || !ok {
		t.Fatal("无终端时向导必须放行")
	}
}
