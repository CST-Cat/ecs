package probe

import (
	"context"
	"encoding/binary"
	"net"
	"testing"
	"time"

	"ecs/internal/config"
)

func TestBuildSTUNRequest(t *testing.T) {
	packet, transaction, err := buildSTUNRequest(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(packet) != 20 {
		t.Fatalf("不带属性的请求应为 20 字节，实际 %d", len(packet))
	}
	if binary.BigEndian.Uint16(packet[0:2]) != stunBindingRequest {
		t.Fatalf("消息类型 = %#x", binary.BigEndian.Uint16(packet[0:2]))
	}
	if binary.BigEndian.Uint16(packet[2:4]) != 0 {
		t.Fatal("不带属性时消息长度必须为 0")
	}
	if binary.BigEndian.Uint32(packet[4:8]) != stunMagicCookie {
		t.Fatal("magic cookie 错误")
	}
	if string(packet[8:20]) != string(transaction[:]) {
		t.Fatal("事务 ID 未写入报文")
	}

	// CHANGE-REQUEST 必须体现在消息长度与属性体里。
	changePacket, _, err := buildSTUNRequest(changeIP | changePort)
	if err != nil {
		t.Fatal(err)
	}
	if len(changePacket) != 28 {
		t.Fatalf("带 CHANGE-REQUEST 的请求应为 28 字节，实际 %d", len(changePacket))
	}
	if binary.BigEndian.Uint16(changePacket[2:4]) != 8 {
		t.Fatalf("消息长度 = %d，want 8", binary.BigEndian.Uint16(changePacket[2:4]))
	}
	if binary.BigEndian.Uint16(changePacket[20:22]) != attrChangeRequest {
		t.Fatal("属性类型不是 CHANGE-REQUEST")
	}
	if got := binary.BigEndian.Uint32(changePacket[24:28]); got != changeIP|changePort {
		t.Fatalf("CHANGE-REQUEST 标志 = %#x", got)
	}

	// 两次请求的事务 ID 必须不同，否则迟到响应会被错认。
	_, second, err := buildSTUNRequest(0)
	if err != nil {
		t.Fatal(err)
	}
	if transaction == second {
		t.Fatal("事务 ID 重复")
	}
}

// buildTestSTUNResponse 按 RFC 5389 组装一个 Binding 响应，用于解析器测试。
func buildTestSTUNResponse(transaction [12]byte, mapped netAddr, other netAddr) []byte {
	var body []byte
	appendAddress := func(attrType uint16, address netAddr, xorEncoded bool) {
		if !address.valid() {
			return
		}
		ip := net.ParseIP(address.IP).To4()
		if ip == nil {
			return
		}
		value := make([]byte, 8)
		value[0] = 0
		value[1] = 0x01
		port := uint16(address.Port)
		raw := make([]byte, 4)
		copy(raw, ip)
		if xorEncoded {
			port ^= uint16(stunMagicCookie >> 16)
			var cookie [4]byte
			binary.BigEndian.PutUint32(cookie[:], stunMagicCookie)
			for i := range raw {
				raw[i] ^= cookie[i]
			}
		}
		binary.BigEndian.PutUint16(value[2:4], port)
		copy(value[4:8], raw)
		header := make([]byte, 4)
		binary.BigEndian.PutUint16(header[0:2], attrType)
		binary.BigEndian.PutUint16(header[2:4], uint16(len(value)))
		body = append(body, header...)
		body = append(body, value...)
	}
	appendAddress(attrXORMappedAddress, mapped, true)
	appendAddress(attrOtherAddress, other, false)

	packet := make([]byte, 20)
	binary.BigEndian.PutUint16(packet[0:2], stunBindingResponse)
	binary.BigEndian.PutUint16(packet[2:4], uint16(len(body)))
	binary.BigEndian.PutUint32(packet[4:8], stunMagicCookie)
	copy(packet[8:20], transaction[:])
	return append(packet, body...)
}

func TestParseSTUNResponse(t *testing.T) {
	transaction := [12]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}
	mapped := netAddr{IP: "203.0.113.45", Port: 54321}
	other := netAddr{IP: "198.51.100.9", Port: 3479}
	packet := buildTestSTUNResponse(transaction, mapped, other)

	result, err := parseSTUNResponse(packet, transaction)
	if err != nil {
		t.Fatal(err)
	}
	if result.Mapped != mapped {
		t.Fatalf("XOR-MAPPED-ADDRESS 还原 = %+v, want %+v", result.Mapped, mapped)
	}
	if result.Other != other {
		t.Fatalf("OTHER-ADDRESS = %+v, want %+v", result.Other, other)
	}

	// 事务 ID 不匹配必须拒绝：UDP 上谁都能往这个端口发包。
	var forged [12]byte
	forged[0] = 0xFF
	if _, err := parseSTUNResponse(packet, forged); err == nil {
		t.Fatal("事务 ID 不匹配的响应必须被拒绝")
	}

	// magic cookie 被篡改同样要拒绝。
	broken := append([]byte(nil), packet...)
	broken[4] ^= 0xFF
	if _, err := parseSTUNResponse(broken, transaction); err == nil {
		t.Fatal("magic cookie 错误的响应必须被拒绝")
	}

	// 截断报文不能 panic，也不能返回成功。
	for cut := 0; cut < len(packet); cut++ {
		if _, err := parseSTUNResponse(packet[:cut], transaction); err == nil && cut < 20 {
			t.Fatalf("长度 %d 的截断报文不应解析成功", cut)
		}
	}

	// 声明长度超过实际数据时不能越界读取。
	lying := append([]byte(nil), packet...)
	binary.BigEndian.PutUint16(lying[2:4], 0xFFFF)
	if _, err := parseSTUNResponse(lying, transaction); err != nil {
		t.Logf("超长声明被拒绝：%v", err)
	}
}

