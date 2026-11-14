package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"

	"localclash/internal/mihomoapi"
	"localclash/internal/poolcontract"
)

func TestChatGPTProviderUpdateKeepsFixedExitWhenCountryChanges(t *testing.T) {
	pool, _ := poolcontract.Lookup(poolcontract.ChatGPTCapability)
	const node = "US label [Statsig US]"
	const group = "ChatGPT/available"
	input, err := poolcontract.NewInput([]map[string]any{{"name": node, "type": "ss", "dialer-proxy": "US entry"}})
	if err != nil {
		t.Fatal(err)
	}
	oldData, _, err := poolcontract.ProviderData(pool, input, []string{node}, map[string]string{node: "US"})
	if err != nil {
		t.Fatal(err)
	}
	newData, _, err := poolcontract.ProviderData(pool, input, []string{node}, map[string]string{node: "DE"})
	if err != nil {
		t.Fatal(err)
	}
	oldName, newName := poolcontract.ChatGPTSmartName(node, "US"), poolcontract.ChatGPTSmartName(node, "DE")
	for _, failSelection := range []bool{false, true} {
		scenario := "success"
		if failSelection {
			scenario = "rollback"
		}
		t.Run(scenario, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), pool.Provider+".yaml")
			if err := poolcontract.WriteFile(path, oldData); err != nil {
				t.Fatal(err)
			}
			var mu sync.Mutex
			member, fixed := oldName, oldName
			var selections []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				switch {
				case r.URL.Path == providerPath(pool) && r.Method == http.MethodGet:
					json.NewEncoder(w).Encode(map[string]any{"proxies": []map[string]string{{"name": member}}})
				case r.URL.Path == providerPath(pool) && r.Method == http.MethodPut:
					data, err := os.ReadFile(path)
					if err != nil {
						http.Error(w, err.Error(), http.StatusInternalServerError)
						return
					}
					members, err := poolcontract.ChatGPTProviderMembers(data)
					if err != nil {
						http.Error(w, err.Error(), http.StatusInternalServerError)
						return
					}
					member = members[node]
					w.WriteHeader(http.StatusNoContent)
				case r.URL.EscapedPath() == "/proxies/ChatGPT%2Favailable" && r.Method == http.MethodGet:
					json.NewEncoder(w).Encode(map[string]string{"fixed": fixed})
				case r.URL.EscapedPath() == "/proxies/ChatGPT%2Favailable" && r.Method == http.MethodPut:
					var selected struct{ Name string }
					if err := json.NewDecoder(r.Body).Decode(&selected); err != nil || selected.Name != member {
						http.Error(w, "invalid fixed selection", http.StatusBadRequest)
						return
					}
					selections = append(selections, selected.Name)
					if failSelection && selected.Name == newName {
						http.Error(w, "test selection failure", http.StatusInternalServerError)
						return
					}
					fixed = selected.Name
					w.WriteHeader(http.StatusNoContent)
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			client, err := mihomoapi.New(server.URL, "")
			if err != nil {
				t.Fatal(err)
			}
			client.HTTPClient = server.Client()
			live := &activeRuntime{client: client}
			err = live.publishProvider(t.Context(), pool, path, newData, []string{newName}, []string{group})
			mu.Lock()
			defer mu.Unlock()
			if failSelection {
				if err == nil || member != oldName || fixed != oldName || !slices.Equal(selections, []string{newName, oldName}) {
					t.Fatalf("rollback error=%v, member=%q, fixed=%q, selections=%v", err, member, fixed, selections)
				}
			} else if err != nil || member != newName || fixed != newName || !slices.Equal(selections, []string{newName}) {
				t.Fatalf("update error=%v, member=%q, fixed=%q, selections=%v", err, member, fixed, selections)
			}
		})
	}
}
