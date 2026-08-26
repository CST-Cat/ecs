package probe

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestIPQualityHTTPAdaptersAndRequestBytes(t *testing.T) {
	client := &http.Client{Transport: fixtureRoundTripper(func(request *http.Request) (*http.Response, error) {
		if request.URL.Host == "ip-api.com" {
			return fixtureResponse(http.StatusOK, io.NopCloser(strings.NewReader(`{"status":"success","countryCode":"US","isp":"Fixture ISP","org":"Fixture Org","proxy":false,"hosting":true,"mobile":false}`))), nil
		}
		return fixtureResponse(http.StatusOK, io.NopCloser(strings.NewReader(`{"country_code":"US","organization":"Fixture Org","asn":64500}`))), nil
	})}
	env := Environment{HTTPClient: client, UserAgent: "fixture-agent"}
	ipapi := fetchIPAPICom(context.Background(), env, client, "203.0.113.9")
	if ipapi.Err != nil || ipapi.Country != "US" || !ipapi.Server.Known || !ipapi.Server.Value || ipapi.Partial == "" {
		t.Fatalf("ip-api adapter = %+v", ipapi)
	}
	ipsb := fetchIPSB(context.Background(), env, client, "203.0.113.9")
	if ipsb.Err != nil || ipsb.Country != "US" || ipsb.Partial == "" {
		t.Fatalf("ip.sb adapter = %+v", ipsb)
	}

	var seen *http.Request
	successClient := &http.Client{Transport: fixtureRoundTripper(func(request *http.Request) (*http.Response, error) {
		seen = request
		return fixtureResponse(http.StatusOK, io.NopCloser(strings.NewReader("fixture body"))), nil
	})}
	body, _, err := requestBytes(context.Background(), successClient, "fixture-agent", "https://fixture.invalid/data", map[string]string{"X-Fixture": "yes"}, 64)
	if err != nil || string(body) != "fixture body" || seen == nil || seen.Header.Get("User-Agent") != "fixture-agent" || seen.Header.Get("X-Fixture") != "yes" {
		t.Fatalf("requestBytes success = %q/%v request=%v", body, err, seen)
	}
	for _, test := range []struct {
		name, marker string
		client       *http.Client
		ctx          context.Context
		limit        int64
	}{
		{name: "HTTP status", marker: "HTTP 503", client: &http.Client{Transport: fixtureRoundTripper(func(*http.Request) (*http.Response, error) {
			return fixtureResponse(503, io.NopCloser(strings.NewReader("rejected"))), nil
		})}, ctx: context.Background(), limit: 64},
		{name: "oversize", marker: "超过大小限制", client: &http.Client{Transport: fixtureRoundTripper(func(*http.Request) (*http.Response, error) {
			return fixtureResponse(200, io.NopCloser(strings.NewReader("too long"))), nil
		})}, ctx: context.Background(), limit: 2},
		{name: "read failure", marker: "读取响应失败", client: &http.Client{Transport: fixtureRoundTripper(func(*http.Request) (*http.Response, error) { return fixtureResponse(200, fixtureReadError{}), nil })}, ctx: context.Background(), limit: 64},
		{name: "cancel", marker: "context canceled", client: &http.Client{Transport: fixtureRoundTripper(func(*http.Request) (*http.Response, error) { return nil, context.Canceled })}, ctx: context.Background(), limit: 64},
		{name: "deadline", marker: "context deadline exceeded", client: &http.Client{Transport: fixtureRoundTripper(func(*http.Request) (*http.Response, error) { return nil, context.DeadlineExceeded })}, ctx: context.Background(), limit: 64},
		{name: "transport", marker: "网络请求失败", client: &http.Client{Transport: fixtureRoundTripper(func(*http.Request) (*http.Response, error) { return nil, errors.New("fixture transport") })}, ctx: context.Background(), limit: 64},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, _, err := requestBytes(test.ctx, test.client, "fixture", "https://fixture.invalid/data", nil, test.limit)
			if err == nil || !strings.Contains(err.Error(), test.marker) {
				t.Fatalf("requestBytes error = %v, want %q", err, test.marker)
			}
		})
	}
}

