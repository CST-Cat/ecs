package probe

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"syscall"
	"testing"
	"time"
)

// 样本按 spiritLHLS/speedtest.cn-CN-ID 的真实表头构造，字段顺序与实际一致。
const realCNNodeCSV = `id,active,https,cros,preferred,host,country_code,province,city,ver,operator,lon,lat,times,high_speed,sponsor,sponsor_url,current_version,distance,pingUrl,downloadUrl,uploadUrl,websocketUrl
427880,1,0,1,0,5gnanjing.speedtest.jsinfo.net:8080,CN,江苏,南京,5,电信,118.7,32.0,0,0,江苏电信,,,,http://5gnanjing.speedtest.jsinfo.net:8080/hello,http://5gnanjing.speedtest.jsinfo.net:8080/download,http://5gnanjing.speedtest.jsinfo.net:8080/upload,
430074,1,1,1,0,node-124-160-78-98.speedtest.cn:51090,CN,山西省,太原市,5,联通,112.5,37.8,0,0,山西联通,,,,https://node-124-160-78-98.speedtest.cn:51090/hello,https://node-124-160-78-98.speedtest.cn:51090/download,https://node-124-160-78-98.speedtest.cn:51090/upload,
429248,1,0,1,0,speedtest.139play.com:8080,CN,江苏,南京,5,移动,118.7,32.0,0,0,江苏移动,,,,http://speedtest.139play.com:8080/hello,http://speedtest.139play.com:8080/download,http://speedtest.139play.com:8080/upload,
999999,0,0,1,0,dead.example.com:8080,CN,广东,深圳,5,电信,113.0,22.5,0,0,已下线,,,,http://dead.example.com:8080/hello,http://dead.example.com:8080/download,http://dead.example.com:8080/upload,
888888,1,0,1,0,nourl.example.com:8080,CN,北京,北京,5,联通,116.4,39.9,0,0,缺字段,,,,,,,
777777,1,0,1,0,local.example:8080,CN,北京,北京,5,联通,116.4,39.9,0,0,内网目标,,,,http://127.0.0.1/hello,http://127.0.0.1/download,,
666666,1,0,1,0,file.example:8080,CN,北京,北京,5,联通,116.4,39.9,0,0,非 HTTP,,,,file:///etc/passwd,gopher://example.com/download,,
`

type cnFakeResolver struct {
	addresses []netip.Addr
	err       error
	calls     int
}

type cnDownloadTestBody struct {
	data    []byte
	readErr error
	read    bool
}

func (body *cnDownloadTestBody) Read(buffer []byte) (int, error) {
	if body.read {
		return 0, io.EOF
	}
	body.read = true
	return copy(buffer, body.data), body.readErr
}

func (body *cnDownloadTestBody) Close() error { return nil }

type cnDownloadTestRoundTripper struct {
	target *url.URL
	next   http.RoundTripper
	body   io.ReadCloser
}

func (transport cnDownloadTestRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	routed := request.Clone(request.Context())
	routed.URL.Scheme = transport.target.Scheme
	routed.URL.Host = transport.target.Host
	routed.Host = transport.target.Host
	response, err := transport.next.RoundTrip(routed)
	if err != nil {
		return nil, err
	}
	if transport.body != nil {
		_ = response.Body.Close()
		response.Body = transport.body
	}
	return response, nil
}

func newCNDownloadTestClient(t *testing.T, body io.ReadCloser) *http.Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("fixture"))
	}))
	t.Cleanup(server.Close)
	target, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse test server URL: %v", err)
	}
	return &http.Client{Transport: cnDownloadTestRoundTripper{
		target: target,
		next:   server.Client().Transport,
		body:   body,
	}}
}

func (resolver *cnFakeResolver) LookupNetIP(context.Context, string, string) ([]netip.Addr, error) {
	resolver.calls++
	return append([]netip.Addr(nil), resolver.addresses...), resolver.err
}

func TestCNSpeedUsesFullDepthBudget(t *testing.T) {
	duration, maxBytes := cnSpeedBudget()
	if duration != 8*time.Second || maxBytes != 100*1024*1024 {
		t.Fatalf("cnspeed budget = %s/%d, want 8s/100MiB", duration, maxBytes)
	}
}

