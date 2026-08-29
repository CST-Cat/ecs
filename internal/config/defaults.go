package config

import (
	"time"

	"ecs/internal/i18n"
)

// Defaults builds the runtime baseline for a profile. Profiles only select
// modules; probe budgets and endpoint pools remain identical for comparable
// measurements.
func Defaults(profile string) (Runtime, error) {
	if profile == "" {
		profile = ProfileStandard
	}
	base := Runtime{
		Profile:          profile,
		Exposure:         ExposureThirdParty,
		Reveal:           false,
		IPVersion:        IPVersionAuto,
		IPQualitySources: []string{"all"},
		Formats:          []string{"json", "md", "html"},
		DiskPath:         ".",
		DiskMatrixMode:   DiskMatrixTime,
		HTTPTimeout:      10 * time.Second,
		CPUTime:          15 * time.Second,
		DiskMiB:          2048,
		DNSAttempts:      8,
		LatencyAttempts:  10,
		SpeedThreads:     8,
		IPerfDuration:    15 * time.Second,
		IPerfTargets:     selectIPerfTargets(3),
		STUNServers:      stunServerPool(),
		DNSResolvers: []Endpoint{
			{Name: "Cloudflare", Address: "1.1.1.1:53"},
			{Name: "Cloudflare IPv6", Address: "[2606:4700:4700::1111]:53"},
			{Name: "Google", Address: "8.8.8.8:53"},
			{Name: "Google IPv6", Address: "[2001:4860:4860::8888]:53"},
			{Name: "Quad9", Address: "9.9.9.9:53"},
			{Name: "Quad9 IPv6", Address: "[2620:fe::fe]:53"},
			{Name: "AliDNS", Address: "223.5.5.5:53"},
			{Name: "DNSPod", Address: "119.29.29.29:53"},
		},
		LatencyTargets: []Endpoint{
			{Name: "Cloudflare", Address: "www.cloudflare.com:443", Kind: LatencyTargetKindGlobalCDN},
			{Name: "Google", Address: "www.google.com:443", Kind: LatencyTargetKindGlobal},
			{Name: "Aliyun", Address: "www.aliyun.com:443", Kind: LatencyTargetKindMainlandChina},
			{Name: "Tencent", Address: "www.tencent.com:443", Kind: LatencyTargetKindMainlandChina},
			{Name: "Amazon", Address: "www.amazon.com:443", Kind: LatencyTargetKindGlobal},
		},
		RouteTargets: []Endpoint{
			{Name: "Cloudflare", Address: "1.1.1.1", Kind: RouteTargetKindGlobal},
			{Name: "Google", Address: "8.8.8.8", Kind: RouteTargetKindGlobal},
			{Name: "AliDNS", Address: "223.5.5.5", Kind: RouteTargetKindMainlandChina},
		},
		BacktraceTargets: BacktraceTargetsFor(defaultBacktraceCityIDs()),
	}

	switch profile {
	case ProfileStandard:
		base.Modules = ModulesForProfile(ProfileStandard)
	case ProfileFull:
		base.Modules = ModulesForProfile(ProfileFull)
	default:
		return Runtime{}, i18n.Errorf("err.unknownProfile", profile)
	}
	return base, nil
}
