package probe

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"ecs/internal/config"
)

func TestAdjacentASNsDeduplicatesASPath(t *testing.T) {
	got := adjacentASNs("64500 64500 {64501,64502} 64503")
	want := []int{64500, 64503}
	if len(got) != len(want) {
		t.Fatalf("adjacentASNs = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("adjacentASNs[%d] = %d, want %d", i, got[i], want[i])
		}
	}
}

func TestQueryRouteViewsPrefixUsesIPv6Width(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/prefix/2001:db8::1/128" {
			t.Fatalf("path = %q", request.URL.Path)
		}
		if request.URL.RawQuery != "" {
			t.Fatalf("query = %q; exact strict matching should not be requested", request.URL.RawQuery)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`[{"prefix":"2001:db8::/32","origin_asn":64500,"rpki_state":"valid","reporting_peers":[{"peer_asn":64501,"collector":"route-views6","as_path":"64501 64500","timestamp":"2026-08-02T00:00:00Z"}]}]`))
	}))
	defer server.Close()

	originalBase, originalInterval, originalLast := routeViewsBaseURL, routeViewsMinInterval, routeViewsGate.last
	defer func() {
		routeViewsBaseURL, routeViewsMinInterval, routeViewsGate.last = originalBase, originalInterval, originalLast
	}()
	routeViewsBaseURL = server.URL
	routeViewsMinInterval = 0
	routeViewsGate.last = time.Time{}

	client := NewHTTPClient(2 * time.Second)
	defer client.CloseIdleConnections()
	got, err := queryRouteViewsPrefix(context.Background(), Environment{HTTPClient: client, UserAgent: "test"}, "2001:db8::1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].OriginASN != 64500 || got[0].ReportingPeers[0].PeerASN != 64501 {
		t.Fatalf("observations = %+v", got)
	}
}

func TestQueryRouteViewsPrefixAcceptsIPv4LongestMatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/prefix/64.23.192.10/32" || request.URL.RawQuery != "" {
			t.Fatalf("request = %s?%s", request.URL.Path, request.URL.RawQuery)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`[{"prefix":"64.23.192.0/19","origin_asn":64500,"rpki_state":"valid"}]`))
	}))
	defer server.Close()

	originalBase, originalInterval, originalLast := routeViewsBaseURL, routeViewsMinInterval, routeViewsGate.last
	defer func() {
		routeViewsBaseURL, routeViewsMinInterval, routeViewsGate.last = originalBase, originalInterval, originalLast
	}()
	routeViewsBaseURL = server.URL
	routeViewsMinInterval = 0
	routeViewsGate.last = time.Time{}

	client := NewHTTPClient(2 * time.Second)
	defer client.CloseIdleConnections()
	got, err := queryRouteViewsPrefix(context.Background(), Environment{HTTPClient: client, UserAgent: "test"}, "64.23.192.10")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Prefix != "64.23.192.0/19" {
		t.Fatalf("longest-match observations = %+v", got)
	}
}

func TestBGPMethodologyIsPublicObservation(t *testing.T) {
	method := MethodologyFor("bgp")
	if method.Engine != "RouteViews current RIB API" || method.Kind != "provider-assessment" {
		t.Fatalf("methodology = %+v", method)
	}
	cfg, err := config.Defaults(config.ProfileStandard)
	if err != nil {
		t.Fatal(err)
	}
	if got := config.SelectModules(cfg.Modules, []string{"ookla"}, nil); len(got) != 1 || got[0] != "ookla" {
		t.Fatalf("explicit ookla selection = %v", got)
	}
}
