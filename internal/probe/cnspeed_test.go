package probe

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"testing"
	"time"

	"ecs/internal/config"
)

type cnFixtureResolver struct {
	addresses []netip.Addr
	err       error
}

func (resolver cnFixtureResolver) LookupNetIP(context.Context, string, string) ([]netip.Addr, error) {
	return resolver.addresses, resolver.err
}

func TestCNSpeedNodeFixturesAndHTTPResults(t *testing.T) {
	oldURL := cnNodeListURLForTest
	defer func() { cnNodeListURLForTest = oldURL }()
	cnNodeListURLForTest = "https://fixture.invalid/CN.csv"
	csvBody := "id,operator,province,city,host,pingUrl,downloadUrl,active\n" +
		"1,电信,上海,上海,cn1,https://8.8.8.8/ping,https://8.8.8.8/file,1\n" +
		"2,联通,北京,北京,cn2,http://127.0.0.1/ping,http://127.0.0.1/file,1\n" +
		"3,移动,广州,广州,cn3,https://8.8.8.8/ping,,1\n" +
		"4,电信,天津,天津,cn4,https://8.8.8.8/ping,https://8.8.8.8/file,0\n"
	client := &http.Client{Transport: fixtureRoundTripper(func(request *http.Request) (*http.Response, error) {
		body := csvBody
		switch request.URL.Path {
		case "/ping":
			body = "pong"
		case "/file":
			body = "abcdefgh"
		}
		if request.URL.Path == "/read-error" {
			return fixtureResponse(http.StatusOK, fixtureReadError{}), nil
		}
		status := http.StatusOK
		if request.URL.Path == "/bad" {
			status = http.StatusServiceUnavailable
		}
		return fixtureResponse(status, io.NopCloser(strings.NewReader(body))), nil
	})}
	nodes, err := fetchCNNodes(context.Background(), client, "fixture")
	if err != nil || len(nodes) != 1 || nodes[0].ID != "1" || nodes[0].Operator != "电信" {
		t.Fatalf("filtered nodes = %+v, err=%v", nodes, err)
	}
	selected := measureCarrier(context.Background(), Environment{HTTPClient: client, UserAgent: "fixture"}, nodes, "电信", 1)
	if selected.Node.ID != "1" || selected.Tried != 1 || selected.Err != "" {
		t.Fatalf("carrier result = %+v", selected)
	}
	nodes[0].PingURL = "https://8.8.8.8/bad"
	if _, err := cnPingNode(context.Background(), client, "fixture", nodes[0]); err == nil || !strings.Contains(err.Error(), "HTTP 503") {
		t.Fatalf("ping failure = %v", err)
	}
	nodes[0].PingURL = "https://8.8.8.8/ping"
	if mbps, bytes, err := cnDownload(context.Background(), client, "fixture", nodes[0], time.Second, 4); err != nil || bytes != 4 || mbps <= 0 {
		t.Fatalf("download result = %v Mbps/%d bytes, err=%v", mbps, bytes, err)
	}
	if mbps, bytes, err := cnDownload(context.Background(), client, "fixture", nodes[0], time.Second, 64); err != nil || bytes != 8 || mbps <= 0 {
		t.Fatalf("download EOF result = %v Mbps/%d bytes, err=%v", mbps, bytes, err)
	}
	nodes[0].DownloadURL = "https://8.8.8.8/read-error"
	if _, _, err := cnDownload(context.Background(), client, "fixture", nodes[0], time.Second, 4); err == nil || !strings.Contains(err.Error(), "fixture read failure") {
		t.Fatalf("download read failure = %v", err)
	}
	csvBody = "id,operator,province,city,host,pingUrl,downloadUrl,active\n"
	if _, err := fetchCNNodes(context.Background(), client, "fixture"); err == nil || !strings.Contains(err.Error(), "清单解析失败") {
		t.Fatalf("empty node list error = %v", err)
	}
	csvBody += "2,联通,北京,北京,cn2,http://127.0.0.1/ping,http://127.0.0.1/file,1\n"
	if _, err := fetchCNNodes(context.Background(), client, "fixture"); err == nil || !strings.Contains(err.Error(), "没有可用节点") {
		t.Fatalf("filtered node list error = %v", err)
	}
}

