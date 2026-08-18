package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// compare.sh 的契约测试。
//
// 与 run_script_test.go 同一个理由：这两个脚本是**产品制品**——用户直接
// curl 下来执行它们，其安全属性（只走 HTTPS、先校验后执行、不碰系统包）
// 是发给用户的承诺，不是 CI 配置。承诺需要有东西钉住。

func compareScriptPath(t *testing.T) string {
	t.Helper()
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(source), "..", "..", "compare.sh"))
}

func compareScriptSource(t *testing.T) string {
	t.Helper()
	script := compareScriptPath(t)
	if output, err := exec.Command("sh", "-n", script).CombinedOutput(); err != nil {
		t.Fatalf("sh -n compare.sh: %v\n%s", err, output)
	}
	contents, err := os.ReadFile(script)
	if err != nil {
		t.Fatalf("read compare.sh: %v", err)
	}
	return string(contents)
}

func TestCompareScriptVerifiesTheDownloadBeforeRunningIt(t *testing.T) {
	contents := compareScriptSource(t)

	// 校验必须发生在执行之前。curl|sh 这条路径上，SHA-256 是唯一能自证
	// 下载内容未被替换的环节；顺序颠倒等于没有校验。
	verify := strings.Index(contents, `[ "$ACTUAL" = "$EXPECTED" ]`)
	if verify < 0 {
		t.Fatal("compare.sh does not compare the actual digest against the expected one")
	}
	extract := strings.Index(contents, "tar -xzf")
	if extract < 0 {
		t.Fatal("compare.sh never extracts the archive")
	}
	if extract < verify {
		t.Error("compare.sh extracts the archive before verifying its digest")
	}
	run := strings.Index(contents, `"\$BINARY\" compare`)
	if run < 0 {
		t.Fatal("compare.sh never runs ecs compare")
	}
	if run < verify {
		t.Error("compare.sh runs the downloaded binary before verifying its digest")
	}

	for _, required := range []string{
		`[ "${#EXPECTED}" -eq 64 ]`,
		"sha256sum",
		"shasum -a 256",
		"openssl dgst -sha256",
	} {
		if !strings.Contains(contents, required) {
			t.Errorf("compare.sh is missing digest handling %q", required)
		}
	}
}

func TestCompareScriptStaysOnHTTPSAndNeverTouchesSystemPackages(t *testing.T) {
	contents := compareScriptSource(t)

	for _, forbidden := range []string{
		"--no-check-certificate",
		"--insecure",
		"sudo ",
		"apt-get",
		"dnf ",
		"yum ",
		"apk ",
	} {
		if strings.Contains(contents, forbidden) {
			t.Errorf("compare.sh must not contain %q", forbidden)
		}
	}
	for _, required := range []string{
		"--proto '=https' --tlsv1.2",
		"--https-only",
		"https://github.com/",
	} {
		if !strings.Contains(contents, required) {
			t.Errorf("compare.sh is missing transport guard %q", required)
		}
	}
	if strings.Contains(contents, "http://") {
		t.Error("compare.sh must not fetch anything over plain HTTP")
	}
}

// 用户要的是"清理执行文件、留下对比数据"。两个目录必须分开，
// 产出一旦落在被清理的目录里，保留承诺就是空的。
func TestCompareScriptRemovesTheBinaryButKeepsTheComparison(t *testing.T) {
	contents := compareScriptSource(t)

	if !strings.Contains(contents, `WORK=$(mktemp -d "$WORK_ROOT/ecs-compare.XXXXXX")`) {
		t.Error("compare.sh does not create a private work directory")
	}
	if !strings.Contains(contents, `OUT=$(mktemp -d "$WORK_ROOT/ecs-comparison.XXXXXX")`) {
		t.Error("compare.sh does not create a separate output directory")
	}
	if !strings.Contains(contents, `rm -rf "$WORK"`) {
		t.Error("compare.sh never removes the work directory that holds the binary")
	}
	if strings.Contains(contents, `rm -rf "$OUT"`) {
		t.Error("compare.sh removes the comparison output; it must be kept")
	}
	if !strings.Contains(contents, `--output \"\$OUT\"`) {
		t.Error("compare.sh does not send the comparison into the kept output directory")
	}
	// 二进制必须在会被删掉的那个目录里。
	if !strings.Contains(contents, `BINARY="$WORK/ecs"`) {
		t.Error("the downloaded binary must live inside the work directory that gets removed")
	}
}

// 不在调用者的当前目录创建任何东西：两个目录都开在 /tmp 下。
func TestCompareScriptWritesOnlyUnderTemp(t *testing.T) {
	contents := compareScriptSource(t)

	if !strings.Contains(contents, `WORK_ROOT="/tmp"`) {
		t.Error("compare.sh does not default its work root to /tmp")
	}
	if !strings.Contains(contents, `die "TMPDIR 必须是绝对路径"`) {
		t.Error("compare.sh accepts a relative TMPDIR, which could escape /tmp")
	}
	if strings.Contains(contents, "./reports") {
		t.Error("compare.sh must not create a reports directory next to the caller")
	}
}

