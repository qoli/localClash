package mcp

import (
	"context"
	"encoding/gob"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"localclash/internal/appinit"
	"localclash/internal/mihomotest"
	"localclash/internal/routertakeover"
	"localclash/internal/rules"
	"localclash/internal/runtimeprofile"

	"gopkg.in/yaml.v3"
)

func TestHandleInitialize(t *testing.T) {
	resp := NewServer().Handle(context.Background(), []byte(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`))
	if resp == nil || resp.Error != nil {
		t.Fatalf("response error = %+v", resp)
	}
	result := resp.Result.(map[string]any)
	if result["protocolVersion"] == "" {
		t.Fatalf("initialize result = %+v, want protocolVersion", result)
	}
}

func TestHTTPHandlerServesJSONRPC(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	NewServer().HTTPHandler("/mcp").ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	var resp rpcResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Error != nil {
		t.Fatalf("http initialize error = %+v", resp.Error)
	}
	if w.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Fatalf("missing CORS header: %+v", w.Header())
	}
}

func TestHTTPHandlerLogsToolCallSummary(t *testing.T) {
	dir := t.TempDir()
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"tools_list","arguments":{"url":"https://example.invalid/secret","view":"durable"}}}`
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	server := NewServerWithState(appinit.RuntimeState{Paths: appinit.RuntimePaths{MihomoRuntimeDir: filepath.Join(dir, "mihomo")}})

	stderr := captureStderr(t, func() {
		server.HTTPHandler("/mcp").ServeHTTP(w, req)
	})
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	for _, want := range []string{"mcp_http", "rpc=tools/call", "tool=tools_list", `view="durable"`, "url=<redacted>", "response=ok"} {
		if !strings.Contains(stderr, want) {
			t.Fatalf("stderr = %q, want %q", stderr, want)
		}
	}
	if strings.Contains(stderr, "https://example.invalid/secret") {
		t.Fatalf("stderr leaked redacted URL: %q", stderr)
	}
	serviceLog := filepath.Join(dir, "logs", "mcp-http.jsonl")
	logData, err := os.ReadFile(serviceLog)
	if err != nil {
		t.Fatalf("read service log: %v", err)
	}
	logText := string(logData)
	if !strings.Contains(logText, `"event":"mcp_http"`) || !strings.Contains(logText, `"tool":"tools_list"`) || !strings.Contains(logText, `url=\u003credacted\u003e`) {
		t.Fatalf("service log = %s, want redacted mcp_http tools_list entry", logText)
	}
	if strings.Contains(logText, "https://example.invalid/secret") {
		t.Fatalf("service log leaked redacted URL: %s", logText)
	}
}

func TestHTTPHandlerServesHealth(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	NewServer().HTTPHandler("/mcp").ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	var result map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result["status"] != "ok" {
		t.Fatalf("health = %+v, want ok", result)
	}
}

func TestToolsListIncludesCoreTools(t *testing.T) {
	resp := NewServer().Handle(context.Background(), []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`))
	if resp == nil || resp.Error != nil {
		t.Fatalf("response error = %+v", resp)
	}
	data, err := json.Marshal(resp.Result)
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Tools []ListedTool `json:"tools"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	byName := map[string]ListedTool{}
	for _, tool := range result.Tools {
		byName[tool.Name] = tool
	}
	for _, name := range []string{"doctor", "environment_inspect", "config_configure", "config_status", "config_render", "config_patch_apply", "config_patch_draft", "config_patch_get", "proxy_group_build", "policy_group_build", "custom_rules_build", "rule_provider_build", "nl_file", "pack_rules_query", "pack_rules_prefetch", "pack_rules_read", "packs_list", "packs_get", "routing_explain", "subscription_nodes_list", "subscription_nodes_search", "runtime_profile_status", "runtime_status", "mihomo_api_request", "mihomo_connections_read", "mihomo_config_test", "mihomo_logs_read", "router_takeover_status", "subscriptions_status", "tools_list", "subscriptions_configure", "subscriptions_refresh", "run_runtime", "restart_runtime", "router_takeover_apply", "router_takeover_stop", "sed_file", "stop_runtime"} {
		if byName[name].Name == "" {
			t.Fatalf("missing tool %q", name)
		}
		if byName[name].SafetyLevel == "" {
			t.Fatalf("tool %q has no safety level", name)
		}
	}
	for _, name := range removedMCPTools() {
		if byName[name].Name != "" {
			t.Fatalf("removed tool %q should not be listed", name)
		}
	}
}

func TestRegistrySafetyLevels(t *testing.T) {
	want := map[string]SafetyLevel{
		"doctor":                    SafeRead,
		"environment_inspect":       SafeRead,
		"config_status":             SafeRead,
		"nl_file":                   SafeRead,
		"pack_rules_query":          SafeRead,
		"packs_get":                 SafeRead,
		"packs_list":                SafeRead,
		"subscription_nodes_list":   SafeRead,
		"subscription_nodes_search": SafeRead,
		"runtime_status":            SafeRead,
		"mihomo_connections_read":   SafeRead,
		"mihomo_logs_read":          SafeRead,
		"runtime_profile_status":    SafeRead,
		"router_takeover_status":    SafeRead,
		"routing_explain":           SafeRead,
		"subscriptions_status":      SafeRead,
		"tools_list":                SafeRead,
		"config_configure":          SafeWrite,
		"config_patch_apply":        SafeWrite,
		"config_patch_draft":        SafeWrite,
		"config_patch_get":          SafeRead,
		"config_render":             SafeWrite,
		"mihomo_api_request":        SafeWrite,
		"mihomo_config_test":        SafeWrite,
		"proxy_group_build":         SafeWrite,
		"policy_group_build":        SafeWrite,
		"custom_rules_build":        SafeWrite,
		"rule_provider_build":       SafeWrite,
		"pack_rules_prefetch":       SafeWrite,
		"pack_rules_read":           SafeWrite,
		"sed_file":                  SafeWrite,
		"subscriptions_configure":   SafeWrite,
		"subscriptions_refresh":     SafeWrite,
		"run_runtime":               ConfirmRequired,
		"restart_runtime":           ConfirmRequired,
		"router_takeover_apply":     ConfirmRequired,
		"router_takeover_stop":      ConfirmRequired,
		"stop_runtime":              ConfirmRequired,
	}
	got := map[string]SafetyLevel{}
	for _, tool := range Registry() {
		got[tool.Name] = tool.SafetyLevel
	}
	for name, level := range want {
		if got[name] != level {
			t.Fatalf("%s safety level = %q, want %q", name, got[name], level)
		}
	}
	for _, name := range removedMCPTools() {
		if _, ok := got[name]; ok {
			t.Fatalf("removed tool %q should not be registered", name)
		}
	}
	for _, tool := range Registry() {
		if tool.Name == "run_runtime" {
			if !strings.Contains(tool.Description, "network connectivity") || !strings.Contains(tool.Description, "Agent itself") || !strings.Contains(tool.Description, "disconnected") {
				t.Fatalf("run_runtime description missing risk warning: %q", tool.Description)
			}
		}
	}
}

func TestToolsCallToolsListReturnsSelfDescription(t *testing.T) {
	resp := NewServer().Handle(context.Background(), []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"tools_list","arguments":{}}}`))
	if resp == nil || resp.Error != nil {
		t.Fatalf("response error = %+v", resp)
	}
	result := marshalToolResult(t, resp.Result)
	var structured ToolsListResult
	data, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &structured); err != nil {
		t.Fatal(err)
	}
	if structured.Count != len(structured.Tools) {
		t.Fatalf("count = %d, tools = %d", structured.Count, len(structured.Tools))
	}
	if structured.Server.Binary == "" {
		t.Fatalf("server binary is empty: %+v", structured.Server)
	}
	if structured.Server.BinarySHA256 == "" {
		t.Fatalf("server binary sha256 is empty: %+v", structured.Server)
	}
	if structured.Server.WorkingDir == "" {
		t.Fatalf("server working dir is empty: %+v", structured.Server)
	}
	if structured.Server.StartedAt == "" {
		t.Fatalf("server started_at is empty: %+v", structured.Server)
	}
	byName := map[string]ToolSummary{}
	for _, tool := range structured.Tools {
		byName[tool.Name] = tool
	}
	if byName["tools_list"].SafetyLevel != SafeRead {
		t.Fatalf("tools_list safety = %q, want %q", byName["tools_list"].SafetyLevel, SafeRead)
	}
	if byName["doctor"].Name == "" || byName["subscriptions_status"].Name == "" {
		t.Fatalf("tools_list missing expected tools: %+v", byName)
	}
}

func TestToolsCallConfigConfigureAndRuntimeProfileStatus(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "localclash-runtime.json")
	server := NewServerWithState(appinit.RuntimeState{
		Paths: appinit.RuntimePaths{RuntimeProfilePath: path},
	})

	configure := callHandleWithServer(t, server, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "config_configure",
			"arguments": map[string]any{"runtime_profile": "router", "core": "smart"},
		},
	})
	if configure.Error != nil {
		t.Fatalf("config_configure returned JSON-RPC error: %+v", configure.Error)
	}
	configureResult := marshalToolResult(t, configure.Result)
	configured := configureResult.StructuredContent.(map[string]any)
	profile := configured["runtime_profile_status"].(map[string]any)
	if profile["mode"] != "router" || profile["core"] != "smart" || profile["exists"] != true {
		t.Fatalf("configure content = %+v, want router smart and exists", configured)
	}

	statusResp := callHandleWithServer(t, server, map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "runtime_profile_status",
			"arguments": map[string]any{},
		},
	})
	if statusResp.Error != nil {
		t.Fatalf("runtime_profile_status returned JSON-RPC error: %+v", statusResp.Error)
	}
	statusResult := marshalToolResult(t, statusResp.Result)
	status := statusResult.StructuredContent.(map[string]any)
	if status["mode"] != "router" || status["core"] != "smart" || status["path"] != path {
		t.Fatalf("status = %+v, want router smart at configured path", status)
	}
	if want := filepath.Join(dir, runtimeprofile.SmartCorePath); server.state.Paths.CorePath != want {
		t.Fatalf("server core path = %q, want %q", server.state.Paths.CorePath, want)
	}
}

func TestMCPDefaultsUseWorkspaceRootWhenProcessCWDIsElsewhere(t *testing.T) {
	root, wrongDir, state := setupMCPWorkspaceRootFixture(t)
	t.Chdir(wrongDir)
	server := NewServerWithState(state)

	statusResp := callHandleWithServer(t, server, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "config_status",
			"arguments": map[string]any{},
		},
	})
	if statusResp.Error != nil {
		t.Fatalf("config_status returned JSON-RPC error: %+v", statusResp.Error)
	}
	statusResult := marshalToolResult(t, statusResp.Result)
	status := statusResult.StructuredContent.(map[string]any)
	source := status["source_of_truth"].(map[string]any)
	if source["path"] != filepath.Join(root, "patches") {
		t.Fatalf("source_of_truth = %+v, want workspace-root patches dir", source)
	}
	compiled := status["compiled_intent"].(map[string]any)
	if compiled["path"] != filepath.Join(root, "localclash-intent.json") || compiled["present"] != true {
		t.Fatalf("compiled_intent = %+v, want workspace-root intent", compiled)
	}
	inputs := status["inputs"].(map[string]any)
	if inputs["selection"] != filepath.Join(root, "localclash-packs.gob") || inputs["patches_dir"] != filepath.Join(root, "patches") {
		t.Fatalf("inputs = %+v, want workspace-root selection and patches", inputs)
	}

	routeResp := callHandleWithServer(t, server, map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "routing_explain",
			"arguments": map[string]any{"query": "DNSProxy", "include_rule_matches": false},
		},
	})
	if routeResp.Error != nil {
		t.Fatalf("routing_explain returned JSON-RPC error: %+v", routeResp.Error)
	}
	routeResult := marshalToolResult(t, routeResp.Result)
	route := routeResult.StructuredContent.(map[string]any)
	if route["config"] != filepath.Join(root, "localclash-intent.json") || route["config_exists"] != true || route["resolved"] != true {
		t.Fatalf("routing_explain = %+v, want resolved workspace-root config", route)
	}

	toolsResp := callHandleWithServer(t, server, map[string]any{
		"jsonrpc": "2.0",
		"id":      3,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "tools_list",
			"arguments": map[string]any{},
		},
	})
	if toolsResp.Error != nil {
		t.Fatalf("tools_list returned JSON-RPC error: %+v", toolsResp.Error)
	}
	toolsResult := marshalToolResult(t, toolsResp.Result)
	tools := toolsResult.StructuredContent.(map[string]any)
	serverInfo := tools["server"].(map[string]any)
	if serverInfo["working_dir"] != wrongDir || serverInfo["workspace_root"] != root {
		t.Fatalf("server = %+v, want working_dir %q and workspace_root %q", serverInfo, wrongDir, root)
	}

	envResp := callHandleWithServer(t, server, map[string]any{
		"jsonrpc": "2.0",
		"id":      4,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "environment_inspect",
			"arguments": map[string]any{},
		},
	})
	if envResp.Error != nil {
		t.Fatalf("environment_inspect returned JSON-RPC error: %+v", envResp.Error)
	}
	envResult := marshalToolResult(t, envResp.Result)
	env := envResult.StructuredContent.(map[string]any)
	localState := env["localclash_state"].(map[string]any)
	if localState["work_dir"] != root {
		t.Fatalf("localclash_state = %+v, want workspace root", localState)
	}
}

func TestMCPDoctorNormalizesRelativeStateCorePath(t *testing.T) {
	root, wrongDir, state := setupMCPWorkspaceRootFixture(t)
	t.Chdir(wrongDir)
	corePath := filepath.Join(root, runtimeprofile.SmartCorePath)
	if err := os.MkdirAll(filepath.Dir(corePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(corePath, []byte("#!/bin/sh\necho mihomo smart test\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	state.Paths.CorePath = runtimeprofile.SmartCorePath
	server := NewServerWithState(state)

	resp := callHandleWithServer(t, server, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "doctor",
			"arguments": map[string]any{},
		},
	})
	if resp.Error != nil {
		t.Fatalf("doctor returned JSON-RPC error: %+v", resp.Error)
	}
	result := marshalToolResult(t, resp.Result)
	content := result.StructuredContent.(map[string]any)
	checks := content["checks"].([]any)
	var core map[string]any
	for _, raw := range checks {
		check := raw.(map[string]any)
		if check["id"] == "core" {
			core = check
			break
		}
	}
	if core == nil {
		t.Fatalf("doctor checks missing core: %+v", checks)
	}
	if core["path"] != corePath || core["status"] != "ok" {
		t.Fatalf("core check = %+v, want absolute core path ok", core)
	}
}

func TestMCPFileToolsUseWorkspaceRootForAbsolutePaths(t *testing.T) {
	root, wrongDir, state := setupMCPWorkspaceRootFixture(t)
	t.Chdir(wrongDir)
	server := NewServerWithState(state)
	logPath := filepath.Join(root, ".runtime", "mcp-tasks", "task.log")
	writeMCPFile(t, logPath, "queued\ndone\n")

	nlResp := callHandleWithServer(t, server, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "nl_file",
			"arguments": map[string]any{"path": logPath},
		},
	})
	if nlResp.Error != nil {
		t.Fatalf("nl_file returned JSON-RPC error: %+v", nlResp.Error)
	}
	nlResult := marshalToolResult(t, nlResp.Result)
	if content := nlResult.StructuredContent.(map[string]any); content["path"] != logPath {
		t.Fatalf("nl_file content = %+v, want absolute path preserved", content)
	}

	outside := filepath.Join(t.TempDir(), "outside.log")
	writeMCPFile(t, outside, "outside\n")
	outsideResp := callHandleWithServer(t, server, map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "nl_file",
			"arguments": map[string]any{"path": outside},
		},
	})
	if outsideResp.Error == nil || !strings.Contains(outsideResp.Error.Message, "escapes repository root") {
		t.Fatalf("outside nl_file response = %+v, want root escape error", outsideResp)
	}

	sedResp := callHandleWithServer(t, server, map[string]any{
		"jsonrpc": "2.0",
		"id":      3,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "sed_file",
			"arguments": map[string]any{
				"path": logPath,
				"edits": []map[string]any{
					{"op": "replace", "old": "queued", "new": "started"},
				},
			},
		},
	})
	if sedResp.Error != nil {
		t.Fatalf("sed_file returned JSON-RPC error: %+v", sedResp.Error)
	}
	sedResult := marshalToolResult(t, sedResp.Result)
	if content := sedResult.StructuredContent.(map[string]any); content["dry_run"] != true || content["path"] != logPath {
		t.Fatalf("sed_file content = %+v, want dry-run under workspace root", content)
	}
}

func TestMCPAsyncTaskLogPathCanBeReadByNLFile(t *testing.T) {
	root, wrongDir, state := setupMCPWorkspaceRootFixture(t)
	t.Chdir(wrongDir)
	server := NewServerWithState(state)
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			t.Fatalf("Shutdown error = %v", err)
		}
	}()

	runResp := callHandleWithServer(t, server, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "run_runtime",
			"arguments": map[string]any{},
		},
	})
	if runResp.Error != nil {
		t.Fatalf("run_runtime returned JSON-RPC error: %+v", runResp.Error)
	}
	runResult := marshalToolResult(t, runResp.Result)
	task := runResult.StructuredContent.(map[string]any)
	logFile := task["log_file"].(string)
	if !strings.HasPrefix(logFile, filepath.Join(root, ".runtime", "mcp-tasks")) {
		t.Fatalf("log_file = %q, want workspace-root task log", logFile)
	}

	nlResp := callHandleWithServer(t, server, map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "nl_file",
			"arguments": map[string]any{"path": logFile, "limit_lines": 1},
		},
	})
	if nlResp.Error != nil {
		t.Fatalf("nl_file could not read task log %q: %+v", logFile, nlResp.Error)
	}
}

func TestToolsCallConfigConfigureDoesNotRenderGeneratedConfig(t *testing.T) {
	paths := setupMCPPlanFixture(t)
	profilePath := filepath.Join(filepath.Dir(paths.subscription), "localclash-runtime.json")
	outputPath := filepath.Join(filepath.Dir(paths.subscription), "generated", "mihomo.yaml")
	server := NewServerWithState(appinit.RuntimeState{
		Paths: appinit.RuntimePaths{
			SubscriptionPath:    paths.subscription,
			RulesCacheDir:       paths.cache,
			PacksSelectionPath:  filepath.Join(filepath.Dir(paths.subscription), "localclash-packs.gob"),
			GeneratedConfig:     outputPath,
			RuntimeProfilePath:  profilePath,
			SubscriptionConfig:  filepath.Join(filepath.Dir(paths.subscription), "localclash-subscriptions.json"),
			SubscriptionRuntime: filepath.Join(filepath.Dir(paths.subscription), ".runtime", "subscriptions"),
		},
	})

	resp := callHandleWithServer(t, server, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "config_configure",
			"arguments": map[string]any{"runtime_profile": "router"},
		},
	})
	if resp.Error != nil {
		t.Fatalf("config_configure returned JSON-RPC error: %+v", resp.Error)
	}
	result := marshalToolResult(t, resp.Result)
	content := result.StructuredContent.(map[string]any)
	profile := content["runtime_profile_status"].(map[string]any)
	if profile["mode"] != "router" {
		t.Fatalf("configure content = %+v, want router mode", content)
	}
	if _, err := os.Stat(outputPath); !os.IsNotExist(err) {
		t.Fatalf("config_configure should not render generated config, stat err = %v", err)
	}
}

func TestToolsCallConfigConfigureWritesLocalClashDefaultTemplate(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "localclash-intent.json")
	cacheDir := filepath.Join(dir, ".runtime", "rules", "packs")
	templatesDir := filepath.Join(dir, "policy-templates")
	writeMCPV2FlyTemplateCache(t, cacheDir)
	writeMCPPolicyTemplateFixture(t, templatesDir)
	server := NewServerWithState(appinit.RuntimeState{
		Paths: appinit.RuntimePaths{
			WorkspaceRoot:      dir,
			RulesCacheDir:      cacheDir,
			RuntimeProfilePath: filepath.Join(dir, "localclash-runtime.json"),
			SubscriptionPath:   filepath.Join(dir, "subscription.gob"),
			SubscriptionConfig: filepath.Join(dir, "localclash-subscriptions.json"),
		},
	})
	resp := callHandleWithServer(t, server, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "config_configure",
			"arguments": map[string]any{"policy_template": "localclash-default", "core": "smart", "runtime_profile": "router"},
		},
	})
	if resp.Error != nil {
		t.Fatalf("config_configure returned JSON-RPC error: %+v", resp.Error)
	}
	result := marshalToolResult(t, resp.Result)
	content := result.StructuredContent.(map[string]any)
	if content["config_updated"] != true || content["effective_subscription"] != false {
		t.Fatalf("config_configure content = %+v, want updated config and missing subscription", content)
	}
	patchRegistry := content["patch_registry"].(map[string]any)
	if patchRegistry["imported"] != true {
		t.Fatalf("patch_registry = %+v, want imported template patches", patchRegistry)
	}
	patchFiles, err := filepath.Glob(filepath.Join(dir, "patches", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(patchFiles) == 0 {
		t.Fatalf("patches dir has no registry files")
	}
	config := readMCPFile(t, configPath)
	for _, want := range []string{`"policy_template": "localclash-default"`, `"source": "patch_registry"`, `"source": "v2fly-dlc"`, `"pack": "openai"`, `"pack": "category-media"`, "template_all_subscription_nodes"} {
		if !strings.Contains(config, want) {
			t.Fatalf("localclash-intent.json missing %q:\n%s", want, config)
		}
	}
}

func TestToolsCallRoutingExplainExplainsLayeredDefaultRoute(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "localclash-intent.json")
	cacheDir := filepath.Join(dir, ".runtime", "rules", "packs")
	writeMCPFile(t, configPath, `version: 2
policy_template: localclash-default
generated:
  source: patch_registry
  provenance:
    "packs[v2fly-dlc/steam]":
      - default.steam.v1
    "policy_groups[Steam]":
      - default.steam.v1
    "proxy_groups[HK]":
      - default.region-exits.v1
proxy_groups:
  "⚡ 自动选择":
    mode: auto
    match:
      type: name_regex
      pattern: .*
    reason: Auto selector.
    boundary: all_subscription_nodes
  HK:
    mode: auto
    match:
      type: name_regex
      pattern: (香港|HK)
    optional: true
    reason: Hong Kong exit group.
    boundary: name_based_region_selector
  DIRECT-EXIT:
    mode: direct
    reason: Dashboard-visible direct exit.
    boundary: builtin_direct_exit
policy_groups:
  Steam:
    mode: manual
    exits:
      - DIRECT-EXIT
      - HK
      - "⚡ 自动选择"
    reason: Steam defaults to direct with fallback exits.
    boundary: business_to_exit_layer
packs:
  - source: v2fly-dlc
    pack: steam
    type: geosite
    target: Steam
    reason: Route Steam domains to the Steam business group.
`)
	writeMCPPackIndex(t, cacheDir, rules.PackCache{
		Version:    1,
		Source:     "v2fly-dlc",
		Adapter:    "v2fly-dlc",
		Renderable: true,
		Packs:      []rules.Pack{mcpV2FlyPack("steam", "Steam")},
	})
	server := NewServerWithState(appinit.RuntimeState{
		Paths: appinit.RuntimePaths{
			WorkspaceRoot:       dir,
			RulesCacheDir:       cacheDir,
			SubscriptionPath:    filepath.Join(dir, "subscription.gob"),
			SubscriptionConfig:  filepath.Join(dir, "localclash-subscriptions.json"),
			SubscriptionRuntime: filepath.Join(dir, ".runtime", "subscriptions"),
		},
	})

	resp := callHandleWithServer(t, server, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "routing_explain",
			"arguments": map[string]any{
				"query":                "Steam",
				"include_rule_matches": false,
			},
		},
	})
	if resp.Error != nil {
		t.Fatalf("routing_explain returned JSON-RPC error: %+v", resp.Error)
	}
	result := marshalToolResult(t, resp.Result)
	content := result.StructuredContent.(map[string]any)
	if content["policy_template"] != "localclash-default" {
		t.Fatalf("content = %+v, want localclash-default", content)
	}
	if content["resolved"] != false || content["resolve_error"] == "" {
		t.Fatalf("content = %+v, want intent explanation despite missing subscription", content)
	}
	routes := content["active_routes"].([]any)
	if len(routes) == 0 {
		t.Fatalf("active_routes missing in %+v", content)
	}
	route := routes[0].(map[string]any)
	if route["target"] != "Steam" || route["target_kind"] != "policy_group" {
		t.Fatalf("route = %+v, want Steam policy group route", route)
	}
	if !containsAnyString(route["provided_by_patches"], "default.steam.v1") || !containsAnyString(content["provided_by_patches"], "default.region-exits.v1") {
		t.Fatalf("provenance route=%+v top=%+v, want patch providers", route["provided_by_patches"], content["provided_by_patches"])
	}
	policy := route["policy_group"].(map[string]any)
	exits := policy["exits"].([]any)
	if len(exits) != 3 || exits[0] != "DIRECT-EXIT" || exits[1] != "HK" || exits[2] != "⚡ 自动选择" {
		t.Fatalf("policy exits = %+v, want layered exits", exits)
	}
	assertNoStructuredCompositeStrings(t, result.StructuredContent, "RULE-SET,", "GEOSITE,", "rendered_backend", "rule_template")
	if _, err := json.Marshal(result.StructuredContent); err != nil {
		t.Fatalf("routing_explain structured content is not serializable: %v", err)
	}
	if content["task_log_file"] == "" || content["task_status_file"] == "" {
		t.Fatalf("content = %+v, want sync task artifact paths", content)
	}
}

func TestToolsCallConfigRenderWritesGeneratedConfigWithoutDurableIntent(t *testing.T) {
	paths := setupMCPPlanFixture(t)
	generated := filepath.Join(filepath.Dir(paths.subscription), ".runtime", "mihomo", "config.yaml")
	server := NewServerWithState(appinit.RuntimeState{
		Paths: appinit.RuntimePaths{
			SubscriptionPath:    paths.subscription,
			RulesCacheDir:       paths.cache,
			RuntimeProfilePath:  filepath.Join(filepath.Dir(paths.subscription), "localclash-runtime.json"),
			SubscriptionConfig:  filepath.Join(filepath.Dir(paths.subscription), "localclash-subscriptions.json"),
			SubscriptionRuntime: filepath.Join(filepath.Dir(paths.subscription), ".runtime", "subscriptions"),
			GeneratedConfig:     generated,
		},
	})

	resp := callHandleWithServer(t, server, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "config_render",
			"arguments": map[string]any{"background": false},
		},
	})
	if resp.Error != nil {
		t.Fatalf("config_render returned JSON-RPC error: %+v", resp.Error)
	}
	result := marshalToolResult(t, resp.Result)
	content := result.StructuredContent.(map[string]any)
	if content["rendered"] != true || content["source"] != "base" {
		t.Fatalf("content = %+v, want rendered base config", content)
	}
	if content["task_log_file"] == "" || content["task_status_file"] == "" {
		t.Fatalf("content = %+v, want sync task artifact paths", content)
	}
	if _, err := os.Stat(generated); err != nil {
		t.Fatalf("generated config should be written, stat err=%v", err)
	}
}

func TestToolsCallConfigRenderReportsMissingSubscription(t *testing.T) {
	dir := t.TempDir()
	server := NewServerWithState(appinit.RuntimeState{
		Paths: appinit.RuntimePaths{
			SubscriptionPath:    filepath.Join(dir, "subscription.gob"),
			RulesCacheDir:       filepath.Join(dir, ".runtime", "rules", "packs"),
			RuntimeProfilePath:  filepath.Join(dir, "localclash-runtime.json"),
			SubscriptionConfig:  filepath.Join(dir, "localclash-subscriptions.json"),
			SubscriptionRuntime: filepath.Join(dir, ".runtime", "subscriptions"),
		},
	})

	resp := callHandleWithServer(t, server, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "config_render",
			"arguments": map[string]any{"background": false},
		},
	})
	if resp.Error != nil {
		t.Fatalf("config_render returned JSON-RPC error: %+v", resp.Error)
	}
	result := marshalToolResult(t, resp.Result)
	content := result.StructuredContent.(map[string]any)
	if content["rendered"] != false {
		t.Fatalf("content = %+v, want not rendered", content)
	}
	missing := content["missing_inputs"].([]any)
	if len(missing) == 0 || missing[0] != "subscription" {
		t.Fatalf("missing = %+v, want subscription", missing)
	}
	actions := content["next_actions"].([]any)
	if len(actions) == 0 {
		t.Fatalf("next_actions missing in %+v", content)
	}
}

func TestToolsCallNLFileReturnsNumberedText(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.WriteFile("config.yaml", []byte("alpha\nbeta\ngamma\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	resp := callHandle(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "nl_file",
			"arguments": map[string]any{
				"path":        "config.yaml",
				"start_line":  2,
				"limit_lines": 2,
			},
		},
	})
	if resp.Error != nil {
		t.Fatalf("nl_file returned JSON-RPC error: %+v", resp.Error)
	}
	result := marshalToolResult(t, resp.Result)
	content := result.StructuredContent.(map[string]any)
	if content["text"] != "2: beta\n3: gamma" {
		t.Fatalf("nl_file text = %q", content["text"])
	}
	if _, err := json.Marshal(result.StructuredContent); err != nil {
		t.Fatalf("nl_file structured content is not serializable: %v", err)
	}
}

func TestToolsCallSedFileDefaultsToDryRun(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.WriteFile("config.yaml", []byte("target: PROXY\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	resp := callHandle(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "sed_file",
			"arguments": map[string]any{
				"path": "config.yaml",
				"edits": []map[string]any{
					{"op": "replace", "old": "target: PROXY", "new": "target: DIRECT"},
				},
			},
		},
	})
	if resp.Error != nil {
		t.Fatalf("sed_file returned JSON-RPC error: %+v", resp.Error)
	}
	result := marshalToolResult(t, resp.Result)
	content := result.StructuredContent.(map[string]any)
	if content["dry_run"] != true || content["changed"] != true || !strings.Contains(content["diff"].(string), "+target: DIRECT") {
		t.Fatalf("sed_file content = %+v, want dry-run diff", content)
	}
	data, err := os.ReadFile("config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "target: PROXY\n" {
		t.Fatalf("file changed during dry-run: %q", data)
	}
}

func TestToolsCallSedFileCanWrite(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.WriteFile("config.yaml", []byte("a\nb\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	resp := callHandle(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "sed_file",
			"arguments": map[string]any{
				"path":    "config.yaml",
				"dry_run": false,
				"edits": []map[string]any{
					{"op": "insert_after", "line": 1, "text": "x"},
				},
			},
		},
	})
	if resp.Error != nil {
		t.Fatalf("sed_file returned JSON-RPC error: %+v", resp.Error)
	}
	result := marshalToolResult(t, resp.Result)
	content := result.StructuredContent.(map[string]any)
	if content["dry_run"] != false || content["changed"] != true {
		t.Fatalf("sed_file content = %+v, want applied change", content)
	}
	data, err := os.ReadFile("config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "a\nx\nb\n" {
		t.Fatalf("file = %q", data)
	}
}

func TestToolsCallSubscriptionsStatusReturnsSerializableResult(t *testing.T) {
	dir := t.TempDir()
	server := NewServerWithState(appinit.RuntimeState{
		Paths: appinit.RuntimePaths{
			SubscriptionConfig:  filepath.Join(dir, "localclash-subscriptions.json"),
			SubscriptionPath:    filepath.Join(dir, "subscription.gob"),
			SubscriptionRuntime: filepath.Join(dir, ".runtime", "subscriptions"),
		},
	})
	resp := callHandleWithServer(t, server, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "subscriptions_status",
			"arguments": map[string]any{},
		},
	})
	if resp.Error != nil {
		t.Fatalf("subscriptions_status returned JSON-RPC error: %+v", resp.Error)
	}
	result := marshalToolResult(t, resp.Result)
	content := result.StructuredContent.(map[string]any)
	if content["configured"] != false {
		t.Fatalf("configured = %v, want false", content["configured"])
	}
	if _, err := json.Marshal(result.StructuredContent); err != nil {
		t.Fatalf("subscriptions_status structured content is not serializable: %v", err)
	}
}

func TestToolsCallSubscriptionsConfigureReturnsSerializableResult(t *testing.T) {
	dir := t.TempDir()
	server := NewServerWithState(appinit.RuntimeState{
		Paths: appinit.RuntimePaths{
			SubscriptionConfig: filepath.Join(dir, "localclash-subscriptions.json"),
		},
	})
	resp := callHandleWithServer(t, server, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "subscriptions_configure",
			"arguments": map[string]any{
				"sources": []map[string]any{
					{"url": "https://example.com/sub?token=secret-token"},
				},
			},
		},
	})
	if resp.Error != nil {
		t.Fatalf("subscriptions_configure returned JSON-RPC error: %+v", resp.Error)
	}
	result := marshalToolResult(t, resp.Result)
	content := result.StructuredContent.(map[string]any)
	if content["configured"] != true {
		t.Fatalf("configured = %v, want true", content["configured"])
	}
	data, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("subscriptions_configure structured content is not serializable: %v", err)
	}
	if strings.Contains(string(data), "secret-token") || strings.Contains(string(data), "token=") {
		t.Fatalf("subscriptions_configure leaked token in %s", data)
	}
}

func TestToolsCallSubscriptionsConfigureRejectsSourceID(t *testing.T) {
	dir := t.TempDir()
	server := NewServerWithState(appinit.RuntimeState{
		Paths: appinit.RuntimePaths{
			SubscriptionConfig: filepath.Join(dir, "localclash-subscriptions.json"),
		},
	})
	resp := callHandleWithServer(t, server, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "subscriptions_configure",
			"arguments": map[string]any{
				"sources": []map[string]any{
					{"id": "primary", "url": "https://example.com/sub?token=secret-token"},
				},
			},
		},
	})
	if resp.Error == nil || !strings.Contains(resp.Error.Message, `unknown field "id"`) {
		t.Fatalf("error = %+v, want unknown field id", resp.Error)
	}
}

func TestToolsCallSubscriptionsRefreshReturnsSerializableResult(t *testing.T) {
	paths := setupMCPSubscriptionsFixture(t)
	server := NewServerWithState(appinit.RuntimeState{
		Paths: appinit.RuntimePaths{
			SubscriptionConfig:  paths.config,
			SubscriptionRuntime: paths.runtimeDir,
			SubscriptionPath:    paths.merged,
		},
	})
	resp := callHandleWithServer(t, server, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "subscriptions_refresh",
			"arguments": map[string]any{
				"background": false,
			},
		},
	})
	if resp.Error != nil {
		t.Fatalf("subscriptions_refresh returned JSON-RPC error: %+v", resp.Error)
	}
	result := marshalToolResult(t, resp.Result)
	content := result.StructuredContent.(map[string]any)
	if content["refreshed"] != true {
		t.Fatalf("refreshed = %v, want true", content["refreshed"])
	}
	nodeDiff := content["node_diff"].(map[string]any)
	if nodeDiff["after_count"] != float64(1) || nodeDiff["added_count"] != float64(1) {
		t.Fatalf("node_diff = %+v, want one added node", nodeDiff)
	}
	if _, err := os.Stat(paths.merged); err != nil {
		t.Fatalf("merged subscription missing: %v", err)
	}
	data, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("subscriptions_refresh structured content is not serializable: %v", err)
	}
	if strings.Contains(string(data), "secret-token") || strings.Contains(string(data), "token=") {
		t.Fatalf("subscriptions_refresh leaked token in %s", data)
	}
}

func TestToolsCallSubscriptionsRefreshAutoAppliesValidLocalClashSelector(t *testing.T) {
	paths := setupMCPPlanFixture(t)
	dir := filepath.Dir(paths.subscription)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`proxies:
  - name: SG 02
    type: ss
    server: sg2.example.com
    password: secret
`))
	}))
	t.Cleanup(server.Close)
	subConfig := filepath.Join(dir, "localclash-subscriptions.json")
	runtimeDir := filepath.Join(dir, ".runtime", "subscriptions")
	localClashConfig := filepath.Join(dir, "localclash-intent.json")
	generated := filepath.Join(dir, "generated", "mihomo.yaml")
	writeMCPFile(t, subConfig, fmt.Sprintf(`version: 1
sources:
  - id: primary
    display_name: "01"
    url: %s/sub?token=secret-token
`, server.URL))
	writeMCPFile(t, localClashConfig, `version: 1
proxy_groups:
  AI:
    mode: manual
    match:
      type: name_regex
      pattern: SG
      min: 1
    selected_nodes:
      - SG 01
    reason: Use Singapore-labelled nodes.
    boundary: name_based_hint_only
packs:
  - source: blackmatrix7
    pack: OpenAI
    target: AI
`)
	refreshServer := NewServerWithState(appinit.RuntimeState{
		Paths: appinit.RuntimePaths{
			WorkspaceRoot:       dir,
			SubscriptionConfig:  subConfig,
			SubscriptionRuntime: runtimeDir,
			SubscriptionPath:    paths.subscription,
			PacksSelectionPath:  filepath.Join(dir, "localclash-packs.gob"),
			RulesCacheDir:       paths.cache,
			RuntimeProfilePath:  filepath.Join(dir, "localclash-runtime.json"),
			GeneratedConfig:     generated,
		},
	})
	resp := callHandleWithServer(t, refreshServer, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "subscriptions_refresh",
			"arguments": map[string]any{
				"background": false,
			},
		},
	})
	if resp.Error != nil {
		t.Fatalf("subscriptions_refresh returned JSON-RPC error: %+v", resp.Error)
	}
	result := marshalToolResult(t, resp.Result)
	content := result.StructuredContent.(map[string]any)
	impact := content["localclash_config"].(map[string]any)
	if impact["applied_auto"] != true || impact["requires_agent_replan"] == true {
		t.Fatalf("localclash impact = %+v, want auto apply", impact)
	}
	if _, err := os.Stat(generated); err != nil {
		t.Fatalf("generated config missing after refresh: %v", err)
	}
	if !strings.Contains(readMCPFile(t, localClashConfig), "SG 02") {
		t.Fatalf("localclash config was not updated: %s", readMCPFile(t, localClashConfig))
	}
}

func TestToolsCallConfigStatusReturnsDurableProxyGroups(t *testing.T) {
	paths := setupMCPPlanFixture(t)
	dir := filepath.Dir(paths.subscription)
	localClashConfig := filepath.Join(dir, "localclash-intent.json")
	writeMCPFile(t, localClashConfig, `version: 1
proxy_groups:
  AI:
    mode: manual
    match:
      type: name_regex
      pattern: SG
      min: 1
    reason: Use Singapore-labelled nodes.
    boundary: name_based_hint_only
custom_rules:
  - id: huggingface_temp
    target: AI
    rules:
      - type: domain_suffix
        value: huggingface.co
packs:
  - source: blackmatrix7
    pack: OpenAI
    target: AI
`)
	resp := callHandle(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "config_status",
			"arguments": map[string]any{
				"resolve": true,
			},
		},
	})
	if resp.Error != nil {
		t.Fatalf("config_status returned JSON-RPC error: %+v", resp.Error)
	}
	result := marshalToolResult(t, resp.Result)
	content := result.StructuredContent.(map[string]any)
	intent := content["intent"].(map[string]any)
	if intent["exists"] != true || intent["valid"] != true || intent["resolved"] != true {
		t.Fatalf("intent status = %+v, want exists/valid/resolved", intent)
	}
	proxyGroups := intent["proxy_groups"].([]any)
	if len(proxyGroups) != 1 || proxyGroups[0].(map[string]any)["id"] != "AI" {
		t.Fatalf("proxy_groups = %+v, want AI", proxyGroups)
	}
	render := content["render"].(map[string]any)
	if render["recommended_tool"] != "config_render" {
		t.Fatalf("render = %+v, want config_render recommendation", render)
	}
	data, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("config_status structured content is not serializable: %v", err)
	}
	if strings.Contains(string(data), "secret") || strings.Contains(string(data), "sg.example.com") {
		t.Fatalf("config_status leaked subscription details in %s", data)
	}
}

func TestToolsCallConfigStatusRejectsInvalidUserProfile(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	runtimePath := filepath.Join(dir, "localclash-runtime.json")
	writeMCPFile(t, filepath.Join(dir, "localclash-user.json"), `rules:
  - MATCH,DIRECT
`)

	resp := callHandle(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "config_status",
			"arguments": map[string]any{},
		},
	})
	if resp.Error == nil || !strings.Contains(resp.Error.Message, "rules") || !strings.Contains(resp.Error.Message, "localclash-user.json") {
		t.Fatalf("config_status error = %+v, want banned user profile key", resp.Error)
	}
	if _, err := os.Stat(runtimePath); !os.IsNotExist(err) {
		t.Fatalf("config_status validation should not create missing runtime selector, err=%v", err)
	}
}

func TestToolsCallConfigRenderUsesDurableSourceOfTruth(t *testing.T) {
	paths := setupMCPPlanFixture(t)
	dir := filepath.Dir(paths.subscription)
	localClashConfig := filepath.Join(dir, "localclash-intent.json")
	selection := filepath.Join(dir, "localclash-packs.gob")
	generated := filepath.Join(dir, "generated", "mihomo.yaml")
	writeMCPFile(t, localClashConfig, `version: 1
proxy_groups:
  AI:
    mode: manual
    nodes: [SG 01]
packs:
  - source: blackmatrix7
    pack: OpenAI
    target: AI
`)
	server := NewServerWithState(appinit.RuntimeState{
		Paths: appinit.RuntimePaths{
			WorkspaceRoot:       dir,
			GeneratedConfig:     generated,
			SubscriptionPath:    paths.subscription,
			RulesCacheDir:       paths.cache,
			PacksSelectionPath:  selection,
			RuntimeProfilePath:  filepath.Join(dir, "localclash-runtime.json"),
			SubscriptionConfig:  filepath.Join(dir, "localclash-subscriptions.json"),
			SubscriptionRuntime: filepath.Join(dir, ".runtime", "subscriptions"),
		},
	})
	resp := callHandleWithServer(t, server, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "config_render",
			"arguments": map[string]any{
				"background": false,
			},
		},
	})
	if resp.Error != nil {
		t.Fatalf("config_render returned JSON-RPC error: %+v", resp.Error)
	}
	result := marshalToolResult(t, resp.Result)
	content := result.StructuredContent.(map[string]any)
	if content["source"] != "compiled_intent" || content["selection"] != selection {
		t.Fatalf("content = %+v, want compiled intent source and selection", content)
	}
	rendered := readMCPFile(t, generated)
	if !strings.Contains(rendered, "RULE-SET,blackmatrix7_OpenAI,AI") || !strings.Contains(rendered, "name: AI") {
		t.Fatalf("generated config missing durable pack/group:\n%s", rendered)
	}
	selectionText := readMCPFile(t, selection)
	if !strings.Contains(selectionText, `"pack": "OpenAI"`) || !strings.Contains(selectionText, `"target": "AI"`) {
		t.Fatalf("selection not derived from durable config:\n%s", selectionText)
	}
}

func TestToolsCallSubscriptionsRefreshReportsStaleExactNodes(t *testing.T) {
	paths := setupMCPPlanFixture(t)
	dir := filepath.Dir(paths.subscription)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`proxies:
  - name: SG 02
    type: ss
    server: sg2.example.com
    password: secret
`))
	}))
	t.Cleanup(server.Close)
	subConfig := filepath.Join(dir, "localclash-subscriptions.json")
	runtimeDir := filepath.Join(dir, ".runtime", "subscriptions")
	localClashConfig := filepath.Join(dir, "localclash-intent.json")
	generated := filepath.Join(dir, "generated", "mihomo.yaml")
	writeMCPFile(t, subConfig, fmt.Sprintf(`version: 1
sources:
  - id: primary
    display_name: "01"
    url: %s/sub?token=secret-token
`, server.URL))
	writeMCPFile(t, localClashConfig, `version: 1
proxy_groups:
  AI:
    mode: manual
    nodes:
      - SG 01
    selected_nodes:
      - SG 01
    reason: User explicitly selected this line.
packs:
  - source: blackmatrix7
    pack: OpenAI
    target: AI
`)
	writeMCPFile(t, generated, "sentinel: keep\n")
	refreshServer := NewServerWithState(appinit.RuntimeState{
		Paths: appinit.RuntimePaths{
			WorkspaceRoot:       dir,
			SubscriptionConfig:  subConfig,
			SubscriptionRuntime: runtimeDir,
			SubscriptionPath:    paths.subscription,
			PacksSelectionPath:  filepath.Join(dir, "localclash-packs.gob"),
			RulesCacheDir:       paths.cache,
			RuntimeProfilePath:  filepath.Join(dir, "localclash-runtime.json"),
			GeneratedConfig:     generated,
		},
	})
	resp := callHandleWithServer(t, refreshServer, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "subscriptions_refresh",
			"arguments": map[string]any{
				"background": false,
			},
		},
	})
	if resp.Error != nil {
		t.Fatalf("subscriptions_refresh returned JSON-RPC error: %+v", resp.Error)
	}
	result := marshalToolResult(t, resp.Result)
	content := result.StructuredContent.(map[string]any)
	impact := content["localclash_config"].(map[string]any)
	if impact["state"] != "stale_exact_nodes" || impact["requires_agent_replan"] == true || impact["applied_auto"] == true {
		t.Fatalf("localclash impact = %+v, want stale exact nodes without agent replan", impact)
	}
	missing := impact["missing_nodes"].([]any)
	if len(missing) != 1 || missing[0] != "SG 01" {
		t.Fatalf("missing_nodes = %+v, want SG 01", missing)
	}
	if got := readMCPFile(t, generated); got != "sentinel: keep\n" {
		t.Fatalf("generated config was overwritten: %q", got)
	}
}

func TestToolsCallSubscriptionNodesListReturnsSafeSummaries(t *testing.T) {
	subscription := setupMCPSubscriptionNodesFixture(t)
	server := NewServerWithState(appinit.RuntimeState{
		Paths: appinit.RuntimePaths{SubscriptionPath: subscription},
	})
	resp := callHandleWithServer(t, server, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "subscription_nodes_list",
			"arguments": map[string]any{
				"limit": 1,
			},
		},
	})
	if resp.Error != nil {
		t.Fatalf("subscription_nodes_list returned JSON-RPC error: %+v", resp.Error)
	}
	result := marshalToolResult(t, resp.Result)
	content := result.StructuredContent.(map[string]any)
	if content["match_basis"] != "subscription_proxy_name" {
		t.Fatalf("match_basis = %v, want subscription_proxy_name", content["match_basis"])
	}
	if content["total"] != float64(2) || content["returned"] != float64(1) {
		t.Fatalf("content = %+v, want total 2 returned 1", content)
	}
	data, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("subscription_nodes_list structured content is not serializable: %v", err)
	}
	if strings.Contains(string(data), "secret") || strings.Contains(string(data), "server") || strings.Contains(string(data), "uuid") {
		t.Fatalf("subscription_nodes_list leaked unsafe fields in %s", data)
	}
}

func TestToolsCallSubscriptionNodesSearchReturnsNameMatches(t *testing.T) {
	subscription := setupMCPSubscriptionNodesFixture(t)
	server := NewServerWithState(appinit.RuntimeState{
		Paths: appinit.RuntimePaths{SubscriptionPath: subscription},
	})
	resp := callHandleWithServer(t, server, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "subscription_nodes_search",
			"arguments": map[string]any{
				"query": "香港",
			},
		},
	})
	if resp.Error != nil {
		t.Fatalf("subscription_nodes_search returned JSON-RPC error: %+v", resp.Error)
	}
	result := marshalToolResult(t, resp.Result)
	content := result.StructuredContent.(map[string]any)
	if content["total"] != float64(1) {
		t.Fatalf("content = %+v, want one name match", content)
	}
	suggestion := content["selector_suggestion"].(map[string]any)
	if suggestion["type"] != "name_regex" || suggestion["boundary"] != "name_based_hint_only" {
		t.Fatalf("selector suggestion = %+v, want name_regex boundary", suggestion)
	}
	if !strings.Contains(fmt.Sprint(content["note"]), "do not verify network egress location") {
		t.Fatalf("note = %v, want egress boundary", content["note"])
	}
	data, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("subscription_nodes_search structured content is not serializable: %v", err)
	}
	if strings.Contains(string(data), "secret") || strings.Contains(string(data), "server") || strings.Contains(string(data), "uuid") {
		t.Fatalf("subscription_nodes_search leaked unsafe fields in %s", data)
	}
}

func TestToolsCallConfigPatchDraftReturnsSerializableResult(t *testing.T) {
	paths := setupMCPPlanFixture(t)
	server := NewServer()
	content := callConfigPatchDraftForOverlay(t, server, paths, "user.ai-test", map[string]any{
		"packs": []map[string]any{
			{"source": "blackmatrix7", "pack": "OpenAI", "target": "AI"},
		},
		"proxy_groups": []map[string]any{
			{"id": "AI", "nodes": []string{"SG 01"}, "mode": "manual"},
		},
	}, false)
	if content["valid"] != true {
		t.Fatalf("config_patch_draft valid = %v, want true", content["valid"])
	}
	if content["apply_args"] == nil || content["base_registry_hash"] == "" {
		t.Fatalf("config_patch_draft content = %+v, want apply args and base hash", content)
	}
	if _, err := json.Marshal(content); err != nil {
		t.Fatalf("config_patch_draft structured content is not serializable: %v", err)
	}
}

func TestToolsCallConfigPatchDraftSupportsPolicyGroups(t *testing.T) {
	paths := setupMCPPlanFixture(t)
	server := NewServer()
	draft := callConfigPatchDraftForOverlay(t, server, paths, "user.ai-policy", map[string]any{
		"packs": []map[string]any{
			{"source": "blackmatrix7", "pack": "OpenAI", "target": "AI"},
		},
		"proxy_groups": []map[string]any{
			{"id": "SG", "nodes": []string{"SG 01"}, "mode": "manual"},
		},
		"policy_groups": []map[string]any{
			{"id": "AI", "mode": "manual", "exits": []string{"SG", "DIRECT"}},
		},
	}, false)
	content := callConfigPatchApplyCurrentDraft(t, server, paths, draft)
	if content["applied"] != true || content["valid"] != true {
		t.Fatalf("config_patch_apply content = %+v, want applied valid", content)
	}
	config := readMCPFile(t, filepath.Join(".runtime", "mihomo", "config.yaml"))
	for _, want := range []string{"RULE-SET,blackmatrix7_OpenAI,AI", "name: AI", "- SG", "- DIRECT"} {
		if !strings.Contains(config, want) {
			t.Fatalf("generated config missing %q:\n%s", want, config)
		}
	}
}

func TestToolsCallProxyGroupBuildReturnsReusableIntent(t *testing.T) {
	paths := setupMCPPlanFixture(t)
	server := NewServerWithState(appinit.RuntimeState{
		Paths: appinit.RuntimePaths{
			SubscriptionPath:    paths.subscription,
			SubscriptionConfig:  filepath.Join(filepath.Dir(paths.subscription), "localclash-subscriptions.json"),
			SubscriptionRuntime: filepath.Join(filepath.Dir(paths.subscription), ".runtime", "subscriptions"),
		},
	})
	resp := callHandleWithServer(t, server, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "proxy_group_build",
			"arguments": map[string]any{
				"id":     "TempLine",
				"mode":   "manual",
				"nodes":  []string{"SG 01"},
				"reason": "User explicitly selected this line.",
			},
		},
	})
	if resp.Error != nil {
		t.Fatalf("proxy_group_build returned JSON-RPC error: %+v", resp.Error)
	}
	result := marshalToolResult(t, resp.Result)
	content := result.StructuredContent.(map[string]any)
	if content["target"] != "TempLine" {
		t.Fatalf("target = %v, want TempLine", content["target"])
	}
	proxyGroup := content["proxy_group"].(map[string]any)
	if proxyGroup["id"] != "TempLine" {
		t.Fatalf("proxy_group = %+v, want reusable intent with id", proxyGroup)
	}
	nodes := content["selected_nodes"].([]any)
	if len(nodes) != 1 || nodes[0] != "SG 01" {
		t.Fatalf("selected_nodes = %+v, want SG 01", nodes)
	}
}

func TestToolsCallPolicyGroupBuildReturnsReusableIntent(t *testing.T) {
	resp := callHandle(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "policy_group_build",
			"arguments": map[string]any{
				"id":     "Steam",
				"mode":   "manual",
				"exits":  []string{"HK", "JP", "direct"},
				"reason": "Steam traffic should choose from regional exits.",
			},
		},
	})
	if resp.Error != nil {
		t.Fatalf("policy_group_build returned JSON-RPC error: %+v", resp.Error)
	}
	result := marshalToolResult(t, resp.Result)
	content := result.StructuredContent.(map[string]any)
	if content["target"] != "Steam" {
		t.Fatalf("target = %v, want Steam", content["target"])
	}
	policyGroup := content["policy_group"].(map[string]any)
	if policyGroup["id"] != "Steam" || policyGroup["mode"] != "manual" {
		t.Fatalf("policy_group = %+v, want reusable Steam intent", policyGroup)
	}
	exits := policyGroup["exits"].([]any)
	want := []string{"HK", "JP", "DIRECT"}
	if len(exits) != len(want) {
		t.Fatalf("policy exits = %+v, want %+v", exits, want)
	}
	for i := range want {
		if exits[i] != want[i] {
			t.Fatalf("policy exits = %+v, want %+v", exits, want)
		}
	}
}

func TestToolsCallCustomRulesBuildReturnsReusableIntent(t *testing.T) {
	resp := callHandle(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "custom_rules_build",
			"arguments": map[string]any{
				"id":     "huggingface_temp",
				"target": "TempLine",
				"rules": []map[string]any{
					{"type": "domain_suffix", "value": "huggingface.co"},
					{"type": "domain_regex", "value": `^a[0-9]+vod-hls-pv-ta-amazon\.akamaized\.net$`},
					{"type": "geoip", "value": "telegram", "no_resolve": true},
				},
				"reason": "User asked huggingface.co to use the temporary line.",
			},
		},
	})
	if resp.Error != nil {
		t.Fatalf("custom_rules_build returned JSON-RPC error: %+v", resp.Error)
	}
	result := marshalToolResult(t, resp.Result)
	content := result.StructuredContent.(map[string]any)
	if content["target"] != "TempLine" || content["rule_count"] != float64(3) {
		t.Fatalf("custom rule content = %+v, want TempLine with three rules", content)
	}
}

func TestToolsCallRuleProviderBuildReturnsReusableIntent(t *testing.T) {
	resp := callHandle(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "rule_provider_build",
			"arguments": map[string]any{
				"id":       "US-Proxy",
				"target":   "⚡ 自动选择",
				"url":      "https://raw.githubusercontent.com/qoli/clash_yaml/refs/heads/main/us_proxy.yaml",
				"behavior": "classical",
				"reason":   "User supplied qoli US proxy rules.",
			},
		},
	})
	if resp.Error != nil {
		t.Fatalf("rule_provider_build returned JSON-RPC error: %+v", resp.Error)
	}
	result := marshalToolResult(t, resp.Result)
	content := result.StructuredContent.(map[string]any)
	if content["id"] != "US-Proxy" || content["target"] != "⚡ 自动选择" {
		t.Fatalf("rule provider content = %+v, want US-Proxy target ⚡ 自动选择", content)
	}
	provider := content["rule_provider"].(map[string]any)
	if provider["path"] != "./rule_provider/US-Proxy.yaml" || provider["interval"] != float64(86400) {
		t.Fatalf("provider defaults = %+v, want default path and interval", provider)
	}
}

func TestToolsCallConfigPatchDraftSupportsCustomRulesWithoutPacks(t *testing.T) {
	paths := setupMCPPlanFixture(t)
	server := NewServer()
	draft := callConfigPatchDraftForOverlay(t, server, paths, "user.huggingface-temp", map[string]any{
		"custom_rules": []map[string]any{
			{
				"id":     "huggingface_temp",
				"target": "TempLine",
				"rules": []map[string]any{
					{"type": "domain_suffix", "value": "huggingface.co"},
				},
			},
		},
		"proxy_groups": []map[string]any{
			{"id": "TempLine", "nodes": []string{"SG 01"}, "mode": "manual"},
		},
	}, false)
	_ = callConfigPatchApplyCurrentDraft(t, server, paths, draft)
	config := readMCPFile(t, filepath.Join(".runtime", "mihomo", "config.yaml"))
	if !strings.Contains(config, "DOMAIN-SUFFIX,huggingface.co,TempLine") || !strings.Contains(config, "name: TempLine") {
		t.Fatalf("generated config missing custom rule or proxy group:\n%s", config)
	}
}

func TestToolsCallConfigPatchDraftSupportsExternalRuleProviders(t *testing.T) {
	paths := setupMCPPlanFixture(t)
	server := NewServer()
	draft := callConfigPatchDraftForOverlay(t, server, paths, "user.us-proxy", map[string]any{
		"rule_providers": []map[string]any{
			{
				"id":       "US-Proxy",
				"target":   "⚡ 自动选择",
				"type":     "http",
				"behavior": "classical",
				"format":   "yaml",
				"path":     "./rule_provider/US-Proxy.yaml",
				"url":      "https://raw.githubusercontent.com/qoli/clash_yaml/refs/heads/main/us_proxy.yaml",
				"interval": 86400,
			},
		},
		"proxy_groups": []map[string]any{
			{"id": "⚡ 自动选择", "nodes": []string{"SG 01"}, "mode": "auto"},
		},
	}, false)
	content := callConfigPatchApplyCurrentDraft(t, server, paths, draft)
	if content["applied"] != true || content["valid"] != true {
		t.Fatalf("config_patch_apply content = %+v, want applied valid", content)
	}
	config := readMCPFile(t, filepath.Join(".runtime", "mihomo", "config.yaml"))
	if !strings.Contains(config, "RULE-SET,US-Proxy,⚡ 自动选择") || !strings.Contains(config, "US-Proxy:") || !strings.Contains(config, "us_proxy.yaml") {
		t.Fatalf("generated config missing external rule-provider:\n%s", config)
	}
}

func TestToolsCallConfigPatchApplyPersistsSelectionAndGeneratedConfig(t *testing.T) {
	paths := setupMCPPlanFixture(t)
	server := NewServer()
	draft := callConfigPatchDraftForOverlay(t, server, paths, "user.ai-test", map[string]any{
		"packs": []map[string]any{
			{"source": "blackmatrix7", "pack": "OpenAI", "target": "AI"},
		},
		"proxy_groups": []map[string]any{
			{"id": "AI", "nodes": []string{"SG 01"}, "mode": "manual"},
		},
	}, false)
	content := callConfigPatchApplyCurrentDraft(t, server, paths, draft)
	if content["applied"] != true || content["valid"] != true {
		t.Fatalf("config_patch_apply content = %+v, want applied valid", content)
	}
	if _, err := os.Stat(filepath.Join(".runtime", "mihomo", "config.yaml")); err != nil {
		t.Fatalf("generated config missing after apply: %v", err)
	}
	if _, err := os.Stat("localclash-intent.json"); err != nil {
		t.Fatalf("localclash config missing after apply: %v", err)
	}
	if _, err := json.Marshal(content); err != nil {
		t.Fatalf("config_patch_apply structured content is not serializable: %v", err)
	}
	selection := readMCPFile(t, "localclash-packs.gob")
	if !strings.Contains(selection, `"pack": "OpenAI"`) || !strings.Contains(selection, `"target": "AI"`) {
		t.Fatalf("selection was not updated: %s", selection)
	}
}

func TestToolsCallConfigPatchDraftInvalidInputReturnsError(t *testing.T) {
	setupMCPPlanFixture(t)
	resp := callHandle(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "config_patch_draft",
			"arguments": map[string]any{
				"test":       true,
				"background": false,
				"operations": []map[string]any{
					{
						"op":       "upsert_patch",
						"patch_id": "user.missing-pack",
						"order_id": "1000.000000",
						"overlay": map[string]any{
							"packs": []map[string]any{
								{"source": "blackmatrix7", "pack": "missing_pack", "target": "DIRECT"},
							},
						},
					},
				},
			},
		},
	})
	if resp.Error == nil {
		t.Fatal("expected config_patch_draft JSON-RPC error")
	}
	if !strings.Contains(resp.Error.Message, "missing_pack") {
		t.Fatalf("error = %+v, want missing pack", resp.Error)
	}
}

func TestToolsCallConfigPatchDraftRejectsLegacyPackID(t *testing.T) {
	setupMCPPlanFixture(t)
	resp := callHandle(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "config_patch_draft",
			"arguments": map[string]any{
				"test":       false,
				"background": false,
				"operations": []map[string]any{
					{
						"op":       "upsert_patch",
						"patch_id": "user.legacy-pack",
						"order_id": "1000.000000",
						"overlay": map[string]any{
							"packs": []map[string]any{
								{"id": "v2fly_dlc_geolocation__cn", "target": "DIRECT"},
							},
						},
					},
				},
			},
		},
	})
	if resp.Error == nil {
		t.Fatal("expected config_patch_draft JSON-RPC error")
	}
	if !strings.Contains(resp.Error.Message, "packs[].id is no longer supported; use packs[].source and packs[].pack from packs_list") ||
		!strings.Contains(resp.Error.Message, "Composite renderer/provider names are not MCP pack selectors") {
		t.Fatalf("error = %+v, want legacy pack id rejection", resp.Error)
	}
}

func TestToolsCallRejectsStringifiedArguments(t *testing.T) {
	resp := callHandle(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "config_patch_draft",
			"arguments": `{"operations":[{"op":"upsert_patch","patch_id":"user.bad","overlay":{"packs":[{"id":"sukkaw_ai","target":"AI_US_JP"}]}}]}`,
		},
	})
	if resp.Error == nil {
		t.Fatal("expected stringified arguments to be rejected")
	}
	if !strings.Contains(resp.Error.Message, "must be a JSON object") || !strings.Contains(resp.Error.Message, "not \"arguments\":\"{...}\"") {
		t.Fatalf("error = %+v, want object-construction guidance", resp.Error)
	}
}

func TestToolsCallPacksListReturnsSerializableResult(t *testing.T) {
	setupMCPPackCache(t)
	resp := callHandle(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "packs_list",
			"arguments": map[string]any{"name": "open"},
		},
	})
	if resp.Error != nil {
		t.Fatalf("packs_list returned JSON-RPC error: %+v", resp.Error)
	}
	result := marshalToolResult(t, resp.Result)
	content := result.StructuredContent.(map[string]any)
	if content["total"] != float64(1) {
		t.Fatalf("packs_list total = %v, want 1", content["total"])
	}
	guidance := content["guidance"].([]any)
	if len(guidance) == 0 || !strings.Contains(guidance[0].(string), "available catalog packs") {
		t.Fatalf("guidance = %+v, want catalog guidance", guidance)
	}
	packs := content["packs"].([]any)
	first := packs[0].(map[string]any)
	if first["source"] != "blackmatrix7" || first["pack"] != "OpenAI" {
		t.Fatalf("pack identity = %+v, want source/pack", first)
	}
	toolArgs := first["tool_args"].(map[string]any)
	if _, ok := toolArgs["packs_get"].(map[string]any); !ok {
		t.Fatalf("tool_args = %+v, want packs_get args", toolArgs)
	}
	if !strings.Contains(first["target_meaning"].(string), "not active configuration") {
		t.Fatalf("pack = %+v, want target meaning", first)
	}
	assertNoStructuredCompositeStrings(t, result.StructuredContent, "blackmatrix7_OpenAI", "RULE-SET,")
	if _, err := json.Marshal(result.StructuredContent); err != nil {
		t.Fatalf("packs_list structured content is not serializable: %v", err)
	}
}

func TestToolsCallPacksListReadsCurrentCatalogWhenServerStateIsStale(t *testing.T) {
	setupMCPPackCache(t)
	state := appinit.RuntimeState{
		Paths: appinit.RuntimePaths{
			RulesCacheDir: filepath.Join(".runtime", "rules", "packs"),
		},
		Rules: appinit.RulesState{
			CatalogAvailable: false,
			Diagnostic:       "stale bootstrap diagnostic",
		},
	}
	resp := callHandleWithServer(t, NewServerWithState(state), map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "packs_list",
			"arguments": map[string]any{"name": "open"},
		},
	})
	if resp.Error != nil {
		t.Fatalf("packs_list returned JSON-RPC error: %+v", resp.Error)
	}
	result := marshalToolResult(t, resp.Result)
	content := result.StructuredContent.(map[string]any)
	if content["total"] != float64(1) {
		t.Fatalf("packs_list total = %v, want 1", content["total"])
	}
}

func TestToolsCallPacksListReturnsCurrentCatalogError(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	state := appinit.RuntimeState{
		Paths: appinit.RuntimePaths{
			RulesCacheDir: filepath.Join(".runtime", "rules", "packs"),
		},
	}
	resp := callHandleWithServer(t, NewServerWithState(state), map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "packs_list",
			"arguments": map[string]any{},
		},
	})
	if resp.Error == nil || !strings.Contains(resp.Error.Message, "pack index not found: run localclash rules adapt") {
		t.Fatalf("response error = %+v, want missing pack index hard fail", resp.Error)
	}
}

func TestToolsCallPacksGetReturnsSerializableResult(t *testing.T) {
	setupMCPPackCache(t)
	resp := callHandle(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "packs_get",
			"arguments": map[string]any{"source": "blackmatrix7", "pack": "OpenAI"},
		},
	})
	if resp.Error != nil {
		t.Fatalf("packs_get returned JSON-RPC error: %+v", resp.Error)
	}
	result := marshalToolResult(t, resp.Result)
	content := result.StructuredContent.(map[string]any)
	pack := content["pack"].(map[string]any)
	if pack["source"] != "blackmatrix7" || pack["pack"] != "OpenAI" {
		t.Fatalf("pack = %+v, want blackmatrix7/OpenAI", pack)
	}
	if _, ok := pack["catalog_path"]; ok {
		t.Fatalf("pack contains catalog_path: %+v", pack)
	}
	providers := pack["providers"].([]any)
	provider := providers[0].(map[string]any)
	if _, ok := provider["name"]; ok {
		t.Fatalf("provider contains renderer name: %+v", provider)
	}
	if provider["path"] != ".runtime/mihomo/rule-packs/blackmatrix7/OpenAI.yaml" {
		t.Fatalf("provider path = %v", provider["path"])
	}
	for _, key := range []string{"url", "provider_path", "resolved_runtime_path", "provider_file_exists"} {
		if _, ok := provider[key]; ok {
			t.Fatalf("provider contains %s: %+v", key, provider)
		}
	}
	toolArgs := pack["tool_args"].(map[string]any)
	if toolArgs["packs_get"].(map[string]any)["pack"] != "OpenAI" {
		t.Fatalf("tool_args = %+v, want OpenAI pack args", toolArgs)
	}
	assertNoStructuredCompositeStrings(t, result.StructuredContent, "blackmatrix7_OpenAI", "RULE-SET,")
	if _, err := json.Marshal(result.StructuredContent); err != nil {
		t.Fatalf("packs_get structured content is not serializable: %v", err)
	}
}

func TestToolsCallPacksGetRejectsLegacyID(t *testing.T) {
	setupMCPPackCache(t)
	resp := callHandle(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "packs_get",
			"arguments": map[string]any{"id": "blackmatrix7_OpenAI"},
		},
	})
	if resp.Error == nil {
		t.Fatal("expected packs_get JSON-RPC error")
	}
	if !strings.Contains(resp.Error.Message, `{"source":"blackmatrix7","pack":"OpenAI"}`) ||
		!strings.Contains(resp.Error.Message, "Composite renderer/provider names are not MCP pack selectors") {
		t.Fatalf("error = %+v, want legacy pack id rejection", resp.Error)
	}
}

func TestToolsCallPacksGetReadsCurrentCatalogWhenServerStateIsStale(t *testing.T) {
	setupMCPPackCache(t)
	state := appinit.RuntimeState{
		Paths: appinit.RuntimePaths{
			RulesCacheDir:    filepath.Join(".runtime", "rules", "packs"),
			MihomoRuntimeDir: ".runtime/mihomo",
		},
		Rules: appinit.RulesState{
			CatalogAvailable: false,
			Diagnostic:       "stale bootstrap diagnostic",
		},
	}
	resp := callHandleWithServer(t, NewServerWithState(state), map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "packs_get",
			"arguments": map[string]any{"source": "blackmatrix7", "pack": "OpenAI"},
		},
	})
	if resp.Error != nil {
		t.Fatalf("packs_get returned JSON-RPC error: %+v", resp.Error)
	}
	result := marshalToolResult(t, resp.Result)
	pack := result.StructuredContent.(map[string]any)["pack"].(map[string]any)
	if pack["source"] != "blackmatrix7" || pack["pack"] != "OpenAI" {
		t.Fatalf("pack = %+v, want OpenAI", pack)
	}
	providers := pack["providers"].([]any)
	provider := providers[0].(map[string]any)
	if _, ok := provider["name"]; ok {
		t.Fatalf("provider contains renderer name: %+v", provider)
	}
	if provider["path"] != ".runtime/mihomo/rule-packs/blackmatrix7/OpenAI.yaml" {
		t.Fatalf("provider = %+v, want runtime-local path", provider)
	}
	if _, ok := provider["provider_file_exists"]; ok {
		t.Fatalf("provider contains provider_file_exists: %+v", provider)
	}
}

func TestToolsCallPackRulesReadReturnsRuleSamples(t *testing.T) {
	cache, providerCache, server := setupMCPPackRulesFixture(t, "DOMAIN-SUFFIX,openai.com\nDOMAIN-SUFFIX,chatgpt.com\n")
	defer server.Close()
	mcpServer := NewServerWithState(appinit.RuntimeState{
		Paths: appinit.RuntimePaths{
			RulesCacheDir: cache,
			RuntimeRoot:   filepath.Dir(filepath.Dir(providerCache)),
		},
	})
	resp := callHandleWithServer(t, mcpServer, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "pack_rules_read",
			"arguments": map[string]any{
				"source": "sukkaw",
				"pack":   "ai",
				"limit":  1,
			},
		},
	})
	if resp.Error != nil {
		t.Fatalf("pack_rules_read returned JSON-RPC error: %+v", resp.Error)
	}
	result := marshalToolResult(t, resp.Result)
	content := result.StructuredContent.(map[string]any)
	summary := content["summary"].(map[string]any)
	if summary["rule_count"] != float64(2) || summary["domain_suffix_count"] != float64(2) {
		t.Fatalf("summary = %+v, want two domain suffix rules", summary)
	}
	components := content["components"].([]any)
	component := components[0].(map[string]any)
	if component["available"] != true || component["truncated"] != true {
		t.Fatalf("component = %+v, want available truncated sample", component)
	}
	assertNoStructuredCompositeStrings(t, result.StructuredContent, "sukkaw_ai", "RULE-SET,")
	if _, err := json.Marshal(result.StructuredContent); err != nil {
		t.Fatalf("pack_rules_read structured content is not serializable: %v", err)
	}
}

func TestToolsCallPackRulesPrefetchThenQuery(t *testing.T) {
	cache, providerCache, server := setupMCPPackRulesFixture(t, "DOMAIN-SUFFIX,huggingface.co\n")
	defer server.Close()
	mcpServer := NewServerWithState(appinit.RuntimeState{
		Paths: appinit.RuntimePaths{
			RulesCacheDir: cache,
			RuntimeRoot:   filepath.Dir(filepath.Dir(providerCache)),
		},
	})
	prefetch := callHandleWithServer(t, mcpServer, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "pack_rules_prefetch",
			"arguments": map[string]any{
				"packs": []map[string]any{{"source": "sukkaw", "pack": "ai"}},
			},
		},
	})
	if prefetch.Error != nil {
		t.Fatalf("pack_rules_prefetch returned JSON-RPC error: %+v", prefetch.Error)
	}
	query := callHandleWithServer(t, mcpServer, map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "pack_rules_query",
			"arguments": map[string]any{
				"query": "cdn.huggingface.co",
			},
		},
	})
	if query.Error != nil {
		t.Fatalf("pack_rules_query returned JSON-RPC error: %+v", query.Error)
	}
	result := marshalToolResult(t, query.Result)
	content := result.StructuredContent.(map[string]any)
	matches := content["matches"].([]any)
	if len(matches) != 1 || matches[0].(map[string]any)["source"] != "sukkaw" || matches[0].(map[string]any)["pack"] != "ai" {
		t.Fatalf("matches = %+v, want sukkaw_ai hit", matches)
	}
	matchArgs := matches[0].(map[string]any)["tool_args"].(map[string]any)
	if matchArgs["pack_rules_read"].(map[string]any)["pack"] != "ai" {
		t.Fatalf("match tool_args = %+v, want ai pack args", matchArgs)
	}
	if content["cache_complete"] != true {
		t.Fatalf("content = %+v, want complete local cache", content)
	}
}

func TestToolsCallPackRulesPrefetchRejectsLegacyIDs(t *testing.T) {
	setupMCPPackCache(t)
	resp := callHandle(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "pack_rules_prefetch",
			"arguments": map[string]any{"ids": []string{"blackmatrix7_OpenAI"}},
		},
	})
	if resp.Error == nil {
		t.Fatal("expected pack_rules_prefetch JSON-RPC error")
	}
	if !strings.Contains(resp.Error.Message, `{"source":"blackmatrix7","pack":"OpenAI"}`) ||
		!strings.Contains(resp.Error.Message, "Composite renderer/provider names are not MCP pack selectors") {
		t.Fatalf("error = %+v, want legacy pack ids rejection", resp.Error)
	}
}

func TestToolsCallConfigStatusReturnsGeneratedSummary(t *testing.T) {
	config := setupMCPInspectConfig(t)
	server := NewServerWithState(appinit.RuntimeState{
		Paths: appinit.RuntimePaths{
			GeneratedConfig:    config,
			RuntimeProfilePath: filepath.Join(filepath.Dir(config), "localclash-runtime.json"),
		},
	})
	resp := callHandleWithServer(t, server, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "config_status",
			"arguments": map[string]any{
				"detail": true,
			},
		},
	})
	if resp.Error != nil {
		t.Fatalf("config_status returned JSON-RPC error: %+v", resp.Error)
	}
	result := marshalToolResult(t, resp.Result)
	content := result.StructuredContent.(map[string]any)
	generated := content["generated"].(map[string]any)
	if generated["present"] != true {
		t.Fatalf("generated = %+v, want present", generated)
	}
	summary := content["generated_summary"].(map[string]any)
	if int(summary["proxies_count"].(float64)) != 1 {
		t.Fatalf("summary = %+v, want one proxy", summary)
	}
	if summary["scope"] != "base_excluding_localclash_overlay" {
		t.Fatalf("summary scope = %v, want base_excluding_localclash_overlay", summary["scope"])
	}
	if summary["rules_count_scope"] != "generated rules after excluding x-localclash.overlay.rules" {
		t.Fatalf("rules_count_scope = %v, want explicit filtered scope", summary["rules_count_scope"])
	}
	if int(summary["rules_count"].(float64)) != 0 || int(summary["rules_total_count"].(float64)) != 1 {
		t.Fatalf("summary rules counts = %+v, want filtered 0 and total 1", summary)
	}
	guidance := content["usage_guidance"].([]any)
	if len(guidance) == 0 || !guidanceContains(guidance, "rules_total_count") {
		t.Fatalf("usage_guidance = %+v, want rule count scope warning", guidance)
	}
	assertNoStructuredCompositeStrings(t, result.StructuredContent, "blackmatrix7_OpenAI", "RULE-SET,")
	if _, err := json.Marshal(result.StructuredContent); err != nil {
		t.Fatalf("config_status structured content is not serializable: %v", err)
	}
}

func assertNoStructuredCompositeStrings(t *testing.T, value any, forbidden ...string) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("structured content is not serializable: %v", err)
	}
	text := string(data)
	for _, needle := range forbidden {
		if strings.Contains(text, needle) {
			t.Fatalf("structured content contains composite %q: %s", needle, text)
		}
	}
}

func guidanceContains(values []any, needle string) bool {
	for _, value := range values {
		if strings.Contains(value.(string), needle) {
			return true
		}
	}
	return false
}

func TestToolsCallConfigStatusReturnsOverlaySummary(t *testing.T) {
	config := setupMCPInspectConfig(t)
	server := NewServerWithState(appinit.RuntimeState{
		Paths: appinit.RuntimePaths{
			GeneratedConfig:    config,
			RuntimeProfilePath: filepath.Join(filepath.Dir(config), "localclash-runtime.json"),
		},
	})
	resp := callHandleWithServer(t, server, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "config_status",
			"arguments": map[string]any{
				"detail": true,
			},
		},
	})
	if resp.Error != nil {
		t.Fatalf("config_status returned JSON-RPC error: %+v", resp.Error)
	}
	result := marshalToolResult(t, resp.Result)
	content := result.StructuredContent.(map[string]any)
	overlay := content["overlay"].(map[string]any)
	if overlay["layer"] != "overlay" || overlay["modifiable"] != true {
		t.Fatalf("overlay = %+v, want overlay modifiable", overlay)
	}
	if _, err := json.Marshal(result.StructuredContent); err != nil {
		t.Fatalf("config_status structured content is not serializable: %v", err)
	}
}

func TestToolsCallConfigStatusWarnsWhenGeneratedSourceIsMissing(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	generated := filepath.Join(dir, "generated", "mihomo.yaml")
	subscription := filepath.Join(dir, "subscription.gob")
	selection := filepath.Join(dir, "localclash-packs.gob")
	runtimeProfile := filepath.Join(dir, "localclash-runtime.json")
	missingConfig := filepath.Join(dir, "localclash-intent.json")
	writeMCPFile(t, generated, "proxies: []\nproxy-groups: []\nrules: []\n")
	writeMCPFile(t, subscription, "proxies: []\n")
	writeMCPFile(t, selection, "version: 1\nproxy_groups: {}\nenabled_packs: []\n")
	writeMCPFile(t, runtimeProfile, "version: 2\nmode: normal\ncore: meta\n")

	server := NewServerWithState(appinit.RuntimeState{
		Paths: appinit.RuntimePaths{
			WorkspaceRoot:       dir,
			GeneratedConfig:     generated,
			SubscriptionPath:    subscription,
			PacksSelectionPath:  selection,
			RuntimeProfilePath:  runtimeProfile,
			SubscriptionConfig:  filepath.Join(dir, "localclash-subscriptions.json"),
			SubscriptionRuntime: filepath.Join(dir, ".runtime", "subscriptions"),
			RulesCacheDir:       filepath.Join(dir, ".runtime", "rules", "packs"),
		},
	})
	resp := callHandleWithServer(t, server, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "config_status",
			"arguments": map[string]any{},
		},
	})
	if resp.Error != nil {
		t.Fatalf("config_status returned JSON-RPC error: %+v", resp.Error)
	}
	result := marshalToolResult(t, resp.Result)
	content := result.StructuredContent.(map[string]any)
	warnings := content["warnings"].([]any)
	if !guidanceContains(warnings, "source file is missing: "+missingConfig) {
		t.Fatalf("warnings = %+v, want missing config source warning", warnings)
	}
	render := content["render"].(map[string]any)
	renderWarnings := render["warnings"].([]any)
	if !guidanceContains(renderWarnings, "localClash cannot safely determine whether it is current") {
		t.Fatalf("render warnings = %+v, want localClash freshness warning", renderWarnings)
	}
	nextActions := content["next_actions"].([]any)
	if !guidanceContains(nextActions, "restore or rebuild missing generated source artifacts") {
		t.Fatalf("next_actions = %+v, want warning repair guidance", nextActions)
	}
}

func TestToolsCallDoctorReturnsSerializableResult(t *testing.T) {
	dir := t.TempDir()
	req := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "doctor",
			"arguments": map[string]any{
				"core":         filepath.Join(dir, "missing-core"),
				"subscription": filepath.Join(dir, "missing-subscription.gob"),
				"config":       filepath.Join(dir, "missing-generated.yaml"),
				"dashboard":    filepath.Join(dir, "missing-dashboard"),
				"workdir":      dir,
			},
		},
	}
	resp := callHandle(t, req)
	if resp.Error != nil {
		t.Fatalf("doctor call returned JSON-RPC error: %+v", resp.Error)
	}
	result := marshalToolResult(t, resp.Result)
	if len(result.Content) == 0 || result.Content[0].Type != "text" {
		t.Fatalf("doctor result content = %+v", result.Content)
	}
	if _, err := json.Marshal(result.StructuredContent); err != nil {
		t.Fatalf("doctor structured content is not serializable: %v", err)
	}
}

func TestToolsCallEnvironmentInspectReturnsSerializableResult(t *testing.T) {
	dir := t.TempDir()
	state := appinit.RuntimeState{
		Paths: appinit.RuntimePaths{
			SubscriptionPath:   filepath.Join(dir, "subscription.gob"),
			SubscriptionConfig: filepath.Join(dir, "localclash-subscriptions.json"),
			GeneratedConfig:    filepath.Join(dir, "generated", "mihomo.yaml"),
			MihomoRuntimeDir:   filepath.Join(dir, ".runtime", "mihomo"),
			RulesCacheDir:      filepath.Join(dir, ".runtime", "rules", "packs"),
			CorePath:           filepath.Join(dir, "bin", "mihomo"),
		},
	}
	server := NewServerWithState(state)
	req, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "environment_inspect",
			"arguments": map[string]any{},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	resp := server.Handle(context.Background(), req)
	if resp.Error != nil {
		t.Fatalf("environment_inspect returned JSON-RPC error: %+v", resp.Error)
	}
	result := marshalToolResult(t, resp.Result)
	content := result.StructuredContent.(map[string]any)
	if _, ok := content["host"].(map[string]any); !ok {
		t.Fatalf("content = %+v, want host object", content)
	}
	if _, ok := content["network_capabilities"]; ok {
		t.Fatalf("content uses old network_capabilities field: %+v", content)
	}
	if _, ok := content["openclash_state"]; ok {
		t.Fatalf("content includes removed openclash_state field: %+v", content)
	}
	if _, ok := content["reference_snapshots"]; ok {
		t.Fatalf("content includes removed reference_snapshots field: %+v", content)
	}
	if _, err := json.Marshal(result.StructuredContent); err != nil {
		t.Fatalf("environment_inspect structured content is not serializable: %v", err)
	}
	data, _ := json.Marshal(result.StructuredContent)
	for _, secret := range []string{"subscription-url", "server.example.com"} {
		if strings.Contains(string(data), secret) {
			t.Fatalf("environment_inspect leaked %q in %s", secret, data)
		}
	}
}

func TestRunRuntimeToolReturnsSerializableResult(t *testing.T) {
	dir := t.TempDir()
	controller := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/version" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"meta":true}`)
	}))
	defer controller.Close()
	controllerHost := strings.TrimPrefix(controller.URL, "http://")
	core := filepath.Join(dir, "lc-mihomo-meta")
	writeTestExecutable(t, core, `#!/bin/sh
if [ "$1" = "-v" ]; then
  echo Mihomo Meta test
  exit 0
fi
for arg in "$@"; do
  if [ "$arg" = "-t" ]; then
    echo configuration test is successful
    exit 0
  fi
done
echo runtime started
sleep 30
`)
	config := filepath.Join(dir, "mihomo.yaml")
	if err := os.WriteFile(config, []byte("external-controller: "+controllerHost+"\nexternal-ui: ui/zashboard\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	workDir := filepath.Join(dir, "runtime")
	server := NewServerWithState(appinit.RuntimeState{
		Paths: appinit.RuntimePaths{
			GeneratedConfig:  config,
			MihomoRuntimeDir: workDir,
			CorePath:         core,
		},
	})
	resp := callHandleWithServer(t, server, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "run_runtime",
			"arguments": map[string]any{
				"background": false,
			},
		},
	})
	if resp.Error != nil {
		t.Fatalf("run_runtime returned JSON-RPC error: %+v", resp.Error)
	}
	result := marshalToolResult(t, resp.Result)
	content := result.StructuredContent.(map[string]any)
	if content["started"] != true || content["already_running"] != false {
		t.Fatalf("run_runtime content = %+v, want started", content)
	}
	pidValue, ok := content["pid"].(float64)
	if !ok || pidValue <= 0 {
		t.Fatalf("run_runtime content = %+v, want numeric pid", content)
	}
	defer killMCPProcess(int(pidValue))
	if content["external_ui_url"] != controller.URL+"/ui" {
		t.Fatalf("external ui url = %v", content["external_ui_url"])
	}
	if _, err := json.Marshal(result.StructuredContent); err != nil {
		t.Fatalf("run_runtime structured content is not serializable: %v", err)
	}
}

