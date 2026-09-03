package app

import (
	"context"
	"fmt"
	"io"
	"strings"

	"ecs/internal/i18n"
)

func Main(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	languageFlags, commandLine := globalLanguagePrefix(args)
	// 语言要在任何输出之前定下来：帮助文本、错误信息都要用它。
	// 显式 --lang 优先，其次看环境变量，最后回落中文。
	i18n.Set(resolveLanguage(languageFlags))
	if err := validateExplicitLanguage(languageFlags); err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", i18n.T("cli.error"), err)
		return 1
	}
	application := newApplication()
	command, commandArgs := dispatchCommand(commandLine)
	definition, ok := application.commands.lookup(command)
	if !ok {
		fmt.Fprintf(stderr, "%s %q\n\n", i18n.T("cli.unknownCommand"), command)
		printHelp(application.commands, stderr)
		return 1
	}
	return definition.Handler(application, ctx, commandArgs, stdout, stderr)
}

// dispatchCommand sees argv only after the global prefix has been removed. A
// leading option still selects the default run command, whose FlagSet owns the
// complete run grammar.
func dispatchCommand(args []string) (string, []string) {
	if len(args) == 0 || args[0] == "--" || strings.HasPrefix(args[0], "-") {
		return "run", args
	}
	return args[0], args[1:]
}

// resolveLanguage 在解析命令前先从已扫描的 occurrences 取出 --lang。
//
// flag 包要等到子命令解析时才能拿到值，但帮助与错误输出比那更早，
// 因此这里先扫一遍参数。
func resolveLanguage(occurrences []languageFlagOccurrence) i18n.Lang {
	var resolved i18n.Lang
	valid := false
	for _, occurrence := range occurrences {
		if occurrence.Missing || strings.TrimSpace(occurrence.Value) == "" {
			continue
		}
		if lang, ok := i18n.Parse(occurrence.Value); ok {
			resolved = lang
			valid = true
		}
	}
	if valid {
		return resolved
	}
	return i18n.DetectFromEnv()
}

func validateExplicitLanguage(occurrences []languageFlagOccurrence) error {
	if len(occurrences) == 0 {
		return nil
	}
	occurrence := occurrences[len(occurrences)-1]
	if occurrence.Missing {
		return fmt.Errorf("--lang requires a value")
	}
	if strings.TrimSpace(occurrence.Value) == "" {
		return fmt.Errorf("--lang value must not be empty")
	}
	if _, ok := i18n.Parse(occurrence.Value); !ok {
		return fmt.Errorf("invalid --lang value %q; choose zh or en", occurrence.Value)
	}
	return nil
}