// --install 复用 install.sh，不造第二套安装逻辑：安装路径只有一个定义。
func TestCompareScriptDelegatesInstallation(t *testing.T) {
	contents := compareScriptSource(t)

	if !strings.Contains(contents, `sh "$WORK/install.sh" --from "$BINARY"`) {
		t.Error("compare.sh does not delegate installation to install.sh")
	}
	for _, forbidden := range []string{"/usr/local/bin", ".local/bin"} {
		if strings.Contains(contents, forbidden) {
			t.Errorf("compare.sh hardcodes an install path %q; install.sh owns that decision", forbidden)
		}
	}
}

// 架构表是两个脚本最容易分叉的地方：run.sh 支持而 compare.sh 不支持的架构，
// 用户会在能跑测试的机器上发现自己不能对比报告。
//
// 连 uname 别名一起比对，而不只比解析出的架构名：某台机器 uname -m 报 x86，
// run.sh 认得而 compare.sh 不认，症状同样是"能测不能比"。
func TestCompareScriptSupportsTheSameArchitecturesAsRunScript(t *testing.T) {
	pattern := regexp.MustCompile(`(?m)^\s*([a-z0-9_|]+)\)\s*ARCH=([a-z0-9]+)\s*;;`)
	collect := func(contents string) map[string]string {
		found := make(map[string]string)
		for _, match := range pattern.FindAllStringSubmatch(contents, -1) {
			for _, alias := range strings.Split(match[1], "|") {
				found[alias] = match[2]
			}
		}
		return found
	}

	runContents, err := os.ReadFile(runScriptPath(t))
	if err != nil {
		t.Fatalf("read run.sh: %v", err)
	}
	runArches := collect(string(runContents))
	compareArches := collect(compareScriptSource(t))

	if len(runArches) == 0 {
		t.Fatal("no architectures parsed from run.sh; the contract test itself is broken")
	}
	for alias, arch := range runArches {
		switch compareArches[alias] {
		case arch:
		case "":
			t.Errorf("run.sh maps uname %q to %q but compare.sh does not recognise it", alias, arch)
		default:
			t.Errorf("uname %q maps to %q in run.sh but %q in compare.sh", alias, arch, compareArches[alias])
		}
	}
	for alias := range compareArches {
		if _, ok := runArches[alias]; !ok {
			t.Errorf("compare.sh recognises uname %q but run.sh does not", alias)
		}
	}
}

// 缓存把"下载并校验过的东西"留在了脚本生命周期之外，因此它必须自带完整性
// 检查——否则缓存目录就成了一个绕过 SHA-256 校验执行任意内容的入口。
func TestCompareScriptReverifiesTheCachedBinaryBeforeRunningIt(t *testing.T) {
	contents := compareScriptSource(t)

	for _, required := range []string{
		"XDG_CACHE_HOME",   // 缓存位置遵循 XDG，不往家目录乱扔
		"digest_of",        // 命中后重新算摘要
		"${CACHED}.sha256", // 摘要与二进制一起存
	} {
		if !strings.Contains(contents, required) {
			t.Errorf("缓存实现缺少 %q", required)
		}
	}

	// 命中缓存的分支必须在 cp 之前比对摘要。顺序反了等于没有校验。
	compare := strings.Index(contents, `"$cache_want" = "$cache_have"`)
	use := strings.Index(contents, `cp "$CACHED" "$BINARY"`)
	if compare < 0 || use < 0 || compare > use {
		t.Fatalf("缓存必须先比对摘要再使用：compare=%d use=%d", compare, use)
	}

	// 摘要不符时不能将就着用，必须删掉重下。
	if !strings.Contains(contents, `rm -f "$CACHED" "${CACHED}.sha256"`) {
		t.Error("摘要不符时必须丢弃缓存")
	}

	// 缓存键必须含具体版本：latest 变了还命中旧二进制，用户会拿到一个
	// 自己没要求的版本，且没有任何提示。
	if !strings.Contains(contents, "releases/tag/") || !strings.Contains(contents, "${TAG}") {
		t.Error("缓存键必须按解析出的具体 Release tag 分目录")
	}
}

// README 承诺 --format / --reference 等原样透传。带值选项若不被识别，它的
// 取值会掉进位置参数分支被当成报告路径，报出"找不到报告文件：txt"——一个
// 与真实原因毫无关系的错误。
func TestCompareScriptUnderstandsSeparatedOptionValues(t *testing.T) {
	contents := compareScriptSource(t)

	if !strings.Contains(contents, "takes_value()") {
		t.Fatal("缺少带值选项的识别")
	}
	// 与 ecs compare 的 normalizeCompareArgs 同一张表：少一个就是一个
	// 用户会踩到的假错误。
	for _, flag := range []string{"--lang", "--format", "--output", "--name", "--reference", "--color"} {
		if !strings.Contains(contents, flag+" |") && !strings.Contains(contents, "| "+flag+" ") {
			t.Errorf("takes_value 缺少 %s", flag)
		}
	}
	if !strings.Contains(contents, "缺少取值") {
		t.Error("选项缺取值时必须明确报错，而不是把下一个参数吞掉")
	}
}

