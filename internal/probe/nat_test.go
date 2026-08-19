package probe

import (
	"encoding/binary"
	"net"
	"strings"
	"testing"
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
}
