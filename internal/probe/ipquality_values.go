package probe

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	"ecs/internal/model"
)

func findingValue(finding qualityFinding, value string) string {
	switch {
	case !finding.Enabled:
		return "probe.network.status.disabled"
	case finding.Err != nil:
		return "probe.network.status.failed"
	case strings.TrimSpace(value) == "":
		return networkMissingValue
	default:
		return value
	}
}

func findingNormalizedValue(finding qualityFinding, value string) model.Value {
	if !finding.Enabled {
		return model.KeyValue("probe.network.status.disabled")
	}
	if finding.Err != nil {
		return model.KeyValue("probe.network.status.failed")
	}
	if strings.TrimSpace(value) == "" {
		return model.KeyValue(networkMissingValue)
	}
	return model.KeyValue(value)
}

func findingRawValue(finding qualityFinding, value string) model.Value {
	if !finding.Enabled {
		return model.KeyValue("probe.network.status.disabled")
	}
	if finding.Err != nil {
		return model.KeyValue("probe.network.status.failed")
	}
	if strings.TrimSpace(value) == "" {
		return model.KeyValue(networkMissingValue)
	}
	return model.RawValue(value)
}

func findingAccess(finding qualityFinding) string {
	switch {
	case !finding.Enabled:
		return "probe.network.status.disabled"
	case finding.Access != "":
		return finding.Access
	default:
		return networkMissingValue
	}
}

func findingAccessValue(finding qualityFinding) model.Value {
	return networkAccessValue(findingAccess(finding))
}

func networkAccessValue(value string) model.Value {
	switch value {
	case networkMissingValue, "probe.network.status.disabled",
		networkChannelDirect, networkChannelAPIKey, networkChannelPublicDemo, networkChannelTryout,
		networkChannelCommunity, networkChannelOfficialFree, networkChannelPublicPage,
		networkChannelJina, networkChannelJinaProxy, networkChannelMixedFallback,
		networkChannelFreeFallback, networkChannelExtendedAPIKey:
		return model.KeyValue(value)
	default:
		return model.RawValue(value)
	}
}

func findingStatus(finding qualityFinding) string {
	return networkStatusKey(finding.Enabled, finding.Err != nil, finding.Partial != "")
}

func originStatus(origin originAssessment) string {
	return networkStatusKey(origin.Enabled, origin.Err != nil, false)
}

func originAccess(origin originAssessment) string {
	if !origin.Enabled {
		return "probe.network.status.disabled"
	}
	return firstNonEmpty(origin.Access, networkMissingValue)
}

func factorCountry(finding qualityFinding) string {
	if !finding.Enabled || finding.Err != nil || finding.Country == "" {
		if !finding.Enabled {
			return "probe.network.status.disabled"
		}
		if finding.Err != nil {
			return "probe.network.status.failed"
		}
		return networkMissingValue
	}
	return finding.Country
}

func factorCountryValue(finding qualityFinding, value string) model.Value {
	if !finding.Enabled || finding.Err != nil || finding.Country == "" {
		return model.KeyValue(value)
	}
	return model.RawValue(value)
}

func factorSignal(finding qualityFinding, signal qualitySignal) string {
	if !finding.Enabled {
		return "probe.network.status.disabled"
	}
	if finding.Err != nil {
		return "probe.network.status.failed"
	}
	if !signal.Known {
		return networkMissingValue
	}
	return networkSignalKey(signal)
}

func factorSignalValue(value string) model.Value {
	return model.KeyValue(value)
}

func scoreText(finding qualityFinding) string {
	switch {
	case !finding.Enabled:
		return "probe.network.status.disabled"
	case finding.Err != nil:
		return "probe.network.status.failed"
	case finding.Score == nil:
		return networkMissingValue
	case finding.ID == "dbip":
		return formatScore(*finding.Score) + "*/100"
	default:
		return formatScore(*finding.Score) + "/100"
	}
}

func scoreTextValue(finding qualityFinding, value string) model.Value {
	if !finding.Enabled || finding.Err != nil || finding.Score == nil {
		return model.KeyValue(value)
	}
	return model.RawValue(value)
}

