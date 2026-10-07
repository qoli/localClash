package mcp

import (
	"encoding/gob"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"localclash/internal/appinit"
	"localclash/internal/customsites"
	"localclash/internal/rules"
)

func TestToolsCallCustomSitesListReturnsDurableSharedSnapshot(t *testing.T) {
	root := t.TempDir()
	paths := customsites.DefaultPaths(root)
	pair := customsites.EmptyPair()
	var err error
	pair, _, err = customsites.Add(pair, customsites.RouteDirect, "older.example", time.Date(2026, 10, 1, 1, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	pair, _, err = customsites.Add(pair, customsites.RouteProxy, "newer.*.example", time.Date(2026, 10, 1, 2, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	writeCustomSiteDocument(t, paths.Proxy, pair.Proxy)
	writeCustomSiteDocument(t, paths.Direct, pair.Direct)

	server := NewServerWithState(appinit.RuntimeState{Paths: appinit.RuntimePaths{
		CustomSitesProxy:  paths.Proxy,
		CustomSitesDirect: paths.Direct,
		MihomoRuntimeDir:  filepath.Join(root, ".runtime", "mihomo"),
	}})
	resp := callHandleWithServer(t, server, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "custom_sites_list",
			"arguments": map[string]any{},
		},
	})
	if resp.Error != nil {
		t.Fatalf("custom_sites_list returned JSON-RPC error: %+v", resp.Error)
	}
	result := marshalToolResult(t, resp.Result)
	content := result.StructuredContent.(map[string]any)
	if content["ok"] != true || !strings.Contains(content["summary"].(string), "coexists") {
		t.Fatalf("content = %+v", content)
	}
	snapshot := content["custom_sites"].(map[string]any)
	if snapshot["initialized"] != true || snapshot["proxy_count"] != float64(1) || snapshot["direct_count"] != float64(1) || snapshot["max_sequence"] != float64(2) {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	if strings.Contains(result.Content[0].Text, paths.Proxy) || strings.Contains(result.Content[0].Text, paths.Direct) {
		t.Fatalf("custom_sites_list leaked internal paths: %s", result.Content[0].Text)
	}
}

func TestToolsCallCustomSitesListReportsUninitializedState(t *testing.T) {
	root := t.TempDir()
	paths := customsites.DefaultPaths(root)
	server := NewServerWithState(appinit.RuntimeState{Paths: appinit.RuntimePaths{
		CustomSitesProxy:  paths.Proxy,
		CustomSitesDirect: paths.Direct,
	}})
	resp := callHandleWithServer(t, server, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "custom_sites_list",
			"arguments": map[string]any{},
		},
	})
	if resp.Error != nil {
		t.Fatalf("custom_sites_list returned JSON-RPC error: %+v", resp.Error)
	}
	result := marshalToolResult(t, resp.Result)
	snapshot := result.StructuredContent.(map[string]any)["custom_sites"].(map[string]any)
	if snapshot["initialized"] != false || snapshot["proxy_count"] != float64(0) || snapshot["direct_count"] != float64(0) {
		t.Fatalf("snapshot = %+v", snapshot)
	}
}

func TestToolsCallCustomSitesTransactReturnsStructuredFailure(t *testing.T) {
	root := t.TempDir()
	paths := customsites.DefaultPaths(root)
	server := NewServerWithState(appinit.RuntimeState{Paths: appinit.RuntimePaths{
		CustomSitesProxy:  paths.Proxy,
		CustomSitesDirect: paths.Direct,
		MihomoRuntimeDir:  filepath.Join(root, ".runtime", "mihomo"),
		GeneratedConfig:   filepath.Join(root, ".runtime", "mihomo", "config.yaml"),
	}})
	resp := callHandleWithServer(t, server, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "custom_sites_transact",
			"arguments": map[string]any{
				"version":   2,
				"operation": "add",
				"pattern":   "example.com",
				"route":     "proxy",
				"wait":      true,
			},
		},
	})
	if resp.Error != nil {
		t.Fatalf("custom_sites_transact returned JSON-RPC error instead of structured transaction failure: %+v", resp.Error)
	}
	result := marshalToolResult(t, resp.Result)
	if !result.IsError {
		t.Fatalf("result = %+v, want isError", result)
	}
	content := result.StructuredContent.(map[string]any)
	if content["ok"] != false || content["code"] != "custom_sites_transaction_failed" || !strings.Contains(content["error"].(string), "version must be 1") {
		t.Fatalf("content = %+v", content)
	}
	if _, ok := content["apply"].(map[string]any); !ok {
		t.Fatalf("content = %+v, want apply rollback evidence", content)
	}
}

