package poolcontract

import (
	"slices"
	"testing"

	"gopkg.in/yaml.v3"
)

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
		data, names, err := ProviderData(pool, input, tt.qualified)
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
	if _, _, err := ProviderData(pool, input, []string{"unknown"}); err == nil {
		t.Fatal("unrecognized qualification was accepted")
	}
}