func networkRiskValue(value string) model.Value {
	if value == "" {
		return model.KeyValue(networkMissingValue)
	}
	return model.KeyValue(value)
}

func scoreBarValue(finding qualityFinding, value string) model.Value {
	if finding.Score == nil {
		return model.KeyValue(value)
	}
	return model.RawValue(value)
}

func networkScoreKindValue(value string) model.Value {
	if value == "" {
		return model.KeyValue(networkMissingValue)
	}
	return model.KeyValue(value)
}

func scoreBar(score float64) string {
	const width = 12
	score = math.Max(0, math.Min(100, score))
	filled := int(math.Round(score / 100 * width))
	return strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
}

func formatScore(value float64) string {
	if math.Abs(value-math.Round(value)) < 0.005 {
		return strconv.FormatInt(int64(math.Round(value)), 10)
	}
	return strconv.FormatFloat(value, 'f', 2, 64)
}

func durationText(value time.Duration) string {
	if value <= 0 {
		return networkMissingValue
	}
	return fmt.Sprintf("%.0f ms", float64(value)/float64(time.Millisecond))
}

func durationValue(value time.Duration) model.Value {
	text := durationText(value)
	if text == networkMissingValue {
		return model.KeyValue(text)
	}
	return model.RawValue(text)
}

func normalizeNetworkType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "isp", "line isp", "fixed line isp", "consumer":
		return "probe.network.network_type.residential"
	case "hosting", "data center", "datacenter", "data center/web hosting/transit":
		return "probe.network.network_type.datacenter"
	case "business", "commercial":
		return "probe.network.network_type.business"
	case "education", "university/college/school":
		return "probe.network.network_type.education"
	case "government":
		return "probe.network.network_type.government"
	case "banking":
		return "probe.network.network_type.banking"
	case "organization":
		return "probe.network.network_type.organization"
	case "military":
		return "probe.network.network_type.military"
	case "library":
		return "probe.network.network_type.library"
	case "content delivery network", "cdn":
		return "probe.network.network_type.cdn"
	case "mobile", "mobile isp":
		return "probe.network.network_type.mobile"
	case "search engine spider", "spider":
		return "probe.network.network_type.search_engine"
	case "reserved":
		return "probe.network.network_type.reserved"
	case "", "unknown", "null":
		return ""
	default:
		return "probe.network.network_type.other"
	}
}

func normalizeIP2LocationType(value string) string {
	code := strings.ToUpper(strings.TrimSpace(strings.Split(value, "/")[0]))
	switch code {
	case "COM":
		return "probe.network.network_type.business"
	case "DCH":
		return "probe.network.network_type.datacenter"
	case "EDU":
		return "probe.network.network_type.education"
	case "GOV":
		return "probe.network.network_type.government"
	case "ORG":
		return "probe.network.network_type.organization"
	case "MIL":
		return "probe.network.network_type.military"
	case "LIB":
		return "probe.network.network_type.library"
	case "CDN":
		return "probe.network.network_type.cdn"
	case "ISP":
		return "probe.network.network_type.residential"
	case "MOB":
		return "probe.network.network_type.mobile"
	case "SES":
		return "probe.network.network_type.search_engine"
	case "RSV":
		return "probe.network.network_type.reserved"
	case "":
		return ""
	default:
		return normalizeNetworkType(value)
	}
}

func normalizeAbuseIPDBType(value string) string {
	return normalizeNetworkType(value)
}

func knownSignal(value bool) qualitySignal {
	return qualitySignal{Known: true, Value: value}
}

func pointerSignal(value *bool) qualitySignal {
	if value == nil {
		return qualitySignal{}
	}
	return knownSignal(*value)
}

func anyPointerSignal(values ...*bool) qualitySignal {
	known := false
	for _, value := range values {
		if value == nil {
			continue
		}
		known = true
		if *value {
			return knownSignal(true)
		}
	}
	if known {
		return knownSignal(false)
	}
	return qualitySignal{}
}

