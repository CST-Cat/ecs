package probe

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"ecs/internal/config"
	"ecs/internal/model"
)

type networkProbe struct{}

func (networkProbe) ID() string { return "network" }

type ipAPIResponse struct {
	IP string `json:"ip"`
	// The current ipapi.is response is flat (asn_num/asn_org/company_name/cc),
	// while older responses used the nested ASN/Company/Location objects below.
	// Keep both representations in the wire type and normalize them immediately
	// after decoding so every consumer sees one canonical shape.
	ASNNum       int     `json:"asn_num"`
	ASNOrg       string  `json:"asn_org"`
	CompanyName  string  `json:"company_name"`
	CountryCode  string  `json:"cc"`
	Latitude     float64 `json:"lat"`
	Longitude    float64 `json:"lon"`
	RIR          string  `json:"rir"`
	IsBogon      bool    `json:"is_bogon"`
	IsMobile     bool    `json:"is_mobile"`
	IsSatellite  bool    `json:"is_satellite"`
	IsCrawler    bool    `json:"is_crawler"`
	IsDatacenter bool    `json:"is_datacenter"`
	IsTor        bool    `json:"is_tor"`
	IsProxy      bool    `json:"is_proxy"`
	IsVPN        bool    `json:"is_vpn"`
	IsAbuser     bool    `json:"is_abuser"`
	ElapsedMS    float64 `json:"elapsed_ms"`
	Error        string  `json:"error"`
	ASN          struct {
		ASN          int    `json:"asn"`
		AbuserScore  string `json:"abuser_score"`
		Route        string `json:"route"`
		Organization string `json:"org"`
		Domain       string `json:"domain"`
		Type         string `json:"type"`
		Country      string `json:"country"`
		RIR          string `json:"rir"`
	} `json:"asn"`
	Company struct {
		Name        string `json:"name"`
		AbuserScore string `json:"abuser_score"`
		Domain      string `json:"domain"`
		Type        string `json:"type"`
		Network     string `json:"network"`
	} `json:"company"`
	Location struct {
		Continent   string `json:"continent"`
		Country     string `json:"country"`
		CountryCode string `json:"country_code"`
		State       string `json:"state"`
		City        string `json:"city"`
		Timezone    string `json:"timezone"`
		Accuracy    string `json:"accuracy"`
	} `json:"location"`
	BooleanPresence ipAPIBooleanPresence `json:"-"`
}

type ipAPIBooleanPresence struct {
	IsBogon      bool
	IsMobile     bool
	IsSatellite  bool
	IsCrawler    bool
	IsDatacenter bool
	IsTor        bool
	IsProxy      bool
	IsVPN        bool
	IsAbuser     bool
}

func (data *ipAPIResponse) UnmarshalJSON(input []byte) error {
	type wireIPAPIResponse ipAPIResponse
	var decoded wireIPAPIResponse
	if err := json.Unmarshal(input, &decoded); err != nil {
		return err
	}
	var presence struct {
		IsBogon      *bool `json:"is_bogon"`
		IsMobile     *bool `json:"is_mobile"`
		IsSatellite  *bool `json:"is_satellite"`
		IsCrawler    *bool `json:"is_crawler"`
		IsDatacenter *bool `json:"is_datacenter"`
		IsTor        *bool `json:"is_tor"`
		IsProxy      *bool `json:"is_proxy"`
		IsVPN        *bool `json:"is_vpn"`
		IsAbuser     *bool `json:"is_abuser"`
	}
	if err := json.Unmarshal(input, &presence); err != nil {
		return err
	}
	*data = ipAPIResponse(decoded)
	data.BooleanPresence = ipAPIBooleanPresence{
		IsBogon:      presence.IsBogon != nil,
		IsMobile:     presence.IsMobile != nil,
		IsSatellite:  presence.IsSatellite != nil,
		IsCrawler:    presence.IsCrawler != nil,
		IsDatacenter: presence.IsDatacenter != nil,
		IsTor:        presence.IsTor != nil,
		IsProxy:      presence.IsProxy != nil,
		IsVPN:        presence.IsVPN != nil,
		IsAbuser:     presence.IsAbuser != nil,
	}
	return nil
}

