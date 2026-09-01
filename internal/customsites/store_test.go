package customsites

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"localclash/internal/rules"
)

func TestNormalizePatternUsesMihomoFullAndWildcardKinds(t *testing.T) {
	for _, test := range []struct {
		input string
		value string
		match string
	}{
		{input: "ABC.123.com.", value: "abc.123.com", match: MatchFull},
		{input: "abc.*CDN.com", value: "abc.*cdn.com", match: MatchWildcard},
		{input: "img?.cdn.com", value: "img?.cdn.com", match: MatchWildcard},
	} {
		value, match, err := NormalizePattern(test.input)
		if err != nil {
			t.Fatalf("NormalizePattern(%q): %v", test.input, err)
		}
		if value != test.value || match != test.match {
			t.Fatalf("NormalizePattern(%q) = %q/%q, want %q/%q", test.input, value, match, test.value, test.match)
		}
	}
	for _, input := range []string{"https://abc.com/a", "abc.com:443", "1.1.1.1", "abc com", "abc/com"} {
		if _, _, err := NormalizePattern(input); err == nil {
			t.Fatalf("NormalizePattern(%q) should fail", input)
		}
	}
}

func TestSnapshotCheckedHashesCanonicalDocumentOrder(t *testing.T) {
	first := EmptyPair()
	first.Proxy.Entries = []Entry{
		{ID: "older", Match: MatchFull, Pattern: "a.example", Sequence: 1, AddedAt: "2026-08-01T00:00:00Z"},
		{ID: "newer", Match: MatchWildcard, Pattern: "*.example", Sequence: 2, AddedAt: "2026-08-02T00:00:00Z"},
	}
	second := first
	second.Proxy.Entries = []Entry{first.Proxy.Entries[1], first.Proxy.Entries[0]}
	one, err := SnapshotChecked(first)
	if err != nil {
		t.Fatal(err)
	}
	two, err := SnapshotChecked(second)
	if err != nil {
		t.Fatal(err)
	}
	if one.ProxySHA256 == "" || one.DirectSHA256 == "" || one.ProxySHA256 != two.ProxySHA256 || one.DirectSHA256 != two.DirectSHA256 {
		t.Fatalf("snapshots one=%+v two=%+v, want stable canonical hashes", one, two)
	}
}

