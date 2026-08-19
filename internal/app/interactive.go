package app

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"ecs/internal/config"
	"ecs/internal/i18n"
	"ecs/internal/probe"
)

// 交互向导。
//
// 关键前提：`curl -fsSL … | sh` 时进程的 stdin 是那条管道，脚本内容正从里面读，
// 用它接收键盘输入只会读到脚本残渣或直接 EOF。因此交互输入一律走 /dev/tty，
// 那才是真正的终端。没有 /dev/tty（cron、容器、CI）时不做交互，按默认值直接跑。

// prompter 封装一次交互会话。
type prompter struct {
	in     *bufio.Reader
	out    io.Writer
	closer io.Closer
	color  bool
}

// newPrompter 打开终端。第二个返回值为 false 表示当前环境无法交互。
func newPrompter(out io.Writer, color bool) (*prompter, bool) {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return nil, false
	}
	return &prompter{in: bufio.NewReader(tty), out: out, closer: tty, color: color}, true
}

func (p *prompter) Close() {
	if p.closer != nil {
		_ = p.closer.Close()
	}
}

func (p *prompter) style(code, value string) string {
	if !p.color {
		return value
	}
	return "\x1b[" + code + "m" + value + "\x1b[0m"
}

func (p *prompter) line(format string, values ...any) {
	fmt.Fprintf(p.out, format+"\n", values...)
}

// ask 读一行输入；EOF 或读取失败时返回空串，由调用方回落到默认值。
func (p *prompter) ask(question string) string {
	fmt.Fprint(p.out, question)
	text, err := p.in.ReadString('\n')
	if err != nil && text == "" {
		fmt.Fprintln(p.out)
		return ""
	}
	return strings.TrimSpace(text)
}

// confirm 询问是/否，defaultYes 决定直接回车时的取值。
func (p *prompter) confirm(question string, defaultYes bool) bool {
	hint := "[y/N]"
	if defaultYes {
		hint = "[Y/n]"
	}
	for {
		answer := strings.ToLower(p.ask(question + " " + p.style("2", hint) + " "))
		switch answer {
		case "":
			return defaultYes
		case "y", "yes", "是":
			return true
		case "n", "no", "否":
			return false
		}
		p.line("  %s", p.style("33", i18n.T("wizard.invalidYesNo")))
	}
}

// choose 让用户从编号选项里选一个，返回下标。
func (p *prompter) choose(title string, options []string, defaultIndex int) int {
	p.line("%s", p.style("1", title))
	for index, option := range options {
		marker := "  "
		if index == defaultIndex {
			marker = p.style("36", "→ ")
		}
		p.line("%s%d) %s", marker, index+1, option)
	}
	for {
		answer := p.ask(fmt.Sprintf("%s [%d] ", i18n.T("wizard.selectPrompt"), defaultIndex+1))
		if answer == "" {
			return defaultIndex
		}
		var choice int
		if _, err := fmt.Sscanf(answer, "%d", &choice); err == nil && choice >= 1 && choice <= len(options) {
			return choice - 1
		}
		p.line("  %s", p.style("33", i18n.T("wizard.invalidChoice")))
	}
}

// wizardModule 是一个可在向导里开关的模块组。
//
// 分组而不是逐个模块问：一次问十几个开关没人愿意答，而用户真正关心的是
// "要不要把出口 IP 交出去""要不要跑大流量""要不要等更久"这几件事。
type wizardModule struct {
	Key         string
	Modules     []string
	DefaultOn   bool
	QuestionKey string
}

func wizardModules() []wizardModule {
	groups := make([]wizardModule, 0)
	positions := make(map[string]int)
	for _, descriptor := range config.ModuleDescriptors() {
		if descriptor.WizardGroup == "" {
			continue
		}
		position, ok := positions[descriptor.WizardGroup]
		if !ok {
			positions[descriptor.WizardGroup] = len(groups)
			groups = append(groups, wizardModule{
				Key:         descriptor.WizardGroup,
				DefaultOn:   true,
				QuestionKey: descriptor.WizardQuestionKey,
			})
			position = len(groups) - 1
		}
		groups[position].Modules = append(groups[position].Modules, descriptor.ID)
	}
	return groups
}

