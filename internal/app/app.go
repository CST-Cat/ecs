package app

import (
	"context"
	"fmt"
	"io"
	"runtime"
	"strings"

	"ecs/internal/buildinfo"
	"ecs/internal/i18n"
)

func Main(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	// 语言要在任何输出之前定下来：帮助文本、错误信息都要用它。
	// 显式 --lang 优先，其次看环境变量，最后回落中文。
	languageFlags := scanEarlyFlags(args, "lang")
	i18n.Set(resolveLanguage(languageFlags))
	if err := validateExplicitLanguage(languageFlags); err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", i18n.T("cli.error"), err)
		return 1
	}
	command, args := dispatchCommand(args, languageFlags)
	switch command {
	case "run":
		return runCommand(ctx, args, stdout, stderr)
	case "plan":
		return planCommand(args, stdout, stderr)
	case "render":
		return renderCommand(args, stdout, stderr)
	case "compare":
		return compareCommand(args, stdout, stderr)
	case "list":
		return listCommand(args, stdout, stderr)
	case "config":
		return configCommand(args, stdout, stderr)
	case "doctor":
		return doctorCommand(ctx, stdout)
	case "leaderboard":
		return leaderboardCommand(args, stdout, stderr)
	case "submit":
		return submitCommand(args, stdout, stderr)
	case "version":
		fmt.Fprintf(stdout, "%s %s commit=%s built=%s go=%s\n", buildinfo.Name, buildinfo.Version, buildinfo.Commit, buildinfo.BuildDate, runtime.Version())
		return 0
	case "help", "-h", "--help":
		printHelp(stdout)
		return 0
	default:
		fmt.Fprintf(stderr, "%s %q\n\n", i18n.T("cli.unknownCommand"), command)
		printHelp(stderr)
		return 1
	}
}

// dispatchCommand accepts the global language flag on either side of a
// command, then hands the command-specific flag set an argv with that global
// flag removed.  A non-language option before a command still means the
// default run command; its value must not be mistaken for a command name.
func dispatchCommand(args []string, languageFlags []earlyFlagOccurrence) (string, []string) {
	position := 0
	for _, occurrence := range languageFlags {
		if occurrence.Position != position {
			break
		}
		position = occurrence.End
	}
	commandFound := position < len(args) && args[position] != "--" && !strings.HasPrefix(args[position], "-")

	cleaned := stripExplicitLanguage(args, languageFlags)
	if !commandFound || len(cleaned) == 0 || strings.HasPrefix(cleaned[0], "-") {
		return "run", cleaned
	}
	return cleaned[0], cleaned[1:]
}

// resolveLanguage 在解析命令前先从已扫描的 occurrences 取出 --lang。
//
// flag 包要等到子命令解析时才能拿到值，但帮助与错误输出比那更早，
// 因此这里先扫一遍参数。
func resolveLanguage(occurrences []earlyFlagOccurrence) i18n.Lang {
	var resolved i18n.Lang
	valid := false
	for _, occurrence := range occurrences {
		if !occurrence.HasValue || strings.TrimSpace(occurrence.Value) == "" {
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

func validateExplicitLanguage(occurrences []earlyFlagOccurrence) error {
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

func printHelp(writer io.Writer) {
	if i18n.Current() == i18n.LangEN {
		fmt.Fprintln(writer, `ecs — ad-free VPS benchmark with local reports by default

Usage:
  ecs [run] [options]         run tests (standard by default)
  ecs plan [options]         print the resolved machine execution plan as JSON
  ecs list                    show profiles and modules
  ecs render --input FILE     re-export JSON/Markdown/HTML from JSON
  ecs compare REPORTS...      compare 2 or more JSON reports safely
  ecs config example          print a sample configuration
  ecs doctor                  check standard benchmark tools
  ecs leaderboard REPORTS...  aggregate a leaderboard reference
  ecs submit --input FILE     export a minimized public submission
  ecs version                 show version

Examples:
  ecs
  ecs --profile standard --exposure local
  ecs --profile full --exposure public
  ecs --profile full --skip media --output ./reports
  ecs --only system,cpu,memory,disk --format json,html
  ecs compare old.json new.json --format json,md,html --output ./compare

Run ecs run --help for all test options or ecs compare --help for comparison options.`)
		return
	}
	fmt.Fprintln(writer, `ecs — 无广告、默认零上传的 VPS 综合测试工具

用法:
  ecs [run] [选项]            运行测试（默认 standard）
  ecs plan [选项]            以 JSON 输出解析后的机器执行计划
  ecs list                    查看配置档与模块
  ecs render --input FILE     从 JSON 重新导出 JSON/Markdown/HTML 三种格式
  ecs compare REPORTS...      安全比较 2 份或更多 JSON 报告
  ecs config example          输出配置文件示例
  ecs doctor                  检查标准基准工具
  ecs leaderboard REPORTS...  从多份报告聚合排行榜参考
  ecs submit --input FILE     导出可公开入库的瘦身提交
  ecs version                 显示版本

常用示例:
  ecs
  ecs --profile standard --exposure local
  ecs --profile full --exposure public
  ecs --profile full --skip media --output ./reports
  ecs --only system,cpu,memory,disk --format json,html
  ecs compare old.json new.json --format json,md,html --output ./compare

运行 ecs run --help 查看测试参数，或运行 ecs compare --help 查看对比参数。`)
}
