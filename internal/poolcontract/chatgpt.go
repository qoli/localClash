package poolcontract

import (
	"encoding/json"
	"regexp"
	"strings"

	"localclash/internal/smartpolicy"
)

const (
	statsigNamePrefix = " [Statsig "
	statsigNameEnd    = "]"
)

// ChatGPTSmartOverride appends the measured country to the Smart adapter name.
// Legacy providers and measurements without a country get the neutral marker.
const ChatGPTSmartOverride = `(select(.type != "reject") | .name) = .name + " [Statsig " + (."x-localclash-statsig-country" // "") + "]"`

func ChatGPTSmartName(name, country string) string {
	return name + statsigNamePrefix + country + statsigNameEnd
}

// ChatGPTSmartPriority preserves the configured tier order and weights. The
// label of each country tier is its ISO code, including in older patches.
func ChatGPTSmartPriority(rules []smartpolicy.Rule) []smartpolicy.Rule {
	priority := smartpolicy.Clone(rules)
	for i, rule := range priority {
		if country := statsigCountry(rule.Label); country != "" {
			// The policy parser trims leading whitespace from each rule.
			priority[i].Pattern = `\x20` + regexp.QuoteMeta(strings.TrimPrefix(ChatGPTSmartName("", country), " ")) + `\z`
		} else {
			priority[i].Pattern = `.*`
		}
	}
	return priority
}

func statsigCountry(country string) string {
	country = strings.ToUpper(strings.TrimSpace(country))
	if len(country) != 2 || country[0] < 'A' || country[0] > 'Z' || country[1] < 'A' || country[1] > 'Z' {
		return ""
	}
	return country
}

// ChatGPTProviderMembers maps subscription names to the names the Smart
// provider override installs. The file keeps the original dialer references.
func ChatGPTProviderMembers(data []byte) (map[string]string, error) {
	var document providerDocument
	if err := json.Unmarshal(data, &document); err != nil {
		return nil, err
	}
	members := make(map[string]string, len(document.Proxies))
	for _, proxy := range document.Proxies {
		name, _ := proxy["name"].(string)
		runtimeName := name
		country, _ := proxy[StatsigCountryKey].(string)
		proxyType, _ := proxy["type"].(string)
		if proxyType != "reject" {
			runtimeName = ChatGPTSmartName(name, country)
		}
		members[name] = runtimeName
	}
	return members, nil
}
