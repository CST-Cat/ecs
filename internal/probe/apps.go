package probe

import (
	"context"
	"fmt"
	"net"
	"sort"
	"sync"
	"time"

	"ecs/internal/model"
)

// 应用服务可达性。
//
// 对应 oneclickvirt 的 -tgdc（Telegram DC）与 -web（热门网站），但把"热门网站"
// 换成了 VPS 用户真正会被卡住的东西：拉不到代码、拉不到镜像、装不了包、签不了
// 证书，这些比"能不能打开某个门户网站"更能决定一台机器可不可用。
//
// 只做 TCP 握手，不发送任何应用层数据；判定的是网络可达性，不是服务可用性。

type appsProbe struct{}

func (appsProbe) ID() string         { return "apps" }
func (appsProbe) Title() string      { return "应用服务可达性" }
func (appsProbe) NeedsNetwork() bool { return true }

// appCategory keeps the machine identity and display title together. The
// machine key is declared at the target descriptor, never recovered from a
// localized title at render time.
type appCategory struct {
	Key   string
	Label string
}

var (
	appCategoryTelegram       = appCategory{Key: "telegram", Label: "Telegram"}
	appCategoryCodeAndImages  = appCategory{Key: "code_and_images", Label: "代码与镜像"}
	appCategoryRepositories   = appCategory{Key: "software_repositories", Label: "软件源"}
	appCategoryInfrastructure = appCategory{Key: "infrastructure", Label: "基础设施"}
)

// appTarget 是一个待测服务端点。
type appTarget struct {
	Category appCategory
	Name     string
	Host     string
	Port     int
	Note     string
}

// appTargets 是待测服务清单。
//
// Telegram 用官方 Web 客户端的 DC 域名而不是硬编码 IP：实测 pluto 与 aurora
// 会解析到同一地址，Telegram 也会不定期调整 DC 的 IP，写死必然过期。
// 其余条目全部于 2026-08-01 实测可解析、可建连。
func appTargets() []appTarget {
	return []appTarget{
		{Category: appCategoryTelegram, Name: "DC1 Miami", Host: "pluto.web.telegram.org", Port: 443, Note: "美洲"},
		{Category: appCategoryTelegram, Name: "DC2 Amsterdam", Host: "venus.web.telegram.org", Port: 443, Note: "欧洲"},
		{Category: appCategoryTelegram, Name: "DC3 Miami", Host: "aurora.web.telegram.org", Port: 443, Note: "美洲"},
		{Category: appCategoryTelegram, Name: "DC4 Amsterdam", Host: "vesta.web.telegram.org", Port: 443, Note: "欧洲"},
		{Category: appCategoryTelegram, Name: "DC5 Singapore", Host: "flora.web.telegram.org", Port: 443, Note: "亚洲"},

		{Category: appCategoryCodeAndImages, Name: "GitHub", Host: "github.com", Port: 443, Note: "git 与发布下载"},
		{Category: appCategoryCodeAndImages, Name: "GitHub Raw", Host: "raw.githubusercontent.com", Port: 443, Note: "脚本与原始文件"},
		{Category: appCategoryCodeAndImages, Name: "Docker Hub", Host: "registry-1.docker.io", Port: 443, Note: "容器镜像"},
		{Category: appCategoryCodeAndImages, Name: "Google GCR", Host: "gcr.io", Port: 443, Note: "容器镜像"},

		{Category: appCategoryRepositories, Name: "npm", Host: "registry.npmjs.org", Port: 443, Note: "Node 包"},
		{Category: appCategoryRepositories, Name: "PyPI", Host: "pypi.org", Port: 443, Note: "Python 包"},
		{Category: appCategoryRepositories, Name: "Go Proxy", Host: "proxy.golang.org", Port: 443, Note: "Go 模块"},
		{Category: appCategoryRepositories, Name: "Debian", Host: "deb.debian.org", Port: 443, Note: "APT 源"},
		{Category: appCategoryRepositories, Name: "Ubuntu", Host: "archive.ubuntu.com", Port: 80, Note: "APT 源"},
		{Category: appCategoryRepositories, Name: "Alpine", Host: "dl-cdn.alpinelinux.org", Port: 443, Note: "APK 源"},

		{Category: appCategoryInfrastructure, Name: "Let's Encrypt", Host: "acme-v02.api.letsencrypt.org", Port: 443, Note: "免费证书签发"},
		{Category: appCategoryInfrastructure, Name: "Cloudflare", Host: "cloudflare.com", Port: 443, Note: "CDN 与 DNS"},
	}
}

// appResult 是一个端点的探测结果。
type appResult struct {
	Target    appTarget
	Reachable bool
	Latency   time.Duration
	Detail    string
}

