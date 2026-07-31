package probe

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"ecs/internal/model"
)

type mediaProbe struct{}

func (mediaProbe) ID() string         { return "media" }
func (mediaProbe) Title() string      { return "流媒体与 AI 服务" }
func (mediaProbe) NeedsNetwork() bool { return true }

type mediaTarget struct {
	Name string
	URL  string
}

type mediaResult struct {
	Target   mediaTarget
	Verdict  string
	HTTPCode int
	Region   string
	Latency  time.Duration
	Evidence string
}

var regionPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)"countryCode"\s*:\s*"([A-Z]{2})"`),
	regexp.MustCompile(`(?i)"country_code"\s*:\s*"([A-Z]{2})"`),
	regexp.MustCompile(`(?i)[?&]gl=([A-Z]{2})`),
}

func (mediaProbe) Run(ctx context.Context, env Environment) model.Result {
	start := time.Now()
	result := model.NewResult("media", "流媒体与 AI 服务")
	result.Description = "公开网页的区域可达性与页面信号，结论不依赖登录账号"
	result.Methodology = model.Methodology{
		Kind:            "heuristic",
		Label:           "启发式判断",
		Engine:          "public HTTP evidence",
		Profile:         "versioned page rules v1",
		ComparisonScope: "仅作当次公开页证据；不等同于账号播放、注册或支付能力",
	}

	targets := []mediaTarget{
		{Name: "Netflix", URL: "https://www.netflix.com/title/80018499"},
		{Name: "YouTube Premium", URL: "https://www.youtube.com/premium"},
		{Name: "Disney+", URL: "https://www.disneyplus.com/"},
		{Name: "TikTok", URL: "https://www.tiktok.com/"},
		{Name: "ChatGPT", URL: "https://chatgpt.com/"},
		{Name: "Claude", URL: "https://claude.ai/"},
		{Name: "Gemini", URL: "https://gemini.google.com/"},
	}
	results := make(chan mediaResult, len(targets))
	var wg sync.WaitGroup
	for _, target := range targets {
		wg.Add(1)
		go func(target mediaTarget) {
			defer wg.Done()
			results <- checkMedia(ctx, env, target)
		}(target)
	}
	wg.Wait()
	close(results)
	collected := make([]mediaResult, 0, len(targets))
	for item := range results {
		collected = append(collected, item)
	}
	order := make(map[string]int, len(targets))
	for index, target := range targets {
		order[target.URL] = index
	}
	sort.SliceStable(collected, func(i, j int) bool {
		return order[collected[i].Target.URL] < order[collected[j].Target.URL]
	})

	table := model.Table{
		Title:   "可达性",
		Columns: []string{"平台", "结论", "HTTP", "地区信号", "耗时", "证据"},
	}
	available, unknown := 0, 0
	for _, item := range collected {
		if item.Verdict == "可达" || item.Verdict == "需登录" {
			available++
		}
		if item.Verdict == "未知" {
			unknown++
		}
		code := "n/a"
		if item.HTTPCode > 0 {
			code = fmt.Sprintf("%d", item.HTTPCode)
		}
		table.Rows = append(table.Rows, []string{
			item.Target.Name,
			item.Verdict,
			code,
			fallback(item.Region, "—"),
			formatMilliseconds(item.Latency),
			item.Evidence,
		})
	}
	result.Tables = []model.Table{table}
	if unknown == len(targets) {
		result.Status = model.StatusWarning
	}
	result.Measurements = []model.Measurement{
		{Key: "reachable_services", Label: "公开页可达", Value: float64(available), Unit: "项", Display: fmt.Sprintf("%d/%d", available, len(targets)), Method: "http-evidence-v1", HigherIsBetter: model.BoolPtr(true)},
	}
	result.Notes = append(result.Notes,
		"“可达”只表示公开页面响应正常，不代表账号订阅、播放版权、注册或支付一定可用。",
		"HTTP 403 可能是地区限制，也可能是反自动化；缺少明确证据时统一返回“未知”，避免把反爬误报成不解锁。",
		"平台页面与规则会变化；报告保留 HTTP 状态和地区信号，便于复核。",
	)
	result.Summary = fmt.Sprintf("%d/%d 公开页可达 · %d 项未知", available, len(targets), unknown)
	result.Finish(start)
	return result
}

func checkMedia(ctx context.Context, env Environment, target mediaTarget) mediaResult {
	item := mediaResult{Target: target, Verdict: "未知", Evidence: "request failed"}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target.URL, nil)
	if err != nil {
		item.Evidence = err.Error()
		return item
	}
	request.Header.Set("User-Agent", env.UserAgent)
	request.Header.Set("Accept-Language", "en-US,en;q=0.8")
	request.Header.Set("Range", "bytes=0-1048575")
	begin := time.Now()
	response, err := env.HTTPClient.Do(request)
	item.Latency = time.Since(begin)
	if err != nil {
		item.Evidence = compactError(err)
		return item
	}
	defer response.Body.Close()
	item.HTTPCode = response.StatusCode
	body, _ := io.ReadAll(io.LimitReader(response.Body, 1024*1024))
	text := string(body)
	item.Region = detectRegion(text, response.Request.URL.String())

	switch {
	case response.StatusCode >= 200 && response.StatusCode < 300:
		item.Verdict = "可达"
		item.Evidence = "2xx public page"
	case response.StatusCode == http.StatusUnauthorized:
		item.Verdict = "需登录"
		item.Evidence = "401 account gate"
	case response.StatusCode == http.StatusForbidden:
		item.Verdict = "未知"
		item.Evidence = "403 geo/anti-bot"
	case response.StatusCode == http.StatusUnavailableForLegalReasons:
		item.Verdict = "受限"
		item.Evidence = "451 legal restriction"
	case response.StatusCode >= 300 && response.StatusCode < 400:
		item.Verdict = "未知"
		item.Evidence = "redirect limit"
	case response.StatusCode >= 500:
		item.Verdict = "未知"
		item.Evidence = "service error"
	default:
		item.Verdict = "不可达"
		item.Evidence = fmt.Sprintf("HTTP %d", response.StatusCode)
	}
	return item
}

func detectRegion(text, finalURL string) string {
	for _, pattern := range regionPatterns {
		if match := pattern.FindStringSubmatch(text); len(match) == 2 {
			return strings.ToUpper(match[1])
		}
		if match := pattern.FindStringSubmatch(finalURL); len(match) == 2 {
			return strings.ToUpper(match[1])
		}
	}
	return ""
}