type ipLookup struct {
	Version         string
	Data            ipAPIResponse
	Latency         time.Duration
	HasIntel        bool
	IntelAttempted  bool
	IntelErr        error
	BGPObservations []routeViewsPrefix
}

func (networkProbe) Run(ctx context.Context, env Environment) model.Result {
	start := time.Now()
	result := model.NewResult("network", networkTitleKey)
	result.Description = networkDescriptionKey
	result.Methodology = model.Methodology{
		Kind:            "provider-assessment",
		Label:           networkMethodologyLabel,
		Engine:          "probe.network.methodology.engine",
		Profile:         networkMethodologyProfile,
		ComparisonScope: networkComparisonScope,
	}
	result.Methodology.Parameters = newComparisonParameters()
	addComparisonParameter(result.Methodology.Parameters, "ip_version", env.Config.IPVersion)
	addComparisonParameterHash(result.Methodology.Parameters, "ip_quality_sources_sha256", env.Config.IPQualitySources)
	addComparisonParameter(result.Methodology.Parameters, "http_timeout", env.Config.HTTPTimeout.String())

	versions := config.IPVersions(env.Config.IPVersion)
	plannedSources := enabledIPQualitySourceCount(env.Config.IPQualitySources)
	// 出口发现由 runner 统一做过（见 egress.go），这里只读结果：
	// network 是第三方级模块，走到这一步一定拿到了完整的情报记录。
	var found []ipLookup
	for _, version := range versions {
		address, ok := env.Egress.Lookup(version)
		if ok && address.Err == nil && address.IP != "" {
			intel, hasIntel := env.Egress.IntelFor(version)
			intel.IP = firstNonEmpty(intel.IP, address.IP)
			found = append(found, ipLookup{
				Version:         version,
				Data:            intel,
				Latency:         address.Latency,
				HasIntel:        hasIntel,
				IntelAttempted:  address.IntelAttempted,
				IntelErr:        address.IntelErr,
				BGPObservations: address.BGPObservations,
			})
			continue
		}
		if ok && address.Err != nil {
			addFailure(&result, "egress", "IPv"+version, address.Err)
		} else if !ok {
			addFailureMessage(&result, "egress", "IPv"+version, "unavailable")
		}
		result.Notes = append(result.Notes, "probe.network.note.egress_lookup_failed")
		result.Fields = append(result.Fields, model.Field{
			Key: "ipv" + version + "_lookup_error", Label: networkFieldLabelKey("ipv" + version + "_lookup_error"), Value: model.KeyValue("probe.network.value.lookup_failed"),
		})
	}
	if len(found) == 0 {
		result.Fail(fmt.Errorf("egress lookup unavailable"))
		result.Notes = append(result.Notes, "probe.network.note.egress_unavailable")
		result.Evidence = model.NewEvidence(0, len(versions)*plannedSources, "source")
		result.Finish(start)
		return result
	}
	sort.Slice(found, func(i, j int) bool {
		return found[i].Version < found[j].Version
	})

	type qualityResult struct {
		version string
		bundle  ipQualityBundle
	}
	qualityResults := make(chan qualityResult, len(found))
	var qualityWG sync.WaitGroup
	for _, lookup := range found {
		qualityWG.Add(1)
		go func(lookup ipLookup) {
			defer qualityWG.Done()
			qualityResults <- qualityResult{
				version: lookup.Version,
				bundle:  collectIPQuality(ctx, env, lookup),
			}
		}(lookup)
	}
	go func() {
		qualityWG.Wait()
		close(qualityResults)
	}()
	bundles := make(map[string]ipQualityBundle, len(found))
	for item := range qualityResults {
		bundles[item.version] = item.bundle
	}

	overview := model.Table{
		Key:   "network.egress.overview",
		Title: "probe.network.table.overview",
		Columns: []model.TableColumn{
			{Key: "ip_family", Label: "probe.network.column.ip_family"},
			{Key: "network_type", Label: "probe.network.column.network_type"},
			{Key: "datacenter", Label: "probe.network.column.datacenter"},
			{Key: "proxy", Label: "probe.network.column.proxy"},
			{Key: "vpn", Label: "probe.network.column.vpn"},
			{Key: "tor", Label: "probe.network.column.tor"},
			{Key: "abuse_record", Label: "probe.network.column.abuse"},
			{Key: "source_duration", Label: "probe.network.column.duration"},
		},
		RowIdentity: "ip_family",
	}
	var summaryMessages []model.Message
	missingIntel := false
	failedSources := false
	partialSources := false
	validSources := 0
	expectedSources := len(versions) * plannedSources
	for _, lookup := range found {
		prefix := "ipv" + lookup.Version
		data := lookup.Data
		bundle := bundles[lookup.Version]
		location := strings.Trim(strings.Join([]string{
			firstNonEmpty(data.Location.Country, data.Location.CountryCode),
			data.Location.State,
			data.Location.City,
		}, " / "), " /")
		if location == "" {
			location = bundleCountry(bundle)
		}
		locationValue := model.RawValue(location)
		if location == "" {
			locationValue = unavailableIPFieldValue(lookup, "unknown")
		}
		originASN, route := egressBGPIdentity(lookup.BGPObservations)
		asnNumber := data.ASN.ASN
		if asnNumber <= 0 {
			asnNumber = originASN
		}
		asn := formatASNWithOrganization(asnNumber, data.ASN.Organization)
		asnValue := model.RawValue(asn)
		if asn == "unknown" || asn == networkMissingValue {
			asnValue = unavailableIPFieldValue(lookup, asn)
		}
		route = firstNonEmpty(data.ASN.Route, route)
		routeValue := model.RawValue(route)
		if route == "" {
			routeValue = unavailableIPFieldValue(lookup, "unknown")
		}
		owner := firstNonEmpty(data.Company.Name, data.ASN.Organization)
		ownerValue := model.RawValue(owner)
		if owner == "" {
			ownerValue = unavailableIPFieldValue(lookup, "unknown")
		}
		ipType := "probe.network.ip_type.unknown"
		usageCountryValue := model.KeyValue(networkMissingValue)
		registeredCountryValue := model.KeyValue(networkMissingValue)
		if bundle.Origin.Enabled {
			ipType = bundle.Origin.Label
			if strings.TrimSpace(bundle.Origin.UsageCountry) != "" {
				usageCountryValue = model.RawValue(bundle.Origin.UsageCountry)
			}
			if strings.TrimSpace(bundle.Origin.RegisteredCountry) != "" {
				registeredCountryValue = model.RawValue(bundle.Origin.RegisteredCountry)
			}
		}
		result.Fields = append(result.Fields,
			model.Field{Key: prefix, Label: networkFieldLabelKey(prefix), Value: model.RawValue(data.IP), Sensitive: true},
			model.Field{Key: prefix + "_asn", Label: networkFieldLabelKey(prefix + "_asn"), Value: asnValue},
			model.Field{Key: prefix + "_route", Label: networkFieldLabelKey(prefix + "_route"), Value: routeValue},
			model.Field{Key: prefix + "_location", Label: networkFieldLabelKey(prefix + "_location"), Value: locationValue},
			model.Field{Key: prefix + "_owner", Label: networkFieldLabelKey(prefix + "_owner"), Value: ownerValue},
			model.Field{Key: prefix + "_ip_type", Label: networkFieldLabelKey(prefix + "_ip_type"), Value: model.KeyValue(ipType)},
			model.Field{Key: prefix + "_usage_country", Label: networkFieldLabelKey(prefix + "_usage_country"), Value: usageCountryValue},
			model.Field{Key: prefix + "_registered_country", Label: networkFieldLabelKey(prefix + "_registered_country"), Value: registeredCountryValue},
		)
		networkType := firstNonEmpty(normalizeNetworkType(firstNonEmpty(data.Company.Type, data.ASN.Type)), networkMissingValue)
		overview.Rows = append(overview.Rows, []model.Value{
			model.KeyValue(networkIPFamilyKey(lookup.Version)),
			model.KeyValue(networkType),
			model.KeyValue(ipAPIBooleanText(data.IsDatacenter, data.BooleanPresence.IsDatacenter)),
			model.KeyValue(ipAPIBooleanText(data.IsProxy, data.BooleanPresence.IsProxy)),
			model.KeyValue(ipAPIBooleanText(data.IsVPN, data.BooleanPresence.IsVPN)),
			model.KeyValue(ipAPIBooleanText(data.IsTor, data.BooleanPresence.IsTor)),
			model.KeyValue(ipAPIBooleanText(data.IsAbuser, data.BooleanPresence.IsAbuser)),
			model.RawValue(fmt.Sprintf("%.0f ms", float64(lookup.Latency)/float64(time.Millisecond))),
		})
		result.Measurements = append(result.Measurements, bundle.measurements()...)
		if !lookup.HasIntel && lookup.IntelErr != nil && !qualitySourceEnabled(env.Config.IPQualitySources, "ipapi") {
			addFailure(&result, "egress", prefix+"/ipapi", lookup.IntelErr)
		}
		if bundle.Origin.Enabled && bundle.Origin.Err != nil {
			addFailure(&result, "provider", prefix+"/maxmind", bundle.Origin.Err)
		}
		for _, sourceID := range config.IPQualitySourceIDs() {
			if sourceID == "maxmind" {
				continue
			}
			finding := bundle.Findings[sourceID]
			if finding.Enabled && finding.Err != nil {
				addFailure(&result, "provider", prefix+"/"+sourceID, finding.Err)
			}
		}
		result.Tables = append(
			result.Tables,
			bundle.typeTable(),
			bundle.scoreTable(),
			bundle.factorTable(),
			bundle.statusTable(),
		)
		successful, enabled := bundle.successfulSources()
		validSources += successful
		if len(summaryMessages) == 0 {
			summaryMessages = append(summaryMessages, model.NewMessage(
				"probe.network.summary.version",
				lookup.Version,
				firstNonEmpty(data.Location.CountryCode, data.Location.Country, networkMissingValue),
				formatASNWithOrganization(asnNumber, data.ASN.Organization),
				firstNonEmpty(bundle.Origin.Label, "probe.network.ip_type.unknown"),
				strconv.Itoa(successful), strconv.Itoa(enabled),
			))
		} else {
			summaryMessages = append(summaryMessages, model.NewMessage(
				"probe.network.summary.version.additional",
				lookup.Version,
				firstNonEmpty(data.Location.CountryCode, data.Location.Country, networkMissingValue),
				formatASNWithOrganization(asnNumber, data.ASN.Organization),
				firstNonEmpty(bundle.Origin.Label, "probe.network.ip_type.unknown"),
				strconv.Itoa(successful), strconv.Itoa(enabled),
			))
		}
		if !lookup.HasIntel {
			result.Status = model.StatusWarning
			missingIntel = true
		}
		if bundle.needsWarning() {
			result.Status = model.StatusWarning
		}
		if len(bundle.failedSourceIDs()) > 0 {
			failedSources = true
		}
		if len(bundle.partialSourceIDs()) > 0 {
			partialSources = true
		}
	}
	if missingIntel {
		result.Notes = append(result.Notes, "probe.network.note.no_ipapi_intel")
	}
	if failedSources {
		result.Notes = append(result.Notes, "probe.network.note.failed_sources")
	}
	if partialSources {
		result.Notes = append(result.Notes, "probe.network.note.partial_sources")
	}
	result.Tables = append([]model.Table{overview}, result.Tables...)
	result.Evidence = model.NewEvidence(validSources, expectedSources, "source")
	result.Fields = append([]model.Field{{
		Key: "ip_version_mode", Label: networkFieldLabelKey("ip_version_mode"), Value: model.RawValue(fallback(env.Config.IPVersion, config.IPVersionAuto)),
	}}, result.Fields...)
	result.Sources = networkSources()
	result.Notes = append(result.Notes,
		"probe.network.note.third_party",
		"probe.network.note.no_upload",
		"probe.network.note.source_semantics",
		"probe.network.note.origin_scope",
		"probe.network.note.dbip_mapping",
	)
	if proxyEnvironmentEnabled() {
		result.Notes = append(result.Notes, "probe.network.note.proxy_fallback")
	}
	result.SummaryMessages = summaryMessages
	result.Finish(start)
	return result
}

