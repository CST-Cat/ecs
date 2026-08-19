package probe

import (
	"context"
	"encoding/binary"
	"net"
	"strings"
	"testing"

	"ecs/internal/config"
	"ecs/internal/model"
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
	result := (natProbe{}).Run(context.Background(), Environment{Config: config.Runtime{}})
	if result.Status != model.StatusSkipped || result.Summary != "未配置 STUN 服务器" || result.Evidence == nil || result.Evidence.Valid != 0 {
		t.Fatalf("NAT no-server result = %+v", result)
	}
}