func TestRunRuntimeToolPreflightErrorReturnsToolResult(t *testing.T) {
	dir := t.TempDir()
	server := NewServerWithState(appinit.RuntimeState{
		Paths: appinit.RuntimePaths{
			GeneratedConfig:  filepath.Join(dir, "missing.yaml"),
			MihomoRuntimeDir: filepath.Join(dir, "runtime"),
			CorePath:         filepath.Join(dir, "lc-mihomo-meta"),
		},
	})
	resp := callHandleWithServer(t, server, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "run_runtime",
			"arguments": map[string]any{
				"background": false,
			},
		},
	})
	if resp.Error != nil {
		t.Fatalf("run_runtime preflight returned JSON-RPC error: %+v", resp.Error)
	}
	result := marshalToolResult(t, resp.Result)
	content := result.StructuredContent.(map[string]any)
	if content["started"] != false || content["error"] == "" {
		t.Fatalf("content = %+v, want started false error", content)
	}
}

func TestRestartRuntimeToolHonorsForceConfigTest(t *testing.T) {
	dir := t.TempDir()
	core := filepath.Join(dir, "lc-mihomo-meta")
	counter := filepath.Join(dir, "test-count")
	writeTestExecutable(t, core, fmt.Sprintf(`#!/bin/sh
if [ "$1" = "-v" ]; then
  echo Mihomo Meta test
  exit 0
fi
for arg in "$@"; do
  if [ "$arg" = "-t" ]; then
    count=0
    [ -f %[1]q ] && count=$(cat %[1]q)
    count=$((count + 1))
    echo "$count" > %[1]q
    echo forced validation failed
    exit 7
  fi
done
echo runtime started
sleep 30
`, counter))
	config := filepath.Join(dir, "mihomo.yaml")
	if err := os.WriteFile(config, []byte("external-controller: 127.0.0.1:9090\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	workDir := filepath.Join(dir, "runtime")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeMCPValidationCache(t, mihomotest.DefaultCachePath(workDir), core, config)

	server := NewServerWithState(appinit.RuntimeState{
		Paths: appinit.RuntimePaths{
			GeneratedConfig:  config,
			MihomoRuntimeDir: workDir,
			CorePath:         core,
		},
	})
	resp := callHandleWithServer(t, server, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "restart_runtime",
			"arguments": map[string]any{
				"strategy":          "process_restart",
				"force_config_test": true,
				"background":        false,
			},
		},
	})
	if resp.Error != nil {
		t.Fatalf("restart_runtime returned JSON-RPC error: %+v", resp.Error)
	}
	result := marshalToolResult(t, resp.Result)
	content := result.StructuredContent.(map[string]any)
	if content["restarted"] == true || content["error"] == "" {
		t.Fatalf("content = %+v, want validation failure before restart", content)
	}
	validation := content["config_validation"].(map[string]any)
	if validation["cached"] == true {
		t.Fatalf("config_validation = %+v, want forced uncached validation", validation)
	}
	if got := strings.TrimSpace(readMCPFile(t, counter)); got != "1" {
		t.Fatalf("forced config test count = %q, want one fresh -t run", got)
	}
}

