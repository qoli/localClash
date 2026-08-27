package main

import (
	"context"
	"encoding/gob"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"localclash/internal/appinit"
	"localclash/internal/chatgptavailable"
	"localclash/internal/coredownload"
	"localclash/internal/rules"
	"localclash/internal/runtimeprofile"
	"localclash/internal/subscriptions"

	"gopkg.in/yaml.v3"
)

func TestRunResetDoesNotBootstrapRuntimeFirst(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	if err := run([]string{"reset", "--dry-run"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(".runtime"); !os.IsNotExist(err) {
		t.Fatalf("reset should run before bootstrap creates .runtime, err=%v", err)
	}
}

func TestProductSubscriptionStageLoggerWritesShareableJSONL(t *testing.T) {
	var output strings.Builder
	logger := productSubscriptionStageLogger(&output)
	logger(subscriptions.StageEvent{
		Stage:      "fetch_subscription_response",
		Event:      "error",
		DurationMS: 12,
		Error:      "subscription 07 request failed: HTTP 400 Bad Request",
		Fields: map[string]any{
			"display_id":  "07",
			"uri":         "https://example.com/sub?...",
			"status_code": 400,
		},
	})

	var record map[string]any
	if err := json.Unmarshal([]byte(output.String()), &record); err != nil {
		t.Fatalf("subscription stage log = %q, error = %v", output.String(), err)
	}
	if record["component"] != "subscription_refresh" || record["stage"] != "fetch_subscription_response" || record["event"] != "error" {
		t.Fatalf("stage log = %+v, want subscription refresh error identity", record)
	}
	if record["display_id"] != "07" || record["uri"] != "https://example.com/sub?..." || record["status_code"] != float64(400) {
		t.Fatalf("stage log = %+v, want display ID, URI, and HTTP status", record)
	}
}

func TestRunRuntimeStatusPrintsJSONEnvelope(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("runtime process-name discovery requires procfs")
	}
	dir := t.TempDir()
	t.Chdir(dir)

	workDir := filepath.Join(".runtime", "mihomo")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(workDir, "config.yaml")
	if err := os.MkdirAll(filepath.Dir(config), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config, []byte("external-controller: 127.0.0.1:9090\nexternal-ui: ui/zashboard\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	core := filepath.Join("bin", "linux-"+runtime.GOARCH, "lc-mihomo-meta")
	cmd := startFakeRuntime(t, core, workDir, config)

	output := captureStdout(t, func() error {
		return run([]string{"runtime", "status", "--json"})
	})
	var result struct {
		OK     bool `json:"ok"`
		Status struct {
			Running       bool   `json:"running"`
			PID           int    `json:"pid"`
			ExternalUIURL string `json:"external_ui_url"`
		} `json:"status"`
	}
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("status JSON = %q, error = %v", output, err)
	}
	if !result.OK || !result.Status.Running || result.Status.PID != cmd.Process.Pid || result.Status.ExternalUIURL != "http://127.0.0.1:9090/ui" {
		t.Fatalf("status result = %+v, want current pid and external UI", result)
	}
}

func TestRunRuntimeStatusUsesDetectedWorkDir(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("runtime process-name discovery requires procfs")
	}
	installDir := t.TempDir()
	wrongDir := t.TempDir()
	t.Setenv("LOCALCLASH_WORKDIR", installDir)
	t.Chdir(wrongDir)

	workDir := filepath.Join(installDir, ".runtime", "mihomo")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(workDir, "config.yaml")
	if err := os.MkdirAll(filepath.Dir(config), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config, []byte("external-controller: 127.0.0.1:9090\nexternal-ui: ui/zashboard\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	core := filepath.Join(installDir, "bin", "linux-"+runtime.GOARCH, "lc-mihomo-meta")
	cmd := startFakeRuntime(t, core, workDir, config)

	output := captureStdout(t, func() error {
		return run([]string{"runtime", "status", "--json"})
	})
	var result struct {
		OK     bool `json:"ok"`
		Status struct {
			Running    bool   `json:"running"`
			PID        int    `json:"pid"`
			RuntimeDir string `json:"runtime_dir"`
			Config     string `json:"config"`
		} `json:"status"`
	}
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("status JSON = %q, error = %v", output, err)
	}
	if !result.OK || !result.Status.Running || result.Status.PID != cmd.Process.Pid {
		t.Fatalf("status result = %+v, want detected runtime", result)
	}
	if result.Status.RuntimeDir != workDir || result.Status.Config != config {
		t.Fatalf("status paths = runtime %q config %q, want detected workdir", result.Status.RuntimeDir, result.Status.Config)
	}
	if _, err := os.Stat(filepath.Join(wrongDir, ".runtime")); !os.IsNotExist(err) {
		t.Fatalf("runtime status should not create state under wrong cwd, err=%v", err)
	}
}

func TestRunProductStatusPrintsJSONEnvelope(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	output := captureStdout(t, func() error {
		return run([]string{"status", "--json"})
	})
	var result struct {
		OK      bool           `json:"ok"`
		Changed bool           `json:"changed"`
		Status  map[string]any `json:"status"`
	}
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("product status JSON = %q, error = %v", output, err)
	}
	if !result.OK || result.Changed || result.Status["runtime"] == nil || result.Status["components"] == nil {
		t.Fatalf("product status result = %+v, want product status envelope", result)
	}
}

func TestRunProductRuntimeFactsUsesGeneratedConfig(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("LOCALCLASH_WORKDIR", dir)
	t.Chdir(dir)
	writeMainTestFile(t, filepath.Join(dir, "localclash-runtime.json"), `{"version":2,"mode":"router","core":"meta"}`)
	writeMainTestFile(t, filepath.Join(dir, ".runtime", "mihomo", "config.yaml"), `redir-port: 17892
tproxy-port: 17895
ipv6: true
external-controller: 127.0.0.1:19090
dns:
  listen: 0.0.0.0:17874
tun:
  enable: true
  device: lc-test-tun
  auto-route: false
  auto-redirect: false
`)

	output := captureStdout(t, func() error {
		return run([]string{"runtime", "facts", "--json"})
	})
	var result struct {
		OK     bool `json:"ok"`
		Status struct {
			SchemaVersion  int    `json:"schema_version"`
			ProfileMode    string `json:"profile_mode"`
			RuntimeRunning bool   `json:"runtime_running"`
			DNSPort        int    `json:"dns_port"`
			RedirPort      int    `json:"redir_port"`
			TProxyPort     int    `json:"tproxy_port"`
			TunDevice      string `json:"tun_device"`
			ConfigSHA256   string `json:"config_sha256"`
		} `json:"status"`
	}
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("runtime facts JSON = %q, error = %v", output, err)
	}
	if !result.OK || result.Status.SchemaVersion != 1 || result.Status.ProfileMode != "router" || result.Status.RuntimeRunning {
		t.Fatalf("runtime facts identity = %+v", result)
	}
	if result.Status.DNSPort != 17874 || result.Status.RedirPort != 17892 || result.Status.TProxyPort != 17895 || result.Status.TunDevice != "lc-test-tun" || result.Status.ConfigSHA256 == "" {
		t.Fatalf("runtime facts network = %+v", result.Status)
	}
}

