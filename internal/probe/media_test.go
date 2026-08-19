package probe

import "testing"

func TestMediaProviderPayloadProducesRegionAndUnlockedVerdict(t *testing.T) {
	verdict := youtubePremiumCheck().Decide([]mediaResponse{{
		Status: 200,
		Body:   `{"countryCode":"US","premium":"available"}`,
	}})
	if verdict.State != stateUnlocked || verdict.Region != "US" {
		t.Fatalf("media verdict = %+v", verdict)
	}
}