func TestMihomoAPIRequestToolUsesConfiguredController(t *testing.T) {
	dir := t.TempDir()
	config := filepath.Join(dir, ".runtime", "mihomo", "config.yaml")
	var gotAuth string
	var gotQuery string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotQuery = r.URL.RawQuery
		if r.URL.Path != "/proxies" {
			t.Fatalf("path = %q, want /proxies", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"proxies":{"香港-Vmess-ARGO":{"type":"Vmess"}}}`))
	}))
	defer api.Close()
	writeMCPFile(t, config, "external-controller: "+api.URL+"\nsecret: test-secret\n")

	mcpServer := NewServerWithState(appinit.RuntimeState{
		Paths: appinit.RuntimePaths{GeneratedConfig: config},
	})
	resp := callHandleWithServer(t, mcpServer, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "mihomo_api_request",
			"arguments": map[string]any{
				"method": "GET",
				"path":   "/proxies",
				"query":  map[string]any{"name": "香港-Vmess-ARGO"},
			},
		},
	})
	if resp.Error != nil {
		t.Fatalf("mihomo_api_request returned JSON-RPC error: %+v", resp.Error)
	}
	result := marshalToolResult(t, resp.Result)
	content := result.StructuredContent.(map[string]any)
	if content["status_code"] != float64(http.StatusOK) {
		t.Fatalf("content = %+v, want 200", content)
	}
	if gotAuth != "Bearer test-secret" {
		t.Fatalf("authorization = %q, want bearer secret", gotAuth)
	}
	if !strings.Contains(gotQuery, "name=") {
		t.Fatalf("query = %q, want name query", gotQuery)
	}
	jsonBody := content["json"].(map[string]any)
	proxies := jsonBody["proxies"].(map[string]any)
	if proxies["香港-Vmess-ARGO"] == nil {
		t.Fatalf("json = %+v, want expected proxy node", jsonBody)
	}
}

func TestMihomoConnectionsReadToolReadsSnapshot(t *testing.T) {
	dir := t.TempDir()
	config := filepath.Join(dir, ".runtime", "mihomo", "config.yaml")
	var gotAuth string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if r.URL.Path != "/connections/" {
			t.Fatalf("path = %q, want /connections/", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"downloadTotal":10,
			"uploadTotal":20,
			"connections":[{
				"id":"active-1",
				"metadata":{"network":"tcp","sourceIP":"192.168.6.2","sourcePort":"50000","destinationPort":"443","host":"example.com"},
				"chains":["香港-Vmess-ARGO","GLOBAL"],
				"upload":5,
				"download":6,
				"rule":"RuleSet",
				"rulePayload":"syncnext_SyncnextProxy"
			}]
		}`)
	}))
	defer api.Close()
	writeMCPFile(t, config, "external-controller: "+api.URL+"\nsecret: conn-secret\n")

	mcpServer := NewServerWithState(appinit.RuntimeState{
		Paths: appinit.RuntimePaths{GeneratedConfig: config},
	})
	resp := callHandleWithServer(t, mcpServer, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "mihomo_connections_read",
			"arguments": map[string]any{
				"mode": "snapshot",
			},
		},
	})
	if resp.Error != nil {
		t.Fatalf("mihomo_connections_read returned JSON-RPC error: %+v", resp.Error)
	}
	result := marshalToolResult(t, resp.Result)
	content := result.StructuredContent.(map[string]any)
	latest := content["latest"].(map[string]any)
	connections := latest["connections"].([]any)
	connection := connections[0].(map[string]any)
	if connection["selected_proxy"] != "香港-Vmess-ARGO" || connection["rule_payload"] != "syncnext_SyncnextProxy" {
		t.Fatalf("connection = %+v, want selected proxy and rule payload", connection)
	}
	if gotAuth != "Bearer conn-secret" {
		t.Fatalf("authorization = %q, want bearer secret", gotAuth)
	}
}