func TestRunProductResetFullDryRunPrintsJSONEnvelope(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "localclash")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	t.Setenv("LOCALCLASH_WORKDIR", dir)
	writeMainTestFile(t, "localclash-intent.json", "version: 1\n")
	expected, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}

	output := captureStdout(t, func() error {
		return run([]string{"reset", "--full", "--dry-run", "--json"})
	})
	var result struct {
		OK      bool `json:"ok"`
		Changed bool `json:"changed"`
		Status  struct {
			Full    bool `json:"full"`
			DryRun  bool `json:"dry_run"`
			Deleted []struct {
				Path string `json:"path"`
			} `json:"deleted"`
		} `json:"status"`
	}
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("product reset JSON = %q, error = %v", output, err)
	}
	if !result.OK || result.Changed || !result.Status.Full || !result.Status.DryRun || len(result.Status.Deleted) != 1 || result.Status.Deleted[0].Path != expected {
		t.Fatalf("product reset result = %+v, want full dry-run envelope for %s", result, expected)
	}
	if _, err := os.Stat(filepath.Join(dir, "localclash-intent.json")); err != nil {
		t.Fatalf("dry-run should keep localclash-intent.json: %v", err)
	}
}

func TestRunProductConfigRenderUsesDurableLocalClashIntent(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	writeMainTestFile(t, "subscription.gob", `proxies:
  - name: "HK 01"
    type: ss
    server: example.com
    port: 443
    cipher: none
    password: test
`)
	writeMainTestFile(t, "localclash-intent.json", `version: 1
policy_template: localclash-default
proxy_groups:
  AI:
    mode: auto
    match:
      type: name_regex
      pattern: ".*"
      min: 1
custom_rules:
  - id: ai_test
    target: AI
    rules:
      - type: DOMAIN
        value: example.ai
`)
	writeMainTestPackIndex(t, filepath.Join(".runtime", "rules", "packs"))

	output := captureStdout(t, func() error {
		return run([]string{"config", "render", "--json"})
	})
	var result struct {
		OK     bool `json:"ok"`
		Status struct {
			Source    string `json:"source"`
			Selection string `json:"selection"`
		} `json:"status"`
	}
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("config render JSON = %q, error = %v", output, err)
	}
	if !result.OK || result.Status.Source != "compiled_intent" || result.Status.Selection != "localclash-packs.gob" {
		t.Fatalf("config render result = %+v, want compiled intent with derived selection", result)
	}
	generated, err := os.ReadFile(filepath.Join(".runtime", "mihomo", "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(generated)
	if !strings.Contains(text, "name: AI") || !strings.Contains(text, "DOMAIN,example.ai,AI") {
		t.Fatalf("generated config did not consume localclash-intent.json intent:\n%s", text)
	}
	if _, err := os.Stat("localclash-packs.gob"); err != nil {
		t.Fatalf("derived localclash-packs.gob missing: %v", err)
	}
}

func TestRunProductConfigRenderUsesQualifiedCapabilitySnapshot(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	writeMainTestFile(t, "subscription.gob", `proxies:
  - name: "US 01"
    type: ss
    server: example.com
    port: 443
    cipher: none
    password: test
`)
	writeMainTestFile(t, "localclash-intent.json", `version: 4
proxy_groups:
  ChatGPT-available:
    mode: auto
    capability: openai.chatgpt.statsig.v1
    optional: true
custom_rules:
  - id: chatgpt_test
    target: ChatGPT-available
    rules:
      - type: domain_suffix
        value: openai.com
`)
	writeMainTestFile(t, filepath.Join(".runtime", "capabilities", "chatgpt-available.json"), `{
  "version": 5,
  "profile": "openai.chatgpt.statsig.v1",
  "updated_at": "2026-08-15T00:00:00Z",
  "qualified": ["US 01"],
  "nodes": {}
}`)
	writeMainTestPackIndex(t, filepath.Join(".runtime", "rules", "packs"))

	output := captureStdout(t, func() error {
		return run([]string{"config", "render", "--json"})
	})
	var result struct {
		OK bool `json:"ok"`
	}
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("config render JSON = %q, error = %v", output, err)
	}
	if !result.OK {
		t.Fatalf("config render result = %+v, want qualified capability snapshot to resolve", result)
	}
	generated, err := os.ReadFile(filepath.Join(".runtime", "mihomo", "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if text := string(generated); !strings.Contains(text, "name: ChatGPT-available") || !strings.Contains(text, "US 01") {
		t.Fatalf("generated config did not consume qualified capability snapshot:\n%s", text)
	}
}

func TestRunProductSubscriptionRefreshBuildsCapabilityForFollowingRender(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("LOCALCLASH_WORKDIR", dir)

	subscriptionServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`proxies:
  - name: US 01
    type: ss
    server: us.example.com
    port: 443
    cipher: aes-128-gcm
    password: secret
`))
	}))
	t.Cleanup(subscriptionServer.Close)
	replace := true
	if _, err := subscriptions.Configure(subscriptions.ConfigureOptions{
		ConfigPath: filepath.Join(dir, "localclash-subscriptions.json"),
		Sources:    []subscriptions.Source{{URL: subscriptionServer.URL + "/sub", DisplayName: "01"}},
		Replace:    &replace,
	}); err != nil {
		t.Fatal(err)
	}
	writeMainTestFile(t, filepath.Join(dir, "localclash-intent.json"), `version: 4
proxy_groups:
  ChatGPT-available:
    mode: auto
    capability: openai.chatgpt.statsig.v1
    optional: true
custom_rules:
  - id: chatgpt_test
    target: ChatGPT-available
    rules:
      - type: domain_suffix
        value: openai.com
`)
	writeMainTestPackIndex(t, filepath.Join(dir, ".runtime", "rules", "packs"))

	previousRebuild := rebuildProductChatGPT
	t.Cleanup(func() { rebuildProductChatGPT = previousRebuild })
	rebuildCalled := false
	rebuildProductChatGPT = func(_ context.Context, proxies []map[string]any, _, runtimeParent, snapshotPath string) (chatgptavailable.Result, error) {
		rebuildCalled = true
		if len(proxies) != 1 || proxies[0]["name"] != "US 01" {
			t.Fatalf("capability proxies = %+v, want refreshed merged proxy", proxies)
		}
		if runtimeParent != filepath.Join(dir, ".runtime", "capabilities") || snapshotPath != filepath.Join(runtimeParent, "chatgpt-available.json") {
			t.Fatalf("capability paths = %q / %q", runtimeParent, snapshotPath)
		}
		writeMainTestFile(t, snapshotPath, `{
  "version": 5,
  "profile": "openai.chatgpt.statsig.v1",
  "updated_at": "2026-08-15T00:00:00Z",
  "qualified": ["US 01"],
  "nodes": {}
}`)
		return chatgptavailable.Result{
			Profile:          chatgptavailable.ProfileID,
			SnapshotPath:     snapshotPath,
			Candidates:       1,
			Probed:           1,
			Qualified:        []string{"US 01"},
			QualifiedCount:   1,
			UnavailableCount: 0,
		}, nil
	}

	refreshOutput := captureStdout(t, func() error {
		return run([]string{"subscription", "refresh", "--json"})
	})
	if !rebuildCalled {
		t.Fatal("subscription refresh did not rebuild the configured capability")
	}
	var refreshResult struct {
		OK     bool `json:"ok"`
		Status struct {
			Capabilities []chatgptavailable.Result `json:"capabilities"`
		} `json:"status"`
	}
	if err := json.Unmarshal([]byte(refreshOutput), &refreshResult); err != nil {
		t.Fatalf("subscription refresh JSON = %q, error = %v", refreshOutput, err)
	}
	if !refreshResult.OK || len(refreshResult.Status.Capabilities) != 1 || refreshResult.Status.Capabilities[0].QualifiedCount != 1 {
		t.Fatalf("subscription refresh result = %+v, want qualified capability evidence", refreshResult)
	}

	renderOutput := captureStdout(t, func() error {
		return run([]string{"config", "render", "--json"})
	})
	var renderResult struct {
		OK bool `json:"ok"`
	}
	if err := json.Unmarshal([]byte(renderOutput), &renderResult); err != nil {
		t.Fatalf("config render JSON = %q, error = %v", renderOutput, err)
	}
	if !renderResult.OK {
		t.Fatalf("config render result = %+v, want snapshot-backed render", renderResult)
	}
	generated, err := os.ReadFile(filepath.Join(dir, ".runtime", "mihomo", "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if text := string(generated); !strings.Contains(text, "name: ChatGPT-available") || !strings.Contains(text, "US 01") {
		t.Fatalf("one-click render did not consume refreshed capability:\n%s", text)
	}
}