func TestCNDownloadReadTermination(t *testing.T) {
	node := cnNode{DownloadURL: "http://speed.example.com/download"}
	tests := []struct {
		name      string
		data      string
		readErr   error
		maxBytes  int64
		wantBytes int64
		wantError bool
	}{
		{
			name:      "EOF after data succeeds",
			data:      "downloaded before EOF",
			readErr:   io.EOF,
			maxBytes:  1024,
			wantBytes: int64(len("downloaded before EOF")),
		},
		{
			name:      "connection reset after partial data fails",
			data:      "partial data",
			readErr:   syscall.ECONNRESET,
			maxBytes:  1024,
			wantBytes: int64(len("partial data")),
			wantError: true,
		},
		{
			name:      "max bytes succeeds",
			data:      "more data than allowed",
			maxBytes:  4,
			wantBytes: 4,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			body := &cnDownloadTestBody{data: []byte(test.data), readErr: test.readErr}
			client := newCNDownloadTestClient(t, body)
			mbps, bytes, err := cnDownload(context.Background(), client, "ecs/test", node, time.Second, test.maxBytes)
			if bytes != test.wantBytes {
				t.Fatalf("bytes = %d, want %d", bytes, test.wantBytes)
			}
			if test.wantError {
				if err == nil {
					t.Fatal("partial read error must fail")
				}
				if !errors.Is(err, syscall.ECONNRESET) {
					t.Fatalf("read error = %v, want connection reset", err)
				}
				if mbps != 0 {
					t.Fatalf("failed read returned Mbps = %f, want 0", mbps)
				}
				return
			}
			if err != nil {
				t.Fatalf("download failed: %v", err)
			}
			if mbps <= 0 {
				t.Fatalf("successful download Mbps = %f, want positive", mbps)
			}
		})
	}
}

func TestCNNodeListIsPinnedToReviewedCommit(t *testing.T) {
	if len(cnNodeListCommit) != 40 || strings.Contains(cnNodeListURL, "/main/") ||
		!strings.Contains(cnNodeListURL, "/"+cnNodeListCommit+"/CN.csv") {
		t.Fatalf("node list URL is not commit-pinned: %q", cnNodeListURL)
	}
}

func TestValidateCNNodeURLRejectsNonHTTPAndSpecialUseTargets(t *testing.T) {
	allowed := []string{
		"http://speed.example.com:8080/download",
		"https://1.1.1.1/download",
		"https://[2606:4700:4700::1111]/download",
	}
	for _, target := range allowed {
		if _, err := validateCNNodeURL(target); err != nil {
			t.Errorf("validateCNNodeURL(%q) rejected safe shape: %v", target, err)
		}
	}
	blocked := []string{
		"", " file:///etc/passwd", "file:///etc/passwd", "gopher://example.com/x",
		"https://user:secret@example.com/x", "https://example.com/x#fragment",
		"https://127.0.0.1/x", "http://169.254.169.254/latest/meta-data",
		"http://10.0.0.1/x", "http://100.64.0.1/x", "http://192.0.2.1/x",
		"http://198.18.0.1/x", "http://[::1]/x", "http://[fc00::1]/x",
		"http://example.com:99999/x",
	}
	for _, target := range blocked {
		if _, err := validateCNNodeURL(target); err == nil {
			t.Errorf("validateCNNodeURL(%q) accepted an unsafe target", target)
		}
	}
}

func TestCNSafeDialRejectsDNSPrivateAndDialsOnlyResolvedPublicLiteral(t *testing.T) {
	privateResolver := &cnFakeResolver{addresses: []netip.Addr{
		netip.MustParseAddr("127.0.0.1"), netip.MustParseAddr("169.254.169.254"),
		netip.MustParseAddr("192.0.2.9"), netip.MustParseAddr("fc00::1"),
	}}
	dialCalls := 0
	client := newCNSpeedHTTPClient(time.Second, "auto", privateResolver, func(context.Context, string, string) (net.Conn, error) {
		dialCalls++
		return nil, errors.New("unexpected dial")
	})
	transport := client.Transport.(*http.Transport)
	if _, err := transport.DialContext(context.Background(), "tcp", "malicious.example:80"); err == nil {
		t.Fatal("DNS answers containing only non-public addresses must be rejected")
	}
	if dialCalls != 0 || privateResolver.calls != 1 {
		t.Fatalf("unsafe DNS result reached dialer: resolver=%d dial=%d", privateResolver.calls, dialCalls)
	}

	publicResolver := &cnFakeResolver{addresses: []netip.Addr{
		netip.MustParseAddr("10.0.0.8"), netip.MustParseAddr("1.1.1.1"),
	}}
	wantErr := errors.New("fixture dial stop")
	var dialedNetwork, dialedAddress string
	client = newCNSpeedHTTPClient(time.Second, "auto", publicResolver, func(_ context.Context, network, address string) (net.Conn, error) {
		dialCalls++
		dialedNetwork, dialedAddress = network, address
		return nil, wantErr
	})
	transport = client.Transport.(*http.Transport)
	if _, err := transport.DialContext(context.Background(), "tcp", "mixed.example:443"); !errors.Is(err, wantErr) {
		t.Fatalf("safe dial error = %v, want wrapped fixture error", err)
	}
	if publicResolver.calls != 1 || dialedNetwork != "tcp4" || dialedAddress != "1.1.1.1:443" {
		t.Fatalf("dial must use the already-validated public literal once: resolver=%d network=%q address=%q",
			publicResolver.calls, dialedNetwork, dialedAddress)
	}
}

