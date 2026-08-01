package probe

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 样本按 spiritLHLS/speedtest.cn-CN-ID 的真实表头构造，字段顺序与实际一致。
const realCNNodeCSV = `id,active,https,cros,preferred,host,country_code,province,city,ver,operator,lon,lat,times,high_speed,sponsor,sponsor_url,current_version,distance,pingUrl,downloadUrl,uploadUrl,websocketUrl
427880,1,0,1,0,5gnanjing.speedtest.jsinfo.net:8080,CN,江苏,南京,5,电信,118.7,32.0,0,0,江苏电信,,,,http://5gnanjing.speedtest.jsinfo.net:8080/hello,http://5gnanjing.speedtest.jsinfo.net:8080/download,http://5gnanjing.speedtest.jsinfo.net:8080/upload,
430074,1,1,1,0,node-124-160-78-98.speedtest.cn:51090,CN,山西省,太原市,5,联通,112.5,37.8,0,0,山西联通,,,,https://node-124-160-78-98.speedtest.cn:51090/hello,https://node-124-160-78-98.speedtest.cn:51090/download,https://node-124-160-78-98.speedtest.cn:51090/upload,
429248,1,0,1,0,speedtest.139play.com:8080,CN,江苏,南京,5,移动,118.7,32.0,0,0,江苏移动,,,,http://speedtest.139play.com:8080/hello,http://speedtest.139play.com:8080/download,http://speedtest.139play.com:8080/upload,
999999,0,0,1,0,dead.example.com:8080,CN,广东,深圳,5,电信,113.0,22.5,0,0,已下线,,,,http://dead.example.com:8080/hello,http://dead.example.com:8080/download,http://dead.example.com:8080/upload,
888888,1,0,1,0,nourl.example.com:8080,CN,北京,北京,5,联通,116.4,39.9,0,0,缺字段,,,,,,,
`

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