// 不带 XOR 的老式 MAPPED-ADDRESS 也要能解析，但不得覆盖 XOR 版本。
func TestParseSTUNResponsePrefersXORMapped(t *testing.T) {
	transaction := [12]byte{9, 9, 9}
	xorMapped := netAddr{IP: "203.0.113.45", Port: 1234}
	packet := buildTestSTUNResponse(transaction, xorMapped, netAddr{})

	plain := make([]byte, 12)
	binary.BigEndian.PutUint16(plain[0:2], attrMappedAddress)
	binary.BigEndian.PutUint16(plain[2:4], 8)
	plain[5] = 0x01
	binary.BigEndian.PutUint16(plain[6:8], 4321)
	copy(plain[8:12], net.ParseIP("192.0.2.7").To4())
	packet = append(packet, plain...)
	binary.BigEndian.PutUint16(packet[2:4], uint16(len(packet)-20))

	result, err := parseSTUNResponse(packet, transaction)
	if err != nil {
		t.Fatal(err)
	}
	if result.Mapped != xorMapped {
		t.Fatalf("应优先采用 XOR-MAPPED-ADDRESS，实际 = %+v", result.Mapped)
	}
}

func TestNATCategory(t *testing.T) {
	cases := []struct {
		name    string
		finding natFinding
		behind  bool
		want    string
	}{
		{"公网直连", natFinding{Mapping: mappingEndpointIndependent}, false, "公网直连（无 NAT）"},
		{"全锥", natFinding{Mapping: mappingEndpointIndependent, Filtering: filteringEndpointIndependent}, true, "全锥型 NAT（NAT1）"},
		{"受限锥", natFinding{Mapping: mappingEndpointIndependent, Filtering: filteringAddressDependent}, true, "受限锥型 NAT（NAT2）"},
		{"端口受限锥", natFinding{Mapping: mappingEndpointIndependent, Filtering: filteringAddressPortDependent}, true, "端口受限锥型 NAT（NAT3）"},
		{"对称", natFinding{Mapping: mappingAddressPortDependent}, true, "对称型 NAT（NAT4）"},
		{"地址相关也是对称", natFinding{Mapping: mappingAddressDependent}, true, "对称型 NAT（NAT4）"},
	}
	for _, testCase := range cases {
		got, note := natCategory(testCase.finding, testCase.behind)
		if got != testCase.want {
			t.Errorf("%s: natCategory = %q, want %q", testCase.name, got, testCase.want)
		}
		if note == "" {
			t.Errorf("%s: 分类必须附带说明", testCase.name)
		}
	}

	// 过滤行为未知时不能凑出一个具体的 NAT 等级。
	got, note := natCategory(natFinding{Mapping: mappingEndpointIndependent, Filtering: filteringUnknown}, true)
	if got == "全锥型 NAT（NAT1）" || got == "端口受限锥型 NAT（NAT3）" {
		t.Fatalf("过滤行为未知时不得给出确定等级：%q", got)
	}
	if note == "" {
		t.Fatal("未判定的情况更需要说明")
	}

	// 映射行为也未知时只能说"位于 NAT 之后"。
	if got, _ := natCategory(natFinding{Mapping: mappingUnknown, Filtering: filteringUnknown}, true); got != "位于 NAT 之后（类型未判定）" {
		t.Fatalf("证据不足时的结论 = %q", got)
	}
}