func TestRunProductConfigRenderUsesEnvWorkspaceFromNeutralCwd(t *testing.T) {
	installDir := t.TempDir()
	wrongDir := t.TempDir()
	t.Setenv("LOCALCLASH_WORKDIR", installDir)
	t.Chdir(wrongDir)

	writeMainTestFile(t, filepath.Join(installDir, "subscription.gob"), `proxies:
  - name: "HK 01"
    type: ss
    server: example.com
    port: 443
    cipher: none
    password: test
`)
	writeMainTestFile(t, filepath.Join(installDir, "localclash-intent.json"), `version: 1
policy_template: localclash-default
proxy_groups:
  AI:
    mode: auto
    match:
      type: name_regex
      pattern: ".*"
      min: 1
`)
	writeMainTestPackIndex(t, filepath.Join(installDir, ".runtime", "rules", "packs"))

	output := captureStdout(t, func() error {
		return run([]string{"config", "render", "--json"})
	})
	var result struct {
		OK     bool `json:"ok"`
		Status struct {
			Selection string `json:"selection"`
			Output    string `json:"output"`
		} `json:"status"`
	}
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("config render JSON = %q, error = %v", output, err)
	}
	if !result.OK || result.Status.Selection != filepath.Join(installDir, "localclash-packs.gob") || result.Status.Output != filepath.Join(installDir, ".runtime", "mihomo", "config.yaml") {
		t.Fatalf("config render result = %+v, want paths under %s", result, installDir)
	}
	if _, err := os.Stat(filepath.Join(installDir, ".runtime", "mihomo", "config.yaml")); err != nil {
		t.Fatalf("generated config should be written under workspace: %v", err)
	}
	if _, err := os.Stat(filepath.Join(wrongDir, "generated", "mihomo.yaml")); !os.IsNotExist(err) {
		t.Fatalf("generated config should not be written under cwd, err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(wrongDir, "localclash-packs.gob")); !os.IsNotExist(err) {
		t.Fatalf("selection should not be written under cwd, err=%v", err)
	}
}

