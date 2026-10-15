package runtimeverify

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestProxiesReadback(t *testing.T) {
	for _, tc := range []struct {
		name       string
		response   string
		missing    []string
		unexpected []string
		wantError  bool
	}{
		{name: "new nodes and groups loaded", response: `{"proxies":{"New":{"name":"New","password":"do-not-publish"},"Auto":{"name":"Auto"},"DIRECT":{"name":"DIRECT"},"GLOBAL":{"name":"GLOBAL"}}}`},
		{name: "Mihomo v1.19.32 built-ins", response: `{"proxies":{"New":{"name":"New"},"Auto":{"name":"Auto"},"DIRECT":{"name":"DIRECT"},"REJECT":{"name":"REJECT"},"REJECT-DROP":{"name":"REJECT-DROP"},"COMPATIBLE":{"name":"COMPATIBLE"},"PASS":{"name":"PASS"},"PASS-RULE":{"name":"PASS-RULE"},"GLOBAL":{"name":"GLOBAL"}}}`},
		{name: "missing new node", response: `{"proxies":{"Auto":{"name":"Auto"}}}`, missing: []string{"New"}, wantError: true},
		{name: "retired node still loaded", response: `{"proxies":{"New":{"name":"New"},"Auto":{"name":"Auto"},"Old":{"name":"Old"}}}`, unexpected: []string{"Old"}, wantError: true},
		{name: "missing group", response: `{"proxies":{"New":{"name":"New"}}}`, missing: []string{"Auto"}, wantError: true},
		{name: "invalid proxy record", response: `{"proxies":{"New":null,"Auto":{"name":"Auto"}}}`, missing: []string{"New"}, wantError: true},
		{name: "empty response", response: `{}`, wantError: true},
		{name: "invalid JSON", response: `<html>error</html>`, wantError: true},
		{name: "truncated JSON", response: `{"proxies":{"New":{"name":"New"}},`, wantError: true},
		{name: "multiple JSON documents", response: `{"proxies":{"New":{"name":"New"},"Auto":{"name":"Auto"}}}{}`, wantError: true},
		{name: "large inventory metadata", response: `{"proxies":{"New":{"name":"New","metadata":"` + strings.Repeat("x", 2*1024*1024) + `"},"Auto":{"name":"Auto"}}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/proxies" || r.Header.Get("Authorization") != "Bearer test-secret" {
					t.Error("wrong controller endpoint or authentication")
				}
				_, _ = w.Write([]byte(tc.response))
			}))
			defer server.Close()
			config := filepath.Join(t.TempDir(), "config.yaml")
			data := "external-controller: " + strings.TrimPrefix(server.URL, "http://") + "\nsecret: test-secret\nproxies:\n  - {name: New, password: do-not-publish}\nproxy-groups:\n  - {name: Auto, type: select, proxies: [New]}\n"
			if err := os.WriteFile(config, []byte(data), 0o600); err != nil {
				t.Fatal(err)
			}
			before, _ := os.ReadFile(config)
			result, err := Proxies(context.Background(), config)
			if (err != nil) != tc.wantError || result.Verified == tc.wantError {
				t.Fatalf("result = %+v, err = %v", result, err)
			}
			if tc.missing != nil && !reflect.DeepEqual(result.Missing, tc.missing) {
				t.Fatalf("missing = %v, want %v", result.Missing, tc.missing)
			}
			if tc.unexpected != nil && !reflect.DeepEqual(result.Unexpected, tc.unexpected) {
				t.Fatalf("unexpected = %v, want %v", result.Unexpected, tc.unexpected)
			}
			encoded, _ := json.Marshal(result)
			if strings.Contains(string(encoded), "do-not-publish") || strings.Contains(string(encoded), "test-secret") {
				t.Fatal("verification leaked credentials")
			}
			after, _ := os.ReadFile(config)
			if string(before) != string(after) {
				t.Fatal("verification mutated config")
			}
		})
	}
}

func TestProxiesAcceptsOnlyDeclaredProviderNodes(t *testing.T) {
	for _, stale := range []bool{false, true} {
		t.Run(map[bool]string{false: "declared", true: "stale provider"}[stale], func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/providers/proxies" {
					_, _ = w.Write([]byte(`{"providers":{"Live":{"proxies":[{"name":"ProviderNode"}]},"Retired":{"proxies":[{"name":"OldNode"}]}}}`))
					return
				}
				proxies := map[string]any{"New": map[string]string{"name": "New"}, "ProviderNode": map[string]string{"name": "ProviderNode"}}
				if stale {
					proxies["OldNode"] = map[string]string{"name": "OldNode"}
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"proxies": proxies})
			}))
			defer server.Close()
			config := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(config, []byte("external-controller: "+strings.TrimPrefix(server.URL, "http://")+"\nproxies: [{name: New}]\nproxy-providers: {Live: {type: file}}\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			result, err := Proxies(context.Background(), config)
			if (err != nil) != stale || result.Verified == stale {
				t.Fatalf("result = %+v, err = %v", result, err)
			}
		})
	}
}

func TestProxiesFailsIfConfigChangesDuringReadback(t *testing.T) {
	config := filepath.Join(t.TempDir(), "config.yaml")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if err := os.WriteFile(config, []byte("proxies: [{name: Changed}]\n"), 0o600); err != nil {
			t.Error(err)
		}
		_, _ = w.Write([]byte(`{"proxies":{"New":{"name":"New"}}}`))
	}))
	defer server.Close()
	if err := os.WriteFile(config, []byte("external-controller: "+strings.TrimPrefix(server.URL, "http://")+"\nproxies: [{name: New}]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := Proxies(context.Background(), config)
	if err == nil || result.Verified || !strings.Contains(err.Error(), "changed during") {
		t.Fatalf("result = %+v, err = %v", result, err)
	}
}
