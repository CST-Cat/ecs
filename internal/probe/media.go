package probe

import (
	"context"
	"fmt"
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

// mediaResult 是一个平台的完整检测记录。
type mediaResult struct {
	Check   mediaCheck
	Verdict mediaVerdict
	Latency time.Duration
	// Statuses 保留每次请求的状态码，便于复核判定依据。
	Statuses []int
}

func (mediaProbe) Run(ctx context.Context, env Environment) model.Result {
	start := time.Now()
	result := model.NewResult("media", "流媒体与 AI 服务")
	result.Description = "按平台规则检测区域可用性，结论区分解锁、仅自制剧、不解锁与未知"
	result.Methodology = model.Methodology{
		Kind:            "heuristic",
		Label:           "启发式判断",
		Engine:          "public HTTP evidence + per-platform rules",
		Profile:         "media rules " + mediaRulesVersion + describeMediaRegions(env.Config.MediaRegions),
		ComparisonScope: "仅当次公开页证据；不等同于账号播放、注册或支付能力",
	}
	client, closeClient := httpClientForMode(env)
	defer closeClient()
	env.HTTPClient = client

	checks := mediaChecksForRegions(env.Config.MediaRegions)
	// 并发上限避免一次运行对同一批 CDN 制造请求突发。
	const concurrency = 8
	semaphore := make(chan struct{}, concurrency)
	results := make([]mediaResult, len(checks))
	var wg sync.WaitGroup
	for index, check := range checks {
		wg.Add(1)
		go func(index int, check mediaCheck) {
			defer wg.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()
			results[index] = runMediaCheck(ctx, env, check)
		}(index, check)
	}
	wg.Wait()

	// 按分类分组呈现，组内保持规则声明顺序。
	categories := make([]mediaCategory, 0, 8)
	grouped := make(map[string][]mediaResult)
	for _, item := range results {
		category := item.Check.Category
		if _, seen := grouped[category.Key]; !seen {
			categories = append(categories, category)
		}
		grouped[category.Key] = append(grouped[category.Key], item)
	}
	sort.SliceStable(categories, func(i, j int) bool {
		return categoryOrder(categories[i]) < categoryOrder(categories[j])
	})

	unlocked, unknown, locked := 0, 0, 0
	for _, category := range categories {
		table := model.Table{
			Key:         "network.media." + category.Key,
			Title:       category.Label,
			Columns:     []string{"平台", "结论", "地区", "证据", "强度", "耗时"},
			ColumnKeys:  []string{"platform", "verdict", "region", "evidence", "strength", "latency_ms"},
			RowIdentity: "platform",
		}
		for _, item := range grouped[category.Key] {
			switch item.Verdict.State {
			case stateUnlocked, stateOriginals:
				unlocked++
			case stateUnknown:
				unknown++
				addFailureMessage(&result, "platform_check", item.Check.Name, item.Verdict.Evidence)
			case stateLocked, stateRestricted, stateUnreachable:
				locked++
			}
			table.Rows = append(table.Rows, []string{
				item.Check.Name,
				item.Verdict.State,
				fallback(item.Verdict.Region, "—"),
				item.Verdict.Evidence,
				strengthLabel(item.Check.Strength),
				formatMilliseconds(item.Latency),
			})
		}
		result.Tables = append(result.Tables, table)
	}

	total := len(checks)
	if unknown == total {
		result.Status = model.StatusWarning
	}
	result.Measurements = []model.Measurement{
		{
			Key: "media_unlocked", Label: "可用平台",
			Value: float64(unlocked), Unit: "项",
			Display: fmt.Sprintf("%d/%d", unlocked, total),
			Method:  "media-rules-" + mediaRulesVersion, HigherIsBetter: model.BoolPtr(true),
		},
		{
			Key: "media_unknown", Label: "无法判定",
			Value: float64(unknown), Unit: "项",
			Display: fmt.Sprintf("%d/%d", unknown, total),
			Method:  "media-rules-" + mediaRulesVersion, HigherIsBetter: model.BoolPtr(false),
		},
	}
	result.Evidence = model.NewEvidence(total-unknown, total, "target")
	result.Summary = fmt.Sprintf("%d/%d 可用 · %d 不可用 · %d 未知", unlocked, total, locked, unknown)
	result.Finish(start)
	return result
}

// runMediaCheck 执行一条规则的全部请求并给出结论。
func runMediaCheck(ctx context.Context, env Environment, check mediaCheck) mediaResult {
	item := mediaResult{Check: check}
	begin := time.Now()
	responses := make([]mediaResponse, 0, len(check.Requests))
	for _, request := range check.Requests {
		response := performMediaRequest(ctx, env, request)
		responses = append(responses, response)
		item.Statuses = append(item.Statuses, response.Status)
	}
	item.Latency = time.Since(begin)
	if len(responses) == 0 {
		item.Verdict = mediaVerdict{State: stateUnknown, Evidence: "规则未声明请求"}
		return item
	}
	item.Verdict = check.Decide(responses)
	if item.Verdict.State == "" {
		item.Verdict.State = stateUnknown
	}
	return item
}

// categoryOrder 固定分类展示顺序。
func categoryOrder(category mediaCategory) int {
	switch category.Key {
	case mediaCategoryStreaming.Key:
		return 0
	case mediaCategoryAIServices.Key:
		return 1
	case mediaCategorySocial.Key:
		return 2
	case mediaCategoryMusic.Key:
		return 3
	case mediaCategoryJapan.Key:
		return 4
	case mediaCategoryTaiwan.Key:
		return 5
	case mediaCategoryHongKong.Key:
		return 6
	case mediaCategoryMainlandChina.Key:
		return 7
	default:
		return 8
	}
}

// strengthLabel 把证据强度翻译成表格用语。
func strengthLabel(strength mediaEvidenceStrength) string {
	if strength == strengthStrong {
		return "强"
	}
	return "弱"
}

// describeMediaRegions 在方法学里注明本次跑了哪些地区，避免不同筛选的结果被混比。
func describeMediaRegions(regions []string) string {
	if len(regions) == 0 {
		return " (all regions)"
	}
	return " (regions: " + strings.Join(regions, ",") + ")"
}