func TestMihomoLogsReadToolReadsBoundedHTTPStream(t *testing.T) {
	dir := t.TempDir()
	config := filepath.Join(dir, ".runtime", "mihomo", "config.yaml")
	var gotAuth string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if r.URL.Path != "/logs" || r.URL.Query().Get("level") != "info" {
			t.Fatalf("request = %s, want /logs?level=info", r.URL.String())
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"type":"info","payload":"first"}`+"\n"+`{"type":"info","payload":"second"}`+"\n")
	}))
	defer api.Close()
	writeMCPFile(t, config, "external-controller: "+api.URL+"\nsecret: log-secret\n")

	mcpServer := NewServerWithState(appinit.RuntimeState{
		Paths: appinit.RuntimePaths{GeneratedConfig: config},
	})
	resp := callHandleWithServer(t, mcpServer, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "mihomo_logs_read",
			"arguments": map[string]any{
				"transport": "http_stream",
				"level":     "info",
				"max_lines": 1,
			},
		},
	})
	if resp.Error != nil {
		t.Fatalf("mihomo_logs_read returned JSON-RPC error: %+v", resp.Error)
	}
	result := marshalToolResult(t, resp.Result)
	content := result.StructuredContent.(map[string]any)
	if content["transport"] != "http_stream" || content["line_count"] != float64(1) || content["truncated"] != true {
		t.Fatalf("content = %+v, want one truncated http_stream line", content)
	}
	if gotAuth != "Bearer log-secret" {
		t.Fatalf("authorization = %q, want bearer secret", gotAuth)
	}
}

func TestMihomoConfigTestToolRecordsAttestation(t *testing.T) {
	dir := t.TempDir()
	core := filepath.Join(dir, "lc-mihomo-meta")
	writeTestExecutable(t, core, `#!/bin/sh
if [ "$1" = "-v" ]; then
  echo Mihomo Meta test
  exit 0
fi
for arg in "$@"; do
  if [ "$arg" = "-t" ]; then
    echo configuration test is successful
    exit 0
  fi
done
exit 9
`)
	runtimeDir := filepath.Join(dir, ".runtime", "mihomo")
	config := filepath.Join(runtimeDir, "config.yaml")
	attestation := filepath.Join(runtimeDir, "config-test-attestation.json")
	writeMCPFile(t, config, "external-controller: 127.0.0.1:9090\n")
	state := appinit.RuntimeState{
		Paths: appinit.RuntimePaths{
			WorkspaceRoot:      dir,
			GeneratedConfig:    config,
			MihomoRuntimeDir:   runtimeDir,
			CorePath:           core,
			RuntimeProfilePath: filepath.Join(dir, "localclash-runtime.json"),
		},
	}

	resp := callHandleWithServer(t, NewServerWithState(state), map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "mihomo_config_test",
			"arguments": map[string]any{
				"wait": true,
			},
		},
	})
	if resp.Error != nil {
		t.Fatalf("mihomo_config_test returned JSON-RPC error: %+v", resp.Error)
	}
	result := marshalToolResult(t, resp.Result)
	content := result.StructuredContent.(map[string]any)
	if content["passed"] != true || content["recorded"] != true || content["config_sha256"] == "" {
		t.Fatalf("content = %+v, want passing recorded config test", content)
	}
	if content["config_path"] != config {
		t.Fatalf("config_path = %v, want server state generated config %q", content["config_path"], config)
	}
	recorded, err := mihomotest.ReadAttestation(attestation)
	if err != nil {
		t.Fatalf("read attestation: %v", err)
	}
	if recorded.Config != config || recorded.WorkDir != runtimeDir || recorded.Core != core {
		t.Fatalf("attestation = %+v, want server state paths", recorded)
	}
}

func TestMihomoConfigTestToolRejectsCallerManagedPaths(t *testing.T) {
	dir := t.TempDir()
	state := appinit.RuntimeState{
		Paths: appinit.RuntimePaths{
			GeneratedConfig:  filepath.Join(dir, ".runtime", "mihomo", "config.yaml"),
			MihomoRuntimeDir: filepath.Join(dir, ".runtime", "mihomo"),
			CorePath:         filepath.Join(dir, "lc-mihomo-meta"),
		},
	}
	resp := callHandleWithServer(t, NewServerWithState(state), map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "mihomo_config_test",
			"arguments": map[string]any{
				"config": ".runtime/mihomo/config.yaml",
				"wait":   true,
			},
		},
	})
	if resp.Error == nil || !strings.Contains(resp.Error.Message, `unknown field "config"`) {
		t.Fatalf("error = %+v, want config field rejection", resp.Error)
	}
}

func TestRuntimeToolsRejectCallerManagedPaths(t *testing.T) {
	dir := t.TempDir()
	state := appinit.RuntimeState{
		Paths: appinit.RuntimePaths{
			RuntimeProfilePath: filepath.Join(dir, "localclash-runtime.json"),
			GeneratedConfig:    filepath.Join(dir, ".runtime", "mihomo", "config.yaml"),
			MihomoRuntimeDir:   filepath.Join(dir, ".runtime", "mihomo"),
			CorePath:           filepath.Join(dir, "lc-mihomo-meta"),
		},
	}
	server := NewServerWithState(state)
	for _, tc := range []struct {
		name string
		args map[string]any
	}{
		{name: "run_runtime", args: map[string]any{"config": "other.yaml", "background": false}},
		{name: "restart_runtime", args: map[string]any{"core": "other-core", "background": false}},
		{name: "runtime_status", args: map[string]any{"runtime_dir": "other-runtime"}},
		{name: "router_takeover_status", args: map[string]any{"state_dir": "other-state"}},
		{name: "router_takeover_apply", args: map[string]any{"dns_port": 1053, "background": false}},
		{name: "router_takeover_stop", args: map[string]any{"tun_device": "utun9", "background": false}},
		{name: "stop_runtime", args: map[string]any{"runtime_profile": "other-profile", "background": false}},
	} {
		resp := callHandleWithServer(t, server, map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"method":  "tools/call",
			"params": map[string]any{
				"name":      tc.name,
				"arguments": tc.args,
			},
		})
		if resp.Error == nil || !strings.Contains(resp.Error.Message, "unknown field") {
			t.Fatalf("%s error = %+v, want strict field rejection", tc.name, resp.Error)
		}
	}
}

func TestAsyncToolsRejectUnknownFieldsBeforeQueue(t *testing.T) {
	dir := t.TempDir()
	state := appinit.RuntimeState{
		Paths: appinit.RuntimePaths{
			RuntimeProfilePath: filepath.Join(dir, "localclash-runtime.json"),
			GeneratedConfig:    filepath.Join(dir, ".runtime", "mihomo", "config.yaml"),
			MihomoRuntimeDir:   filepath.Join(dir, ".runtime", "mihomo"),
			CorePath:           filepath.Join(dir, "lc-mihomo-meta"),
			SubscriptionConfig: filepath.Join(dir, "localclash-subscriptions.json"),
			SubscriptionPath:   filepath.Join(dir, "subscription.gob"),
		},
	}
	server := NewServerWithState(state)
	for _, tc := range []struct {
		name string
		args map[string]any
	}{
		{name: "config_render", args: map[string]any{"config": "other.yaml"}},
		{name: "config_patch_draft", args: map[string]any{"config": "other.yaml"}},
		{name: "config_patch_apply", args: map[string]any{"config": "other.yaml"}},
		{name: "mihomo_config_test", args: map[string]any{"config": "other.yaml"}},
		{name: "subscriptions_refresh", args: map[string]any{"config": "other.yaml"}},
		{name: "run_runtime", args: map[string]any{"config": "other.yaml"}},
		{name: "restart_runtime", args: map[string]any{"config": "other.yaml"}},
		{name: "router_takeover_apply", args: map[string]any{"config": "other.yaml"}},
		{name: "router_takeover_stop", args: map[string]any{"config": "other.yaml"}},
		{name: "stop_runtime", args: map[string]any{"config": "other.yaml"}},
	} {
		resp := callHandleWithServer(t, server, map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"method":  "tools/call",
			"params": map[string]any{
				"name":      tc.name,
				"arguments": tc.args,
			},
		})
		if resp.Error == nil || !strings.Contains(resp.Error.Message, `unknown field "config"`) {
			t.Fatalf("%s error = %+v, want config field rejection before async queue", tc.name, resp.Error)
		}
	}
}

func TestExecutionToolReturnsAsyncTaskLogByDefault(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	server := NewServerWithState(appinit.RuntimeState{
		Paths: appinit.RuntimePaths{
			GeneratedConfig:  filepath.Join(dir, ".runtime", "mihomo", "config.yaml"),
			MihomoRuntimeDir: filepath.Join(dir, ".runtime", "mihomo"),
			CorePath:         filepath.Join(dir, "lc-mihomo-meta"),
		},
	})
	resp := callHandleWithServer(t, server, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "run_runtime",
			"arguments": map[string]any{},
		},
	})
	if resp.Error != nil {
		t.Fatalf("run_runtime returned JSON-RPC error: %+v", resp.Error)
	}
	result := marshalToolResult(t, resp.Result)
	content := result.StructuredContent.(map[string]any)
	if content["queued"] != true || content["task_id"] == "" || content["log_file"] == "" || content["diagnostics_dir"] == "" {
		t.Fatalf("content = %+v, want queued task with log file", content)
	}
	nextActions := content["next_actions"].([]any)
	if len(nextActions) == 0 || !strings.Contains(nextActions[0].(string), "nl_file") {
		t.Fatalf("next_actions = %+v, want nl_file guidance", nextActions)
	}
	logFile := content["log_file"].(string)
	statusFile := content["status_file"].(string)
	var statusText []byte
	for i := 0; i < 50; i++ {
		data, err := os.ReadFile(statusFile)
		if err == nil && (strings.Contains(string(data), `"status": "done"`) || strings.Contains(string(data), `"status": "error"`)) {
			statusText = data
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(statusText) == 0 {
		t.Fatalf("task status did not finish; status_file=%s", statusFile)
	}
	if !strings.Contains(string(statusText), `"status": "error"`) {
		t.Fatalf("status = %s, want error for missing runtime inputs", statusText)
	}
	logText, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}
	if !strings.Contains(string(logText), `"event":"queued"`) ||
		!strings.Contains(string(logText), `"event":"started"`) {
		t.Fatalf("log = %s, want queued and started events", logText)
	}
}

func TestServerShutdownCancelsQueuedAsyncTasks(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	server := NewServer()
	started := make(chan struct{})
	cancelled := make(chan struct{})
	_, err := server.queueAsyncTool("slow_tool", []byte(`{}`), func(ctx context.Context, args json.RawMessage) (toolResult, error) {
		close(started)
		<-ctx.Done()
		close(cancelled)
		return jsonToolResult(map[string]any{"cancelled": true})
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("async task did not start")
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("Shutdown error = %v", err)
	}
	select {
	case <-cancelled:
	default:
		t.Fatal("async task did not observe server shutdown cancellation")
	}
}

func TestRuntimeStatusToolReturnsSerializableResult(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("runtime process-name discovery requires procfs")
	}
	dir := t.TempDir()
	workDir := filepath.Join(dir, "runtime")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(dir, "mihomo.yaml")
	if err := os.WriteFile(config, []byte("external-controller: 127.0.0.1:9090\nexternal-ui: ui/zashboard\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	core := filepath.Join(dir, "lc-mihomo-meta")
	writeTestExecutable(t, core, "#!/bin/sh\nsleep 30\n")
	cmd := exec.Command(core, "-d", workDir, "-f", config)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer killMCPProcess(cmd.Process.Pid)
	go func() { _ = cmd.Wait() }()
	server := NewServerWithState(appinit.RuntimeState{
		Paths: appinit.RuntimePaths{
			GeneratedConfig:  config,
			MihomoRuntimeDir: workDir,
			CorePath:         core,
		},
	})
	resp := callHandleWithServer(t, server, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "runtime_status",
			"arguments": map[string]any{},
		},
	})
	if resp.Error != nil {
		t.Fatalf("runtime_status returned JSON-RPC error: %+v", resp.Error)
	}
	result := marshalToolResult(t, resp.Result)
	content := result.StructuredContent.(map[string]any)
	if content["running"] != true || int(content["pid"].(float64)) != cmd.Process.Pid {
		t.Fatalf("runtime_status content = %+v, want current pid running", content)
	}
	if content["external_ui_url"] != "http://127.0.0.1:9090/ui" {
		t.Fatalf("external ui url = %v", content["external_ui_url"])
	}
	if _, err := json.Marshal(result.StructuredContent); err != nil {
		t.Fatalf("runtime_status structured content is not serializable: %v", err)
	}
}

func TestStopRuntimeToolStopsStartedRuntime(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("runtime process-name discovery requires procfs")
	}
	dir := t.TempDir()
	core := filepath.Join(dir, "lc-mihomo-meta")
	writeTestExecutable(t, core, `#!/bin/sh
if [ "$1" = "-v" ]; then
  echo Mihomo Meta test
  exit 0
fi
for arg in "$@"; do
  if [ "$arg" = "-t" ]; then
    echo configuration test is successful
    exit 0
  fi
done
sleep 30
`)
	config := filepath.Join(dir, "mihomo.yaml")
	controller := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/version" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"version":"test"}`))
	}))
	t.Cleanup(controller.Close)
	controllerAddress := strings.TrimPrefix(controller.URL, "http://")
	if err := os.WriteFile(config, []byte(fmt.Sprintf("external-controller: %s\nexternal-ui: ui/zashboard\n", controllerAddress)), 0o644); err != nil {
		t.Fatal(err)
	}
	workDir := filepath.Join(dir, "runtime")
	server := NewServerWithState(appinit.RuntimeState{
		Paths: appinit.RuntimePaths{
			GeneratedConfig:  config,
			MihomoRuntimeDir: workDir,
			CorePath:         core,
		},
	})
	runResp := callHandleWithServer(t, server, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "run_runtime",
			"arguments": map[string]any{
				"background": false,
			},
		},
	})
	if runResp.Error != nil {
		t.Fatalf("run_runtime returned JSON-RPC error: %+v", runResp.Error)
	}
	runResult := marshalToolResult(t, runResp.Result)
	runContent := runResult.StructuredContent.(map[string]any)
	if runContent["started"] != true {
		t.Fatalf("run_runtime content = %+v, want started runtime", runContent)
	}
	pidValue, ok := runContent["pid"].(float64)
	if !ok || pidValue <= 0 {
		t.Fatalf("run_runtime content = %+v, want numeric pid", runContent)
	}
	pid := int(pidValue)
	defer killMCPProcess(pid)

	stopResp := callHandleWithServer(t, server, map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "stop_runtime",
			"arguments": map[string]any{
				"timeout_ms": 2000,
				"force":      true,
				"background": false,
			},
		},
	})
	if stopResp.Error != nil {
		t.Fatalf("stop_runtime returned JSON-RPC error: %+v", stopResp.Error)
	}
	stopResult := marshalToolResult(t, stopResp.Result)
	stopContent := stopResult.StructuredContent.(map[string]any)
	if stopContent["stopped"] != true || stopContent["was_running"] != true || int(stopContent["pid"].(float64)) != pid {
		t.Fatalf("stop_runtime content = %+v, want stopped pid %d", stopContent, pid)
	}
	if _, err := json.Marshal(stopResult.StructuredContent); err != nil {
		t.Fatalf("stop_runtime structured content is not serializable: %v", err)
	}
}

