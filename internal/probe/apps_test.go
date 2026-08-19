package probe

import (
	"testing"
	"time"
)

func TestAppTargetCarriesPortAndCategory(t *testing.T) {
	targets := appTargets()
	if len(targets) == 0 {
		t.Fatal("app target list is empty")
	}
	target := targets[0]
	if target.Port != 443 || target.Host == "" || target.Category.Key != appCategoryTelegram.Key {
		t.Fatalf("representative app target = %+v", target)
	}
	seen := map[string]bool{}
	for _, item := range targets {
		if item.Host == "" || item.Port < 1 || item.Port > 65535 || seen[item.Host] {
			t.Fatalf("invalid or duplicate app target = %+v", item)
		}
		seen[item.Host] = true
	}
	for _, category := range []string{appCategoryTelegram.Key, appCategoryCodeAndImages.Key, appCategoryRepositories.Key, appCategoryInfrastructure.Key} {
		found := false
		for _, item := range targets {
			found = found || item.Category.Key == category
		}
		if !found {
			t.Fatalf("app category %q has no targets", category)
		}
	}
	results := []appResult{
		{Target: appTarget{Name: "slow", Host: "slow.example", Port: 443}, Reachable: true, Latency: 20 * time.Millisecond},
		{Target: appTarget{Name: "blocked", Host: "blocked.example", Port: 443}, Detail: "connection refused"},
		{Target: appTarget{Name: "fast", Host: "fast.example", Port: 443}, Reachable: true, Latency: 5 * time.Millisecond},
	}
	sortAppResults(results)
	if results[0].Target.Name != "blocked" || results[1].Latency != 5*time.Millisecond || results[2].Latency != 20*time.Millisecond {
		t.Fatalf("app result order = %+v", results)
	}
}