func TestRunProductConfigApplyTemplateWritesV2RuntimeWithoutUserProfile(t *testing.T) {
	dir := t.TempDir()
	wrongDir := t.TempDir()
	t.Setenv("LOCALCLASH_WORKDIR", dir)
	t.Chdir(wrongDir)
	writeMainTestFile(t, filepath.Join(dir, "policy-templates", "localclash-default.json"), `{
  "id": "localclash-default",
  "name": "Test Default",
  "description": "Test default template.",
  "config": {
    "version": 3,
    "policy_template": "localclash-default",
    "proxy_groups": {},
    "packs": []
  }
}`)
	input := filepath.Join(dir, "template-input.json")
	writeMainTestFile(t, input, `{
  "version": 1,
  "template": "localclash-default",
  "runtime_profile": "router",
  "core": "meta",
  "allow_overwrite_modified": false
}`)

	output := captureStdout(t, func() error {
		return run([]string{"config", "apply-template", "--input", input, "--json"})
	})
	var result struct {
		OK bool `json:"ok"`
	}
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("config apply-template JSON = %q, error = %v", output, err)
	}
	if !result.OK {
		t.Fatalf("config apply-template result = %+v, want ok", result)
	}
	if _, err := os.Stat(filepath.Join(dir, "localclash-intent.json")); err != nil {
		t.Fatalf("localclash-intent.json should be written: %v", err)
	}
	if _, err := os.Stat(filepath.Join(wrongDir, "localclash-intent.json")); !os.IsNotExist(err) {
		t.Fatalf("localclash-intent.json should not be written under cwd, err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "localclash-user.json")); !os.IsNotExist(err) {
		t.Fatalf("localclash-user.json should not be created by default bootstrap path, err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "profiles")); !os.IsNotExist(err) {
		t.Fatalf("profiles working directory should not be created, err=%v", err)
	}
	runtimeData, err := os.ReadFile(filepath.Join(dir, "localclash-runtime.json"))
	if err != nil {
		t.Fatal(err)
	}
	var runtimeFile struct {
		Version int    `json:"version"`
		Mode    string `json:"mode"`
		Core    string `json:"core"`
	}
	if err := json.Unmarshal(runtimeData, &runtimeFile); err != nil {
		t.Fatal(err)
	}
	if runtimeFile.Version != 2 || runtimeFile.Mode != runtimeprofile.ModeRouter || runtimeFile.Core != runtimeprofile.CoreMeta {
		t.Fatalf("runtime file = %+v, want v2 router/meta", runtimeFile)
	}
}

func TestRunProductConfigStatusRejectsInvalidUserProfile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("LOCALCLASH_WORKDIR", dir)
	t.Chdir(dir)
	writeMainTestFile(t, "localclash-runtime.json", `{"version":2,"mode":"router","core":"meta"}`)
	writeMainTestFile(t, "localclash-user.json", `{"rules":["MATCH,DIRECT"]}`)

	err := run([]string{"config", "status", "--json"})
	if err == nil || !strings.Contains(err.Error(), "rules") || !strings.Contains(err.Error(), "localclash-user.json") {
		t.Fatalf("config status error = %v, want banned user profile key", err)
	}
}

