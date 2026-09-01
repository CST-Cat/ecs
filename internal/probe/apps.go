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

func (appsProbe) ID() string { return "apps" }

func newAppsResult() model.Result {
	result := model.NewResult("apps", "module.apps.title")
	result.Description = "probe.apps.description"
	result.Methodology = model.Methodology{
		Kind:            "protocol-measurement",
		Label:           "methodology.protocol-measurement",
		Engine:          "native TCP connect",
		Profile:         "probe.apps.profile",
		ComparisonScope: "probe.apps.comparison_scope",
	}
	return result
}

func appsStableNotes() []string {
	return []string{
		"probe.apps.note.handshake_only",
		"probe.apps.note.service_scope",
		"probe.apps.note.telegram_targets",
	}
}

// appCategory is the machine identity of a target group. Display titles are
// stable presentation keys resolved at render time ("probe.apps.table."+Key),
// never recovered from a localized string here.
type appCategory struct {
	Key string
}

var (
	appCategoryTelegram       = appCategory{Key: "telegram"}
	appCategoryCodeAndImages  = appCategory{Key: "code_and_images"}
	appCategoryRepositories   = appCategory{Key: "software_repositories"}
	appCategoryInfrastructure = appCategory{Key: "infrastructure"}
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
		{Category: appCategoryTelegram, Name: "DC1 Miami", Host: "pluto.web.telegram.org", Port: 443, Note: "probe.apps.note.region_americas"},
		{Category: appCategoryTelegram, Name: "DC2 Amsterdam", Host: "venus.web.telegram.org", Port: 443, Note: "probe.apps.note.region_europe"},
		{Category: appCategoryTelegram, Name: "DC3 Miami", Host: "aurora.web.telegram.org", Port: 443, Note: "probe.apps.note.region_americas"},
		{Category: appCategoryTelegram, Name: "DC4 Amsterdam", Host: "vesta.web.telegram.org", Port: 443, Note: "probe.apps.note.region_europe"},
		{Category: appCategoryTelegram, Name: "DC5 Singapore", Host: "flora.web.telegram.org", Port: 443, Note: "probe.apps.note.region_asia"},

		{Category: appCategoryCodeAndImages, Name: "GitHub", Host: "github.com", Port: 443, Note: "probe.apps.note.git_and_releases"},
		{Category: appCategoryCodeAndImages, Name: "GitHub Raw", Host: "raw.githubusercontent.com", Port: 443, Note: "probe.apps.note.raw_files"},
		{Category: appCategoryCodeAndImages, Name: "Docker Hub", Host: "registry-1.docker.io", Port: 443, Note: "probe.apps.note.container_images"},
		{Category: appCategoryCodeAndImages, Name: "Google GCR", Host: "gcr.io", Port: 443, Note: "probe.apps.note.container_images"},

		{Category: appCategoryRepositories, Name: "npm", Host: "registry.npmjs.org", Port: 443, Note: "probe.apps.note.node_packages"},
		{Category: appCategoryRepositories, Name: "PyPI", Host: "pypi.org", Port: 443, Note: "probe.apps.note.python_packages"},
		{Category: appCategoryRepositories, Name: "Go Proxy", Host: "proxy.golang.org", Port: 443, Note: "probe.apps.note.go_modules"},
		{Category: appCategoryRepositories, Name: "Debian", Host: "deb.debian.org", Port: 443, Note: "probe.apps.note.apt_repository"},
		{Category: appCategoryRepositories, Name: "Ubuntu", Host: "archive.ubuntu.com", Port: 80, Note: "probe.apps.note.apt_repository"},
		{Category: appCategoryRepositories, Name: "Alpine", Host: "dl-cdn.alpinelinux.org", Port: 443, Note: "probe.apps.note.apk_repository"},

		{Category: appCategoryInfrastructure, Name: "Let's Encrypt", Host: "acme-v02.api.letsencrypt.org", Port: 443, Note: "probe.apps.note.certificate_issuance"},
		{Category: appCategoryInfrastructure, Name: "Cloudflare", Host: "cloudflare.com", Port: 443, Note: "probe.apps.note.cdn_and_dns"},
	}
}

// appResult 是一个端点的探测结果。
type appResult struct {
	Target    appTarget
	Reachable bool
	Latency   time.Duration
	Detail    string
}

var appProbeTargetFunc = probeAppTarget

func (appsProbe) Run(ctx context.Context, env Environment) model.Result {
	start := time.Now()
	result := newAppsResult()
	result.Methodology.Parameters = newComparisonParameters()
	addComparisonParameter(result.Methodology.Parameters, "ip_version", env.Config.IPVersion)
	addComparisonParameter(result.Methodology.Parameters, "target_set", "apps-v1")

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
			results[index] = appProbeTargetFunc(ctx, target, env.Config.IPVersion)
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
			Key:   "network.apps." + category.Key,
			Title: "probe.apps.table." + category.Key,
			Columns: []model.TableColumn{
				{Key: "service", Label: "probe.apps.column.service"},
				{Key: "endpoint", Label: "probe.apps.column.endpoint"},
				{Key: "purpose", Label: "probe.apps.column.purpose"},
				{Key: "status", Label: "probe.apps.column.status"},
				{Key: "detail", Label: "probe.apps.column.detail"},
			},
			RowIdentity: "endpoint",
		}
		items := grouped[category.Key]
		sortAppResults(items)
		for _, item := range items {
			if item.Reachable || item.Detail != "" {
				validAttempts++
			}
			status := "probe.apps.status.unreachable"
			detail := item.Detail
			if item.Reachable {
				status = "probe.apps.status.reachable"
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
			table.Rows = append(table.Rows, []model.Value{
				model.RawValue(item.Target.Name),
				model.RawValue(fmt.Sprintf("%s:%d", item.Target.Host, item.Target.Port)),
				model.KeyValue(item.Target.Note),
				model.KeyValue(status),
				model.RawValue(detail),
			})
		}
		result.Tables = append(result.Tables, table)
	}

	total := len(targets)
	result.Measurements = []model.Measurement{
		{
			Key: "apps_reachable", Label: "probe.apps.metric.apps_reachable",
			Value: float64(reachable), Unit: "count", Display: model.RawValue(fmt.Sprintf("%d/%d", reachable, total)),
			Method: "tcp-connect-v1", HigherIsBetter: model.BoolPtr(true),
		},
	}
	result.Evidence = model.NewEvidence(validAttempts, total, "target")
	if telegramBestName != "" {
		result.Fields = append(result.Fields, model.Field{
			Key: "telegram_nearest_dc", Label: "probe.apps.field.telegram_nearest_dc",
			Value: model.RawValue(fmt.Sprintf("%s · %s", telegramBestName, formatMilliseconds(telegramBest))),
		})
	}
	result.Notes = appsStableNotes()
	if reachable < total {
		result.Status = model.StatusWarning
	}
	if result.Status == model.StatusSkipped {
		result.SummaryMessages = []model.Message{model.NewMessage("probe.apps.summary.skipped")}
	} else {
		result.SummaryMessages = []model.Message{model.NewMessage("probe.apps.summary.values", fmt.Sprintf("%d/%d", reachable, total))}
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
