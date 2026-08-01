package runner

// 模块调度：按干扰特性分组，组间串行、组内并行。
//
// 完全串行是安全但慢的做法；完全并行会让测量结果互相污染。分组的依据是
// "这个模块跑起来会抢占什么资源"：
//
//   - 性能基准抢 CPU/内存/磁盘，任何并发都会压低成绩；
//   - 大流量测速抢带宽，同时跑两个测出来是瓜分后的结果；
//   - traceroute 类对 ICMP/UDP 限速极敏感——本项目实测过并发 6 个追踪会让关键
//     跳全部变成 `*`，同一目标单独跑却能稳定命中（见 backtraceConcurrency 的注释）；
//   - 轻量探测只发少量请求、等的是网络往返，并行不会互相影响，反而能把串行的
//     等待时间叠起来。
//
// 未列出的模块一律按独占处理：新增模块时漏配只会慢一点，不会得到错误数据。

// concurrencyClass 描述一个模块的并发特性。
type concurrencyClass int

const (
	// classExclusive 必须独占运行。
	classExclusive concurrencyClass = iota
	// classProbe 是轻量探测，可与同类并行。
	classProbe
)

// moduleClass 给出各模块的并发特性。
var moduleClass = map[string]concurrencyClass{
	// 性能基准：抢 CPU、内存、磁盘，必须独占。
	"cpu":    classExclusive,
	"memory": classExclusive,
	"disk":   classExclusive,

	// 大流量：抢带宽，必须独占。
	"speed":   classExclusive,
	"cnspeed": classExclusive,

	// 路由类：对 ICMP/UDP 限速敏感，必须独占。
	"route":     classExclusive,
	"backtrace": classExclusive,

	// 轻量探测：等的是网络往返，并行只叠加等待时间。
	"system":    classProbe,
	"network":   classProbe,
	"dns":       classProbe,
	"latency":   classProbe,
	"ports":     classProbe,
	"nat":       classProbe,
	"blacklist": classProbe,
	"apps":      classProbe,
	"media":     classProbe,
}

// classOf 返回模块的并发特性，未登记的一律独占。
func classOf(id string) concurrencyClass {
	if class, ok := moduleClass[id]; ok {
		return class
	}
	return classExclusive
}

// scheduleGroup 是一批可以同时执行的模块。
type scheduleGroup struct {
	// Parallel 为真时组内模块并行执行。
	Parallel bool
	Indices  []int
}

// planSchedule 把模块序列切成执行批次。
//
// 保持原有顺序：连续的可并行模块合成一批，遇到独占模块就单独成批。
// 这样报告里的结果顺序仍与用户指定的模块顺序一致，只是执行方式变了。
func planSchedule(ids []string) []scheduleGroup {
	var groups []scheduleGroup
	var pending []int
	flush := func() {
		if len(pending) == 0 {
			return
		}
		groups = append(groups, scheduleGroup{Parallel: len(pending) > 1, Indices: pending})
		pending = nil
	}
	for index, id := range ids {
		if classOf(id) == classProbe {
			pending = append(pending, index)
			continue
		}
		flush()
		groups = append(groups, scheduleGroup{Indices: []int{index}})
	}
	flush()
	return groups
}