func TestRunProductComponentUpdateMihomoRefreshesCoreVersionCache(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("LOCALCLASH_WORKDIR", dir)
	t.Chdir(dir)
	core := filepath.Join(dir, runtimeprofile.MetaCorePath)
	oldDownloadCore := downloadCore
	downloadCore = func(ctx context.Context, opts coredownload.Options) ([]coredownload.Result, error) {
		writeMainExecutableCore(t, core, "Mihomo component update")
		return []coredownload.Result{{OutputPath: core, Flavor: coredownload.FlavorMeta, Target: opts.Target}}, nil
	}
	t.Cleanup(func() {
		downloadCore = oldDownloadCore
	})

	output := captureStdout(t, func() error {
		return run([]string{"component", "update", "mihomo", "--json"})
	})
	var result struct {
		OK       bool     `json:"ok"`
		Warnings []string `json:"warnings"`
	}
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("component update JSON = %q, error = %v", output, err)
	}
	if !result.OK || len(result.Warnings) != 0 {
		t.Fatalf("component update result = %+v, want ok without cache warning", result)
	}
	cache := readMainCoreCache(t, appinit.CoreVersionCachePath(filepath.Join(dir, ".runtime")))
	if cache.CorePath != core || cache.Version != "Mihomo component update" {
		t.Fatalf("cache = %+v, want refreshed component update core %s", cache, core)
	}
}