func TestIPQualityProviderAdaptersWithoutStandaloneParsers(t *testing.T) {
	for _, key := range []string{
		"IPINFO_TOKEN", "ABUSEIPDB_API_KEY", "SCAMALYTICS_USER", "SCAMALYTICS_API_KEY",
		"IPWHOIS_API_KEY", "IP2LOCATION_API_KEY", "IPREGISTRY_API_KEY", "IPQS_API_KEY", "DBIP_API_KEY",
	} {
		t.Setenv(key, "")
	}
	ip := "203.0.113.9"
	env := Environment{UserAgent: "fixture"}

	fetchOrigin := func(body string) originAssessment {
		client := &http.Client{Transport: fixtureRoundTripper(func(*http.Request) (*http.Response, error) {
			return fixtureResponse(http.StatusOK, io.NopCloser(strings.NewReader(body))), nil
		})}
		return fetchMaxMindOrigin(context.Background(), client, "fixture", ip)
	}
	for _, test := range []struct {
		name, body, label, registered string
	}{
		{name: "native", body: `{"City":{"Country":{"IsoCode":"US"}},"Country":{"IsoCode":"US","RegisteredCountry":{"IsoCode":"US"}}}`, label: "probe.network.ip_type.native", registered: "US"},
		{name: "broadcast", body: `{"City":{"Country":{"IsoCode":"US"}},"Country":{"IsoCode":"US","RegisteredCountry":{"IsoCode":"CA"}}}`, label: "probe.network.ip_type.broadcast", registered: "CA"},
	} {
		origin := fetchOrigin(test.body)
		if origin.Err != nil || origin.Label != test.label || origin.UsageCountry != "US" || origin.RegisteredCountry != test.registered {
			t.Fatalf("MaxMind %s = %+v", test.name, origin)
		}
	}
	if got := fetchOrigin(`{"Country":{}}`); got.Err == nil || got.Label != "probe.network.ip_type.unknown" || !strings.Contains(got.Err.Error(), "缺少使用地或注册地") {
		t.Fatalf("MaxMind missing geography = %+v", got)
	}

	cases := []struct {
		name  string
		body  string
		run   func(context.Context, Environment, *http.Client, string) qualityFinding
		check func(*testing.T, qualityFinding)
	}{
		{
			name: "IPinfo",
			body: `{"data":{"country_code":"US","asn":{"type":"hosting"},"privacy":{"proxy":true,"hosting":true,"vpn":false}}}`,
			run:  fetchIPinfo,
			check: func(t *testing.T, got qualityFinding) {
				if got.Country != "US" || got.Usage != "probe.network.network_type.datacenter" || !got.Proxy.Value || !got.Server.Value || got.Access != networkChannelPublicDemo {
					t.Fatalf("IPinfo finding = %+v", got)
				}
			},
		},
		{
			name: "AbuseIPDB",
			body: `{"data":{"countryCode":"US","usageType":"Data Center/Web Hosting/Transit","abuseConfidenceScore":75}}`,
			run:  fetchAbuseIPDB,
			check: func(t *testing.T, got qualityFinding) {
				if got.Country != "US" || got.Usage != "probe.network.network_type.datacenter" || got.Score == nil || *got.Score != 75 || !got.Abuser.Value || got.Risk != "probe.network.risk.high" {
					t.Fatalf("AbuseIPDB finding = %+v", got)
				}
			},
		},
		{
			name: "Scamalytics",
			body: `{"scamalytics":{"scamalytics_score":95,"is_blacklisted_external":true,"scamalytics_proxy":{"is_vpn":true,"is_datacenter":true}},"external_datasources":{"maxmind_geolite2":{"ip_country_code":"US"},"firehol":{"is_proxy":true},"x4bnet":{"is_tor":true,"is_blacklisted_spambot":true}}}`,
			run:  fetchScamalytics,
			check: func(t *testing.T, got qualityFinding) {
				if got.Country != "US" || got.Score == nil || *got.Score != 95 || !got.VPN.Value || !got.Server.Value || !got.Tor.Value || !got.Abuser.Value || !got.Robot.Value {
					t.Fatalf("Scamalytics finding = %+v", got)
				}
			},
		},
		{
			name: "ipdata",
			body: `{"country_code":"US","threat":{"is_proxy":true,"is_tor":false,"is_datacenter":true,"is_threat":true}}`,
			run:  fetchIPdata,
			check: func(t *testing.T, got qualityFinding) {
				if got.Country != "US" || !got.Proxy.Value || !got.Tor.Known || got.Tor.Value || !got.Server.Value || !got.Abuser.Value {
					t.Fatalf("ipdata finding = %+v", got)
				}
			},
		},
		{
			name: "IPWHOIS",
			body: `{"success":true,"country_code":"US","security":{"proxy":true,"vpn":false,"tor":false,"hosting":true}}`,
			run:  fetchIPWhois,
			check: func(t *testing.T, got qualityFinding) {
				if got.Country != "US" || !got.Proxy.Value || !got.VPN.Known || got.VPN.Value || !got.Server.Value || got.Access != networkChannelDirect {
					t.Fatalf("IPWHOIS finding = %+v", got)
				}
			},
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			client := &http.Client{Transport: fixtureRoundTripper(func(*http.Request) (*http.Response, error) {
				return fixtureResponse(http.StatusOK, io.NopCloser(strings.NewReader(test.body))), nil
			})}
			env.HTTPClient = client
			got := test.run(context.Background(), env, client, ip)
			if got.Err != nil {
				t.Fatalf("provider error = %v", got.Err)
			}
			test.check(t, got)
		})
	}
	denied := &http.Client{Transport: fixtureRoundTripper(func(*http.Request) (*http.Response, error) {
		return fixtureResponse(http.StatusOK, io.NopCloser(strings.NewReader(`{"success":false,"message":"denied"}`))), nil
	})}
	if got := fetchIPWhois(context.Background(), Environment{HTTPClient: denied, UserAgent: "fixture"}, denied, ip); got.Err == nil || !strings.Contains(got.Err.Error(), "IPWHOIS 未返回安全数据") {
		t.Fatalf("IPWHOIS rejected response = %+v", got)
	}
}