func TestLastAddedRuleSortsFirstAcrossDocuments(t *testing.T) {
	pair := EmptyPair()
	var err error
	pair, older, err := Add(pair, RouteDirect, "abc.com", time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	pair, newer, err := Add(pair, RouteProxy, "abc.com", time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if newer.Sequence <= older.Sequence {
		t.Fatalf("newer sequence %d must exceed older %d", newer.Sequence, older.Sequence)
	}

	selection := testSelection()
	resolved, err := ApplyToSelection(selection, pair)
	if err != nil {
		t.Fatal(err)
	}
	if len(resolved.PriorityCustomRules) != 2 {
		t.Fatalf("priority rules = %+v", resolved.PriorityCustomRules)
	}
	if got := resolved.PriorityCustomRules[0].Target; got != ProxyPolicyGroup {
		t.Fatalf("newest target = %q, want %q", got, ProxyPolicyGroup)
	}
	if got := resolved.PriorityCustomRules[1].Target; got != DirectPolicyGroup {
		t.Fatalf("older target = %q, want %q", got, DirectPolicyGroup)
	}
	if got := resolved.PriorityCustomRules[1].Rules[0].Type; got != "domain_suffix" {
		t.Fatalf("plain custom site rule type = %q, want domain_suffix", got)
	}

	pair, _, err = Delete(pair, newer.ID)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err = ApplyToSelection(selection, pair)
	if err != nil {
		t.Fatal(err)
	}
	if len(resolved.PriorityCustomRules) != 1 || resolved.PriorityCustomRules[0].Target != DirectPolicyGroup {
		t.Fatalf("after delete priority rules = %+v, want older direct rule", resolved.PriorityCustomRules)
	}
}

func TestApplyToSelectionUsesRequiredAndAvailableRegionalExits(t *testing.T) {
	selection := testSelection()
	selection.ProxyGroups["🇭🇰 香港节点"] = rules.ProxyGroup{Auto: true, Optional: true, Nodes: []string{"HK 1"}}
	selection.ProxyGroups["🇯🇵 日本节点"] = rules.ProxyGroup{Auto: true, Optional: true}
	selection.ProxyGroups["ChatGPT-available"] = rules.ProxyGroup{Auto: true, Optional: true, Nodes: []string{"US 1"}}
	pair := EmptyPair()
	pair, _, _ = Add(pair, RouteProxy, "abc.*cdn.com", time.Now())

	resolved, err := ApplyToSelection(selection, pair)
	if err != nil {
		t.Fatal(err)
	}
	exits := resolved.PolicyGroups[ProxyPolicyGroup].Exits
	joined := strings.Join(exits, "|")
	if joined != RequiredAutoExit+"|"+RequiredManualExit+"|🇭🇰 香港节点" {
		t.Fatalf("custom proxy exits = %q", joined)
	}
	if got := resolved.PolicyGroups[DirectPolicyGroup].Exits; len(got) != 1 || got[0] != rules.TerminalDirect {
		t.Fatalf("custom direct exits = %+v", got)
	}
	if got := resolved.RequiredTargets; len(got) < 2 || got[len(got)-2] != ProxyPolicyGroup || got[len(got)-1] != DirectPolicyGroup {
		t.Fatalf("required targets = %+v, want both reserved policy groups", got)
	}
}

func TestLoadRequiresBothStrictDocuments(t *testing.T) {
	dir := t.TempDir()
	paths := DefaultPaths(dir)
	if err := os.MkdirAll(filepath.Dir(paths.Proxy), 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := MarshalDocument(EmptyPair().Proxy)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.Proxy, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(paths); err == nil || !strings.Contains(err.Error(), "state is incomplete") {
		t.Fatalf("Load error = %v, want incomplete-state failure", err)
	}
	if err := os.WriteFile(paths.Direct, []byte(`{"version":1,"route":"direct","entries":[],"unknown":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(paths); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("Load error = %v, want strict decode failure", err)
	}
}

func TestValidatePairRejectsDuplicateSequence(t *testing.T) {
	pair := EmptyPair()
	pair.Proxy.Entries = []Entry{{ID: "proxy", Match: MatchFull, Pattern: "a.com", Sequence: 1, AddedAt: time.Now().UTC().Format(time.RFC3339Nano)}}
	pair.Direct.Entries = []Entry{{ID: "direct", Match: MatchFull, Pattern: "b.com", Sequence: 1, AddedAt: time.Now().UTC().Format(time.RFC3339Nano)}}
	if err := ValidatePair(pair); err == nil || !strings.Contains(err.Error(), "duplicate custom site sequence") {
		t.Fatalf("ValidatePair error = %v", err)
	}
}

func TestApplyToSelectionRejectsReservedNameInSharedTargetNamespace(t *testing.T) {
	selection := testSelection()
	selection.ProxyGroups[ProxyPolicyGroup] = rules.ProxyGroup{Manual: true, Nodes: []string{"A"}}
	if _, err := ApplyToSelection(selection, EmptyPair()); err == nil || !strings.Contains(err.Error(), "reserved_policy_group_name") {
		t.Fatalf("ApplyToSelection error = %v, want reserved name failure", err)
	}
}

func testSelection() rules.Selection {
	return rules.Selection{
		Version: 1,
		ProxyGroups: map[string]rules.ProxyGroup{
			RequiredAutoExit:   {Auto: true, Nodes: []string{"A"}},
			RequiredManualExit: {Manual: true, Nodes: []string{"A"}},
		},
		PolicyGroups: map[string]rules.PolicyGroup{},
	}
}