// startTestSTUNServer 在回环起一个最小 STUN 服务器，用于端到端验证事务流程。
//
// 用真实 UDP 往返而不是直接调用解析函数：这样能覆盖发包、超时、事务 ID 校验
// 和迟到响应过滤，而且仍然不依赖公网。
func startTestSTUNServer(t *testing.T, other netAddr) *net.UDPAddr {
	return startTestSTUNServerWithMode(t, other, false)
}

// startTestSTUNServerWithMode 的 ignoreChangeRequest 为真时，服务器会像
// stun.l.google.com、stun.cloudflare.com 那样忽略 CHANGE-REQUEST 属性、
// 照常从原地址回复，用于验证这种情况不会被误判成过滤行为端点无关。
func startTestSTUNServerWithMode(t *testing.T, other netAddr, ignoreChangeRequest bool) *net.UDPAddr {
	t.Helper()
	connection, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	go func() {
		buffer := make([]byte, stunMaxResponse)
		for {
			count, source, err := connection.ReadFromUDP(buffer)
			if err != nil {
				return
			}
			if count < 20 {
				continue
			}
			var transaction [12]byte
			copy(transaction[:], buffer[8:20])
			// 带 CHANGE-REQUEST 的请求默认不回应，模拟服务器禁用该属性；
			// ignoreChangeRequest 模式下则照常从原地址回复，模拟服务器忽略该属性。
			if binary.BigEndian.Uint16(buffer[2:4]) > 0 && !ignoreChangeRequest {
				continue
			}
			mapped := netAddr{IP: source.IP.String(), Port: source.Port}
			_, _ = connection.WriteToUDP(buildTestSTUNResponse(transaction, mapped, other), source)
		}
	}()
	return connection.LocalAddr().(*net.UDPAddr)
}

func TestSTUNTransactionAgainstLocalServer(t *testing.T) {
	server := startTestSTUNServer(t, netAddr{IP: "198.51.100.9", Port: 3479})
	client, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	result, err := stunTransaction(client, server, 0, 3*time.Second)
	if err != nil {
		t.Fatalf("本地 STUN 事务失败：%v", err)
	}
	local := client.LocalAddr().(*net.UDPAddr)
	if result.Mapped.Port != local.Port {
		t.Fatalf("映射端口 = %d，want %d", result.Mapped.Port, local.Port)
	}
	if result.Other.Port != 3479 {
		t.Fatalf("OTHER-ADDRESS = %+v", result.Other)
	}
	if result.From.Port != server.Port {
		t.Fatalf("响应源地址 = %+v", result.From)
	}

	// 服务器不回应 CHANGE-REQUEST 时必须超时报错，而不是返回零值当成功。
	start := time.Now()
	if _, err := stunTransaction(client, server, changeIP|changePort, 500*time.Millisecond); err == nil {
		t.Fatal("无响应的 CHANGE-REQUEST 必须返回错误")
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("超时控制失效，耗时 %s", elapsed)
	}
}

