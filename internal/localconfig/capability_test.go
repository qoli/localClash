package localconfig

import (
	"reflect"
	"strings"
	"testing"

	"localclash/internal/smartpolicy"
)

func TestResolveCapabilityProxyGroup(t *testing.T) {
	priority := []smartpolicy.Rule{{Label: "US", Pattern: "US", Factor: 5}, {Label: "Other", Pattern: ".*", Factor: 1}}
	config := Config{
		ProxyGroups: map[string]ProxyGroup{
			"ChatGPT-available": {
				Mode:          "smart",
				Capability:    "openai.chatgpt.mobile.v1",
				SmartPriority: priority,
				Optional:      true,
			},
		},
	}
	resolved, err := Resolve(ResolveOptions{
		Config: config,
		SubscriptionNodes: []SubscriptionNode{
			{Name: "US 01"},
			{Name: "JP 01"},
		},
		CapabilityNodes: map[string][]string{
			"openai.chatgpt.mobile.v1": {"US 01"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	group := resolved.Config.ProxyGroups["ChatGPT-available"]
	if !reflect.DeepEqual(group.SelectedNodes, []string{"US 01"}) {
		t.Fatalf("selected nodes = %+v", group.SelectedNodes)
	}
	if got := resolved.Selection.ProxyGroups["ChatGPT-available"].Nodes; !reflect.DeepEqual(got, []string{"US 01"}) {
		t.Fatalf("selection nodes = %+v", got)
	}
	if got := resolved.Selection.ProxyGroups["ChatGPT-available"].SmartPriority; !reflect.DeepEqual(got, priority) {
		t.Fatalf("selection smart priority = %#v, want %#v", got, priority)
	}
	resolved.Selection.ProxyGroups["ChatGPT-available"].SmartPriority[0].Factor = 99
	if config.ProxyGroups["ChatGPT-available"].SmartPriority[0].Factor != 5 {
		t.Fatalf("Resolve mutated input smart priority: %+v", config.ProxyGroups["ChatGPT-available"].SmartPriority)
	}
}

func TestResolveRejectsSmartPriorityOnManualGroup(t *testing.T) {
	_, err := Resolve(ResolveOptions{Config: Config{ProxyGroups: map[string]ProxyGroup{
		"Manual": {Mode: "manual", Nodes: []string{"US 01"}, SmartPriority: []smartpolicy.Rule{{Label: "US", Pattern: "US", Factor: 5}}},
	}}, SubscriptionNodes: []SubscriptionNode{{Name: "US 01"}}})
	if err == nil || !strings.Contains(err.Error(), "requires auto or smart mode") {
		t.Fatalf("error = %v, want smart_priority mode rejection", err)
	}
}

func TestResolveOptionalCapabilityAllowsExplicitEmptyQualification(t *testing.T) {
	resolved, err := Resolve(ResolveOptions{
		Config: Config{ProxyGroups: map[string]ProxyGroup{
			"ChatGPT-available": {Mode: "auto", Capability: "openai.chatgpt.mobile.v1", Optional: true},
		}},
		SubscriptionNodes: []SubscriptionNode{{Name: "US 01"}},
		CapabilityNodes: map[string][]string{
			"openai.chatgpt.mobile.v1": {},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	group := resolved.Selection.ProxyGroups["ChatGPT-available"]
	if !group.Optional || len(group.Nodes) != 0 {
		t.Fatalf("resolved optional capability = %+v, want explicit empty optional group", group)
	}
}

func TestResolveRequiredCapabilityRejectsEmptyQualification(t *testing.T) {
	_, err := Resolve(ResolveOptions{
		Config: Config{ProxyGroups: map[string]ProxyGroup{
			"Required": {Mode: "auto", Capability: "openai.chatgpt.mobile.v1"},
		}},
		SubscriptionNodes: []SubscriptionNode{{Name: "US 01"}},
		CapabilityNodes: map[string][]string{
			"openai.chatgpt.mobile.v1": {},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "has no nodes") {
		t.Fatalf("error = %v, want required empty capability rejection", err)
	}
}

func TestResolveCapabilityUsesPersistedSelectionOutsideRefresh(t *testing.T) {
	config := Config{
		ProxyGroups: map[string]ProxyGroup{
			"ChatGPT-available": {
				Mode:          "smart",
				Capability:    "openai.chatgpt.mobile.v1",
				SelectedNodes: []string{"US 01"},
				Optional:      true,
			},
		},
	}
	resolved, err := Resolve(ResolveOptions{Config: config, SubscriptionNodes: []SubscriptionNode{{Name: "US 01"}}})
	if err != nil {
		t.Fatal(err)
	}
	if got := resolved.Selection.ProxyGroups["ChatGPT-available"].Nodes; !reflect.DeepEqual(got, []string{"US 01"}) {
		t.Fatalf("selection nodes = %+v", got)
	}
}

func TestResolveCapabilityRequiresFreshOrPersistedSelection(t *testing.T) {
	_, err := Resolve(ResolveOptions{
		Config: Config{ProxyGroups: map[string]ProxyGroup{
			"ChatGPT-available": {Mode: "smart", Capability: "openai.chatgpt.mobile.v1", Optional: true},
		}},
		SubscriptionNodes: []SubscriptionNode{{Name: "US 01"}},
	})
	if err == nil || !strings.Contains(err.Error(), "run subscriptions_refresh") {
		t.Fatalf("error = %v, want explicit unresolved capability", err)
	}
}

func TestResolveCapabilityRejectsMixedSelectors(t *testing.T) {
	_, err := Resolve(ResolveOptions{
		Config: Config{ProxyGroups: map[string]ProxyGroup{
			"ChatGPT-available": {
				Mode:       "smart",
				Capability: "openai.chatgpt.mobile.v1",
				Nodes:      []string{"US 01"},
			},
		}},
		SubscriptionNodes: []SubscriptionNode{{Name: "US 01"}},
		CapabilityNodes: map[string][]string{
			"openai.chatgpt.mobile.v1": {"US 01"},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "cannot be combined") {
		t.Fatalf("error = %v, want mixed selector rejection", err)
	}
}

func TestResolveCapabilityDoesNotMutateInputConfig(t *testing.T) {
	config := Config{ProxyGroups: map[string]ProxyGroup{
		"ChatGPT-available": {
			Mode:       "auto",
			Capability: "openai.chatgpt.mobile.v1",
			Optional:   true,
		},
	}}
	resolved, err := Resolve(ResolveOptions{
		Config:            config,
		SubscriptionNodes: []SubscriptionNode{{Name: "US 01"}},
		CapabilityNodes: map[string][]string{
			"openai.chatgpt.mobile.v1": {"US 01"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if config.ProxyGroups["ChatGPT-available"].SelectedNodes != nil {
		t.Fatalf("input config was mutated: %+v", config.ProxyGroups["ChatGPT-available"])
	}
	if got := resolved.Config.ProxyGroups["ChatGPT-available"].SelectedNodes; !reflect.DeepEqual(got, []string{"US 01"}) {
		t.Fatalf("resolved selected nodes = %+v", got)
	}
}
