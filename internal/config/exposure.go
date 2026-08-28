package config

import (
	"strconv"
	"strings"

	"ecs/internal/i18n"
)

// 外联分级。
//
// 每个模块都会碰外部世界，但碰的方式完全不同：跑 sysbench 不发一个包，查公共
// DNS 只让对方看到出口 IP，而把出口 IP 提交给商业风控 API 是把**被查询对象**
// 交了出去。以前这些差别只写在 THIRD_PARTY.md 里，代码里缺少统一的分级事实。
//
// 分级的依据是"对方看到了什么"，不是"用了什么协议"：
//
//   - Local       不发包；
//   - Public      连公共基础设施，对方只看到出口 IP。任何联网都免不了这一层；
//   - ThirdParty  把被查询对象交给第三方，或结论依赖第三方的闭源判定；
//   - Consent     保留为最高级别名称，供显式 --exposure any 配置使用。
//
// 用户真正想表达的意图是横切的（"别碰商业 API""我只要本地"），所以这里用一个
// 上限过滤器表达，而不是让用户逐个模块记住该关什么。
type Exposure int

const (
	ExposureLocal Exposure = iota
	ExposurePublic
	ExposureThirdParty
	ExposureConsent
)

// CLI 取值。最高一级叫 any 而不是 consent：它表达的是"允许到任何级别"。
const (
	ExposureNameLocal      = "local"
	ExposureNamePublic     = "public"
	ExposureNameThirdParty = "thirdparty"
	ExposureNameAny        = "any"
	ExposureNameInvalid    = "invalid"

	// DefaultExposure 保持重构前的默认行为：商业 IP 情报可用，闭源客户端不可用。
	DefaultExposure = ExposureNameThirdParty
)

// ModuleExposure 描述一个模块的外联性质。具体值来自 ModuleDescriptor；
// ExposureFor 直接查询 canonical moduleDescriptors。
type ModuleExposure struct {
	// Level 是该模块的外联级别。
	Level Exposure
	// NeedsEgressIP 表示模块依赖出口 IP 发现的结果。
	NeedsEgressIP bool
}

// ExposureFor 返回模块的外联性质。
//
// 未登记的模块按最高级处理：新增模块时漏配只会让它默认不跑，
// 而不会悄悄把数据发出去。
func ExposureFor(id string) ModuleExposure {
	for _, descriptor := range moduleDescriptors {
		if descriptor.ID == id {
			return ModuleExposure{Level: descriptor.Exposure, NeedsEgressIP: descriptor.NeedsEgressIP}
		}
	}
	return ModuleExposure{Level: ExposureConsent}
}

// ParseExposure 解析 --exposure 的取值。
func ParseExposure(raw string) (Exposure, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case ExposureNameLocal:
		return ExposureLocal, nil
	case ExposureNamePublic:
		return ExposurePublic, nil
	case ExposureNameThirdParty:
		return ExposureThirdParty, nil
	case ExposureNameAny:
		return ExposureConsent, nil
	default:
		return 0, i18n.Errorf("err.unknownExposure", raw, i18n.JoinList(ExposureNames()))
	}
}

// ExposureNames 按从严到松的顺序返回全部级别名。
func ExposureNames() []string {
	return []string{ExposureNameLocal, ExposureNamePublic, ExposureNameThirdParty, ExposureNameAny}
}

// String 返回级别的 CLI 名称。
func (e Exposure) String() string {
	switch e {
	case ExposureLocal:
		return ExposureNameLocal
	case ExposurePublic:
		return ExposureNamePublic
	case ExposureThirdParty:
		return ExposureNameThirdParty
	case ExposureConsent:
		return ExposureNameAny
	default:
		return ExposureNameInvalid
	}
}

func validExposure(exposure Exposure) bool {
	return exposure >= ExposureLocal && exposure <= ExposureConsent
}

func exposureError(exposure Exposure) error {
	return i18n.Errorf("err.unknownExposure", strconv.Itoa(int(exposure)), i18n.JoinList(ExposureNames()))
}

// AllowsModule 判断模块是否在给定上限内。
func AllowsModule(limit Exposure, id string) bool {
	if !validExposure(limit) {
		return false
	}
	info := ExposureFor(id)
	return info.Level <= limit
}

// FilterModulesByExposure 按上限裁剪模块集，保持原有顺序。
func FilterModulesByExposure(modules []string, limit Exposure) []string {
	if !validExposure(limit) {
		return nil
	}
	out := make([]string, 0, len(modules))
	for _, id := range modules {
		if AllowsModule(limit, id) {
			out = append(out, id)
		}
	}
	return out
}

// RequiresEgressIP 判断这批模块里有没有人需要出口 IP。
func RequiresEgressIP(modules []string) bool {
	for _, id := range modules {
		if ExposureFor(id).NeedsEgressIP {
			return true
		}
	}
	return false
}

// EgressNeedsIPIntel 判断出口 IP 发现能否使用商业情报接口。
//
// 只有真正要用 ASN、地理与公司字段的模块才值得走 ipapi.is；只需要知道
// "我的 IP 是什么"的场景用 STUN 就够，也少交一次待查 IP 给第三方。
func EgressNeedsIPIntel(modules []string, limit Exposure) bool {
	if !validExposure(limit) || limit < ExposureThirdParty {
		return false
	}
	for _, id := range modules {
		if ExposureFor(id).Level >= ExposureThirdParty && ExposureFor(id).NeedsEgressIP {
			return true
		}
	}
	return false
}

// OfflineOnly 表示本次运行完全不联网。
func (r Runtime) OfflineOnly() bool { return r.Exposure == ExposureLocal }

// CheckModuleExposure 校验用户显式点名的模块是否被外联设置允许。
//
// 只对 --only 点名的模块报错：档位带进来的模块被静默过滤是预期行为，
// 而用户亲手写下的模块被悄悄丢掉不是。
func CheckModuleExposure(named []string, limit Exposure) error {
	if err := validateExposure(limit); err != nil {
		return err
	}
	for _, id := range named {
		if AllowsModule(limit, id) {
			continue
		}
		info := ExposureFor(id)
		return i18n.Errorf("err.moduleAboveLimit",
			id, info.Level.String(), limit.String(), info.Level.String())
	}
	return nil
}

func validateExposure(exposure Exposure) error {
	if !validExposure(exposure) {
		return exposureError(exposure)
	}
	return nil
}