func firstKnownSignal(values ...qualitySignal) qualitySignal {
	for _, value := range values {
		if value.Known {
			return value
		}
	}
	return qualitySignal{}
}

func anyKnownSignal(values ...qualitySignal) bool {
	for _, value := range values {
		if value.Known {
			return true
		}
	}
	return false
}

func findingHasEvidence(finding qualityFinding) bool {
	return finding.Country != "" ||
		finding.Usage != "" ||
		finding.Company != "" ||
		finding.Score != nil ||
		anyKnownSignal(
			finding.Proxy,
			finding.Tor,
			finding.VPN,
			finding.Server,
			finding.Abuser,
			finding.Robot,
		)
}

func knownWhenNonEmpty(value bool, evidence string) qualitySignal {
	if strings.TrimSpace(evidence) == "" {
		return qualitySignal{}
	}
	return knownSignal(value)
}

func threatDetailSignal(details []string) qualitySignal {
	if details == nil {
		return qualitySignal{}
	}
	for _, detail := range details {
		detail = strings.ToLower(detail)
		if detail == "attack-source" || detail == "bot" || strings.HasPrefix(detail, "bot-") {
			return knownSignal(true)
		}
	}
	return knownSignal(false)
}

func yesNoSignal(value string) qualitySignal {
	switch strings.ToLower(strings.TrimSpace(strings.Trim(value, "*_`"))) {
	case "yes", "true", "detected":
		return knownSignal(true)
	case "no", "false", "not detected":
		return knownSignal(false)
	default:
		return qualitySignal{}
	}
}

func signalFromPageText(value, positivePattern, negativePattern string) qualitySignal {
	if pattern, err := regexp.Compile(negativePattern); err == nil && pattern.MatchString(value) {
		return knownSignal(false)
	}
	if pattern, err := regexp.Compile(positivePattern); err == nil && pattern.MatchString(value) {
		return knownSignal(true)
	}
	return qualitySignal{}
}

func providerPageText(value string) string {
	value = htmlTagPattern.ReplaceAllString(value, " ")
	value = strings.NewReplacer("**", "", "__", "", "`", "").Replace(value)
	return strings.TrimSpace(whitespacePattern.ReplaceAllString(value, " "))
}

func markdownTableValues(page, requiredHeader string) map[string]string {
	lines := strings.Split(page, "\n")
	requiredHeader = strings.ToLower(strings.TrimSpace(requiredHeader))
	for index, line := range lines {
		headers := markdownCells(line)
		if len(headers) == 0 || !containsFold(headers, requiredHeader) {
			continue
		}
		for next := index + 1; next < len(lines); next++ {
			values := markdownCells(lines[next])
			if len(values) == 0 || markdownSeparator(values) {
				continue
			}
			if len(values) != len(headers) {
				break
			}
			result := make(map[string]string, len(headers))
			for cell := range headers {
				result[strings.ToLower(headers[cell])] = values[cell]
			}
			return result
		}
	}
	return nil
}

func markdownCells(line string) []string {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "|") || !strings.HasSuffix(line, "|") {
		return nil
	}
	line = strings.TrimPrefix(strings.TrimSuffix(line, "|"), "|")
	raw := strings.Split(line, "|")
	cells := make([]string, 0, len(raw))
	for _, value := range raw {
		cells = append(cells, strings.TrimSpace(strings.Trim(value, "*_`")))
	}
	return cells
}

func markdownSeparator(cells []string) bool {
	if len(cells) == 0 {
		return false
	}
	for _, value := range cells {
		if strings.Trim(value, " :-") != "" {
			return false
		}
	}
	return true
}

func containsFold(values []string, target string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), target) {
			return true
		}
	}
	return false
}

func appendPartial(existing, addition string) string {
	existing = strings.TrimSpace(existing)
	addition = strings.TrimSpace(addition)
	switch {
	case existing == "":
		return addition
	case addition == "":
		return existing
	case existing == addition:
		return existing
	default:
		return networkPartialMultiple
	}
}

