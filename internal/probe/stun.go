package probe

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"time"
)

// STUN 协议实现（RFC 5389）与 NAT 行为发现（RFC 5780）。
//
// 为什么自己实现：核心坚持零第三方依赖，而 STUN 的 Binding 请求只有二十字节
// 固定头加几个 TLV 属性，用标准库足够。这里只实现 NAT 行为发现需要的部分，
// 不做 TURN、ICE、认证或消息完整性。
//
// 判定原则与本项目其余模块一致：拿不到证据就报"未知"，绝不硬猜。这一点尤其
// 重要——多数公共 STUN 服务器出于放大攻击的顾虑禁用了 CHANGE-REQUEST，
// 此时过滤行为测试会静默无响应。把"服务器不支持"当成"被 NAT 过滤"就会把
// 一台公网机器误报成 Port-Restricted NAT。

const (
	stunBindingRequest  = uint16(0x0001)
	stunBindingResponse = uint16(0x0101)
	stunMagicCookie     = uint32(0x2112A442)

	// 属性类型。
	attrMappedAddress    = uint16(0x0001)
	attrChangeRequest    = uint16(0x0003)
	attrChangedAddress   = uint16(0x0005) // RFC 3489 对 OTHER-ADDRESS 的旧称
	attrXORMappedAddress = uint16(0x0020)
	attrOtherAddress     = uint16(0x802C)

	// CHANGE-REQUEST 的标志位。
	changePort = uint32(0x02)
	changeIP   = uint32(0x04)

	// STUN 响应远小于此；限制读取长度避免被超大报文拖住。
	stunMaxResponse = 1500
)

// stunResult 是一次 Binding 事务的结果。
type stunResult struct {
	// Mapped 是服务器看到的本机公网映射地址。
	Mapped netAddr
	// Other 是服务器提供的备用地址（RFC 5780 的 OTHER-ADDRESS）。
	Other netAddr
	// From 是实际发回响应的源地址，用于确认 CHANGE-REQUEST 是否生效。
	From netAddr
}

// netAddr 是一个可比较的 IP:端口。
type netAddr struct {
	IP   string
	Port int
}

func (a netAddr) valid() bool { return a.IP != "" && a.Port > 0 }

func (a netAddr) String() string {
	if !a.valid() {
		return ""
	}
	return net.JoinHostPort(a.IP, fmt.Sprint(a.Port))
}

// contextCauseError keeps both the standard cancellation category and a
// caller-provided cause available to failure.Classify and errors.Is callers.
func contextCauseError(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	err := ctx.Err()
	if err == nil {
		if deadline, ok := ctx.Deadline(); !ok || time.Now().Before(deadline) {
			return nil
		}
		err = context.DeadlineExceeded
	}
	cause := context.Cause(ctx)
	if cause == nil || (cause == context.Canceled && err == context.Canceled) || (cause == context.DeadlineExceeded && err == context.DeadlineExceeded) {
		return err
	}
	return errors.Join(err, cause)
}

// buildSTUNRequest 组装一个 Binding 请求。change 为 0 时不带 CHANGE-REQUEST。
func buildSTUNRequest(change uint32) ([]byte, [12]byte, error) {
	var transaction [12]byte
	if _, err := rand.Read(transaction[:]); err != nil {
		return nil, transaction, err
	}
	bodyLength := 0
	if change != 0 {
		bodyLength = 8 // 4 字节属性头 + 4 字节值
	}
	packet := make([]byte, 20, 28)
	binary.BigEndian.PutUint16(packet[0:2], stunBindingRequest)
	binary.BigEndian.PutUint16(packet[2:4], uint16(bodyLength))
	binary.BigEndian.PutUint32(packet[4:8], stunMagicCookie)
	copy(packet[8:20], transaction[:])
	if change != 0 {
		attribute := make([]byte, 8)
		binary.BigEndian.PutUint16(attribute[0:2], attrChangeRequest)
		binary.BigEndian.PutUint16(attribute[2:4], 4)
		binary.BigEndian.PutUint32(attribute[4:8], change)
		packet = append(packet, attribute...)
	}
	return packet, transaction, nil
}