func (appsProbe) Run(ctx context.Context, env Environment) model.Result {
	start := time.Now()
	result := model.NewResult("apps", "应用服务可达性")
	result.Description = "Telegram 各数据中心与代码、镜像、软件源、证书服务的 TCP 可达性"
	result.Methodology = model.Methodology{
		Kind:            "protocol-measurement",
		Label:           "协议测量",
		Engine:          "native TCP connect",
		Profile:         fmt.Sprintf("%d 个固定端点", len(appTargets())),
		ComparisonScope: "网络可达性诊断；不代表服务本身可用，也不是性能基准",
	}

	targets := appTargets()
	results := make([]appResult, len(targets))
	semaphore := make(chan struct{}, 8)
	var wg sync.WaitGroup
	for index, target := range targets {
		wg.Add(1)
		go func(index int, target appTarget) {
			defer wg.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()
			results[index] = probeAppTarget(ctx, target, env.Config.IPVersion)
		}(index, target)
	}
	wg.Wait()

	// 按声明顺序分组，组内保持清单顺序。
	categories := make([]appCategory, 0, 4)
	grouped := make(map[string][]appResult)
	for _, item := range results {
		category := item.Target.Category
		if _, seen := grouped[category.Key]; !seen {
			categories = append(categories, category)
		}
		grouped[category.Key] = append(grouped[category.Key], item)
	}

	reachable := 0
	validAttempts := 0
	var telegramBest time.Duration
	var telegramBestName string
	for _, category := range categories {
		table := model.Table{
			Key:         "network.apps." + category.Key,
			Title:       category.Label,
			Columns:     []string{"服务", "端点", "用途", "结果", "延迟/原因"},
			ColumnKeys:  []string{"service", "endpoint", "purpose", "status", "detail"},
			RowIdentity: "endpoint",
		}
		items := grouped[category.Key]
		sortAppResults(items)
		for _, item := range items {
			if item.Reachable || item.Detail != "" {
				validAttempts++
			}
			status := "unreachable"
			detail := item.Detail
			if item.Reachable {
				status = "reachable"
				detail = formatMilliseconds(item.Latency)
				reachable++
				if category.Key == appCategoryTelegram.Key && (telegramBest == 0 || item.Latency < telegramBest) {
					telegramBest = item.Latency
					telegramBestName = item.Target.Name
				}
			}
			if !item.Reachable && item.Detail != "" {
				addFailureMessage(&result, "connect", net.JoinHostPort(item.Target.Host, fmt.Sprint(item.Target.Port)), item.Detail)
			}
			table.Rows = append(table.Rows, []string{
				item.Target.Name,
				fmt.Sprintf("%s:%d", item.Target.Host, item.Target.Port),
				item.Target.Note,
				status,
				detail,
			})
		}
		result.Tables = append(result.Tables, table)
	}

	total := len(targets)
	result.Measurements = []model.Measurement{
		{
			Key: "apps_reachable", Label: "可达服务",
			Value: float64(reachable), Unit: "项", Display: fmt.Sprintf("%d/%d", reachable, total),
			Method: "tcp-connect-v1", HigherIsBetter: model.BoolPtr(true),
		},
	}
	result.Evidence = model.NewEvidence(validAttempts, total, "target")
	if telegramBestName != "" {
		result.Fields = append(result.Fields, model.Field{
			Key: "telegram_nearest_dc", Label: "最快 Telegram DC",
			Value: fmt.Sprintf("%s · %s", telegramBestName, formatMilliseconds(telegramBest)),
		})
	}
	result.Notes = append(result.Notes,
		"只完成 TCP 握手，不发送任何应用层数据，也不做鉴权或下载。",
		"Telegram 用官方 Web 客户端的 DC 域名而非硬编码 IP：实测多个 DC 会解析到同一地址，"+
			"且 Telegram 会不定期调整 DC 地址，写死 IP 必然过期。最快的 DC 通常就是客户端会选用的那个。",
		"可达不等于可用：CDN 会让握手在最近的边缘节点完成，实际拉取仍可能被限速或被上游拒绝。",
	)
	if reachable < total {
		result.Status = model.StatusWarning
	}
	result.Summary = fmt.Sprintf("%d/%d 可达", reachable, total)
	if telegramBestName != "" {
		result.Summary += " · Telegram 最近 " + telegramBestName
	}
	result.Finish(start)
	return result
}

// probeAppTarget 对单个端点做一次 TCP 握手。
func probeAppTarget(ctx context.Context, target appTarget, ipVersion string) appResult {
	item := appResult{Target: target}
	address := net.JoinHostPort(target.Host, fmt.Sprint(target.Port))
	dialer := net.Dialer{Timeout: 6 * time.Second}
	begin := time.Now()
	connection, err := dialer.DialContext(ctx, tcpNetworkForMode(ipVersion), address)
	item.Latency = time.Since(begin)
	if err != nil {
		item.Detail = compactError(err)
		return item
	}
	_ = connection.Close()
	item.Reachable = true
	return item
}

// sortAppResults 让不可达的排在组内最前，其余按延迟升序。
func sortAppResults(items []appResult) {
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Reachable != items[j].Reachable {
			return !items[i].Reachable
		}
		return items[i].Latency < items[j].Latency
	})
}
