package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"localclash/internal/mihomoapi"
	"localclash/internal/poolcontract"
	"localclash/internal/runtimesupervision"

	"gopkg.in/yaml.v3"
)

func TestReadVideoDegradationFromManagedAutoPoolGroups(t *testing.T) {
	pool, _ := poolcontract.Lookup(poolcontract.AutoCapability)
	const group = "⚡ 自動/選擇"
	now := time.Now().UTC()
	since := now.Add(-20 * time.Minute)
	config := []byte("proxy-groups:\n" +
		"  - {name: '" + group + "', type: smart, use: [" + pool.Provider + "]}\n" +
		"  - {name: Other Smart, type: smart, use: [localclash-qualified-chatgpt]}\n" +
		"  - {name: Meta auto, type: url-test, use: [" + pool.Provider + "]}\n")
	for _, mode := range []string{"current", "older Smart", "malformed"} {
		t.Run(mode, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != "/proxies/"+group || !strings.Contains(r.URL.EscapedPath(), "%2F") || r.Header.Get("Authorization") != "Bearer test" {
					t.Errorf("unexpected Smart query: %s %s", r.Method, r.URL.EscapedPath())
					http.NotFound(w, r)
					return
				}
				result := map[string]any{"type": "Smart"}
				switch mode {
				case "current":
					result["videoDegradations"] = []map[string]any{
						{"node": "active", "network": "udp", "observed_at": now.Add(-time.Minute), "until": now.Add(4 * time.Minute)},
						{"node": "from previous round", "network": "tcp", "observed_at": now.Add(-10 * time.Minute), "until": now.Add(-5 * time.Minute)},
						{"node": "old", "network": "udp", "observed_at": since.Add(-10 * time.Minute), "until": since.Add(-5 * time.Minute)},
					}
				case "malformed":
					result["videoDegradations"] = []map[string]any{{"node": "node", "network": "udp"}}
				}
				json.NewEncoder(w).Encode(result)
			}))
			defer server.Close()
			client, err := mihomoapi.New(server.URL, "test")
			if err != nil {
				t.Fatal(err)
			}
			client.HTTPClient = server.Client()
			live := &activeRuntime{client: client}
			got, err := live.videoDegradations(t.Context(), config, pool, since)
			switch mode {
			case "current":
				if err != nil || len(got) != 2 || !got["active"] || !got["from previous round"] {
					t.Fatalf("degradation = %v, error = %v", got, err)
				}
				manual, err := live.videoDegradations(t.Context(), config, pool, time.Time{})
				if err != nil || len(manual) != 1 || !manual["active"] {
					t.Fatalf("manual refresh degradation = %v, error = %v", manual, err)
				}
			case "older Smart":
				if err != nil || got != nil {
					t.Fatalf("unsupported degradation = %v, error = %v", got, err)
				}
			case "malformed":
				if err == nil {
					t.Fatal("malformed evidence was accepted")
				}
			}
		})
	}
}

func TestProviderPublicationRestoresFileAndMembershipOnReadbackFailure(t *testing.T) {
	pool, _ := poolcontract.Lookup(poolcontract.AutoCapability)
	input, err := poolcontract.NewInput([]map[string]any{
		{"name": "before", "type": "ss"},
		{"name": "after", "type": "ss"},
	})
	if err != nil {
		t.Fatal(err)
	}
	old, oldNames, err := providerData(poolcontract.Pool{}, input, []string{"before"})
	if err != nil {
		t.Fatal(err)
	}
	replacement, newNames, err := providerData(pool, input, []string{"after"})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), pool.Provider+".yaml")
	if err := os.WriteFile(path, old, 0o600); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	members := oldNames
	var updates []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.URL.Path != providerPath(pool) {
			http.NotFound(w, r)
			return
		}
		switch r.Method {
		case http.MethodPut:
			data, err := os.ReadFile(path)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			switch {
			case bytes.Equal(data, replacement):
				updates = append(updates, "new file")
				members = []string{"unexpected"}
			case bytes.Equal(data, old):
				updates = append(updates, "old file")
				members = oldNames
			default:
				http.Error(w, "non-atomic or unexpected provider file", http.StatusBadRequest)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		case http.MethodGet:
			proxies := make([]map[string]string, 0, len(members))
			for _, name := range members {
				proxies = append(proxies, map[string]string{"name": name})
			}
			json.NewEncoder(w).Encode(map[string]any{"proxies": proxies})
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	defer server.Close()
	client, err := mihomoapi.New(server.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	client.HTTPClient = server.Client()
	live := &activeRuntime{client: client}
	err = live.publishProvider(context.Background(), pool, path, replacement, newNames)
	if err == nil || !strings.Contains(err.Error(), "membership mismatch") {
		t.Fatalf("publication error = %v, want original readback failure", err)
	}
	if got, err := os.ReadFile(path); err != nil || !bytes.Equal(got, old) {
		t.Fatalf("original provider not restored: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if !slices.Equal(members, oldNames) || !slices.Equal(updates, []string{"new file", "old file"}) {
		t.Fatalf("provider after rollback = %v with updates %v", members, updates)
	}
}

func TestDecodePoolConfigSkipsDisabledCapability(t *testing.T) {
	_, pools, err := decodePoolConfig([]byte("x-localclash-pools-disabled: true\n"))
	if err != nil || len(pools) != 0 {
		t.Fatalf("disabled configuration returned pools %v, error %v", pools, err)
	}
}

func TestVerifyPoolInputsAfterRuntimeChange(t *testing.T) {
	pool, _ := poolcontract.Lookup(poolcontract.AutoCapability)
	input, err := poolcontract.NewInput([]map[string]any{{"name": "node", "type": "ss", "port": 443}})
	if err != nil {
		t.Fatal(err)
	}
	changed, err := poolcontract.NewInput([]map[string]any{{"name": "node", "type": "ss", "port": 8443}})
	if err != nil {
		t.Fatal(err)
	}
	config := func(in poolcontract.Input, path string) []byte {
		data, err := yaml.Marshal(map[string]any{
			poolcontract.RevisionKey: in.Revision,
			"proxies":                in.Proxies,
			"proxy-providers":        map[string]any{pool.Provider: map[string]any{"type": "file", "path": path}},
		})
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	previous := runtimesupervision.State{PID: 100, CoreSHA256: "core", ConfigSHA256: "old-config"}
	for _, tt := range []struct {
		name    string
		current runtimesupervision.State
		config  []byte
		wantErr bool
	}{
		{"same inputs after restart", runtimesupervision.State{PID: 101, CoreSHA256: "core", ConfigSHA256: "old-config"}, nil, false},
		{"same inputs after config reload", runtimesupervision.State{PID: 100, CoreSHA256: "core", ConfigSHA256: "new-config"}, config(input, pool.Path(input.Revision)), false},
		{"subscription changed", runtimesupervision.State{PID: 100, CoreSHA256: "core", ConfigSHA256: "new-config"}, config(changed, pool.Path(changed.Revision)), true},
		{"provider path changed", runtimesupervision.State{PID: 100, CoreSHA256: "core", ConfigSHA256: "new-config"}, config(input, "elsewhere.yaml"), true},
		{"core changed", runtimesupervision.State{PID: 101, CoreSHA256: "new-core", ConfigSHA256: "old-config"}, nil, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if err := verifyPoolInputs(previous, tt.current, tt.config, pool, input.Revision); (err != nil) != tt.wantErr {
				t.Fatalf("verifyPoolInputs = %v, want error: %v", err, tt.wantErr)
			}
		})
	}
}
