package probe

import (
	"bytes"
	"context"
	"encoding/binary"
	"net"
	"reflect"
	"strings"
	"testing"
	"time"

	"ecs/internal/config"
	"ecs/internal/i18n"
	"ecs/internal/model"
	"ecs/internal/report"
	"ecs/internal/termcolor"
)

func buildTestSTUNResponse(transaction [12]byte, mapped netAddr) []byte {
	ip := mapped.IP
	address := net.ParseIP(ip).To4()
	if address == nil {
		return nil
	}
	value := make([]byte, 8)
	value[1] = 1
	binary.BigEndian.PutUint16(value[2:4], uint16(mapped.Port)^uint16(stunMagicCookie>>16))
	var cookie [4]byte
	binary.BigEndian.PutUint32(cookie[:], stunMagicCookie)
	for index, octet := range address {
		value[4+index] = octet ^ cookie[index]
	}
	packet := make([]byte, 20, 32)
	binary.BigEndian.PutUint16(packet[0:2], stunBindingResponse)
	binary.BigEndian.PutUint16(packet[2:4], 12)
	binary.BigEndian.PutUint32(packet[4:8], stunMagicCookie)
	copy(packet[8:20], transaction[:])
	attribute := make([]byte, 12)
	binary.BigEndian.PutUint16(attribute[0:2], attrXORMappedAddress)
	binary.BigEndian.PutUint16(attribute[2:4], 8)
	copy(attribute[4:], value)
	return append(packet, attribute...)
}

func TestSTUNMessageParsesMappingAndClassifiesUnknownFiltering(t *testing.T) {
	transaction := [12]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}
	want := netAddr{IP: "203.0.113.45", Port: 54321}
	result, err := parseSTUNResponse(buildTestSTUNResponse(transaction, want), transaction)
	if err != nil || result.Mapped != want {
		t.Fatalf("STUN mapped address = %+v, err=%v", result.Mapped, err)
	}

	category, note := natCategory(natFinding{Mapping: mappingEndpointIndependent, Filtering: filteringUnknown}, true)
	if !strings.Contains(category, "锥型") || note == "" {
		t.Fatalf("NAT classification = %q, note=%q", category, note)
	}
	for _, test := range []struct {
		name, want string
		finding    natFinding
		behind     bool
	}{
		{name: "public", finding: natFinding{Mapping: mappingEndpointIndependent}, want: "公网直连", behind: false},
		{name: "symmetric", finding: natFinding{Mapping: mappingAddressDependent}, want: "NAT4", behind: true},
		{name: "full cone", finding: natFinding{Mapping: mappingEndpointIndependent, Filtering: filteringEndpointIndependent}, want: "NAT1", behind: true},
		{name: "restricted cone", finding: natFinding{Mapping: mappingEndpointIndependent, Filtering: filteringAddressDependent}, want: "NAT2", behind: true},
		{name: "port restricted", finding: natFinding{Mapping: mappingEndpointIndependent, Filtering: filteringAddressPortDependent}, want: "NAT3", behind: true},
		{name: "unknown mapping", finding: natFinding{Mapping: mappingUnknown}, want: "类型未判定", behind: true},
	} {
		category, note := natCategory(test.finding, test.behind)
		if !strings.Contains(category, test.want) || note == "" {
			t.Errorf("%s category = %q, note=%q", test.name, category, note)
		}
	}
	wrongTransaction := transaction
	wrongTransaction[0]++
	noMapping := make([]byte, 20)
	binary.BigEndian.PutUint16(noMapping[0:2], stunBindingResponse)
	binary.BigEndian.PutUint32(noMapping[4:8], stunMagicCookie)
	copy(noMapping[8:20], transaction[:])
	for _, test := range []struct {
		name, marker string
		packet       []byte
		id           [12]byte
	}{
		{name: "short", marker: "过短", packet: []byte{1}, id: transaction},
		{name: "transaction", marker: "事务 ID", packet: buildTestSTUNResponse(transaction, want), id: wrongTransaction},
		{name: "missing mapping", marker: "未包含映射地址", packet: noMapping, id: transaction},
	} {
		if _, err := parseSTUNResponse(test.packet, test.id); err == nil || !strings.Contains(err.Error(), test.marker) {
			t.Errorf("%s STUN error = %v", test.name, err)
		}
	}
}

func TestNATWithoutServersSkipsWithoutNetwork(t *testing.T) {
	result := (natSemanticProbe{}).Run(context.Background(), Environment{Config: config.Runtime{}})
	if result.Status != model.StatusSkipped || len(result.SummaryMessages) != 1 ||
		result.SummaryMessages[0].Key != "probe.nat.summary.skipped" || result.Evidence == nil || result.Evidence.Valid != 0 {
		t.Fatalf("NAT no-server result = %+v", result)
	}
	if len(result.Tables) != 0 {
		t.Fatalf("NAT no-server result unexpectedly has tables: %+v", result.Tables)
	}
}

func startTestSTUNServer(t *testing.T) string {
	t.Helper()
	connection, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	go func() {
		packet := make([]byte, stunMaxResponse)
		for {
			count, source, err := connection.ReadFromUDP(packet)
			if err != nil {
				return
			}
			if count < 20 {
				continue
			}
			var transaction [12]byte
			copy(transaction[:], packet[8:20])
			response := buildTestSTUNResponse(transaction, netAddr{IP: "127.0.0.1", Port: source.Port})
			_, _ = connection.WriteToUDP(response, source)
		}
	}()
	return connection.LocalAddr().String()
}