func TestExecuteDesiredConfigRefreshesCoreVersionCacheAfterRuntimeProfileSwitch(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("LOCALCLASH_WORKDIR", dir)
	t.Chdir(dir)
	meta := filepath.Join(dir, runtimeprofile.MetaCorePath)
	smart := filepath.Join(dir, runtimeprofile.SmartCorePath)
	writeMainExecutableCore(t, meta, "Mihomo meta")
	writeMainExecutableCore(t, smart, "Mihomo smart")
	state := appinit.Bootstrap(context.Background(), appinit.Options{})

	changed, warnings, err := executeDesiredConfig(context.Background(), &desiredConfig{
		RuntimeProfile: runtimeprofile.ModeRouter,
		Core:           runtimeprofile.CoreSmart,
	}, state)
	if err != nil {
		t.Fatal(err)
	}
	if !changed || len(warnings) != 0 {
		t.Fatalf("changed=%v warnings=%v, want profile switch without warnings", changed, warnings)
	}
	cache := readMainCoreCache(t, appinit.CoreVersionCachePath(filepath.Join(dir, ".runtime")))
	if cache.CorePath != smart || cache.Version != "Mihomo smart" || !cache.SmartSupported {
		t.Fatalf("cache = %+v, want smart core refresh", cache)
	}
}