func enabledIPQualitySourceCount(configured []string) int {
	count := 0
	for _, source := range config.IPQualitySourceIDs() {
		if qualitySourceEnabled(configured, source) {
			count++
		}
	}
	return count
}

func networkSources() []model.Source {
	qualitySourceURLs := map[string]string{
		"maxmind":     "https://www.maxmind.com/",
		"ipinfo":      "https://ipinfo.io/developers",
		"ipregistry":  "https://ipregistry.co/docs/",
		"ipapi":       "https://ipapi.is/",
		"ip2location": "https://www.ip2location.io/ip2location-documentation",
		"abuseipdb":   "https://docs.abuseipdb.com/",
		"scamalytics": "https://scamalytics.com/",
		"ipqs":        "https://www.ipqualityscore.com/documentation/proxy-detection-api/overview",
		"dbip":        "https://db-ip.com/api/doc.php",
		"ipdata":      "https://ipdata.co/",
		"ipwhois":     "https://ipwhois.io/documentation",
		"ipapicom":    "http://ip-api.com/json/",
		"ipsb":        "https://api.ip.sb/geoip/",
	}
	sources := make([]model.Source, 0, len(config.IPQualitySourceIDs())+4)
	for _, id := range config.IPQualitySourceIDs() {
		sources = append(sources, model.Source{
			Name:    networkSourceNameKey(id),
			URL:     qualitySourceURLs[id],
			Purpose: networkSourcePurposeKey(id),
		})
	}
	sources = append(sources,
		model.Source{Name: networkSourceNameKey("routeviews"), URL: "https://api.routeviews.org/", Purpose: networkSourcePurposeKey("routeviews")},
		model.Source{Name: networkSourceNameKey("ipquality"), URL: "https://github.com/xykt/IPQuality", Purpose: networkSourcePurposeKey("ipquality")},
		model.Source{Name: networkSourceNameKey("checkplace"), URL: "https://check.place/", Purpose: networkSourcePurposeKey("checkplace")},
		model.Source{Name: networkSourceNameKey("jina"), URL: "https://github.com/jina-ai/reader", Purpose: networkSourcePurposeKey("jina")},
	)
	return sources
}

