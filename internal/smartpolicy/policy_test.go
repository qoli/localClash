package smartpolicy

import (
	"math"
	"testing"
)

func TestCompilePreservesOrderAndEscapesMihomoGrammar(t *testing.T) {
	rules := []Rule{
		{Label: "US", Pattern: `(🇺🇸|美国|\bUS\b|edge:us)`, Factor: 5},
		{Label: "Other", Pattern: `.*`, Factor: 1},
	}
	got, err := Compile(rules)
	if err != nil {
		t.Fatal(err)
	}
	want := `(🇺🇸|美国|\\bUS\\b|edge\:us):5;.*:1`
	if got != want {
		t.Fatalf("Compile() = %q, want %q", got, want)
	}
}

func TestCompileRejectsInvalidRules(t *testing.T) {
	tests := []struct {
		name  string
		rules []Rule
	}{
		{name: "missing label", rules: []Rule{{Pattern: ".*", Factor: 1}}},
		{name: "duplicate label", rules: []Rule{{Label: "US", Pattern: "US", Factor: 2}, {Label: "US", Pattern: "USA", Factor: 1}}},
		{name: "missing pattern", rules: []Rule{{Label: "US", Factor: 1}}},
		{name: "invalid regex", rules: []Rule{{Label: "US", Pattern: "[", Factor: 1}}},
		{name: "semicolon", rules: []Rule{{Label: "US", Pattern: "US;JP", Factor: 1}}},
		{name: "zero", rules: []Rule{{Label: "US", Pattern: "US", Factor: 0}}},
		{name: "negative", rules: []Rule{{Label: "US", Pattern: "US", Factor: -1}}},
		{name: "nan", rules: []Rule{{Label: "US", Pattern: "US", Factor: math.NaN()}}},
		{name: "infinity", rules: []Rule{{Label: "US", Pattern: "US", Factor: math.Inf(1)}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Compile(tt.rules); err == nil {
				t.Fatal("Compile() error = nil")
			}
		})
	}
}