func TestCNSpeedRedirectPolicyRejectsPrivateTarget(t *testing.T) {
	client := newCNSpeedHTTPClient(time.Second, "auto", &cnFakeResolver{}, nil)
	request, err := http.NewRequest(http.MethodGet, "http://127.0.0.1/admin", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.CheckRedirect(request, nil); err == nil {
		t.Fatal("redirect to a loopback literal must be rejected before dialing")
	}
}

func TestFetchCNNodesParsesRealFormat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(realCNNodeCSV))
	}))
	defer server.Close()

	// 直接用解析路径：把清单 URL 指向本地服务器。
	original := cnNodeListURLForTest
	cnNodeListURLForTest = server.URL
	defer func() { cnNodeListURLForTest = original }()

	nodes, err := fetchCNNodes(context.Background(), server.Client(), "ecs/test")
	if err != nil {
		t.Fatal(err)
	}
	// active=0 的已下线节点与缺 URL 的条目都必须被剔除。
	if len(nodes) != 3 {
		t.Fatalf("应解析出 3 个可用节点，实际 %d：%+v", len(nodes), nodes)
	}
	byOperator := map[string]cnNode{}
	for _, node := range nodes {
		byOperator[node.Operator] = node
		if node.PingURL == "" || node.DownloadURL == "" {
			t.Fatalf("缺 URL 的节点不应入选：%+v", node)
		}
		if node.ID == "999999" {
			t.Fatal("active=0 的下线节点不应入选")
		}
	}
	for _, carrier := range cnCarriers {
		if _, ok := byOperator[carrier]; !ok {
			t.Errorf("缺少运营商 %s 的节点", carrier)
		}
	}
	telecom := byOperator["电信"]
	if telecom.ID != "427880" || telecom.Province != "江苏" || telecom.City != "南京" {
		t.Fatalf("电信节点字段解析错误：%+v", telecom)
	}
	if !strings.HasSuffix(telecom.PingURL, "/hello") {
		t.Fatalf("pingUrl 解析错误：%s", telecom.PingURL)
	}
}

func TestFetchCNNodesRejectsBadResponses(t *testing.T) {
	// 清单抓不到时必须报错让模块跳过，绝不能退回内置的陈旧快照。
	notFound := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer notFound.Close()
	original := cnNodeListURLForTest
	cnNodeListURLForTest = notFound.URL
	defer func() { cnNodeListURLForTest = original }()
	if _, err := fetchCNNodes(context.Background(), notFound.Client(), "ecs/test"); err == nil {
		t.Fatal("HTTP 404 必须返回错误")
	}

	empty := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("id,operator\n"))
	}))
	defer empty.Close()
	cnNodeListURLForTest = empty.URL
	if _, err := fetchCNNodes(context.Background(), empty.Client(), "ecs/test"); err == nil {
		t.Fatal("空清单必须返回错误")
	}
}

func TestCarrierKey(t *testing.T) {
	want := map[string]string{"电信": "telecom", "联通": "unicom", "移动": "mobile", "教育网": "other"}
	for carrier, expected := range want {
		if got := carrierKey(carrier); got != expected {
			t.Errorf("carrierKey(%q) = %q, want %q", carrier, got, expected)
		}
	}
	// 指标键必须是 ASCII，否则下游按键名处理会很难受。
	for _, carrier := range cnCarriers {
		key := carrierKey(carrier)
		for _, r := range key {
			if r > 127 {
				t.Fatalf("指标键含非 ASCII 字符：%q", key)
			}
		}
	}
}