func parseProbabilityScore(value string) *float64 {
	fields := strings.Fields(strings.TrimSpace(value))
	if len(fields) == 0 {
		return nil
	}
	number, err := strconv.ParseFloat(strings.TrimSuffix(fields[0], "%"), 64)
	if err != nil {
		return nil
	}
	if strings.Contains(fields[0], "%") {
		return validScore(&number)
	}
	number *= 100
	return validScore(&number)
}

func scoreLabel(value string) string {
	start := strings.Index(value, "(")
	end := strings.LastIndex(value, ")")
	if start < 0 || end <= start {
		return ""
	}
	return strings.TrimSpace(value[start+1 : end])
}

func translateRiskLabel(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "very low":
		return "probe.network.risk.very_low"
	case "low":
		return "probe.network.risk.low"
	case "elevated", "medium":
		return "probe.network.risk.medium"
	case "suspicious":
		return "probe.network.risk.suspicious"
	case "high":
		return "probe.network.risk.high"
	case "very high":
		return "probe.network.risk.very_high"
	case "":
		return ""
	default:
		return networkRiskUnknown
	}
}

func validScore(value *float64) *float64 {
	if value == nil || math.IsNaN(*value) || math.IsInf(*value, 0) || *value < 0 || *value > 100 {
		return nil
	}
	copy := *value
	return &copy
}

func riskIP2Location(score *float64) string {
	if score == nil {
		return ""
	}
	switch {
	case *score < 33:
		return "probe.network.risk.low"
	case *score < 66:
		return "probe.network.risk.medium"
	default:
		return "probe.network.risk.high"
	}
}

func riskScamalytics(score *float64) string {
	if score == nil {
		return ""
	}
	switch {
	case *score < 20:
		return "probe.network.risk.low"
	case *score < 60:
		return "probe.network.risk.medium"
	case *score < 90:
		return "probe.network.risk.high"
	default:
		return "probe.network.risk.very_high"
	}
}

func riskAbuseIPDB(score *float64) string {
	if score == nil {
		return ""
	}
	switch {
	case *score < 25:
		return "probe.network.risk.low"
	case *score < 75:
		return "probe.network.risk.suspicious"
	default:
		return "probe.network.risk.high"
	}
}

func riskIPQS(score *float64) string {
	if score == nil {
		return ""
	}
	switch {
	case *score < 75:
		return "probe.network.risk.low"
	case *score < 85:
		return "probe.network.risk.suspicious"
	case *score < 90:
		return "probe.network.risk.high"
	default:
		return "probe.network.risk.very_high"
	}
}

func setDBIPRisk(finding *qualityFinding, level string) {
	level = strings.ToLower(strings.TrimSpace(level))
	var score float64
	switch level {
	case "low":
		score = 0
		finding.Risk = "probe.network.risk.low"
	case "medium":
		score = 50
		finding.Risk = "probe.network.risk.medium"
	case "high":
		score = 100
		finding.Risk = "probe.network.risk.high"
	default:
		return
	}
	finding.Score = &score
	finding.ScoreKind = networkScoreKindThreat
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func firstMatch(pattern *regexp.Regexp, value string) string {
	match := pattern.FindStringSubmatch(value)
	if len(match) < 2 {
		return ""
	}
	return strings.TrimSpace(match[1])
}

func browserUserAgent() string {
	return "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126 Safari/537.36"
}

var (
	dbIPThreatPattern        = regexp.MustCompile(`(?is)Estimated threat level for this IP address is\s*<span[^>]*>\s*([^<]+)`)
	dbIPCountryPattern       = regexp.MustCompile(`(?is)"countryCode"\s*:\s*"([A-Za-z]{2})"`)
	ipqsPublicScorePattern   = regexp.MustCompile(`(?i)(?:scoring|scored|score(?:\s+of|\s+is)?)\s*([0-9]{1,3}(?:\.[0-9]+)?)\s+out of 100`)
	ipqsPublicCountryPattern = regexp.MustCompile(`(?i)\blocated in\b.{0,240}?\b([A-Z]{2})\b\s+that is assigned`)
	htmlTagPattern           = regexp.MustCompile(`(?s)<[^>]+>`)
	whitespacePattern        = regexp.MustCompile(`\s+`)
)
