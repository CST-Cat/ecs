//go:build integration

// 集成测试的共享支撑。
//
// 本包所有 //go:build integration 文件都跑宿主上真实安装的基准工具，不使用脚本
// 替身：替身只能证明解析器认得它自己造出来的输出，证明不了它认得工具的真实输出。
// 用 build tag 隔离而不是在 CI 里写测试名黑名单，是为了让"这个测试需要什么"由
// 源码回答——`go test ./...` 在没装工具的机器上因此不受影响。

package probe

import (
	"ecs/internal/model"
	"os"
	"os/exec"
	"testing"
)

// requireTool 返回真实基准工具的路径。
//
// 缺少工具时的行为按环境分岔：CI 的 integration job 自己负责装齐这些工具，
// 所以那里缺工具属于环境配置错误，必须失败；开发机上跳过，避免逼着每个人
// 为了跑一次 `go test -tags=integration` 先装满一台机器。
func requireTool(t *testing.T, name string) string {
	t.Helper()
	path, err := exec.LookPath(name)
	if err == nil {
		return path
	}
	if os.Getenv("CI") != "" {
		t.Fatalf("%s 未安装：CI 必须以真实工具运行标准基准测试", name)
	}
	t.Skipf("%s 未安装，跳过真实基准测试；安装：apt-get install -y sysbench fio iperf3", name)
	return ""
}

func findMeasurement(measurements []model.Measurement, key string) (model.Measurement, bool) {
	for _, measurement := range measurements {
		if measurement.Key == key {
			return measurement, true
		}
	}
	return model.Measurement{}, false
}
