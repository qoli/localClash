package localconfig

import (
	"reflect"
	"strings"
	"testing"
)

func TestResolveCapabilityProxyGroup(t *testing.T) {
	config := Config{
		ProxyGroups: map[string]ProxyGroup{
			"ChatGPT-available": {
				Mode:       "smart",
				Capability: "openai.chatgpt.mobile.v1",
				Optional:   true,
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
