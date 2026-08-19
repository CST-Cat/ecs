package app

import (
	"bufio"
	"strings"
	"testing"

	"ecs/internal/config"
)

func TestPrompterChooseAcceptsSelection(t *testing.T) {
	var output strings.Builder
	prompt := &prompter{
		in:  bufio.NewReader(strings.NewReader("2\n")),
		out: &output,
	}
	if choice := prompt.choose("choose", []string{"first", "second"}, 0); choice != 1 {
		t.Fatalf("choice = %d, want second option", choice)
	}
}

// 没有可用终端时向导必须放行，否则 cron 与 CI 会永远卡在等输入。
func TestWizardWithoutTerminalDoesNotBlock(t *testing.T) {
	var output strings.Builder
	if prompt, ok := newPrompter(&output, false); ok {
		prompt.Close()
		t.Skip("当前环境有可用终端，跳过无终端路径测试")
	}
	var cfg config.Runtime
	if !runWizard(&cfg, &output) {
		t.Fatal("无终端时向导必须放行")
	}
}
