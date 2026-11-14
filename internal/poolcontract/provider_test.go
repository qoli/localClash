package poolcontract

import (
	"path/filepath"
	"slices"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestPreparedAutoProviderRequiresYouTubeQualificationVersion(t *testing.T) {
	pool, _ := Lookup(AutoCapability)
	path := filepath.Join(t.TempDir(), pool.Provider+".yaml")
	legacy := []byte(`{"x-localclash-qualification-version":2,"proxies":[{"name":"old","type":"ss"}]}`)
	if err := WriteFile(path, legacy); err != nil {
		t.Fatal(err)
	}
	if ready, err := PreparedProvider(pool, path); err != nil || ready {
		t.Fatalf("old provider ready = %v, error %v", ready, err)
	}

	input, err := NewInput([]map[string]any{{"name": "old", "type": "ss"}})
	if err != nil {
		t.Fatal(err)
	}
	data, _, err := ProviderData(pool, input, []string{"old"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteFile(path, data); err != nil {
		t.Fatal(err)
	}
	if ready, err := PreparedProvider(pool, path); err != nil || !ready {
		t.Fatalf("new provider ready = %v, error %v", ready, err)
	}
}

func TestProviderDataKeepsSourceOrderAndRejectsEmptyPool(t *testing.T) {
	pool, _ := Lookup(AutoCapability)
	input, err := NewInput([]map[string]any{
		{"name": "first", "type": "ss", "port": 443, "password": "fixture"},
		{"name": "second", "type": "trojan", "port": 8443, "password": "fixture"},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct{ qualified, want []string }{
		{[]string{"second", "first"}, []string{"first", "second"}},
		{nil, []string{RejectName}},
	} {
		data, names, err := ProviderData(pool, input, tt.qualified, nil)
		if err != nil || !slices.Equal(names, tt.want) {
			t.Fatalf("provider names = %v, %v; want %v", names, err, tt.want)
		}
		var provider struct{ Proxies []struct{ Name, Type string } }
		if err := yaml.Unmarshal(data, &provider); err != nil {
			t.Fatal(err)
		}
		if len(provider.Proxies) != len(names) {
			t.Fatalf("published %d definitions for %d names", len(provider.Proxies), len(names))
		}
		for i, p := range provider.Proxies {
			if p.Name != names[i] || (p.Name == RejectName && p.Type != "reject") {
				t.Fatalf("published definition = %+v, want %s", p, names[i])
			}
		}
	}
	if _, _, err := ProviderData(pool, input, []string{"unknown"}, nil); err == nil {
		t.Fatal("unrecognized qualification was accepted")
	}
}

func TestChatGPTProviderPublishesOnlyMeasuredCountries(t *testing.T) {
	pool, _ := Lookup(ChatGPTCapability)
	input, err := NewInput([]map[string]any{
		{"name": "US entry", "type": "ss"},
		{"name": "US label", "type": "ss", "dialer-proxy": "US entry"},
		{"name": "JP label", "type": "ss", "dialer-proxy": "US entry"},
		{"name": "invented [Statsig US]", "type": "ss", StatsigCountryKey: "US"},
	})
	if err != nil {
		t.Fatal(err)
	}
	qualified := []string{"JP label", "US label", "invented [Statsig US]"}
	data, names, err := ProviderData(pool, input, qualified, map[string]string{
		"US entry": "US", "US label": "DE", "JP label": " us ", "invented [Statsig US]": "unknown",
	})
	if err != nil || !slices.Equal(names, []string{"US label", "JP label", "invented [Statsig US]"}) {
		t.Fatalf("provider members = %v, error = %v", names, err)
	}
	path := filepath.Join(t.TempDir(), pool.Provider+".yaml")
	if err := WriteFile(path, []byte(`{"proxies":[{"name":"US label","type":"ss"}]}`)); err != nil {
		t.Fatal(err)
	}
	if ready, err := PreparedProvider(pool, path); err != nil || ready {
		t.Fatalf("legacy provider ready = %t, error = %v", ready, err)
	}
	if err := WriteFile(path, data); err != nil {
		t.Fatal(err)
	}
	if ready, err := PreparedProvider(pool, path); err != nil || !ready {
		t.Fatalf("country-aware provider ready = %t, error = %v", ready, err)
	}
	proxies, err := ReadProviderProxies(path)
	if err != nil || len(proxies) != 3 {
		t.Fatalf("read provider definitions: %v (%d)", err, len(proxies))
	}
	for i, want := range []string{"DE", "US", ""} {
		if got := proxies[i][StatsigCountryKey]; got != want {
			t.Errorf("country for %q = %v, want %q", names[i], got, want)
		}
	}
	if proxies[0]["dialer-proxy"] != "US entry" || input.Proxies[1][StatsigCountryKey] != nil ||
		input.Proxies[3][StatsigCountryKey] != "US" {
		t.Fatal("provider publication changed the input or dialer reference")
	}
	members, err := ChatGPTProviderMembers(data)
	if err != nil || members["US label"] != "US label [Statsig DE]" ||
		members["JP label"] != "JP label [Statsig US]" || members["invented [Statsig US]"] != "invented [Statsig US] [Statsig ]" {
		t.Fatalf("Smart provider members = %v, error = %v", members, err)
	}
}