func TestRequestBytesRedirectAndEndpointValidation(t *testing.T) {
	redirected := false
	redirectClient := &http.Client{Transport: fixtureRoundTripper(func(request *http.Request) (*http.Response, error) {
		if request.URL.Hostname() == "fixture.invalid" {
			response := fixtureResponse(http.StatusFound, io.NopCloser(strings.NewReader("")))
			response.Request = request
			response.Header.Set("Location", "https://allowed.invalid/next")
			return response, nil
		}
		redirected = true
		return fixtureResponse(http.StatusOK, io.NopCloser(strings.NewReader("redirected"))), nil
	})}
	if _, _, err := requestBytes(context.Background(), redirectClient, "fixture", "https://fixture.invalid/start", nil, 64); err == nil || !strings.Contains(err.Error(), "网络请求失败") || redirected {
		t.Fatalf("cross-host redirect error = %v", err)
	}
	body, _, err := requestBytesAllowingRedirectHosts(context.Background(), redirectClient, "fixture", "https://fixture.invalid/start", nil, 64, []string{"allowed.invalid"})
	if err != nil || string(body) != "redirected" {
		t.Fatalf("allowed redirect = %q/%v", body, err)
	}
	if _, _, err := requestBytes(context.Background(), redirectClient, "fixture", "://invalid", nil, 64); err == nil || !strings.Contains(err.Error(), "创建请求失败") {
		t.Fatalf("invalid endpoint error = %v", err)
	}
}