func TestRunProductRuntimeStartRefreshesCoreVersionCache(t *testing.T) {
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
	t.Setenv("LOCALCLASH_WORKDIR", dir)
	t.Chdir(dir)
	core := filepath.Join(dir, runtimeprofile.MetaCorePath)
	writeMainExecutableCore(t, core, "Mihomo runtime start")
	writeMainCoreCache(t, appinit.CoreVersionCachePath(filepath.Join(dir, ".runtime")), core, "Mihomo stale")
	config := filepath.Join(dir, ".runtime", "mihomo", "config.yaml")
	writeMainTestFile(t, config, "mixed-port: 7890\nexternal-controller: "+strings.TrimPrefix(controller.URL, "http://")+"\n")

	output := captureStdout(t, func() error {
		return run([]string{"runtime", "start", "--json"})
	})
	var result struct {
		OK     bool `json:"ok"`
		Status struct {
			PID int `json:"pid"`
		} `json:"status"`
		Warnings []string `json:"warnings"`
	}
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("runtime start JSON = %q, error = %v", output, err)
	}
	if result.Status.PID > 0 {
		t.Cleanup(func() {
			if process, err := os.FindProcess(result.Status.PID); err == nil {
				_ = process.Kill()
				_, _ = process.Wait()
			}
		})
	}
	if !result.OK || result.Status.PID == 0 {
		t.Fatalf("runtime start result = %+v, want started runtime", result)
	}
	cache := readMainCoreCache(t, appinit.CoreVersionCachePath(filepath.Join(dir, ".runtime")))
	if cache.CorePath != core || cache.Version != "Mihomo runtime start" {
		t.Fatalf("cache = %+v, want runtime start refresh", cache)
	}
}

func TestRunProductRuntimeRestartAcceptsStrategy(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("LOCALCLASH_WORKDIR", dir)
	t.Chdir(dir)
	core := filepath.Join(dir, runtimeprofile.MetaCorePath)
	writeMainExecutableCore(t, core, "Mihomo runtime restart")
	config := filepath.Join(dir, ".runtime", "mihomo", "config.yaml")
	writeMainTestFile(t, config, "mixed-port: 7890\n")

	output := captureStdout(t, func() error {
		return run([]string{"runtime", "restart", "--strategy", "process_restart", "--json"})
	})
	var result struct {
		OK     bool `json:"ok"`
		Status struct {
			AppliedStrategy string `json:"applied_strategy"`
			Start           struct {
				PID int `json:"pid"`
			} `json:"start"`
		} `json:"status"`
	}
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("runtime restart JSON = %q, error = %v", output, err)
	}
	if result.Status.Start.PID > 0 {
		t.Cleanup(func() {
			if process, err := os.FindProcess(result.Status.Start.PID); err == nil {
				_ = process.Kill()
				_, _ = process.Wait()
			}
		})
	}
	if !result.OK || result.Status.AppliedStrategy != "process_restart" || result.Status.Start.PID == 0 {
		t.Fatalf("runtime restart result = %+v, want process_restart runtime", result)
	}
}

func TestRunDoctorUsesLiveCoreProbeWhenBootstrapUsesCachedVersion(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("LOCALCLASH_WORKDIR", dir)
	t.Chdir(dir)
	core := filepath.Join(dir, runtimeprofile.MetaCorePath)
	countPath := filepath.Join(dir, "version-count")
	writeMainCountingCore(t, core, countPath, "Mihomo doctor live")
	writeMainCoreCache(t, appinit.CoreVersionCachePath(filepath.Join(dir, ".runtime")), core, "Mihomo cached")

	_ = captureStdout(t, func() error {
		return run([]string{"doctor", "--json"})
	})
	if got := readMainCount(t, countPath); got != 1 {
		t.Fatalf("doctor core -v count = %d, want 1 live probe", got)
	}
}