func egressBGPIdentity(observations []routeViewsPrefix) (asn int, route string) {
	bestLength := -1
	for _, observation := range observations {
		length := prefixLength(observation.Prefix)
		if length < 0 {
			continue
		}
		if length < bestLength {
			continue
		}
		if length == bestLength && route != "" {
			continue
		}
		bestLength = length
		route = observation.Prefix
		asn = observation.OriginASN
	}
	return asn, route
}

func formatASNWithOrganization(asn int, organization string) string {
	if asn <= 0 {
		return networkMissingValue
	}
	if organization == "" {
		return formatASN(asn)
	}
	return fmt.Sprintf("%s %s", formatASN(asn), organization)
}

func bundleCountry(bundle ipQualityBundle) string {
	if bundle.Origin.Err == nil {
		if country := firstNonEmpty(bundle.Origin.UsageCountry, bundle.Origin.RegisteredCountry); country != "" {
			return country
		}
	}
	for _, id := range canonicalQualitySourceSubset(ipQualityTypeSources) {
		finding := bundle.Findings[id]
		if finding.Enabled && finding.Err == nil && finding.Country != "" {
			return finding.Country
		}
	}
	return ""
}

func unavailableIPField(lookup ipLookup, normalFallback string) string {
	if lookup.HasIntel {
		if normalFallback == "" || normalFallback == "unknown" {
			return networkMissingValue
		}
		return normalFallback
	}
	if lookup.IntelAttempted {
		return "probe.network.value.intel_unavailable"
	}
	return "probe.network.value.intel_not_attempted"
}