func TestStopRuntimeRefusesWhenRouterTakeoverIsEffective(t *testing.T) {
	original := routerTakeoverStatus
	routerTakeoverStatus = func(ctx context.Context, opts routertakeover.Options) (routertakeover.Result, error) {
		return routertakeover.Result{
			ProfileMode:    runtimeprofile.ModeRouter,
			RuntimeRunning: true,
			Effective:      true,
		}, nil
	}
	t.Cleanup(func() {
		routerTakeoverStatus = original
	})

	dir := t.TempDir()
	workDir := filepath.Join(dir, "runtime")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}
	server := NewServerWithState(appinit.RuntimeState{
		Paths: appinit.RuntimePaths{
			RuntimeProfilePath: filepath.Join(dir, "localclash-runtime.json"),
			MihomoRuntimeDir:   workDir,
		},
	})
	resp := callHandleWithServer(t, server, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "stop_runtime",
			"arguments": map[string]any{
				"background": false,
			},
		},
	})
	if resp.Error != nil {
		t.Fatalf("stop_runtime returned JSON-RPC error: %+v", resp.Error)
	}
	result := marshalToolResult(t, resp.Result)
	content := result.StructuredContent.(map[string]any)
	if content["refused"] != true || !strings.Contains(content["error"].(string), "router takeover") {
		t.Fatalf("stop_runtime content = %+v, want router takeover refusal", content)
	}
	actions := content["next_actions"].([]any)
	if len(actions) == 0 || !strings.Contains(actions[0].(string), "router_takeover_stop") {
		t.Fatalf("next_actions = %+v, want router_takeover_stop guidance", actions)
	}
}

