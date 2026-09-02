package app

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"ecs/internal/i18n"
)

func catalogTestHandler(context.Context, []string, io.Writer, io.Writer) int {
	return 0
}

func TestDefaultCommandCatalogInvariantsAndOrder(t *testing.T) {
	wantNames := []string{"run", "plan", "list", "render", "compare", "config", "doctor", "leaderboard", "submit", "version", "help"}
	catalog := defaultCommandCatalog()
	definitions := catalog.definitionsInOrder()
	if got := len(definitions); got != len(wantNames) {
		t.Fatalf("catalog definition count = %d, want %d", got, len(wantNames))
	}
	seen := make(map[string]struct{}, len(definitions))
	for index, definition := range definitions {
		if definition.Name != wantNames[index] {
			t.Fatalf("catalog order[%d] = %q, want %q", index, definition.Name, wantNames[index])
		}
		if strings.TrimSpace(definition.Name) == "" {
			t.Fatalf("catalog definition %d has an empty name", index)
		}
		if _, exists := seen[definition.Name]; exists {
			t.Fatalf("catalog contains duplicate command %q", definition.Name)
		}
		seen[definition.Name] = struct{}{}
		if definition.Handler == nil {
			t.Fatalf("catalog command %q has a nil handler", definition.Name)
		}
		if strings.TrimSpace(definition.UsageKey) == "" && strings.TrimSpace(definition.DescriptionKey) == "" {
			t.Fatalf("catalog command %q has no help metadata", definition.Name)
		}
	}

	second := defaultCommandCatalog().definitionsInOrder()
	for index := range definitions {
		if definitions[index].Name != second[index].Name || definitions[index].UsageKey != second[index].UsageKey || definitions[index].DescriptionKey != second[index].DescriptionKey {
			t.Fatalf("catalog order/metadata changed between constructions: first=%+v second=%+v", definitions, second)
		}
	}
}

func TestNewCommandCatalogRejectsInvalidDefinitions(t *testing.T) {
	valid := commandDefinition{Name: "valid", Handler: catalogTestHandler, UsageKey: "help.valid"}
	for _, test := range []struct {
		name        string
		definitions []commandDefinition
		wantError   string
	}{
		{name: "empty name", definitions: []commandDefinition{{Name: "  ", Handler: catalogTestHandler, UsageKey: "help.valid"}}, wantError: "empty name"},
		{name: "leading whitespace name", definitions: []commandDefinition{{Name: " valid", Handler: catalogTestHandler, UsageKey: "help.valid"}}, wantError: "surrounding whitespace"},
		{name: "trailing whitespace name", definitions: []commandDefinition{{Name: "valid ", Handler: catalogTestHandler, UsageKey: "help.valid"}}, wantError: "surrounding whitespace"},
		{name: "whitespace variant of existing name", definitions: []commandDefinition{
			{Name: "run", Handler: catalogTestHandler, UsageKey: "help.valid"},
			{Name: " run ", Handler: catalogTestHandler, UsageKey: "help.valid"},
		}, wantError: "surrounding whitespace"},
		{name: "duplicate name", definitions: []commandDefinition{valid, valid}, wantError: "duplicate command"},
		{name: "nil handler", definitions: []commandDefinition{{Name: "nil", UsageKey: "help.nil"}}, wantError: "nil handler"},
		{name: "missing help metadata", definitions: []commandDefinition{{Name: "no-help", Handler: catalogTestHandler}}, wantError: "missing help metadata"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := newCommandCatalog(test.definitions)
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("newCommandCatalog error = %v, want substring %q", err, test.wantError)
			}
		})
	}
}

func TestCommandCatalogCopiesDefinitionsAndLookupIsImmutable(t *testing.T) {
	input := []commandDefinition{{Name: "valid", Handler: catalogTestHandler, UsageKey: "help.valid"}}
	catalog, err := newCommandCatalog(input)
	if err != nil {
		t.Fatal(err)
	}
	input[0].Name = "mutated-input"
	input[0].UsageKey = "mutated-input"

	definition, ok := catalog.lookup("valid")
	if !ok || definition.Name != "valid" || definition.UsageKey != "help.valid" {
		t.Fatalf("lookup after input mutation = %+v, ok=%v", definition, ok)
	}
	definition.Name = "mutated-result"
	definition.UsageKey = "mutated-result"
	definition.Handler = nil
	if _, ok := catalog.lookup("mutated-result"); ok {
		t.Fatal("mutating lookup result changed catalog identity")
	}
	definition, ok = catalog.lookup("valid")
	if !ok || definition.Name != "valid" || definition.UsageKey != "help.valid" || definition.Handler == nil {
		t.Fatalf("catalog changed through lookup result: %+v, ok=%v", definition, ok)
	}

	ordered := catalog.definitionsInOrder()
	ordered[0].Name = "mutated-copy"
	ordered[0].UsageKey = "mutated-copy"
	orderedAgain := catalog.definitionsInOrder()
	if len(orderedAgain) != 1 || orderedAgain[0].Name != "valid" || orderedAgain[0].UsageKey != "help.valid" || orderedAgain[0].Handler == nil {
		t.Fatalf("catalog changed through definitions copy: %+v", orderedAgain)
	}
}

func TestCommandCatalogUnknownLookupHasNoDefaultHandler(t *testing.T) {
	catalog := defaultCommandCatalog()
	definition, ok := catalog.lookup("does-not-exist")
	if ok {
		t.Fatalf("unknown lookup reported a match: %+v", definition)
	}
	if definition.Handler != nil || definition.Name != "" || definition.UsageKey != "" || definition.DescriptionKey != "" {
		t.Fatalf("unknown lookup returned a default definition: %+v", definition)
	}
}

func TestHelpListsEachCatalogCommandExactlyOnce(t *testing.T) {
	originalLanguage := i18n.Current()
	t.Cleanup(func() { i18n.Set(originalLanguage) })

	for _, language := range []i18n.Lang{i18n.LangZH, i18n.LangEN} {
		t.Run(string(language), func(t *testing.T) {
			i18n.Set(language)
			var output bytes.Buffer
			printHelp(&output)
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
			for _, definition := range defaultCommandCatalog().definitionsInOrder() {
				line := "  " + commandHelpText(definition) + "\n"
				if count := strings.Count(section, line); count != 1 {
					t.Errorf("catalog command %q appears %d times in help command list, want exactly once", definition.Name, count)
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
				"ecs render --input FILE     从 JSON 重新导出 JSON/Markdown/HTML 三种格式",
				"ecs compare REPORTS...      安全比较 2 份或更多 JSON 报告",
				"ecs config example          输出配置文件示例",
				"ecs doctor                  检查标准基准工具",
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
				"ecs render --input FILE     re-export JSON/Markdown/HTML from JSON",
				"ecs compare REPORTS...      compare 2 or more JSON reports safely",
				"ecs config example          print a sample configuration",
				"ecs doctor                  check standard benchmark tools",
				"ecs leaderboard REPORTS...  aggregate a leaderboard reference",
				"ecs submit --input FILE     export a minimized public submission",
				"ecs version                 show version",
			},
		},
	} {
		t.Run(string(test.language), func(t *testing.T) {
			i18n.Set(test.language)
			var output bytes.Buffer
			printHelp(&output)
			for _, line := range test.lines {
				if count := strings.Count(output.String(), "  "+line+"\n"); count != 1 {
					t.Errorf("existing help line %q appears %d times, want exactly once; output=%q", line, count, output.String())
				}
			}
		})
	}
}
