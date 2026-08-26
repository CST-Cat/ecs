package probe

import "strings"

const (
	networkTitleKey           = "module.network.title"
	networkDescriptionKey     = "probe.network.description"
	networkMethodologyLabel   = "methodology.provider-assessment"
	networkMethodologyProfile = "probe.network.profile"
	networkComparisonScope    = "probe.network.comparison_scope"
	networkMissingValue       = "probe.network.value.missing"

	networkChannelDirect         = "probe.network.channel.direct"
	networkChannelAPIKey         = "probe.network.channel.api_key"
	networkChannelPublicDemo     = "probe.network.channel.public_demo"
	networkChannelTryout         = "probe.network.channel.tryout"
	networkChannelCommunity      = "probe.network.channel.community"
	networkChannelOfficialFree   = "probe.network.channel.official_free"
	networkChannelPublicPage     = "probe.network.channel.public_page"
	networkChannelJina           = "probe.network.channel.jina"
	networkChannelJinaProxy      = "probe.network.channel.jina_proxy"
	networkChannelMixedFallback  = "probe.network.channel.mixed_fallback"
	networkChannelFreeFallback   = "probe.network.channel.free_fallback"
	networkChannelExtendedAPIKey = "probe.network.channel.extended_api_key"

	networkScoreKindCompanyAbuse = "probe.network.score_kind.company_abuse_probability"
	networkScoreKindASNAbuse     = "probe.network.score_kind.asn_abuse_probability"
	networkScoreKindIP2Proxy     = "probe.network.score_kind.ip2proxy_fraud_score"
	networkScoreKindAbuse        = "probe.network.score_kind.abuse_confidence"
	networkScoreKindWebFraud     = "probe.network.score_kind.web_fraud_score"
	networkScoreKindIPFraud      = "probe.network.score_kind.ip_fraud_score"
	networkScoreKindThreat       = "probe.network.score_kind.threat_level"
	networkRiskUnknown           = "probe.network.risk.unknown"

	networkPartialPrivacy      = "probe.network.partial.missing_privacy_fields"
	networkPartialScore        = "probe.network.partial.missing_score"
	networkPartialThreat       = "probe.network.partial.missing_threat_level"
	networkPartialSecurity     = "probe.network.partial.missing_security_fields"
	networkPartialPublicFields = "probe.network.partial.public_page_fields"
	networkPartialCachedFields = "probe.network.partial.cached_fields"
	networkPartialFallback     = "probe.network.partial.fallback"
	networkPartialMultiple     = "probe.network.partial.multiple"
)

func networkFieldLabelKey(key string) string {
	switch {
	case key == "ip_version_mode":
		return "probe.network.field.ip_version_mode"
	case strings.HasSuffix(key, "_lookup_error"):
		return "probe.network.field.lookup_error"
	case strings.HasSuffix(key, "_usage_country"):
		return "probe.network.field.usage_country"
	case strings.HasSuffix(key, "_registered_country"):
		return "probe.network.field.registered_country"
	case strings.HasSuffix(key, "_asn"):
		return "probe.network.field.asn"
	case strings.HasSuffix(key, "_route"):
		return "probe.network.field.route"
	case strings.HasSuffix(key, "_location"):
		return "probe.network.field.location"
	case strings.HasSuffix(key, "_owner"):
		return "probe.network.field.owner"
	case strings.HasSuffix(key, "_ip_type"):
		return "probe.network.field.ip_type"
	case strings.HasPrefix(key, "ipv"):
		return "probe.network.field.egress"
	default:
		return "probe.network.field.value"
	}
}

func networkIPFamilyKey(version string) string {
	return "probe.network.ip_family.ipv" + version
}

func networkSourceNameKey(id string) string {
	return "probe.network.source_name." + id
}

func networkScoreBandKey(id string) string {
	return "probe.network.score_band." + id
}

func networkScoreMethodKey(id string) string {
	return "probe.network.method.score." + id
}

func networkSignalKey(signal qualitySignal) string {
	if !signal.Known {
		return "probe.network.boolean.unknown"
	}
	if signal.Value {
		return "probe.network.boolean.yes"
	}
	return "probe.network.boolean.no"
}

func networkStatusKey(enabled, failed, partial bool) string {
	switch {
	case !enabled:
		return "probe.network.status.disabled"
	case failed:
		return "probe.network.status.failed"
	case partial:
		return "probe.network.status.partial"
	default:
		return "probe.network.status.ok"
	}
}