func unavailableIPFieldValue(lookup ipLookup, normalFallback string) model.Value {
	if lookup.HasIntel {
		switch normalFallback {
		case "", "unknown", networkMissingValue:
			return model.KeyValue(networkMissingValue)
		default:
			return model.RawValue(normalFallback)
		}
	}
	return model.KeyValue(unavailableIPField(lookup, normalFallback))
}

func lookupIP(ctx context.Context, env Environment, version string) (ipAPIResponse, time.Duration, error) {
	var data ipAPIResponse
	network := "tcp" + version
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DialContext = func(ctx context.Context, _ string, address string) (net.Conn, error) {
		return (&net.Dialer{Timeout: env.Config.HTTPTimeout}).DialContext(ctx, network, address)
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{
		Transport: transport,
		Timeout:   env.Config.HTTPTimeout,
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.ipapi.is/", nil)
	if err != nil {
		return data, 0, err
	}
	request.Header.Set("User-Agent", env.UserAgent)
	start := time.Now()
	response, err := client.Do(request)
	latency := time.Since(start)
	if err != nil {
		return data, latency, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return data, latency, fmt.Errorf("HTTP %d", response.StatusCode)
	}
	reader := io.LimitReader(response.Body, 512*1024)
	if err := json.NewDecoder(reader).Decode(&data); err != nil {
		return data, latency, err
	}
	data = normalizeIPAPIResponse(data)
	if data.Error != "" {
		return data, latency, fmt.Errorf("%s", data.Error)
	}
	if net.ParseIP(data.IP) == nil {
		return data, latency, fmt.Errorf("数据源未返回有效 IP")
	}
	return data, latency, nil
}

// normalizeIPAPIResponse converts the current flat ipapi.is schema into the
// canonical nested fields used by the quality and report code.  The endpoint
// may return only a subset of the optional fields, so each value is filled
// independently and missing data remains missing instead of being guessed.
func normalizeIPAPIResponse(data ipAPIResponse) ipAPIResponse {
	if data.ASN.ASN <= 0 {
		data.ASN.ASN = data.ASNNum
	}
	if data.ASN.Organization == "" {
		data.ASN.Organization = data.ASNOrg
	}
	if data.Company.Name == "" {
		data.Company.Name = data.CompanyName
	}
	if data.Company.Type == "" && data.BooleanPresence.IsDatacenter && data.IsDatacenter {
		// The flat response does not expose the old nested type field, but its
		// datacenter boolean is an explicit infrastructure classification.
		data.Company.Type = "hosting"
	}
	if data.Location.CountryCode == "" {
		data.Location.CountryCode = data.CountryCode
	}
	if data.Location.Country == "" {
		data.Location.Country = data.CountryCode
	}
	return data
}

func ipAPIBooleanText(value, known bool) string {
	if !known {
		return "probe.network.boolean.unknown"
	}
	if value {
		return "probe.network.boolean.yes"
	}
	return "probe.network.boolean.no"
}

func proxyEnvironmentEnabled() bool {
	for _, key := range []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "all_proxy"} {
		if strings.TrimSpace(os.Getenv(key)) != "" {
			return true
		}
	}
	return false
}
