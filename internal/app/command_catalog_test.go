package app

import (
	"bytes"
	"strings"
	"testing"

	"ecs/internal/i18n"
)

func TestCommandDefinitionsHaveCanonicalOrderAndHandlers(t *testing.T) {
	wantNames := []string{"run", "plan", "list", "render", "compare", "config", "leaderboard", "submit", "version", "help"}
	definitions := commandDefinitions()
	if got := len(definitions); got != len(wantNames) {
		t.Fatalf("command definition count = %d, want %d", got, len(wantNames))
	}
	seen := make(map[string]struct{}, len(definitions))
	for index, definition := range definitions {
		if definition.Name != wantNames[index] {
			t.Fatalf("command order[%d] = %q, want %q", index, definition.Name, wantNames[index])
		}
		if strings.TrimSpace(definition.Name) == "" {
			t.Fatalf("command definition %d has an empty name", index)
		}
		if _, exists := seen[definition.Name]; exists {
			t.Fatalf("command definitions contain duplicate command %q", definition.Name)
		}
		seen[definition.Name] = struct{}{}
		if definition.Handler == nil {
			t.Fatalf("command %q has a nil handler", definition.Name)
		}
		if strings.TrimSpace(definition.UsageKey) == "" && strings.TrimSpace(definition.DescriptionKey) == "" {
			t.Fatalf("command %q has no help metadata", definition.Name)
		}
	}

	second := commandDefinitions()
	for index := range definitions {
		if definitions[index].Name != second[index].Name || definitions[index].UsageKey != second[index].UsageKey || definitions[index].DescriptionKey != second[index].DescriptionKey {
			t.Fatalf("command order/metadata changed between constructions: first=%+v second=%+v", definitions, second)
		}
	}
}

func TestLookupCommandMatchesExactNames(t *testing.T) {
	definitions := commandDefinitions()
	for _, name := range []string{"run", "plan", "help"} {
		definition, ok := lookupCommand(definitions, name)
		if !ok || definition.Name != name || definition.Handler == nil {
			t.Fatalf("lookup %q = %+v/%t", name, definition, ok)
		}
	}
	if definition, ok := lookupCommand(definitions, "does-not-exist"); ok || definition.Name != "" || definition.Handler != nil || definition.UsageKey != "" || definition.DescriptionKey != "" {
		t.Fatalf("unknown lookup = %+v/%t, want zero/false", definition, ok)
	}
}

func TestHelpListsEachCommandExactlyOnce(t *testing.T) {
	originalLanguage := i18n.Current()
	t.Cleanup(func() { i18n.Set(originalLanguage) })

	for _, language := range []i18n.Lang{i18n.LangZH, i18n.LangEN} {
		t.Run(string(language), func(t *testing.T) {
			i18n.Set(language)
			var output bytes.Buffer
			printHelp(newApplication().commands, &output)
			text := output.String()
			sectionStart := strings.Index(text, i18n.T("cli.usage")+":\n")
			if sectionStart < 0 {
				t.Fatalf("help is missing the %q heading: %q", i18n.T("cli.usage"), text)
			}
			sectionStart += len(i18n.T("cli.usage") + ":\n")
			examplesHeading := "\n\nExamples:\n"
			if language == i18n.LangZH {
				examplesHeading = "\n\n常用示例:\n"
			}
			sectionEnd := strings.Index(text[sectionStart:], examplesHeading)
			if sectionEnd < 0 {
				t.Fatalf("help is missing the examples boundary: %q", text)
			}
			section := text[sectionStart : sectionStart+sectionEnd+1]
			for _, definition := range newApplication().commands {
				line := "  " + commandHelpText(definition) + "\n"
				if count := strings.Count(section, line); count != 1 {
					t.Errorf("command %q appears %d times in help command list, want exactly once", definition.Name, count)
				}
			}
			if !strings.Contains(text, "ecs compare old.json new.json --format json,md,html --output ./compare") {
				t.Error("help examples are missing")
			}
		})
	}
}

func TestHelpPreservesExistingCommandLines(t *testing.T) {
	originalLanguage := i18n.Current()
	t.Cleanup(func() { i18n.Set(originalLanguage) })

	for _, test := range []struct {
		language i18n.Lang
		lines    []string
	}{
		{
			language: i18n.LangZH,
			lines: []string{
				"ecs [run] [选项]            运行测试（默认 standard）",
				"ecs plan [选项]            以 JSON 输出解析后的机器执行计划",
				"ecs list                    查看配置档与模块",
				"ecs render --input FILE     默认重新导出 Markdown/HTML；显式 json 可重新导出，路径相同时可能覆盖输入文件",
				"ecs compare REPORTS...      安全比较 2 份或更多 JSON 报告",
				"ecs config example          输出配置文件示例",
				"ecs leaderboard REPORTS...  从多份报告聚合排行榜参考",
				"ecs submit --input FILE     导出可公开入库的瘦身提交",
				"ecs version                 显示版本",
			},
		},
		{
			language: i18n.LangEN,
			lines: []string{
				"ecs [run] [options]         run tests (standard by default)",
				"ecs plan [options]         print the resolved machine execution plan as JSON",
				"ecs list                    show profiles and modules",
				"ecs render --input FILE     re-export Markdown/HTML by default; json may re-export and overwrite when the path matches",
				"ecs compare REPORTS...      compare 2 or more JSON reports safely",
				"ecs config example          print a sample configuration",
				"ecs leaderboard REPORTS...  aggregate a leaderboard reference",
				"ecs submit --input FILE     export a minimized public submission",
				"ecs version                 show version",
			},
		},
	} {
		t.Run(string(test.language), func(t *testing.T) {
			i18n.Set(test.language)
			var output bytes.Buffer
			printHelp(newApplication().commands, &output)
			for _, line := range test.lines {
				if count := strings.Count(output.String(), "  "+line+"\n"); count != 1 {
					t.Errorf("existing help line %q appears %d times, want exactly once; output=%q", line, count, output.String())
				}
			}
		})
	}
}