// 用户自己给了 --output 时，脚本不该再建一个自己的结果目录：那会在 /tmp
// 下留一个空目录，而且随后"结果保留在 …"会指向一个没有结果的地方。
func TestCompareScriptDoesNotCreateAnUnusedOutputDirectory(t *testing.T) {
	contents := compareScriptSource(t)

	if !strings.Contains(contents, `[ "$OUTPUT_GIVEN" -eq 1 ] || OUT=$(mktemp`) {
		t.Error("用户指定 --output 时不应创建脚本自己的结果目录")
	}
	if !strings.Contains(contents, `if [ -n "$OUT" ]; then`) {
		t.Error("只有目录是脚本挑的，才由脚本报告它")
	}
}

// ---- run.sh 的 --compare 转发 ----
//
// 两个入口等价，但实现只能有一份。这几条钉住的是"转发"这件事本身：一旦有人
// 图省事在 run.sh 里重写一遍对比逻辑，缓存、--install、输出目录语义就会立刻
// 出现两份会分叉的实现。

func runScriptSource(t *testing.T) string {
	t.Helper()
	script := runScriptPath(t)
	if output, err := exec.Command("sh", "-n", script).CombinedOutput(); err != nil {
		t.Fatalf("sh -n run.sh: %v\n%s", err, output)
	}
	contents, err := os.ReadFile(script)
	if err != nil {
		t.Fatalf("read run.sh: %v", err)
	}
	return string(contents)
}

func TestRunScriptForwardsCompareInsteadOfReimplementingIt(t *testing.T) {
	contents := runScriptSource(t)

	if !strings.Contains(contents, `sh "$WORK/compare.sh" "$@"`) {
		t.Error("run.sh does not hand --compare over to compare.sh")
	}
	// 属于 compare.sh 的东西不该在 run.sh 里出现第二份。
	for _, reimplementation := range []string{
		"ecs compare",
		"ECS_CACHE",
		"ecs-comparison.XXXXXX",
		"install.sh --from",
	} {
		if strings.Contains(contents, reimplementation) {
			t.Errorf("run.sh reimplements %q; that logic belongs to compare.sh alone", reimplementation)
		}
	}
}

// exec 会替换掉当前 shell，EXIT trap 不再触发，run.sh 自己的 WORK 会永远留在
// /tmp。这个错误只在真机上跑过才看得见，因此用测试钉住。
func TestRunScriptDoesNotExecAwayItsOwnCleanup(t *testing.T) {
	contents := runScriptSource(t)

	forward := strings.Index(contents, `sh "$WORK/compare.sh"`)
	if forward < 0 {
		t.Fatal("run.sh has no compare forward")
	}
	if strings.Contains(contents, `exec sh "$WORK/compare.sh"`) {
		t.Error("run.sh execs the forward; its EXIT trap would never remove WORK")
	}
	if !strings.Contains(contents, `exit "$COMPARE_STATUS"`) {
		t.Error("run.sh does not propagate compare.sh's exit status")
	}
}

// 转发必须发生在依赖准备之前：对比不需要任何基准工具，跑完 apt/QEMU/语料那
// 一整套再去比较两个 JSON 是纯粹的浪费，也会在没有网络的机器上无谓失败。
func TestRunScriptForwardsCompareBeforePreparingAnyTooling(t *testing.T) {
	contents := runScriptSource(t)

	forward := strings.Index(contents, `sh "$WORK/compare.sh"`)
	if forward < 0 {
		t.Fatal("run.sh has no compare forward")
	}
	for _, tooling := range []string{
		"select_package_manager()",
		"prepare_tools_archive()",
		"prepare_zstd_corpus()",
		`fetch "${BASE}/${ASSET}"`,
	} {
		at := strings.Index(contents, tooling)
		if at < 0 {
			t.Fatalf("run.sh no longer contains %q; this contract test needs updating", tooling)
		}
		if at < forward {
			t.Errorf("run.sh reaches %q before forwarding --compare", tooling)
		}
	}
}

func TestRunScriptRejectsCompareCombinedWithSubmit(t *testing.T) {
	contents := runScriptSource(t)

	if !strings.Contains(contents, `--compare 不能与 --submit 同用`) {
		t.Error("run.sh accepts --compare together with --submit; a comparison is not a test result")
	}
	if !strings.Contains(contents, `--compare=*) die`) {
		t.Error("run.sh does not reject --compare=VALUE")
	}
	// --compare 这个 token 本身必须被滤掉，否则会被当成未知选项转给 ecs compare。
	if !strings.Contains(contents, "COMPARE_SENTINEL") {
		t.Error("run.sh does not filter the --compare token out of the forwarded arguments")
	}
}
