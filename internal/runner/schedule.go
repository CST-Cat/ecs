package runner

import "ecs/internal/module"

// 模块调度：按干扰特性分组，组间串行、组内并行。
//
// 完全串行是安全但慢的做法；完全并行会让测量结果互相污染。分组的依据是
// "这个模块跑起来会抢占什么资源"：
//
//   - 性能基准抢 CPU/内存/磁盘，任何并发都会压低成绩；
//   - 大流量测速抢带宽，同时跑两个测出来是瓜分后的结果；
//   - NextTrace 路由探测对 ICMP/UDP 限速极敏感——本项目实测过并发 6 个追踪会让关键
//     跳全部变成 `*`，同一目标单独跑却能稳定命中（见 backtraceConcurrency 的注释）；
//   - 轻量探测只发少量请求、等的是网络往返，并行不会互相影响，反而能把串行的
//     等待时间叠起来。
//
// 未列出的模块一律按独占处理：新增模块时漏配只会慢一点，不会得到错误数据。

// scheduleGroup 是一批可以同时执行的模块。
type scheduleGroup struct {
	Indices []int
}

// planSchedule 把已绑定的模块序列切成执行批次。
//
// 保持绑定后的规范顺序：连续的可并行模块合成一批，遇到独占模块就单独成批。
// 这样报告里的结果顺序仍与 canonical descriptor order 一致，只是执行方式变了。
func planSchedule(bindings []moduleBinding) []scheduleGroup {
	var groups []scheduleGroup
	var pending []int
	flush := func() {
		if len(pending) == 0 {
			return
		}
		groups = append(groups, scheduleGroup{Indices: pending})
		pending = nil
	}
	for index, binding := range bindings {
		if binding.Descriptor.ID != "" && binding.Descriptor.Concurrency == module.ConcurrencyProbe {
			pending = append(pending, index)
			continue
		}
		flush()
		groups = append(groups, scheduleGroup{Indices: []int{index}})
	}
	flush()
	return groups
}