// runWizard 引导用户调整配置。返回 false 表示用户放弃本次运行。
func runWizard(cfg *config.Runtime, out io.Writer) bool {
	prompt, ok := newPrompter(out, !cfg.NoColor)
	if !ok {
		// 没有可用终端：保持默认配置直接跑，不阻塞自动化场景。
		return true
	}
	defer prompt.Close()

	prompt.line("")
	prompt.line("%s", prompt.style("1;36", i18n.T("wizard.title")))
	prompt.line("%s", prompt.style("2", i18n.T("wizard.subtitle")))
	prompt.line("")

	// 一、配置档
	profiles := []string{config.ProfileStandard, config.ProfileFull}
	labels := make([]string, len(profiles))
	defaultIndex := 0 // standard
	for index, profile := range profiles {
		labels[index] = fmt.Sprintf("%-9s %s", profile, i18n.T("profile."+profile))
		if profile == cfg.Profile {
			defaultIndex = index
		}
	}
	chosen := profiles[prompt.choose(i18n.T("wizard.profileTitle"), labels, defaultIndex)]
	if chosen != cfg.Profile {
		updated, err := config.Defaults(chosen)
		if err == nil {
			// 保留用户已经通过命令行指定的输出与协议族偏好。
			updated.Output, updated.Formats = cfg.Output, cfg.Formats
			updated.NoColor, updated.Reveal = cfg.NoColor, cfg.Reveal
			updated.IPVersion = cfg.IPVersion
			// 外联设置同样是用户的显式选择：换档位不该悄悄把它放宽回默认值。
			updated.Exposure = cfg.Exposure
			*cfg = updated
		}
	}
	prompt.line("")

	// 二、外联级别
	//
	// 放在模块问答之前：它决定了后面还有哪些模块可问。用户在这一步表达的是
	// "我愿意让多少东西离开这台机器"，比逐个模块勾选更接近真实意图。
	levels := config.ExposureNames()
	exposureLabels := make([]string, len(levels))
	exposureDefault := 0
	for index, name := range levels {
		exposureLabels[index] = fmt.Sprintf("%-11s %s", name, i18n.T("exposure."+name))
		if name == cfg.Exposure.String() {
			exposureDefault = index
		}
	}
	chosenExposure, err := config.ParseExposure(levels[prompt.choose(i18n.T("wizard.exposureTitle"), exposureLabels, exposureDefault)])
	if err == nil {
		cfg.Exposure = chosenExposure
	}
	cfg.Modules = config.FilterModulesByExposure(cfg.Modules, cfg.Exposure)
	if len(cfg.Modules) == 0 {
		prompt.line("")
		prompt.line("%s", prompt.style("33", i18n.T("wizard.noModules")))
		return false
	}
	prompt.line("")

	// 三、按组开关模块
	selected := make(map[string]bool, len(cfg.Modules))
	for _, id := range cfg.Modules {
		selected[id] = true
	}
	for _, group := range wizardModules() {
		// 当前档位里本来就没有的模块不问——问了也没意义。
		present := false
		for _, id := range group.Modules {
			if selected[id] {
				present = true
				break
			}
		}
		if !present {
			continue
		}
		if !prompt.confirm(i18n.T(group.QuestionKey), group.DefaultOn) {
			for _, id := range group.Modules {
				delete(selected, id)
			}
		}
	}

	// 四、隐私
	cfg.Reveal = prompt.confirm(i18n.T("wizard.askReveal"), cfg.Reveal)

	modules := make([]string, 0, len(selected))
	for _, id := range config.ModuleIDs() {
		if selected[id] {
			modules = append(modules, id)
		}
	}
	if len(modules) == 0 {
		prompt.line("")
		prompt.line("%s", prompt.style("33", i18n.T("wizard.noModules")))
		return false
	}
	cfg.Modules = modules

	// 五、确认
	prompt.line("")
	estimate := probe.EstimateFor(*cfg)
	prompt.line("%s", prompt.style("1", i18n.T("wizard.summaryTitle")))
	prompt.line("  %s %s", i18n.T("term.profileLine"), cfg.Profile)
	prompt.line("  %s %s — %s", i18n.T("report.exposure"), cfg.Exposure.String(), i18n.T("exposure."+cfg.Exposure.String()))
	prompt.line("  %s %d — %s", i18n.T("term.moduleCount"), len(modules), strings.Join(modules, ", "))
	prompt.line("  %s %s", i18n.T("term.estimate"), estimate.DurationText)
	if estimate.NetworkMiB < 0 {
		prompt.line("  %s %s", i18n.T("term.networkUsage"), i18n.T("term.uncapped"))
	}
	if cfg.Reveal {
		prompt.line("  %s", prompt.style("33", i18n.T("wizard.revealWarning")))
	}
	prompt.line("")
	if !prompt.confirm(i18n.T("wizard.askStart"), true) {
		prompt.line("%s", i18n.T("wizard.aborted"))
		return false
	}
	prompt.line("")
	return true
}
