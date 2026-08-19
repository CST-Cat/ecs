package probe

import "testing"

func TestAppTargetCarriesPortAndCategory(t *testing.T) {
	targets := appTargets()
	if len(targets) == 0 {
		t.Fatal("app target list is empty")
	}
	target := targets[0]
	if target.Port != 443 || target.Host == "" || target.Category.Key != appCategoryTelegram.Key {
		t.Fatalf("representative app target = %+v", target)
	}
}
