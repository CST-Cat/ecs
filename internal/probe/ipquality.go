package probe

import (
	"context"
	"errors"
	"net"
	"net/http"
	"sync"
	"time"

	"ecs/internal/config"
)

const communityIPInfoBase = "https://ipinfo.check.place"

var communityRequestGate = make(chan struct{}, 1)

type qualitySignal struct {
	Known bool
	Value bool
}

type qualityFinding struct {
	ID        string
	Enabled   bool
	Access    string
	Country   string
	Usage     string
	Company   string
	Score     *float64
	ScoreKind string
	Risk      string
	Proxy     qualitySignal
	Tor       qualitySignal
	VPN       qualitySignal
	Server    qualitySignal
	Abuser    qualitySignal
	Robot     qualitySignal
	Latency   time.Duration
	Err       error
	Partial   string
}

type originAssessment struct {
	Enabled           bool
	Label             string
	UsageCountry      string
	RegisteredCountry string
	Access            string
	Latency           time.Duration
	Err               error
}

type ipQualityBundle struct {
	Version  string
	Origin   originAssessment
	Findings map[string]qualityFinding
}

func collectIPQuality(ctx context.Context, env Environment, lookup ipLookup) ipQualityBundle {
	bundle := ipQualityBundle{
		Version:  lookup.Version,
		Findings: make(map[string]qualityFinding, len(config.IPQualitySourceIDs())),
	}
	for _, id := range config.IPQualitySourceIDs() {
		if id == "maxmind" {
			continue
		}
		bundle.Findings[id] = qualityFinding{
			ID:      id,
			Enabled: qualitySourceEnabled(env.Config.IPQualitySources, id),
		}
	}

	if qualitySourceEnabled(env.Config.IPQualitySources, "ipapi") {
		finding := newFinding("ipapi")
		if lookup.HasIntel {
			finding = findingFromIPAPI(lookup.Data, lookup.Latency)
			if !findingHasEvidence(finding) {
				finding.Err = errors.New("ipapi 响应缺少所需字段")
			}
		} else {
			finding.Access = networkChannelDirect
			finding.Latency = lookup.Latency
			if lookup.IntelErr != nil {
				finding.Err = lookup.IntelErr
			} else {
				finding.Err = errors.New("ipapi 情报未查询")
			}
		}
		finding.Enabled = true
		bundle.Findings["ipapi"] = finding
	}

	communityClient := newIPVersionHTTPClient(env.Config.HTTPTimeout, lookup.Version)
	defer communityClient.CloseIdleConnections()

	type outcome struct {
		id      string
		finding qualityFinding
		origin  *originAssessment
	}
	outcomes := make(chan outcome, len(config.IPQualitySourceIDs()))
	var wg sync.WaitGroup

	if qualitySourceEnabled(env.Config.IPQualitySources, "maxmind") {
		wg.Add(1)
		go func() {
			defer wg.Done()
			value := fetchMaxMindOrigin(ctx, communityClient, env.UserAgent, lookup.Data.IP)
			outcomes <- outcome{id: "maxmind", origin: &value}
		}()
	}

	fetchers := map[string]func(context.Context, Environment, *http.Client, string) qualityFinding{
		"ipinfo":      fetchIPinfo,
		"ipregistry":  fetchIPregistry,
		"ip2location": fetchIP2Location,
		"abuseipdb":   fetchAbuseIPDB,
		"scamalytics": fetchScamalytics,
		"ipqs":        fetchIPQS,
		"dbip":        fetchDBIP,
		"ipdata":      fetchIPdata,
		"ipwhois":     fetchIPWhois,
		"ipapicom":    fetchIPAPICom,
		"ipsb":        fetchIPSB,
	}
	for _, id := range config.IPQualitySourceIDs() {
		fetcher, ok := fetchers[id]
		if !ok || !qualitySourceEnabled(env.Config.IPQualitySources, id) {
			continue
		}
		wg.Add(1)
		go func(id string, fetcher func(context.Context, Environment, *http.Client, string) qualityFinding) {
			defer wg.Done()
			outcomes <- outcome{id: id, finding: fetcher(ctx, env, communityClient, lookup.Data.IP)}
		}(id, fetcher)
	}

	go func() {
		wg.Wait()
		close(outcomes)
	}()
	for item := range outcomes {
		if item.origin != nil {
			bundle.Origin = *item.origin
			continue
		}
		item.finding.Enabled = true
		bundle.Findings[item.id] = item.finding
	}
	return bundle
}

func qualitySourceEnabled(configured []string, source string) bool {
	for _, item := range configured {
		switch item {
		case "all":
			return true
		case "none":
			return false
		case source:
			return true
		}
	}
	return false
}

// canonicalQualitySourceSubset keeps table-specific field capabilities as
// membership only; every resulting order still comes from the config catalog.
func canonicalQualitySourceSubset(allowed map[string]struct{}) []string {
	ordered := make([]string, 0, len(allowed))
	for _, id := range config.IPQualitySourceIDs() {
		if _, ok := allowed[id]; ok {
			ordered = append(ordered, id)
		}
	}
	return ordered
}

func newIPVersionHTTPClient(timeout time.Duration, version string) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	network := "tcp" + version
	transport.DialContext = func(ctx context.Context, _ string, address string) (net.Conn, error) {
		return (&net.Dialer{Timeout: timeout}).DialContext(ctx, network, address)
	}
	transport.MaxIdleConnsPerHost = 8
	transport.TLSHandshakeTimeout = timeout
	return &http.Client{
		Transport: transport,
		Timeout:   timeout,
		CheckRedirect: func(request *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return http.ErrUseLastResponse
			}
			return nil
		},
	}
}

func newProxyFallbackHTTPClient(timeout time.Duration) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = http.ProxyFromEnvironment
	transport.MaxIdleConnsPerHost = 4
	transport.TLSHandshakeTimeout = timeout
	return &http.Client{
		Transport: transport,
		Timeout:   timeout,
		CheckRedirect: func(request *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return http.ErrUseLastResponse
			}
			return nil
		},
	}
}
