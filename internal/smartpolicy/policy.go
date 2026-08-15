package smartpolicy

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// Rule is one ordered, first-match-wins Smart proxy-name priority rule.
// Label is localClash metadata; Mihomo receives only Pattern and Factor.
type Rule struct {
	Label   string  `json:"label" yaml:"label"`
	Pattern string  `json:"pattern" yaml:"pattern"`
	Factor  float64 `json:"factor" yaml:"factor"`
}

// Compile validates ordered rules and serializes them for Mihomo Alpha's
// policy-priority parser. The parser removes one escaping layer, so literal
// backslashes and colons must be escaped here. Its semicolon separator cannot
// be escaped and is therefore forbidden in patterns.
func Compile(rules []Rule) (string, error) {
	if len(rules) == 0 {
		return "", nil
	}
	seen := make(map[string]struct{}, len(rules))
	parts := make([]string, 0, len(rules))
	for i, rule := range rules {
		label := strings.TrimSpace(rule.Label)
		pattern := strings.TrimSpace(rule.Pattern)
		if label == "" {
			return "", fmt.Errorf("smart priority rule %d label is required", i)
		}
		if _, exists := seen[label]; exists {
			return "", fmt.Errorf("smart priority label %q is defined more than once", label)
		}
		seen[label] = struct{}{}
		if pattern == "" {
			return "", fmt.Errorf("smart priority rule %q pattern is required", label)
		}
		if strings.Contains(pattern, ";") {
			return "", fmt.Errorf("smart priority rule %q pattern cannot contain semicolon", label)
		}
		if _, err := regexp.Compile(pattern); err != nil {
			return "", fmt.Errorf("smart priority rule %q pattern: %w", label, err)
		}
		if rule.Factor <= 0 || math.IsNaN(rule.Factor) || math.IsInf(rule.Factor, 0) {
			return "", fmt.Errorf("smart priority rule %q factor must be a finite positive number", label)
		}
		encoded := strings.ReplaceAll(pattern, `\`, `\\`)
		encoded = strings.ReplaceAll(encoded, ":", `\:`)
		parts = append(parts, encoded+":"+strconv.FormatFloat(rule.Factor, 'f', -1, 64))
	}
	return strings.Join(parts, ";"), nil
}

func Clone(rules []Rule) []Rule {
	return append([]Rule(nil), rules...)
}
