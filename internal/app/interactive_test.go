package app

import (
	"strings"
	"testing"

	"ecs/internal/config"
	"ecs/internal/i18n"
)

func TestWizardModuleGroupsCoverOptionalCost(t *testing.T) {
	groups := wizardModules()
	if len(groups) == 0 {
		t.Fatal("向导必须提供可开关的模块组")
	}
	known := make(map[string]bool, len(config.ModuleOrder))
	for _, id := range config.ModuleOrder {
		known[id] = true
	}
	seen := make(map[string]bool)
	for _, group := range groups {
		if group.QuestionKey == "" {
			t.Fatalf("模块组 %q 缺少问题文案", group.Key)
		}
		// 问题必须两种语言都有，否则英文用户会看到 key 名。
		for _, lang := range i18n.Supported() {
			if !i18n.Has(lang, group.QuestionKey) {
				t.Errorf("模块组 %q 的问题缺少 %s 译文", group.Key, lang)
			}
		}
		for _, id := range group.Modules {
			if !known[id] {
				t.Errorf("模块组 %q 引用了不存在的模块 %q", group.Key, id)
			}
			if seen[id] {
				t.Errorf("模块 %q 出现在多个组里，开关会互相覆盖", id)
			}
			seen[id] = true
		}
	}
	// 三类真正有代价的操作必须可关：交出出口 IP、跑满带宽、长耗时。
	for _, id := range []string{"network", "speed", "route"} {
		if !seen[id] {
			t.Errorf("有代价的模块 %q 必须可以在向导里关掉", id)
		}
	}
	// 本地基准不该出现在开关里——它们没有隐私或流量代价，关掉只会让报告残缺。
	for _, id := range []string{"cpu", "memory", "disk", "system"} {
		if seen[id] {
			t.Errorf("本地基准 %q 不应作为向导开关", id)
		}
	}
}

func TestWizardTextsAreTranslated(t *testing.T) {
	keys := []string{
		"wizard.title", "wizard.subtitle", "wizard.profileTitle", "wizard.selectPrompt",
		"wizard.invalidChoice", "wizard.invalidYesNo", "wizard.askReveal", "wizard.askStart",
		"wizard.summaryTitle", "wizard.revealWarning", "wizard.noModules", "wizard.aborted",
		"flag.interactive", "flag.yes",
	}
	for _, key := range keys {
		for _, lang := range i18n.Supported() {
			if !i18n.Has(lang, key) {
				t.Errorf("%q 缺少 %s 译文", key, lang)
			}
		}
	}
}

// 没有可用终端时向导必须放行，否则 cron 与 CI 会永远卡在等输入。
func TestWizardWithoutTerminalDoesNotBlock(t *testing.T) {
	cfg, err := config.Defaults(config.ProfileQuick)
	if err != nil {
		t.Fatal(err)
	}
	before := len(cfg.Modules)
	var out strings.Builder
	// 测试进程通常没有 /dev/tty；即便有，这里也只验证不阻塞与不改配置。
	if _, ok := newPrompter(&out, false); ok {
		t.Skip("当前环境有可用终端，跳过无终端路径测试")
	}
	if !runWizard(&cfg, &out) {
		t.Fatal("无终端时向导必须放行")
	}
	if len(cfg.Modules) != before {
		t.Fatal("无终端时不应改动配置")
	}
}