// 端到端：服务器不支持 CHANGE-REQUEST 时，过滤行为必须报未知而不是硬判。
func TestProbeNATReportsUnknownFilteringHonestly(t *testing.T) {
	server := startTestSTUNServer(t, netAddr{IP: "198.51.100.9", Port: 3479})
	finding := probeNAT(context.Background(), config.Endpoint{
		Name:    "local",
		Address: server.String(),
	})
	if finding.Err != nil {
		t.Fatalf("本地探测失败：%v", finding.Err)
	}
	if !finding.Mapped.valid() {
		t.Fatal("未拿到映射地址")
	}
	if finding.FilteringTested {
		t.Fatal("测试服务器从不回应 CHANGE-REQUEST，不应标记为已测出过滤行为")
	}
	if finding.Filtering != filteringUnknown {
		t.Fatalf("过滤行为 = %q，服务器不配合时必须是未知", finding.Filtering)
	}
	// 备用地址与主地址不同 IP，但那是个不可达的文档地址，映射行为测不出来。
	// 关键是不能因此编造一个映射行为。
	if finding.Mapping == mappingAddressDependent || finding.Mapping == mappingAddressPortDependent {
		t.Fatalf("备用地址不可达时不得推断映射行为：%q", finding.Mapping)
	}
}

func TestNATServerPoolIsWellFormed(t *testing.T) {
	cfg, err := config.Defaults(config.ProfileStandard)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.STUNServers) == 0 {
		t.Fatal("standard 档必须配置 STUN 服务器")
	}
	if err := config.Validate(cfg); err != nil {
		t.Fatalf("默认 STUN 清单未通过校验：%v", err)
	}
	seen := make(map[string]bool)
	for _, server := range cfg.STUNServers {
		if seen[server.Address] {
			t.Fatalf("STUN 服务器重复：%s", server.Address)
		}
		seen[server.Address] = true
		host, port, err := net.SplitHostPort(server.Address)
		if err != nil || host == "" || port == "" {
			t.Fatalf("STUN 地址格式错误：%s", server.Address)
		}
	}
}

// 服务器忽略 CHANGE-REQUEST、照常从原地址回包时，绝不能判成"过滤行为端点无关"。
//
// 这是实网测试抓到的真实缺陷：stun.l.google.com 与 stun.cloudflare.com 都没有
// 备用地址，却对 CHANGE-REQUEST 正常回包，早先只看"有无响应"的实现会把它们
// 判成全锥型 NAT1——把一台对称型 NAT 后的机器报成 P2P 友好，是会误导人的结论。
func TestProbeNATRejectsIgnoredChangeRequest(t *testing.T) {
	server := startTestSTUNServerWithMode(t, netAddr{}, true)
	finding := probeNAT(context.Background(), config.Endpoint{Name: "ignores-change", Address: server.String()})
	if finding.Err != nil {
		t.Fatalf("本地探测失败：%v", finding.Err)
	}
	if finding.FilteringTested {
		t.Fatal("响应来自原地址时不得认定过滤行为已测出")
	}
	if finding.Filtering != filteringUnknown {
		t.Fatalf("过滤行为 = %q，want 未知", finding.Filtering)
	}
	if !finding.ChangeRequestIgnored {
		t.Fatal("应记录服务器忽略了 CHANGE-REQUEST")
	}
	// 最终分类也不能因此升级成全锥型。
	if category, _ := natCategory(finding, true); category == "全锥型 NAT（NAT1）" {
		t.Fatalf("忽略 CHANGE-REQUEST 的服务器不得推出 NAT1：%q", category)
	}
}
