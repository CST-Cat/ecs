package probe

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"ecs/internal/model"
)

type mediaProbe struct{}

func (mediaProbe) ID() string         { return "media" }
func (mediaProbe) Title() string      { return "module.media.title" }
func (mediaProbe) NeedsNetwork() bool { return true }

// mediaResult 是一个平台的完整检测记录。
type mediaResult struct {
	Check   mediaCheck
	Verdict mediaVerdict
	Latency time.Duration
	// Statuses 保留每次请求的状态码，便于复核判定依据。
	Statuses []int
	// ResponseErrors preserves each typed request error for final assembly.
	ResponseErrors []mediaResponseError
}

type mediaResponseError struct {
	Index int
	Err   error
}

func (mediaProbe) Run(ctx context.Context, env Environment) model.Result {
	start := time.Now()
	result := model.NewResult("media", "module.media.title")
	result.Description = "probe.media.description"
	result.Methodology = model.Methodology{
		Kind:            "heuristic",
		Label:           "methodology.heuristic",
		Engine:          "probe.media.methodology.engine",
		Profile:         "probe.media.profile",
		ComparisonScope: "probe.media.comparison_scope",
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

	unlocked, locked, unknown := mediaVerdictCounts(results)
	for _, category := range categories {
		table := model.Table{
			Key:   "network.media." + category.Key,
			Title: "probe.media.table." + category.Key,
			Columns: []string{
				"probe.media.column.platform", "probe.media.column.verdict", "probe.media.column.region",
				"probe.media.column.evidence", "probe.media.column.strength", "probe.media.column.http_status",
				"probe.media.column.duration",
			},
			ColumnKeys:  []string{"platform", "verdict", "region", "evidence", "strength", "http_status", "latency_ms"},
			RowIdentity: "platform",
		}
		for _, item := range grouped[category.Key] {
			for _, responseError := range item.ResponseErrors {
				target := fmt.Sprintf("%s/request_%d", item.Check.ID, responseError.Index+1)
				addFailure(&result, "platform_check", target, responseError.Err)
			}
			table.Rows = append(table.Rows, []string{
				mediaPlatformNameKey(item.Check.ID),
				item.Verdict.State,
				item.Verdict.Region,
				item.Verdict.Evidence,
				string(item.Check.Strength),
				mediaStatusDisplay(item.Statuses),
				formatMilliseconds(item.Latency),
			})
		}
		result.Tables = append(result.Tables, table)
	}

	total := len(checks)
	if total > 0 && unknown == total {
		result.Status = model.StatusWarning
	}
	result.Measurements = []model.Measurement{
		{
			Key: "media_unlocked", Label: "probe.media.metric.unlocked",
			Value: float64(unlocked), Unit: "count",
			Display: fmt.Sprintf("%d/%d", unlocked, total),
			Method:  "media-rules-" + mediaRulesVersion, HigherIsBetter: model.BoolPtr(true),
		},
		{
			Key: "media_unknown", Label: "probe.media.metric.unknown",
			Value: float64(unknown), Unit: "count",
			Display: fmt.Sprintf("%d/%d", unknown, total),
			Method:  "media-rules-" + mediaRulesVersion, HigherIsBetter: model.BoolPtr(false),
		},
	}
	result.Evidence = model.NewEvidence(total-unknown, total, "target")
	result.Summary = ""
	result.SummaryMessages = []model.Message{
		model.NewMessage("probe.media.summary.values", unlocked, total, locked, unknown),
	}
	result.Notes = []string{
		"probe.media.note.public_evidence",
		"probe.media.note.account_scope",
		"probe.media.note.unknown_semantics",
	}
	result.Finish(start)
	return result
}

// runMediaCheck 执行一条规则的全部请求并给出结论。
func runMediaCheck(ctx context.Context, env Environment, check mediaCheck) mediaResult {
	item := mediaResult{Check: check}
	begin := time.Now()
	responses := make([]mediaResponse, 0, len(check.Requests))
	for index, request := range check.Requests {
		response := performMediaRequest(ctx, env, request)
		responses = append(responses, response)
		item.Statuses = append(item.Statuses, response.Status)
		if response.Err != nil {
			item.ResponseErrors = append(item.ResponseErrors, mediaResponseError{Index: index, Err: response.Err})
		}
	}
	item.Latency = time.Since(begin)
	if len(responses) == 0 {
		item.Verdict = mediaVerdict{State: stateUnknown, Evidence: mediaEvidenceMissingRequests}
		return item
	}
	item.Verdict = check.Decide(responses)
	if item.Verdict.State == "" {
		item.Verdict.State = stateUnknown
	}
	return item
}

func mediaVerdictCounts(results []mediaResult) (unlocked, locked, unknown int) {
	for _, item := range results {
		switch item.Verdict.State {
		case stateUnlocked, stateOriginals:
			unlocked++
		case stateLocked, stateRestricted, stateUnreachable:
			locked++
		case stateUnknown, stateNeedLogin:
			unknown++
		default:
			unknown++
		}
	}
	return unlocked, locked, unknown
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

func mediaStatusDisplay(statuses []int) string {
	if len(statuses) == 0 {
		return ""
	}
	values := make([]string, len(statuses))
	for index, status := range statuses {
		values[index] = strconv.Itoa(status)
	}
	return strings.Join(values, ",")
}

func mediaPlatformNameKey(id string) string {
	return "probe.media.platform." + id
}