func TestRouterTakeoverApplyFailureMarksToolResultError(t *testing.T) {
	dir := t.TempDir()
	profile := filepath.Join(dir, "localclash-runtime.json")
	if _, err := runtimeprofile.Configure(profile, runtimeprofile.ModeRouter, runtimeprofile.CoreMeta); err != nil {
		t.Fatal(err)
	}
	state := appinit.RuntimeState{
		Paths: appinit.RuntimePaths{
			RuntimeProfilePath: profile,
			GeneratedConfig:    filepath.Join(dir, "generated", "mihomo.yaml"),
			MihomoRuntimeDir:   filepath.Join(dir, ".runtime", "mihomo"),
		},
	}

	resp := callHandleWithServer(t, NewServerWithState(state), map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "router_takeover_apply",
			"arguments": map[string]any{
				"background": false,
			},
		},
	})
	if resp.Error != nil {
		t.Fatalf("router_takeover_apply returned JSON-RPC error: %+v", resp.Error)
	}
	result := marshalToolResult(t, resp.Result)
	if !result.IsError {
		t.Fatalf("router_takeover_apply IsError = false, content = %+v", result.StructuredContent)
	}
	content := result.StructuredContent.(map[string]any)
	if !strings.Contains(content["error"].(string), "run_runtime") {
		t.Fatalf("content = %+v, want run_runtime error", content)
	}
	if statusFile, ok := content["task_status_file"].(string); ok && statusFile != "" {
		statusText, err := os.ReadFile(statusFile)
		if err != nil {
			t.Fatalf("read task status: %v", err)
		}
		if !strings.Contains(string(statusText), `"status": "error"`) {
			t.Fatalf("task status = %s, want error", statusText)
		}
	}
}