func startFakeRuntime(t *testing.T, core, workDir, config string) *exec.Cmd {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(core), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(core, []byte("#!/bin/sh\nif [ \"$1\" = \"-v\" ]; then echo \"mihomo fake\"; exit 0; fi\nsleep 300\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(core, "-d", workDir, "-f", config)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	})
	return cmd
}

type mainCoreCache struct {
	CorePath       string `json:"core_path"`
	Version        string `json:"version"`
	SmartSupported bool   `json:"smart_supported"`
	UpdatedAt      string `json:"updated_at"`
}

func writeMainExecutableCore(t *testing.T, path, version string) {
	t.Helper()
	script := "#!/bin/sh\n" +
		"if [ \"$1\" = \"-v\" ]; then echo " + strconv.Quote(version) + "; exit 0; fi\n" +
		"for arg in \"$@\"; do if [ \"$arg\" = \"-t\" ]; then echo ok; exit 0; fi; done\n" +
		"sleep 300\n"
	writeMainTestFile(t, path, script)
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

func writeMainCountingCore(t *testing.T, path, countPath, version string) {
	t.Helper()
	script := "#!/bin/sh\n" +
		"if [ \"$1\" = \"-v\" ]; then\n" +
		"  count=0\n" +
		"  [ -f " + strconv.Quote(countPath) + " ] && count=$(cat " + strconv.Quote(countPath) + ")\n" +
		"  count=$((count + 1))\n" +
		"  echo \"$count\" > " + strconv.Quote(countPath) + "\n" +
		"  echo " + strconv.Quote(version) + "\n" +
		"  exit 0\n" +
		"fi\n" +
		"for arg in \"$@\"; do if [ \"$arg\" = \"-t\" ]; then echo ok; exit 0; fi; done\n" +
		"sleep 300\n"
	writeMainTestFile(t, path, script)
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

func writeMainCoreCache(t *testing.T, path, corePath, version string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	cache := mainCoreCache{
		CorePath:       corePath,
		Version:        version,
		SmartSupported: strings.Contains(strings.ToLower(version), "smart"),
		UpdatedAt:      "2026-05-28T09:00:00Z",
	}
	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readMainCoreCache(t *testing.T, path string) mainCoreCache {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var cache mainCoreCache
	if err := json.Unmarshal(data, &cache); err != nil {
		t.Fatal(err)
	}
	return cache
}

func readMainCount(t *testing.T, path string) int {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	count, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		t.Fatal(err)
	}
	return count
}

func captureStdout(t *testing.T, fn func() error) string {
	t.Helper()
	output, err := captureStdoutAllowError(t, fn)
	if err != nil {
		t.Fatal(err)
	}
	return output
}

func captureStdoutAllowError(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	original := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		return "", err
	}
	os.Stdout = writer
	err = fn()
	if closeErr := writer.Close(); closeErr != nil && err == nil {
		err = closeErr
	}
	os.Stdout = original
	t.Cleanup(func() {
		os.Stdout = original
		_ = reader.Close()
	})
	data, readErr := io.ReadAll(reader)
	if readErr != nil {
		return "", readErr
	}
	return string(data), err
}

func writeMainTestFile(t *testing.T, path string, content string) {
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
		encodeErr := gob.NewEncoder(file).Encode(struct {
			Version int
			Data    map[string]any
			Raw     []byte
		}{Version: 1, Data: doc, Raw: []byte(content)})
		closeErr := file.Close()
		if encodeErr != nil {
			t.Fatal(encodeErr)
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

func writeMainTestPackIndex(t *testing.T, cacheDir string) {
	t.Helper()
	if err := rules.WritePackIndex(rules.PackIndexPath(cacheDir), map[string]rules.PackCache{
		"blackmatrix7": {
			Version:    1,
			Source:     "blackmatrix7",
			Adapter:    "blackmatrix7",
			Renderable: true,
			Packs: []rules.Pack{{
				ID:         "OpenAI",
				Name:       "OpenAI",
				Target:     "AI",
				Renderable: true,
				Components: []rules.Component{{
					ID:         "OpenAI",
					Behavior:   "classical",
					Format:     "yaml",
					OrderClass: "mixed",
					URL:        "https://example.com/OpenAI.yaml",
					Path:       "./rule-packs/blackmatrix7/OpenAI.yaml",
				}},
			}},
		},
	}); err != nil {
		t.Fatal(err)
	}
}
