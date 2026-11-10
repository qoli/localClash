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

	"localclash/internal/mihomoapi"
	"localclash/internal/poolcontract"
)

func TestProviderPublicationRestoresFileAndMembershipOnReadbackFailure(t *testing.T) {
	pool, _ := poolcontract.Lookup(poolcontract.AutoCapability)
	input, err := poolcontract.NewInput([]map[string]any{
		{"name": "before", "type": "ss"},
		{"name": "after", "type": "ss"},
	})
	if err != nil {
		t.Fatal(err)
	}
	old, oldNames, err := providerData(input, []string{"before"})
	if err != nil {
		t.Fatal(err)
	}
	replacement, newNames, err := providerData(input, []string{"after"})
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