func TestRunRuntimeToolUsesBootstrapDiagnostics(t *testing.T) {
	state := appinit.RuntimeState{
		Paths: appinit.RuntimePaths{
			GeneratedConfig:  ".runtime/mihomo/config.yaml",
			MihomoRuntimeDir: ".runtime/mihomo",
			CorePath:         "bin/lc-mihomo-meta",
		},
		Config: appinit.ConfigState{
			Available:  false,
			Diagnostic: "config render skipped because effective subscription is unavailable",
		},
	}
	resp := callHandleWithServer(t, NewServerWithState(state), map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "run_runtime",
			"arguments": map[string]any{"background": false},
		},
	})
	if resp.Error != nil {
		t.Fatalf("run_runtime returned JSON-RPC error: %+v", resp.Error)
	}
	result := marshalToolResult(t, resp.Result)
	content := result.StructuredContent.(map[string]any)
	if !strings.Contains(content["error"].(string), "effective subscription is unavailable") {
		t.Fatalf("content = %+v, want bootstrap diagnostic", content)
	}
	nextActions := content["next_actions"].([]any)
	if len(nextActions) == 0 {
		t.Fatalf("content = %+v, want next actions", content)
	}
}

func TestRunRuntimeToolRefusesMissingGeneratedConfigWithoutAutoRender(t *testing.T) {
	paths := setupMCPPlanFixture(t)
	dir := filepath.Dir(paths.subscription)
	core := filepath.Join(dir, "lc-mihomo-meta")
	writeTestExecutable(t, core, `#!/bin/sh
if [ "$1" = "-v" ]; then
  echo Mihomo Meta test
  exit 0
fi
for arg in "$@"; do
  if [ "$arg" = "-t" ]; then
    echo configuration test is successful
    exit 0
  fi
done
echo runtime started
sleep 30
`)
	generated := filepath.Join(dir, "generated", "mihomo.yaml")
	state := appinit.RuntimeState{
		Paths: appinit.RuntimePaths{
			GeneratedConfig:     generated,
			SubscriptionPath:    paths.subscription,
			RulesCacheDir:       paths.cache,
			MihomoRuntimeDir:    filepath.Join(dir, ".runtime", "mihomo"),
			CorePath:            core,
			PacksSelectionPath:  filepath.Join(dir, "missing-packs.yaml"),
			SubscriptionConfig:  filepath.Join(dir, "localclash-subscriptions.json"),
			SubscriptionRuntime: filepath.Join(dir, ".runtime", "subscriptions"),
		},
		Config: appinit.ConfigState{
			Available:  false,
			Diagnostic: "config render skipped because effective subscription is unavailable",
		},
	}

	resp := callHandleWithServer(t, NewServerWithState(state), map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "run_runtime",
			"arguments": map[string]any{"background": false},
		},
	})
	if resp.Error != nil {
		t.Fatalf("run_runtime returned JSON-RPC error: %+v", resp.Error)
	}
	result := marshalToolResult(t, resp.Result)
	content := result.StructuredContent.(map[string]any)
	if content["started"] != false || !strings.Contains(content["error"].(string), "call config_render") {
		t.Fatalf("content = %+v, want explicit config_render guidance", content)
	}
	if _, err := os.Stat(generated); !os.IsNotExist(err) {
		t.Fatalf("generated config should not be created by run_runtime, err=%v", err)
	}
}