func TestCNSpeedNodeListSizeBoundary(t *testing.T) {
	oldURL := cnNodeListURLForTest
	defer func() { cnNodeListURLForTest = oldURL }()
	cnNodeListURLForTest = "https://fixture.invalid/CN.csv"
	base := "id,operator,province,city,host,pingUrl,downloadUrl,active\n" +
		"1,电信,上海,上海,cn1,https://8.8.8.8/ping,https://8.8.8.8/file,1\n"
	client := &http.Client{Transport: fixtureRoundTripper(func(_ *http.Request) (*http.Response, error) {
		return fixtureResponse(http.StatusOK, io.NopCloser(strings.NewReader(""))), nil
	})}
	for _, test := range []struct {
		name      string
		size      int
		wantNodes bool
	}{
		{name: "limit minus one", size: int(cnNodeListMaxBytes - 1), wantNodes: true},
		{name: "limit", size: int(cnNodeListMaxBytes), wantNodes: true},
		{name: "limit plus one", size: int(cnNodeListMaxBytes + 1), wantNodes: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			body := base + strings.Repeat("\n", test.size-len(base))
			client.Transport = fixtureRoundTripper(func(_ *http.Request) (*http.Response, error) {
				response := fixtureResponse(http.StatusOK, io.NopCloser(strings.NewReader(body)))
				response.ContentLength = int64(len(body))
				return response, nil
			})
			nodes, err := fetchCNNodes(context.Background(), client, "fixture")
			if test.wantNodes {
				if err != nil || len(nodes) != 1 {
					t.Fatalf("node list size %d = nodes:%+v err:%v", test.size, nodes, err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), "清单过大") {
				t.Fatalf("oversize node list = nodes:%+v err:%v", nodes, err)
			}
		})
	}
	client.Transport = fixtureRoundTripper(func(_ *http.Request) (*http.Response, error) {
		response := fixtureResponse(http.StatusOK, io.NopCloser(strings.NewReader(base)))
		response.ContentLength = int64(len(base) + 1)
		return response, nil
	})
	if _, err := fetchCNNodes(context.Background(), client, "fixture"); err == nil || !strings.Contains(err.Error(), "清单读取被截断") {
		t.Fatalf("truncated node list = %v", err)
	}
}

func TestCNSpeedURLAndDialSafety(t *testing.T) {
	for _, test := range []struct {
		name string
		url  string
		want bool
	}{
		{name: "public HTTPS", url: "https://8.8.8.8/path", want: true},
		{name: "private address", url: "http://127.0.0.1/path"},
		{name: "link-local address", url: "http://169.254.1.1/path"},
		{name: "unspecified address", url: "http://0.0.0.0/path"},
		{name: "unsupported scheme", url: "ftp://8.8.8.8/path"},
		{name: "userinfo", url: "https://user@8.8.8.8/path"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := validateCNNodeURL(test.url)
			if (err == nil) != test.want {
				t.Fatalf("validateCNNodeURL(%q) err=%v, want valid=%v", test.url, err, test.want)
			}
		})
	}
	public := netip.MustParseAddr("8.8.8.8")
	private := netip.MustParseAddr("10.0.0.1")
	addresses, err := cnResolvePublicAddresses(context.Background(), cnFixtureResolver{addresses: []netip.Addr{private, public}}, "fixture.invalid", config.IPVersion4)
	if err != nil || len(addresses) != 1 || addresses[0] != public {
		t.Fatalf("resolved public addresses = %v, err=%v", addresses, err)
	}
	var network, address string
	dial := func(_ context.Context, gotNetwork, gotAddress string) (net.Conn, error) {
		network, address = gotNetwork, gotAddress
		return nil, nil
	}
	if _, err := cnSafeDialContext(config.IPVersion4, cnFixtureResolver{addresses: []netip.Addr{public}}, dial)(context.Background(), "tcp", "fixture.invalid:443"); err != nil {
		t.Fatalf("safe dial = %v", err)
	}
	if network != "tcp4" || address != "8.8.8.8:443" {
		t.Fatalf("safe dial target = %s %s", network, address)
	}
	if _, err := cnSafeDialContext(config.IPVersion4, cnFixtureResolver{addresses: []netip.Addr{public}}, func(context.Context, string, string) (net.Conn, error) {
		return nil, errors.New("fixture dial")
	})(context.Background(), "tcp", "fixture.invalid:443"); err == nil || !strings.Contains(err.Error(), "公网节点连接失败") || !strings.Contains(err.Error(), "fixture dial") {
		t.Fatalf("all dial attempts failed = %v", err)
	}
	if _, err := cnResolvePublicAddresses(context.Background(), cnFixtureResolver{err: io.ErrUnexpectedEOF}, "fixture.invalid", config.IPVersion4); err == nil || !strings.Contains(err.Error(), "DNS 解析失败") {
		t.Fatalf("resolver failure = %v", err)
	}
	if _, err := cnResolvePublicAddresses(context.Background(), cnFixtureResolver{addresses: []netip.Addr{private}}, "fixture.invalid", config.IPVersion4); err == nil || !strings.Contains(err.Error(), "公网地址") {
		t.Fatalf("no public address = %v", err)
	}
	if _, err := cnResolvePublicAddresses(context.Background(), cnFixtureResolver{}, "8.8.8.8", config.IPVersion6); err == nil || !strings.Contains(err.Error(), "IP 版本") {
		t.Fatalf("family mismatch = %v", err)
	}
}
