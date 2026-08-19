package probe

import "testing"

func TestValidateCNNodeURLRejectsPrivateAddress(t *testing.T) {
	if _, err := validateCNNodeURL("http://127.0.0.1/download"); err == nil {
		t.Fatal("private node address must be rejected")
	}
}