func TestRemovedMCPToolsReturnUnknownTool(t *testing.T) {
	for _, name := range removedMCPTools() {
		resp := callHandle(t, map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"method":  "tools/call",
			"params": map[string]any{
				"name":      name,
				"arguments": map[string]any{},
			},
		})
		if resp.Error == nil {
			t.Fatalf("%s expected unknown tool JSON-RPC error", name)
		}
		if !strings.Contains(resp.Error.Message, "unknown tool") {
			t.Fatalf("%s error = %+v, want unknown tool", name, resp.Error)
		}
	}
}

func TestUnknownToolReturnsError(t *testing.T) {
	resp := callHandle(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "missing_tool",
		},
	})
	if resp.Error == nil {
		t.Fatal("expected unknown tool JSON-RPC error")
	}
}

func callHandle(t *testing.T, value any) *rpcResponse {
	t.Helper()
	return callHandleWithServer(t, NewServer(), value)
}

func callHandleWithServer(t *testing.T, server *Server, value any) *rpcResponse {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	resp := server.Handle(context.Background(), data)
	if resp == nil {
		t.Fatal("nil response")
	}
	return resp
}

func callConfigPatchDraftForOverlay(t *testing.T, server *Server, paths mcpPlanFixture, patchID string, overlay map[string]any, test bool) map[string]any {
	t.Helper()
	_ = paths
	resp := callHandleWithServer(t, server, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "config_patch_draft",
			"arguments": map[string]any{
				"draft_name": patchID,
				"test":       test,
				"background": false,
				"operations": []map[string]any{
					{
						"op":       "upsert_patch",
						"patch_id": patchID,
						"title":    patchID,
						"order_id": "1000.000000",
						"summary":  patchID,
						"overlay":  overlay,
					},
				},
			},
		},
	})
	if resp.Error != nil {
		t.Fatalf("config_patch_draft returned JSON-RPC error: %+v", resp.Error)
	}
	result := marshalToolResult(t, resp.Result)
	return result.StructuredContent.(map[string]any)
}

func callConfigPatchApplyCurrentDraft(t *testing.T, server *Server, paths mcpPlanFixture, draft map[string]any) map[string]any {
	t.Helper()
	_ = paths
	applyArgs := map[string]any{}
	for key, value := range draft["apply_args"].(map[string]any) {
		applyArgs[key] = value
	}
	applyArgs["test"] = false
	applyArgs["background"] = false
	resp := callHandleWithServer(t, server, map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "config_patch_apply",
			"arguments": applyArgs,
		},
	})
	if resp.Error != nil {
		t.Fatalf("config_patch_apply returned JSON-RPC error: %+v", resp.Error)
	}
	result := marshalToolResult(t, resp.Result)
	return result.StructuredContent.(map[string]any)
}

func removedMCPTools() []string {
	return []string{
		"config_base_inspect",
		"config_intent_inspect",
		"config_overlay_inspect",
		"config_draft_apply",
		"config_draft_render",
		"config_test",
		"config_plan_apply",
		"config_plan_render",
		"config_patch_create",
		"inspect_generated_config",
		"rules_adapt",
		"rules_render",
		"switch_proxy_group",
		"apply_router_config",
	}
}

func marshalToolResult(t *testing.T, value any) toolResult {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var result toolResult
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	original := os.Stderr
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = writer
	defer func() {
		os.Stderr = original
		_ = reader.Close()
	}()
	fn()
	_ = writer.Close()
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func writeTestExecutable(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}

func writeMCPValidationCache(t *testing.T, cachePath, corePath, configPath string) {
	t.Helper()
	configInfo, err := os.Stat(configPath)
	if err != nil {
		t.Fatal(err)
	}
	coreInfo, err := os.Stat(corePath)
	if err != nil {
		t.Fatal(err)
	}
	configSHA, err := sha256File(configPath)
	if err != nil {
		t.Fatal(err)
	}
	coreSHA, err := sha256File(corePath)
	if err != nil {
		t.Fatal(err)
	}
	cache := map[string]any{
		"version": 1,
		"entries": []map[string]any{{
			"enabled":         true,
			"passed":          true,
			"cached":          false,
			"validated_at":    time.Date(2026, 5, 27, 12, 0, 0, 0, time.UTC).Format(time.RFC3339),
			"config_path":     configPath,
			"config_sha256":   configSHA,
			"config_size":     configInfo.Size(),
			"config_mod_time": configInfo.ModTime().UTC().Format(time.RFC3339Nano),
			"core_path":       corePath,
			"core_type":       "meta",
			"core_version":    "Mihomo Meta test",
			"core_sha256":     coreSHA,
			"core_size":       coreInfo.Size(),
			"core_mod_time":   coreInfo.ModTime().UTC().Format(time.RFC3339Nano),
		}},
	}
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(cachePath, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func killMCPProcess(pid int) {
	process, err := os.FindProcess(pid)
	if err == nil {
		_ = process.Kill()
	}
}

func setupMCPPackCache(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Chdir(dir)
	cacheDir := filepath.Join(dir, ".runtime", "rules", "packs")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeMCPPackIndex(t, cacheDir, rules.PackCache{
		Version:    1,
		Source:     "blackmatrix7",
		Adapter:    "blackmatrix7",
		Renderable: true,
		Packs:      []rules.Pack{mcpBlackmatrixPack("OpenAI", "AI")},
	})
	providerPath := filepath.Join(dir, ".runtime", "mihomo", "rule-packs", "blackmatrix7", "OpenAI.yaml")
	if err := os.MkdirAll(filepath.Dir(providerPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(providerPath, []byte("payload:\n  - DOMAIN,openai.com\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeMCPV2FlyTemplateCache(t *testing.T, cacheDir string) {
	t.Helper()
	ids := []string{
		"category-ads-all",
		"cn",
		"openai",
		"anthropic",
		"category-media",
		"category-communication",
		"google",
		"apple",
		"microsoft",
		"category-dev",
		"category-games",
	}
	packs := make([]rules.Pack, 0, len(ids))
	for _, id := range ids {
		packs = append(packs, mcpV2FlyPack(id, id))
	}
	writeMCPPackIndex(t, cacheDir, rules.PackCache{
		Version:    1,
		Source:     "v2fly-dlc",
		Adapter:    "v2fly-dlc",
		Renderable: true,
		Packs:      packs,
	})
}

func writeMCPPackIndex(t *testing.T, cacheDir string, caches ...rules.PackCache) {
	t.Helper()
	bySource := make(map[string]rules.PackCache, len(caches))
	for _, cache := range caches {
		bySource[cache.Source] = cache
	}
	if err := rules.WritePackIndex(rules.PackIndexPath(cacheDir), bySource); err != nil {
		t.Fatal(err)
	}
}

func mcpV2FlyPack(id, target string) rules.Pack {
	return rules.Pack{
		ID:         id,
		Name:       id,
		Target:     target,
		Renderable: true,
		Components: []rules.Component{{
			ID:         "domain",
			Behavior:   "v2fly-dlc",
			Format:     "text",
			OrderClass: "domain",
			URL:        "https://example.com/" + id,
			Path:       "./rule-packs/v2fly-dlc/" + id + ".txt",
		}},
	}
}

func mcpBlackmatrixPack(id, target string) rules.Pack {
	return rules.Pack{
		ID:         id,
		Name:       id,
		Target:     target,
		Renderable: true,
		Components: []rules.Component{{
			ID:         id,
			Behavior:   "classical",
			Format:     "yaml",
			OrderClass: "mixed",
			URL:        "https://example.com/" + id + ".yaml",
			Path:       "./rule-packs/blackmatrix7/" + id + ".yaml",
		}},
	}
}

func writeMCPPolicyTemplateFixture(t *testing.T, dir string) {
	t.Helper()
	writeMCPFile(t, filepath.Join(dir, "localclash-default.json"), `id: localclash-default
name: localClash Default
description: ACL4SSR-like default policy.
default: true
config:
  version: 1
  policy_template: localclash-default
  proxy_groups:
    AI:
      mode: auto
      match:
        type: name_regex
        pattern: .*
        min: 1
      boundary: template_all_subscription_nodes
    STREAMING:
      mode: auto
      match:
        type: name_regex
        pattern: .*
        min: 1
      boundary: template_all_subscription_nodes
  packs:
    - source: v2fly-dlc
      pack: openai
      type: geosite
      target: AI
    - source: v2fly-dlc
      pack: category-media
      type: geosite
      target: STREAMING
`)
}

func setupMCPPackRulesFixture(t *testing.T, providerBody string) (string, string, *httptest.Server) {
	t.Helper()
	dir := t.TempDir()
	cacheDir := filepath.Join(dir, "packs")
	providerCache := filepath.Join(dir, ".runtime", "rules", "provider-cache")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(providerBody))
	}))
	if err := rules.WritePackIndex(rules.PackIndexPath(cacheDir), map[string]rules.PackCache{
		"sukkaw": {
			Version:    1,
			Source:     "sukkaw",
			Adapter:    "sukkaw",
			Renderable: true,
			Packs: []rules.Pack{
				{
					ID:         "ai",
					Name:       "ai",
					Target:     "AI",
					Renderable: true,
					Components: []rules.Component{
						{
							ID:         "non_ip",
							Behavior:   "classical",
							Format:     "text",
							OrderClass: "non_ip",
							URL:        server.URL + "/ai.txt",
							Path:       "./rule-packs/sukkaw/ai_non_ip.txt",
						},
					},
				},
			},
		},
	}); err != nil {
		t.Fatal(err)
	}
	return cacheDir, providerCache, server
}

func setupMCPInspectConfig(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "mihomo.yaml")
	if err := os.WriteFile(path, []byte(`
mode: rule
external-controller: 127.0.0.1:9090
proxies:
  - name: SG 01
    type: ss
    server: sg.example.com
    password: secret
proxy-groups:
  - name: PROXY
    type: select
    proxies: [SG 01]
rule-providers:
  blackmatrix7_OpenAI:
    type: http
    behavior: classical
rules:
  - RULE-SET,blackmatrix7_OpenAI,AI
x-localclash:
  version: 1
  base:
    modifiable: false
    description: localClash generated base config
  overlay:
    modifiable: true
    packs:
      - source: blackmatrix7
        pack: OpenAI
        target: AI
    proxy_groups:
      - id: AI
        mode: manual
        nodes: [SG 01]
    rule_providers:
      - name: blackmatrix7_OpenAI
        behavior: classical
        type: http
    rules:
      - type: RULE-SET
        provider: blackmatrix7_OpenAI
        target: AI
    insertion: after local safety baseline, before configured fallback
`), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

type mcpPlanFixture struct {
	subscription string
	cache        string
	outputDir    string
}

func setupMCPWorkspaceRootFixture(t *testing.T) (string, string, appinit.RuntimeState) {
	t.Helper()
	root := t.TempDir()
	wrongDir := t.TempDir()
	writeMCPFile(t, filepath.Join(root, "localclash-intent.json"), `
version: 4
policy_template: localclash-default
proxy_groups:
  Direct:
    mode: direct
    reason: Direct exit.
policy_groups:
  DNSProxy:
    mode: manual
    exits:
      - Direct
    reason: DNS proxy exit.
`)
	writeMCPFile(t, filepath.Join(root, "subscription.gob"), `
proxies:
  - name: Direct
    type: direct
`)
	writeMCPFile(t, filepath.Join(root, ".runtime", "mihomo", "config.yaml"), `
proxies: []
proxy-groups: []
rules: []
`)
	writeMCPFile(t, filepath.Join(root, "localclash-runtime.json"), `
version: 2
mode: router
core: smart
`)
	if err := os.MkdirAll(filepath.Join(root, ".runtime", "rules", "packs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".runtime", "mihomo"), 0o755); err != nil {
		t.Fatal(err)
	}
	return root, wrongDir, appinit.RuntimeState{
		Paths: appinit.RuntimePaths{
			WorkspaceRoot:       root,
			RuntimeRoot:         filepath.Join(root, ".runtime"),
			RuleSourcesDir:      filepath.Join(root, "rule-sources"),
			RulesCacheDir:       filepath.Join(root, ".runtime", "rules", "packs"),
			GeneratedConfig:     filepath.Join(root, ".runtime", "mihomo", "config.yaml"),
			SubscriptionConfig:  filepath.Join(root, "localclash-subscriptions.json"),
			SubscriptionPath:    filepath.Join(root, "subscription.gob"),
			SubscriptionRuntime: filepath.Join(root, ".runtime", "subscriptions"),
			MihomoRuntimeDir:    filepath.Join(root, ".runtime", "mihomo"),
			CorePath:            runtimeprofile.SmartCorePath,
			RuntimeProfilePath:  filepath.Join(root, "localclash-runtime.json"),
		},
	}
}

func setupMCPPlanFixture(t *testing.T) mcpPlanFixture {
	t.Helper()
	dir := t.TempDir()
	t.Chdir(dir)
	paths := mcpPlanFixture{
		subscription: filepath.Join(dir, "subscription.gob"),
		cache:        filepath.Join(dir, ".runtime", "rules", "packs"),
		outputDir:    filepath.Join(dir, ".runtime", "plans"),
	}
	if err := os.MkdirAll(paths.cache, 0o755); err != nil {
		t.Fatal(err)
	}
	writeMCPFile(t, paths.subscription, `proxies:
  - name: SG 01
    type: ss
    server: sg.example.com
    password: secret
`)
	writeMCPFile(t, filepath.Join(dir, "localclash-packs.gob"), `version: 1
proxy_groups: {}
enabled_packs: []
`)
	writeMCPPackIndex(t, paths.cache, rules.PackCache{
		Version:    1,
		Source:     "blackmatrix7",
		Adapter:    "blackmatrix7",
		Renderable: true,
		Packs:      []rules.Pack{mcpBlackmatrixPack("OpenAI", "AI")},
	})
	return paths
}

type mcpSubscriptionsFixture struct {
	config     string
	runtimeDir string
	merged     string
}

func setupMCPSubscriptionsFixture(t *testing.T) mcpSubscriptionsFixture {
	t.Helper()
	dir := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`proxies:
  - name: SG 01
    type: ss
    server: sg.example.com
    password: secret
`))
	}))
	t.Cleanup(server.Close)
	paths := mcpSubscriptionsFixture{
		config:     filepath.Join(dir, "localclash-subscriptions.json"),
		runtimeDir: filepath.Join(dir, ".runtime", "subscriptions"),
		merged:     filepath.Join(dir, "subscription.gob"),
	}
	writeMCPFile(t, paths.config, fmt.Sprintf(`version: 1
sources:
  - id: primary
    display_name: "01"
    url: %s/sub?token=secret-token
`, server.URL))
	return paths
}

func setupMCPSubscriptionNodesFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	subscription := filepath.Join(dir, "subscription.gob")
	writeMCPFile(t, subscription, `proxies:
  - name: SG 01
    type: ss
    server: sg.example.com
    password: secret
  - name: 🇭🇰香港 01 | HK
    type: vmess
    server: hk.example.com
    uuid: private-uuid
`)
	return subscription
}

func writeMCPFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	var data []byte
	var err error
	switch filepath.Ext(path) {
	case ".json":
		var doc any
		if err := yaml.Unmarshal([]byte(content), &doc); err != nil {
			t.Fatal(err)
		}
		data, err = json.MarshalIndent(doc, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
	case ".gob":
		gob.Register(map[string]any{})
		gob.Register([]any{})
		var doc map[string]any
		if err := yaml.Unmarshal([]byte(content), &doc); err != nil {
			t.Fatal(err)
		}
		file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := doc["proxies"]; ok {
			err = gob.NewEncoder(file).Encode(struct {
				Version int
				Data    map[string]any
				Raw     []byte
			}{Version: 1, Data: doc, Raw: []byte(content)})
		} else {
			var selection rules.Selection
			data, err := json.Marshal(doc)
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(data, &selection); err != nil {
				t.Fatal(err)
			}
			err = gob.NewEncoder(file).Encode(selection)
		}
		closeErr := file.Close()
		if err != nil {
			t.Fatal(err)
		}
		if closeErr != nil {
			t.Fatal(closeErr)
		}
		return
	default:
		data = []byte(content)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func containsAnyString(value any, want string) bool {
	items, ok := value.([]any)
	if !ok {
		return false
	}
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

func readMCPFile(t *testing.T, path string) string {
	t.Helper()
	if filepath.Ext(path) == ".gob" {
		file, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		defer file.Close()
		var selection rules.Selection
		if err := gob.NewDecoder(file).Decode(&selection); err != nil {
			t.Fatal(err)
		}
		data, err := json.MarshalIndent(selection, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