// parseSTUNResponse 解析 Binding 响应。
//
// 事务 ID 不匹配一律丢弃：UDP 上任何人都能往这个端口发包，不校验就等于让
// 第三方决定检测结果。
func parseSTUNResponse(packet []byte, transaction [12]byte) (stunResult, error) {
	var result stunResult
	if len(packet) < 20 {
		return result, errors.New("STUN 响应过短")
	}
	if binary.BigEndian.Uint16(packet[0:2]) != stunBindingResponse {
		return result, errors.New("不是 STUN Binding 响应")
	}
	if binary.BigEndian.Uint32(packet[4:8]) != stunMagicCookie {
		return result, errors.New("STUN magic cookie 不匹配")
	}
	if string(packet[8:20]) != string(transaction[:]) {
		return result, errors.New("STUN 事务 ID 不匹配")
	}
	length := int(binary.BigEndian.Uint16(packet[2:4]))
	end := 20 + length
	if end > len(packet) {
		end = len(packet)
	}
	for offset := 20; offset+4 <= end; {
		attrType := binary.BigEndian.Uint16(packet[offset : offset+2])
		attrLength := int(binary.BigEndian.Uint16(packet[offset+2 : offset+4]))
		valueStart := offset + 4
		valueEnd := valueStart + attrLength
		if valueEnd > end {
			break
		}
		value := packet[valueStart:valueEnd]
		switch attrType {
		case attrXORMappedAddress:
			if address, ok := decodeAddress(value, true, transaction); ok {
				result.Mapped = address
			}
		case attrMappedAddress:
			// 只在 XOR 版本缺失时使用：老服务器可能只发这个。
			if !result.Mapped.valid() {
				if address, ok := decodeAddress(value, false, transaction); ok {
					result.Mapped = address
				}
			}
		case attrOtherAddress, attrChangedAddress:
			if !result.Other.valid() {
				if address, ok := decodeAddress(value, false, transaction); ok {
					result.Other = address
				}
			}
		}
		// 属性按 4 字节对齐填充。
		offset = valueEnd + (4-attrLength%4)%4
	}
	if !result.Mapped.valid() {
		return result, errors.New("STUN 响应未包含映射地址")
	}
	return result, nil
}

// decodeAddress 解析 STUN 的地址属性；xorEncoded 为真时按 XOR-MAPPED-ADDRESS 还原。
func decodeAddress(value []byte, xorEncoded bool, transaction [12]byte) (netAddr, bool) {
	if len(value) < 8 {
		return netAddr{}, false
	}
	family := value[1]
	port := binary.BigEndian.Uint16(value[2:4])
	var cookie [4]byte
	binary.BigEndian.PutUint32(cookie[:], stunMagicCookie)
	if xorEncoded {
		port ^= uint16(stunMagicCookie >> 16)
	}
	switch family {
	case 0x01: // IPv4
		raw := make([]byte, 4)
		copy(raw, value[4:8])
		if xorEncoded {
			for i := range raw {
				raw[i] ^= cookie[i]
			}
		}
		return netAddr{IP: net.IP(raw).String(), Port: int(port)}, true
	case 0x02: // IPv6
		if len(value) < 20 {
			return netAddr{}, false
		}
		raw := make([]byte, 16)
		copy(raw, value[4:20])
		if xorEncoded {
			for i := range raw {
				if i < 4 {
					raw[i] ^= cookie[i]
				} else {
					raw[i] ^= transaction[i-4]
				}
			}
		}
		return netAddr{IP: net.IP(raw).String(), Port: int(port)}, true
	default:
		return netAddr{}, false
	}
}

// stunTransaction 在给定 socket 上向 target 发一次 Binding 请求并等待响应。
//
// 复用同一个 socket 是 NAT 检测的前提：换 socket 就换了本地端口，NAT 会分配
// 新映射，所有对比都失去意义。
func stunTransaction(connection *net.UDPConn, target *net.UDPAddr, change uint32, timeout time.Duration) (stunResult, error) {
	// The egress one-shot lookup has no parent context; NAT uses the context-aware
	// variant below so its socket remains interruptible.
	return stunTransactionWithContext(context.Background(), connection, target, change, timeout)
}

func stunTransactionWithContext(ctx context.Context, connection *net.UDPConn, target *net.UDPAddr, change uint32, timeout time.Duration) (stunResult, error) {
	if err := contextCauseError(ctx); err != nil {
		return stunResult{}, err
	}
	packet, transaction, err := buildSTUNRequest(change)
	if err != nil {
		return stunResult{}, err
	}
	if _, err := connection.WriteToUDP(packet, target); err != nil {
		return stunResult{}, err
	}
	if err := contextCauseError(ctx); err != nil {
		return stunResult{}, err
	}
	deadline := time.Now().Add(timeout)
	if parentDeadline, ok := ctx.Deadline(); ok && parentDeadline.Before(deadline) {
		deadline = parentDeadline
	}
	if err := connection.SetReadDeadline(deadline); err != nil {
		return stunResult{}, err
	}
	stopCancel := context.AfterFunc(ctx, func() {
		_ = connection.SetReadDeadline(time.Now())
	})
	defer stopCancel()
	buffer := make([]byte, stunMaxResponse)
	for {
		count, source, err := connection.ReadFromUDP(buffer)
		if err != nil {
			if cause := contextCauseError(ctx); cause != nil {
				return stunResult{}, cause
			}
			return stunResult{}, err
		}
		if cause := contextCauseError(ctx); cause != nil {
			return stunResult{}, cause
		}
		result, parseErr := parseSTUNResponse(buffer[:count], transaction)
		if parseErr != nil {
			// 事务 ID 不符的包可能是上一次测试的迟到响应，继续等真正的那个。
			if cause := contextCauseError(ctx); cause != nil {
				return stunResult{}, cause
			}
			if !time.Now().Before(deadline) {
				return stunResult{}, parseErr
			}
			continue
		}
		if source != nil {
			result.From = netAddr{IP: source.IP.String(), Port: source.Port}
		}
		return result, nil
	}
}
