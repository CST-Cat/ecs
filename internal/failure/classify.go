// Package failure converts operational errors into the stable failure
// taxonomy stored in ecs reports. It prefers typed Go errors and only uses
// conservative text matching for errors returned by external executables.
package failure

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"errors"
	"net"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"syscall"

	"ecs/internal/model"
)

// Classified is the machine-readable part of a failure.
type Classified struct {
	Category  model.FailureCategory
	Retryable bool
}

// FromError builds a complete report entry while preserving the original
// message for diagnostics.
func FromError(stage, target string, err error) model.Failure {
	classified := Classify(err)
	message := ""
	if err != nil {
		message = err.Error()
	}
	return model.Failure{
		Category: classified.Category, Stage: stage, Target: target,
		Retryable: classified.Retryable, Count: 1, Message: message,
	}
}

// FromMessage handles external tools and already-compacted errors.
func FromMessage(stage, target, message string) model.Failure {
	classified := classifyText(message)
	return model.Failure{
		Category: classified.Category, Stage: stage, Target: target,
		Retryable: classified.Retryable, Count: 1, Message: message,
	}
}

// Classify maps typed errors first, then falls back to conservative matching.
func Classify(err error) Classified {
	if err == nil {
		return Classified{Category: model.FailureUnknown}
	}
	if errors.Is(err, context.Canceled) {
		return Classified{Category: model.FailureCanceled}
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, os.ErrDeadlineExceeded) {
		return Classified{Category: model.FailureTimeout, Retryable: true}
	}
	if errors.Is(err, exec.ErrNotFound) {
		return Classified{Category: model.FailureToolMissing}
	}
	if errors.Is(err, os.ErrPermission) || errors.Is(err, syscall.EACCES) || errors.Is(err, syscall.EPERM) {
		return Classified{Category: model.FailurePermissionDenied}
	}
	if errors.Is(err, syscall.ECONNREFUSED) {
		return Classified{Category: model.FailureConnectionRefused, Retryable: true}
	}
	if errors.Is(err, syscall.ENETUNREACH) || errors.Is(err, syscall.EHOSTUNREACH) || errors.Is(err, syscall.ENETDOWN) {
		return Classified{Category: model.FailureNetworkUnreachable, Retryable: true}
	}
	var dnsError *net.DNSError
	if errors.As(err, &dnsError) {
		if dnsError.IsTimeout {
			return Classified{Category: model.FailureTimeout, Retryable: true}
		}
		return Classified{Category: model.FailureDNS, Retryable: dnsError.IsTemporary}
	}
	var certificateError x509.UnknownAuthorityError
	if errors.As(err, &certificateError) {
		return Classified{Category: model.FailureTLS}
	}
	var hostnameError x509.HostnameError
	if errors.As(err, &hostnameError) {
		return Classified{Category: model.FailureTLS}
	}
	// *url.Error implements net.Error, so the interface check below already
	// covers it; a separate branch would be the same test written twice.
	var netError net.Error
	if errors.As(err, &netError) && netError.Timeout() {
		return Classified{Category: model.FailureTimeout, Retryable: true}
	}
	var syntaxError *json.SyntaxError
	if errors.As(err, &syntaxError) {
		return Classified{Category: model.FailureParse}
	}
	var typeError *json.UnmarshalTypeError
	if errors.As(err, &typeError) {
		return Classified{Category: model.FailureParse}
	}
	return classifyText(err.Error())
}

func classifyText(message string) Classified {
	text := strings.ToLower(strings.TrimSpace(message))
	statusCode := httpStatusCode(text)
	switch {
	case text == "":
		return Classified{Category: model.FailureUnknown}
	case containsAny(text, "context canceled", "context cancelled", "已取消", "用户中断"):
		return Classified{Category: model.FailureCanceled}
	case statusCode == 429 || containsAny(text, "too many requests", "rate limit", "ratelimit", "限流", "频率限制"):
		return Classified{Category: model.FailureRateLimited, Retryable: true}
	case statusCode == 408 || statusCode == 504 || containsAny(text, "timeout", "timed out", "deadline exceeded", "i/o timeout", "超时", "无响应"):
		return Classified{Category: model.FailureTimeout, Retryable: true}
	case statusCode >= 400 && statusCode <= 599:
		return Classified{Category: model.FailureHTTPRejected, Retryable: statusCode >= 500}
	case containsAny(text, "connection refused", "actively refused", "连接被拒绝", "拒绝连接"):
		return Classified{Category: model.FailureConnectionRefused, Retryable: true}
	case containsAny(text, "network is unreachable", "no route to host", "host is unreachable", "network unreachable", "网络不可达", "无路由"):
		return Classified{Category: model.FailureNetworkUnreachable, Retryable: true}
	case containsAny(text, "no such host", "server misbehaving", "temporary failure in name resolution", "name or service not known", "dns rcode", "dns 响应", "dns 事务", "dns 无应答", "域名解析失败", "dns 解析失败"):
		return Classified{Category: model.FailureDNS, Retryable: true}
	// Numeric forms such as "http 401" are already handled by statusCode above.
	case containsAny(text, "unauthorized", "forbidden", "访问被拒", "http 拒绝"):
		return Classified{Category: model.FailureHTTPRejected}
	case containsAny(text, "tls", "x509", "certificate", "handshake failure", "证书", "握手失败"):
		return Classified{Category: model.FailureTLS}
	case containsAny(text, "permission denied", "operation not permitted", "权限不足", "不允许的操作"):
		return Classified{Category: model.FailurePermissionDenied}
	case containsAny(text, "executable file not found", "command not found", "未找到", "没有安装", "not installed", "missing tool"):
		return Classified{Category: model.FailureToolMissing}
	case containsAny(text, "unsupported", "not supported", "不支持", "不可用的架构"):
		return Classified{Category: model.FailureUnsupported}
	case containsAny(text, "parse", "parsing", "invalid json", "decode", "unmarshal", "malformed", "unexpected eof", "解析 json", "json 解析", "解析失败", "无法解析", "格式无效", "响应过短", "未包含可用"):
		return Classified{Category: model.FailureParse}
	default:
		return Classified{Category: model.FailureUnknown}
	}
}

var httpStatusPattern = regexp.MustCompile(`(?:http(?: status)?|status(?: code)?)[ :=]*(\d{3})`)

func httpStatusCode(text string) int {
	match := httpStatusPattern.FindStringSubmatch(text)
	if len(match) != 2 {
		return 0
	}
	code, _ := strconv.Atoi(match[1])
	return code
}

func containsAny(text string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(text, needle) {
			return true
		}
	}
	return false
}