func natCandidateFixture(t *testing.T, servers []config.Endpoint) model.Report {
	t.Helper()
	result := (natSemanticProbe{}).Run(context.Background(), Environment{Config: config.Runtime{STUNServers: servers}})
	data := model.Report{
		SchemaVersion: "ecs.report/v1",
		Tool:          model.ToolInfo{Name: "ecs", Version: "test"},
		Run: model.RunInfo{
			ID: "nat-fixture", Profile: "standard", StartedAt: time.Unix(0, 0).UTC(),
			Exposure: "local", Offline: false,
		},
		Summary: model.Summary{Status: result.Status, Warnings: 1},
		Results: []model.Result{result},
	}
	return data
}

func natPoolTable(t *testing.T, result model.Result) model.Table {
	t.Helper()
	for _, table := range result.Tables {
		if table.Key == "network.nat.stun_pool" {
			return table
		}
	}
	t.Fatalf("NAT result has no candidate pool table: %+v", result.Tables)
	return model.Table{}
}

func TestNATSemanticPoolRetainsOrderedCandidatesAfterEarlyStop(t *testing.T) {
	servers := []config.Endpoint{
		{Name: "Alpha STUN", Address: startTestSTUNServer(t)},
		{Name: "Beta STUN", Address: "beta.example:bad"},
	}
	data := natCandidateFixture(t, servers)
	result := data.Results[0]
	pool := natPoolTable(t, result)
	if pool.Title != "probe.nat.table.stun_pool" ||
		!reflect.DeepEqual(pool.Columns, []string{"probe.nat.column.server_name", "probe.nat.column.server_address"}) ||
		!reflect.DeepEqual(pool.ColumnKeys, []string{"server_name", "server_address"}) ||
		pool.RowIdentity != "" || len(pool.SensitiveColumns) != 0 {
		t.Fatalf("candidate pool shape = %+v", pool)
	}
	wantRows := [][]string{{servers[0].Name, servers[0].Address}, {servers[1].Name, servers[1].Address}}
	if !reflect.DeepEqual(pool.Rows, wantRows) {
		t.Fatalf("candidate pool rows = %v, want %v", pool.Rows, wantRows)
	}
	if len(result.Tables) != 2 || len(result.Tables[0].Rows) != 1 {
		t.Fatalf("early-stop probe details = %+v", result.Tables)
	}
	for _, field := range result.Fields {
		if field.Key == "stun_pool" {
			t.Fatalf("legacy scalar stun_pool field remains: %+v", result.Fields)
		}
	}
}

func TestNATSemanticPoolRetainsCandidatesWhenAllAttemptsFail(t *testing.T) {
	servers := []config.Endpoint{
		{Name: "Alpha STUN", Address: "alpha.example:bad"},
		{Name: "Beta STUN", Address: "beta.example:bad"},
	}
	data := natCandidateFixture(t, servers)
	result := data.Results[0]
	if result.Status != model.StatusWarning || result.Evidence == nil || result.Evidence.Valid != 0 {
		t.Fatalf("all-failed NAT result = %+v", result)
	}
	pool := natPoolTable(t, result)
	if len(pool.Rows) != len(servers) {
		t.Fatalf("all-failed candidate pool = %v, want %d rows", pool.Rows, len(servers))
	}
	for index, server := range servers {
		if got := pool.Rows[index]; len(got) != 2 || got[0] != server.Name || got[1] != server.Address {
			t.Fatalf("all-failed pool row %d = %v", index, got)
		}
	}
}

func TestNATCandidatePoolRendersLocalizedWithoutMutatingCanonical(t *testing.T) {
	servers := []config.Endpoint{
		{Name: "Alpha STUN", Address: "alpha.example:bad"},
		{Name: "Beta STUN", Address: "beta.example:bad"},
	}
	data := natCandidateFixture(t, servers)
	before, err := report.JSON(data)
	if err != nil {
		t.Fatal(err)
	}
	originalLanguage := i18n.Current()
	t.Cleanup(func() { i18n.Set(originalLanguage) })
	for _, language := range []struct {
		lang    i18n.Lang
		markers []string
	}{
		{lang: i18n.LangZH, markers: []string{"STUN 候选服务器", "服务器名称", "服务器地址"}},
		{lang: i18n.LangEN, markers: []string{"STUN candidate servers", "Server name", "Server address"}},
	} {
		i18n.Set(language.lang)
		text := report.Text(data, report.TextOptions{Color: termcolor.LevelNone, Width: 120})
		markdown := report.Markdown(data, nil)
		html, err := report.HTML(data, nil)
		if err != nil {
			t.Fatalf("HTML %s: %v", language.lang, err)
		}
		outputs := []string{text, markdown, string(html)}
		for format, output := range outputs {
			for _, marker := range append(append([]string{}, language.markers...), servers[0].Name, servers[0].Address, servers[1].Name, servers[1].Address) {
				if !strings.Contains(output, marker) {
					t.Fatalf("%s format %d output missing %q:\n%s", language.lang, format, marker, output)
				}
			}
			for _, forbidden := range []string{"network.nat.stun_pool", "probe.nat.column.server_name", "%!"} {
				if strings.Contains(output, forbidden) {
					t.Fatalf("%s format %d output contains forbidden %q:\n%s", language.lang, format, forbidden, output)
				}
			}
			if language.lang == i18n.LangEN {
				if strings.Contains(output, "、") {
					t.Fatalf("English format %d output contains Chinese list punctuation:\n%s", format, output)
				}
				for _, runeValue := range output {
					if runeValue >= '\u3400' && runeValue <= '\u9fff' {
						t.Fatalf("English format %d output contains ECS Han text %q:\n%s", format, runeValue, output)
					}
				}
			}
		}
		after, err := report.JSON(data)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(before, after) {
			t.Fatalf("%s rendering mutated canonical report", language.lang)
		}
	}
}
