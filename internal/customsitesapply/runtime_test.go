package customsitesapply

import (
	"strings"
	"testing"
	"time"

	"localclash/internal/customsites"
	"localclash/internal/mihomoapi"
)

func TestRejectRuntimeReadBackRequiresRulesAndOnlyRejectExit(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(map[string]any, []any)
		want   string
	}{
		{"loaded", func(map[string]any, []any) {}, ""},
		{"missing group", func(groups map[string]any, _ []any) { delete(groups, customsites.RejectPolicyGroup) }, "missing reserved policy group"},
		{"wrong selection", func(groups map[string]any, _ []any) {
			groups[customsites.RejectPolicyGroup].(map[string]any)["now"] = "DIRECT"
		}, "select only REJECT"},
		{"extra exit", func(groups map[string]any, _ []any) {
			groups[customsites.RejectPolicyGroup].(map[string]any)["all"] = []any{"REJECT", "DIRECT"}
		}, "select only REJECT"},
		{"wrong target", func(_ map[string]any, rules []any) {
			rules[1].(map[string]any)["proxy"] = customsites.DirectPolicyGroup
		}, "rule 2 mismatch"},
		{"wrong order", func(_ map[string]any, rules []any) { rules[0], rules[1] = rules[1], rules[0] }, "rule 1 mismatch"},
	} {
		t.Run(test.name, func(t *testing.T) {
			state := customsites.EmptyState()
			for _, route := range []string{customsites.RouteDirect, customsites.RouteReject, customsites.RouteProxy} {
				var err error
				state, _, err = customsites.Add(state, route, "*.example.com", time.Now())
				if err != nil {
					t.Fatal(err)
				}
			}
			groups := map[string]any{
				customsites.ProxyPolicyGroup:  map[string]any{},
				customsites.DirectPolicyGroup: map[string]any{},
				customsites.RejectPolicyGroup: map[string]any{"all": []any{"REJECT"}, "now": "REJECT"},
			}
			rules := []any{}
			for _, target := range []string{customsites.ProxyPolicyGroup, customsites.RejectPolicyGroup, customsites.DirectPolicyGroup} {
				rules = append(rules, map[string]any{"type": "DomainWildcard", "payload": "*.example.com", "proxy": target})
			}
			test.mutate(groups, rules)
			err := VerifyRuntimeReadBack(state,
				mihomoapi.Response{JSON: map[string]any{"rules": rules}},
				mihomoapi.Response{JSON: map[string]any{"proxies": groups}})
			if test.want == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("read-back error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestRuntimeReadBackCanRestoreLegacyConfigWithoutRejectGroup(t *testing.T) {
	state := customsites.EmptyState()
	groups := mihomoapi.Response{JSON: map[string]any{"proxies": map[string]any{
		customsites.ProxyPolicyGroup:  map[string]any{},
		customsites.DirectPolicyGroup: map[string]any{},
	}}}
	rules := mihomoapi.Response{JSON: map[string]any{"rules": []any{}}}
	if err := VerifyRuntimeReadBack(state, rules, groups); err == nil || !strings.Contains(err.Error(), "missing reserved policy group") {
		t.Fatalf("new state must require reject group: %v", err)
	}
	state.LegacyTwoFiles = true
	if err := VerifyRuntimeReadBack(state, rules, groups); err != nil {
		t.Fatalf("restored two-file legacy state: %v", err)
	}
	proxyMap := groups.JSON.(map[string]any)["proxies"].(map[string]any)
	proxyMap[customsites.RejectPolicyGroup] = map[string]any{"all": []any{"REJECT"}, "now": "REJECT"}
	if err := VerifyRuntimeReadBack(state, rules, groups); err != nil {
		t.Fatalf("legacy data with a regenerated three-group config: %v", err)
	}
	proxyMap[customsites.RejectPolicyGroup].(map[string]any)["now"] = "DIRECT"
	if err := VerifyRuntimeReadBack(state, rules, groups); err == nil {
		t.Fatal("legacy compatibility must not accept a reject group selecting DIRECT")
	}
}
