package probe

import (
	"strings"
	"testing"
	"time"
)

func TestAppTargetsAreWellFormed(t *testing.T) {
	targets := appTargets()
	if len(targets) < 10 {
		t.Fatalf("清单过短：%d", len(targets))
	}
	seen := make(map[string]bool)
	for _, target := range targets {
		if target.Name == "" || target.Host == "" || target.Category.Key == "" || target.Category.Label == "" || target.Note == "" {
			t.Fatalf("清单项缺字段：%+v", target)
		}
		if target.Port < 1 || target.Port > 65535 {
			t.Fatalf("%s 端口无效：%d", target.Name, target.Port)
		}
		key := target.Host + ":" + target.Note
		if seen[key] {
			t.Fatalf("重复端点：%s", key)
		}
		seen[key] = true
		// Telegram 必须用域名而不是硬编码 IP：多个 DC 会解析到同一地址，
		// 且 Telegram 会不定期调整，写死必然过期。
		if target.Category.Key == appCategoryTelegram.Key && !strings.HasSuffix(target.Host, ".telegram.org") {
			t.Fatalf("Telegram 端点应使用官方域名：%s", target.Host)
		}
		if strings.Count(target.Host, ".") == 3 && !strings.ContainsAny(target.Host, "abcdefghijklmnopqrstuvwxyz") {
			t.Fatalf("%s 疑似硬编码 IP，应使用域名：%s", target.Name, target.Host)
		}
	}
}

func TestSortAppResultsPutsFailuresFirst(t *testing.T) {
	items := []appResult{
		{Target: appTarget{Name: "快"}, Reachable: true, Latency: 10 * time.Millisecond},
		{Target: appTarget{Name: "挂"}, Reachable: false},
		{Target: appTarget{Name: "慢"}, Reachable: true, Latency: 900 * time.Millisecond},
		{Target: appTarget{Name: "也挂"}, Reachable: false},
	}
	sortAppResults(items)
	// 不可达的必须排最前——那是读者最需要先看到的。
	if items[0].Reachable || items[1].Reachable {
		t.Fatalf("不可达项未排到最前：%+v", items)
	}
	// 可达的按延迟升序。
	if items[2].Target.Name != "快" || items[3].Target.Name != "慢" {
		t.Fatalf("可达项未按延迟升序：%+v", items[2:])
	}
}
