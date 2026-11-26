package configrender

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"localclash/internal/runtimeprofile"
)

func TestRenderBuiltinRouterAppleTVDNSPolicy(t *testing.T) {
	for _, core := range []string{runtimeprofile.CoreMeta, runtimeprofile.CoreSmart} {
		t.Run(core, func(t *testing.T) {
			paths := writeRenderFixture(t)
			profilePath := filepath.Join(paths.dir, runtimeprofile.DefaultPath)
			// Existing runtime selectors inherit embedded defaults without a user profile.
			writeFile(t, profilePath, `{"version":2,"mode":"router","core":"`+core+`"}`)
			writeFile(t, paths.selection, `version: 1
proxy_groups:
  "⚡ 自动选择":
    nodes: ["🇯🇵日本01 | JP"]
    auto: true
policy_groups:
  DNSProxy:
    exits: ["⚡ 自动选择"]
    manual: true
enabled_packs: []
`)
			result, err := Render(Options{
				SourcePath:         paths.subscription,
				OutputPath:         filepath.Join(paths.dir, "apple-tv.yaml"),
				PacksSelectionPath: paths.selection,
				RulesCacheDir:      paths.cacheDir,
				RuntimeProfilePath: profilePath,
			})
			if err != nil {
				t.Fatal(err)
			}
			config := readTestYAML(t, result.OutputPath)
			dns := config["dns"].(map[string]any)
			policy := dns["nameserver-policy"].(map[string]any)
			want := []any{"https://8.8.8.8/dns-query#DNSProxy&ecs=0.0.0.0/0&ecs-override=true"}
			for _, key := range []string{"geosite:apple-tvplus", "hls-svod-aoc-ap1.itunes.g.aaplimg.com"} {
				if !reflect.DeepEqual(policy[key], want) {
					t.Errorf("rendered DNS policy[%q] = %#v, want %#v", key, policy[key], want)
				}
			}
			if dns["direct-nameserver-follow-policy"] != true || dns["ipv6"] != true {
				t.Fatalf("Apple TV direct DNS must follow policy with IPv6 enabled: %#v", dns)
			}
			if !reflect.DeepEqual(dns["direct-nameserver"], []any{"https://223.5.5.5/dns-query"}) {
				t.Fatalf("unmatched direct domains changed DNS: %#v", dns["direct-nameserver"])
			}
			for _, broad := range []string{"geosite:apple", "+.apple.com", "+.aaplimg.com", "+.itunes.g.aaplimg.com"} {
				if _, exists := policy[broad]; exists {
					t.Errorf("Apple TV DNS policy must not include broad Apple scope %q", broad)
				}
			}
			if got := dnsProxyGroupReferences(policy); !reflect.DeepEqual(got, []string{"DNSProxy"}) {
				t.Fatalf("DNS policy proxy references = %#v, want DNSProxy without ECS parameters", got)
			}
			if !proxyGroupNamesFromConfig(config)["DNSProxy"] {
				t.Fatal("Apple TV DNS references an unrendered proxy group")
			}
			rules := testStringSlice(config["rules"])
			if rules[len(rules)-1] != "MATCH,DIRECT" || proxyGroupNamesFromConfig(config)["DIRECT"] {
				t.Fatal("Apple TV DNS policy must retain builtin DIRECT and the direct catch-all")
			}
			data, err := os.ReadFile(result.OutputPath)
			if err != nil {
				t.Fatal(err)
			}
			text := string(data)
			if strings.Index(text, "geosite:apple-tvplus:") >= strings.Index(text, "geosite:gfw:") {
				t.Fatal("serialized Apple TV DNS policy must precede the broader GFW policy")
			}
		})
	}
}
