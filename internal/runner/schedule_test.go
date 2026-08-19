package runner

import (
	"reflect"
	"testing"

	"ecs/internal/config"
)

func TestPlanSchedulePreservesOrderAndIsolatesExclusive(t *testing.T) {
	ids := []string{"system", "network", "cpu", "memory", "disk", "dns", "latency", "speed", "ports", "nat", "media", "route", "backtrace"}
	bindings := scheduleBindings(ids)
	groups := planSchedule(bindings)

	// 展平后必须与原顺序完全一致：报告顺序不能因为并行而漂移。
	var flat []int
	for _, group := range groups {
		flat = append(flat, group.Indices...)
	}
	want := make([]int, len(ids))
	for i := range ids {
		want[i] = i
	}
	if !reflect.DeepEqual(flat, want) {
		t.Fatalf("调度打乱了模块顺序：%v", flat)
	}

	// 每个独占模块必须单独成组。
	for _, group := range groups {
		if len(group.Indices) == 1 {
			continue
		}
		if !group.Parallel {
			t.Fatalf("多模块组必须标记为并行：%+v", group)
		}
		for _, index := range group.Indices {
			if classOf(bindings[index]) != classProbe {
				t.Fatalf("独占模块 %q 混进了并行组", ids[index])
			}
		}
	}

	// cpu/memory/disk 三个性能基准必须各自独占，任何并发都会压低成绩。
	for _, id := range []string{"cpu", "memory", "disk", "speed", "cnspeed", "route", "backtrace"} {
		if classOf(scheduleBindings([]string{id})[0]) != classExclusive {
			t.Errorf("%q 必须独占运行", id)
		}
	}
	// route/backtrace 尤其重要：实测并发 NextTrace 会让关键跳全部变成 *。
	for _, group := range groups {
		if len(group.Indices) <= 1 {
			continue
		}
		for _, index := range group.Indices {
			if ids[index] == "route" || ids[index] == "backtrace" {
				t.Fatal("NextTrace 路由模块并发会被限速，绝不能进并行组")
			}
		}
	}
}

func TestPlanScheduleGroupsAdjacentProbes(t *testing.T) {
	// 连续的轻量探测应合成一批。
	groups := planSchedule(scheduleBindings([]string{"dns", "latency", "ports"}))
	if len(groups) != 1 || !groups[0].Parallel || len(groups[0].Indices) != 3 {
		t.Fatalf("连续探测未合批：%+v", groups)
	}
	// 被独占模块隔断时分成三批。
	groups = planSchedule(scheduleBindings([]string{"dns", "cpu", "latency"}))
	if len(groups) != 3 {
		t.Fatalf("独占模块未隔断批次：%+v", groups)
	}
	if groups[0].Parallel || groups[2].Parallel {
		t.Fatal("单模块组不应标记为并行")
	}
	// 空输入不应 panic。
	if got := planSchedule(nil); len(got) != 0 {
		t.Fatalf("空输入 = %+v", got)
	}
}

func TestUnknownBindingDefaultsToExclusive(t *testing.T) {
	// 新增模块时漏配分类，只会慢一点，不会得到被污染的数据。
	binding := moduleBinding{Descriptor: config.ModuleDescriptor{ID: "某个未来才有的模块"}}
	if classOf(binding) != classExclusive {
		t.Fatal("未登记的模块必须默认独占")
	}
}

func scheduleBindings(ids []string) []moduleBinding {
	bindings := make([]moduleBinding, len(ids))
	for index, id := range ids {
		descriptor, ok := config.ModuleDescriptorFor(id)
		if !ok {
			descriptor.ID = id
		}
		bindings[index] = moduleBinding{Descriptor: descriptor}
	}
	return bindings
}
