package probe

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
)

// comparisonParameterRevision changes only when the rules used to build a
// module's machine comparison scope change. Measurement.Method remains the
// version for the workload itself.
const comparisonParameterRevision = "1"

// newComparisonParameters creates the language-independent scope map owned by
// a producer. The helper deliberately contains no module schema: producers
// decide which structured facts belong in their own scope.
func newComparisonParameters() map[string]string {
	return map[string]string{"scope_revision": comparisonParameterRevision}
}

func addComparisonParameter(parameters map[string]string, key, value string) {
	value = strings.TrimSpace(value)
	if value != "" {
		parameters[key] = value
	}
}

// comparisonParameterHash is the one canonical hash for ordered runtime
// inputs and tagged model.Value selections. encoding/json preserves Value's
// raw/key tag, so equal display text with different variants remains distinct.
func comparisonParameterHash(value any) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

func addComparisonParameterHash(parameters map[string]string, key string, value any) {
	addComparisonParameter(parameters, key, comparisonParameterHash(value))
}
