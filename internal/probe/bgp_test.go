package probe

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

func TestBGPNormalizationIdentityAndSummaries(t *testing.T) {
	ip := net.ParseIP("203.0.113.9")
	observations := []routeViewsPrefix{
		{Prefix: "0.0.0.0/0", OriginASN: 1},
		{Prefix: "bad", OriginASN: 2},
		{Prefix: "198.51.100.0/24", OriginASN: 3},
		{Prefix: "2001:db8::/32", OriginASN: 4},
		{Prefix: "203.0.0.0/16", OriginASN: 64500},
		{Prefix: "203.0.113.0/24", OriginASN: 64501},
		{Prefix: "203.0.113.0/24", OriginASN: 64502},
	}
	got := normalizeRouteViewsObservations(ip, observations)
	if len(got) != 3 || got[0].Prefix != "203.0.113.0/24" || got[1].Prefix != "203.0.113.0/24" || got[2].Prefix != "203.0.0.0/16" || got[0].OriginASN != 64501 {
		t.Fatalf("normalized IPv4 observations = %+v", got)
	}
	v6 := normalizeRouteViewsObservations(net.ParseIP("2001:db8::9"), []routeViewsPrefix{{Prefix: "2001:db8::/32"}, {Prefix: "203.0.113.0/24"}})
	if len(v6) != 1 || v6[0].Prefix != "2001:db8::/32" {
		t.Fatalf("normalized IPv6 observations = %+v", v6)
	}
	identity, route := egressBGPIdentity([]routeViewsPrefix{{Prefix: "bad", OriginASN: 9}, {Prefix: "203.0.0.0/16", OriginASN: 64500}, {Prefix: "203.0.113.0/24", OriginASN: 64501}, {Prefix: "203.0.113.0/24", OriginASN: 64502}})
	if identity != 64501 || route != "203.0.113.0/24" {
		t.Fatalf("BGP identity = %d/%q", identity, route)
	}
	if identity, route := egressBGPIdentity([]routeViewsPrefix{{Prefix: "bad"}}); identity != 0 || route != "" {
		t.Fatalf("invalid BGP identity = %d/%q", identity, route)
	}

	peers := []routeViewsPeer{
		{PeerASN: 64502, Collector: "rr2", ASPath: "64500 64500 {64501,64502} 64503"},
		{PeerASN: 64501, Collector: "rr1", ASPath: "64504 64505"},
		{PeerASN: 64502, Collector: "rr1", ASPath: "64506"},
		{PeerASN: 0, Collector: "", ASPath: ""},
		{PeerASN: 64507, Collector: "rr3", ASPath: "64508"},
		{PeerASN: 64508, Collector: "rr4", ASPath: "64509"},
	}
	peerText, pathText, collectors, observed := summarizeRouteViews(peers)
	if peerText != "AS64501 · AS64502 · AS64507 · AS64508" || collectors != "rr1 · rr2 · rr3 · rr4" || !strings.Contains(pathText, "64500") || strings.Contains(pathText, "64509") || observed != "AS64500 · AS64503 · AS64504 · AS64505 · AS64506 · AS64508 · AS64509" {
		t.Fatalf("BGP summaries = peers:%q paths:%q collectors:%q observed:%q", peerText, pathText, collectors, observed)
	}
	if strings.Count(pathText, " · ")+1 > 4 {
		t.Fatalf("path summary exceeded four samples: %q", pathText)
	}
	if formatASN(64500) != "AS64500" || formatASN(0) != "unknown" || joinOrDash(nil) != "—" {
		t.Fatal("BGP display helpers failed")
	}

	cached := []routeViewsPrefix{{Prefix: "203.0.0.0/16"}}
	cachedErr := errors.New("cached failure")
	address := EgressAddress{BGPQueried: true, BGPObservations: cached, BGPError: cachedErr}
	gotCached, gotErr := routeViewsObservations(context.Background(), Environment{}, address, "203.0.113.9")
	if !reflect.DeepEqual(gotCached, cached) || !errors.Is(gotErr, cachedErr) {
		t.Fatalf("cached RouteViews result = %+v/%v", gotCached, gotErr)
	}
}

func TestQueryRouteViewsPrefixUsesHTTPAdapter(t *testing.T) {
	oldBase, oldInterval, oldLast := routeViewsBaseURL, routeViewsMinInterval, routeViewsGate.last
	defer func() {
		routeViewsBaseURL, routeViewsMinInterval, routeViewsGate.last = oldBase, oldInterval, oldLast
	}()
	routeViewsBaseURL = "https://routeviews.fixture"
	routeViewsMinInterval = 0
	client := &http.Client{Transport: fixtureRoundTripper(func(*http.Request) (*http.Response, error) {
		return fixtureResponse(http.StatusOK, io.NopCloser(strings.NewReader(`[{"prefix":"203.0.0.0/16","origin_asn":64500},{"prefix":"203.0.113.0/24","origin_asn":64501}]`))), nil
	})}
	env := Environment{HTTPClient: client, UserAgent: "fixture"}
	got, err := queryRouteViewsPrefix(context.Background(), env, "203.0.113.9")
	if err != nil || len(got) != 2 || got[0].Prefix != "203.0.113.0/24" || got[1].Prefix != "203.0.0.0/16" || got[0].OriginASN != 64501 {
		t.Fatalf("RouteViews adapter = %+v/%v", got, err)
	}
	if _, err := queryRouteViewsPrefix(context.Background(), env, "not-an-ip"); err == nil || !strings.Contains(err.Error(), "出口 IP 无效") {
		t.Fatalf("invalid RouteViews IP error = %v", err)
	}
	client.Transport = fixtureRoundTripper(func(*http.Request) (*http.Response, error) {
		return fixtureResponse(http.StatusOK, io.NopCloser(strings.NewReader("["))), nil
	})
	if _, err := queryRouteViewsPrefix(context.Background(), env, "203.0.113.9"); err == nil || !strings.Contains(err.Error(), "解析 RouteViews 响应失败") {
		t.Fatalf("malformed RouteViews response error = %v", err)
	}
}