func TestToolsCallCustomSitesTransactPromotesStoppedRuntimeState(t *testing.T) {
	root := t.TempDir()
	paths := customsites.DefaultPaths(root)
	runtimeDir := filepath.Join(root, ".runtime", "mihomo")
	corePath := filepath.Join(root, "mihomo-test")
	writeExecutable(t, corePath, "#!/bin/sh\nif [ \"$1\" = \"-v\" ]; then echo 'Mihomo Meta test'; exit 0; fi\necho ok\nexit 0\n")
	subscriptionPath := filepath.Join(root, "subscription.gob")
	writeSubscriptionArtifact(t, subscriptionPath, `proxies:
  - name: "Test 01"
    type: ss
    server: example.invalid
    port: 443
    cipher: none
    password: test
`)
	selectionPath := filepath.Join(root, "localclash-packs.gob")
	if err := rules.WriteSelection(selectionPath, rules.Selection{
		Version: 1,
		ProxyGroups: map[string]rules.ProxyGroup{
			customsites.RequiredAutoExit:   {Auto: true, Nodes: []string{"Test 01"}},
			customsites.RequiredManualExit: {Manual: true, Nodes: []string{"Test 01"}},
		},
		EnabledPack:    []rules.SelectedPack{},
		FallbackTarget: rules.TerminalDirect,
	}); err != nil {
		t.Fatal(err)
	}
	rulesCache := filepath.Join(root, ".runtime", "rules", "packs")
	if err := os.MkdirAll(rulesCache, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := rules.WritePackIndex(rules.PackIndexPath(rulesCache), map[string]rules.PackCache{
		"fixture": {
			Version: 1, Source: "fixture", Adapter: "fixture", Renderable: true,
			Packs: []rules.Pack{{ID: "unused", Name: "Unused", Renderable: true}},
		},
	}); err != nil {
		t.Fatal(err)
	}
	server := NewServerWithState(appinit.RuntimeState{Paths: appinit.RuntimePaths{
		WorkspaceRoot:      root,
		RuntimeRoot:        filepath.Join(root, ".runtime"),
		RulesCacheDir:      rulesCache,
		GeneratedConfig:    filepath.Join(runtimeDir, "config.yaml"),
		SubscriptionPath:   subscriptionPath,
		MihomoRuntimeDir:   runtimeDir,
		CorePath:           corePath,
		PacksSelectionPath: selectionPath,
		RuntimeProfilePath: filepath.Join(root, "localclash-runtime.json"),
		CustomSitesProxy:   paths.Proxy,
		CustomSitesDirect:  paths.Direct,
	}})
	resp := callHandleWithServer(t, server, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "custom_sites_transact",
			"arguments": map[string]any{
				"version":   1,
				"operation": "add",
				"pattern":   "MCP.Example.",
				"route":     "proxy",
				"wait":      true,
			},
		},
	})
	if resp.Error != nil {
		t.Fatalf("custom_sites_transact returned JSON-RPC error: %+v", resp.Error)
	}
	result := marshalToolResult(t, resp.Result)
	if result.IsError {
		t.Fatalf("result = %+v, want success", result)
	}
	content := result.StructuredContent.(map[string]any)
	if content["ok"] != true || content["changed"] != true || !strings.Contains(content["summary"].(string), "runtime is stopped") {
		t.Fatalf("content = %+v", content)
	}
	entry := content["entry"].(map[string]any)
	if entry["pattern"] != "mcp.example" || entry["route"] != customsites.RouteProxy || entry["match"] != customsites.MatchFull {
		t.Fatalf("entry = %+v", entry)
	}
	apply := content["apply"].(map[string]any)
	if apply["validated"] != true || apply["promoted"] != true || apply["pending_next_start"] != true || apply["effective"] != false {
		t.Fatalf("apply = %+v", apply)
	}
	if _, ok := apply["generated_config"]; ok {
		t.Fatalf("apply leaked generated config path: %+v", apply)
	}
	if _, ok := apply["attestation"]; ok {
		t.Fatalf("apply leaked attestation path: %+v", apply)
	}
	pair, err := customsites.Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	if pair.Proxy.Entries[0].Pattern != "mcp.example" {
		t.Fatalf("durable pair = %+v", pair)
	}
}

func TestToolsCallCustomSitesTransactRejectsUnknownArgumentsBeforeQueue(t *testing.T) {
	root := t.TempDir()
	paths := customsites.DefaultPaths(root)
	server := NewServerWithState(appinit.RuntimeState{Paths: appinit.RuntimePaths{
		CustomSitesProxy:  paths.Proxy,
		CustomSitesDirect: paths.Direct,
	}})
	resp := callHandleWithServer(t, server, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "custom_sites_transact",
			"arguments": map[string]any{
				"version":   1,
				"operation": "add",
				"pattern":   "example.com",
				"route":     "proxy",
				"path":      "/tmp/forbidden",
			},
		},
	})
	if resp.Error == nil || !strings.Contains(resp.Error.Message, `unknown field "path"`) {
		t.Fatalf("response = %+v, want unknown path rejection", resp)
	}
}

func writeCustomSiteDocument(t *testing.T, path string, document customsites.Document) {
	t.Helper()
	data, err := customsites.MarshalDocument(document)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeExecutable(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}

func writeSubscriptionArtifact(t *testing.T, path, raw string) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	gob.Register(map[string]any{})
	gob.Register([]any{})
	doc := map[string]any{
		"proxies": []any{map[string]any{
			"name": "Test 01", "type": "ss", "server": "example.invalid", "port": 443,
			"cipher": "none", "password": "test",
		}},
	}
	encodeErr := gob.NewEncoder(file).Encode(struct {
		Version int
		Data    map[string]any
		Raw     []byte
	}{Version: 1, Data: doc, Raw: []byte(raw)})
	closeErr := file.Close()
	if encodeErr != nil {
		t.Fatal(encodeErr)
	}
	if closeErr != nil {
		t.Fatal(closeErr)
	}
}
