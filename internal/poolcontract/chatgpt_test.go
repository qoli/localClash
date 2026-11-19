package poolcontract

import (
	"regexp"
	"testing"

	"localclash/internal/smartpolicy"
)

func TestChatGPTMissingCountryKeepsNeutralWeight(t *testing.T) {
	configured := []smartpolicy.Rule{
		{Label: "US", Pattern: "^US$", Factor: 5},
		{Label: "TW", Pattern: "^TW$", Factor: 2},
		{Label: "Other", Pattern: ".*", Factor: 0.5},
	}
	rules := ChatGPTSmartPriority(configured)
	if _, err := smartpolicy.Compile(rules); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name string
		want float64
	}{
		{ChatGPTSmartName("US label", "US"), 5},
		{ChatGPTSmartName("TW label", "TW"), 2},
		{ChatGPTSmartName("US label", "DE"), 0.5},
		{ChatGPTSmartName("US label", ""), 1},
		{ChatGPTSmartName("invented [Statsig US]", ""), 1},
		{"legacy US label", 1},
	} {
		factor := 1.0 // Mihomo Smart's factor when no priority rule matches.
		for _, rule := range rules {
			if regexp.MustCompile(rule.Pattern).MatchString(tt.name) {
				factor = rule.Factor
				break
			}
		}
		if factor != tt.want {
			t.Errorf("priority for %q = %v, want %v", tt.name, factor, tt.want)
		}
	}
	if configured[2].Pattern != ".*" {
		t.Fatal("country priority compilation mutated configured intent")
	}
}
