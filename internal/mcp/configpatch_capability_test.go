package mcp

import (
	"fmt"
	"path/filepath"
	"reflect"
	"testing"

	"localclash/internal/appinit"
	"localclash/internal/configpatch"
	"localclash/internal/localconfig"
	"localclash/internal/poolcontract"
)

func TestConfigPatchPreservesPoolProvidersForUnrelatedChanges(t *testing.T) {
	for _, tc := range []struct {
		name       string
		apply      bool
		test       bool
		statePaths bool
	}{
		{name: "draft_default_paths"},
		{name: "tested_draft_state_paths", test: true, statePaths: true},
		{name: "apply_default_paths", apply: true},
		{name: "tested_apply_state_paths", apply: true, test: true, statePaths: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			paths := setupMCPPlanFixture(t)
			dir := filepath.Dir(paths.subscription)
			runtimeRoot := filepath.Join(dir, ".runtime")
			if tc.statePaths {
				runtimeRoot = filepath.Join(dir, "state-runtime")
			}
			generated := filepath.Join(runtimeRoot, "mihomo", "config.yaml")
			writeMCPFile(t, filepath.Join(dir, "patches", "template.capability_chatgpt.json"), fmt.Sprintf(`{
  "version": 1,
  "patch_id": "template.capability",
  "title": "ChatGPT",
  "source": "policy_template",
  "status": "enabled",
  "order_id": "0200.000000",
  "overlay": {
    "proxy_groups": [{"id":"ChatGPT-available","mode":"auto","capability":%q,"optional":true}],
    "packs": [{"source":"blackmatrix7","pack":"OpenAI","target":"ChatGPT-available"}]
  }
}`, poolcontract.ChatGPTCapability))

			server := NewServer()
			if tc.statePaths {
				core := filepath.Join(dir, "fake-mihomo")
				writeTestExecutable(t, core, "#!/bin/sh\nif [ \"$1\" = \"-v\" ]; then echo 'Mihomo Meta test'; exit 0; fi\necho ok\n")
				server = NewServerWithState(appinit.RuntimeState{Paths: appinit.RuntimePaths{
					WorkspaceRoot:      dir,
					RuntimeRoot:        runtimeRoot,
					SubscriptionPath:   paths.subscription,
					RulesCacheDir:      paths.cache,
					GeneratedConfig:    generated,
					MihomoRuntimeDir:   filepath.Dir(generated),
					RuntimeProfilePath: filepath.Join(dir, "localclash-runtime.json"),
					PacksSelectionPath: filepath.Join(dir, "localclash-packs.gob"),
					CorePath:           core,
				}})
			}

			// Both product paths must preserve the same provider reference.
			render := callHandleWithServer(t, server, map[string]any{
				"jsonrpc": "2.0", "id": 1, "method": "tools/call",
				"params": map[string]any{"name": "config_render", "arguments": map[string]any{"background": false}},
			})
			if render.Error != nil {
				t.Fatalf("render existing capability group: %+v", render.Error)
			}
			group := findMCPProxyGroup(t, readMCPYAML(t, generated), "ChatGPT-available")
			if !reflect.DeepEqual(group["use"], []any{"localclash-qualified-chatgpt"}) {
				t.Fatalf("rendered capability provider = %v", group["use"])
			}

			draft := callConfigPatchDraftForOverlay(t, server, paths, "user.unrelated", map[string]any{
				"proxy_groups": []map[string]any{{"id": "Unrelated-Direct", "mode": "direct"}},
			}, tc.test && !tc.apply)
			if draft["valid"] != true {
				t.Fatalf("draft = %+v, want valid", draft)
			}
			if !tc.apply {
				return
			}
			args := draft["apply_args"].(map[string]any)
			args["test"] = tc.test
			args["background"] = false
			apply := callHandleWithServer(t, server, map[string]any{
				"jsonrpc": "2.0", "id": 2, "method": "tools/call",
				"params": map[string]any{"name": "config_patch_apply", "arguments": args},
			})
			if apply.Error != nil {
				t.Fatalf("apply unrelated change: %+v", apply.Error)
			}
			content := marshalToolResult(t, apply.Result).StructuredContent.(map[string]any)
			if content["applied"] != true || content["valid"] != true {
				t.Fatalf("apply = %+v, want applied and valid", content)
			}

			registry, err := configpatch.Load(filepath.Join(dir, "patches"))
			if err != nil {
				t.Fatal(err)
			}
			if _, persisted := registry.ByID["user.unrelated"]; !persisted {
				t.Fatal("unrelated patch was not persisted")
			}
			intent, err := localconfig.Load(filepath.Join(dir, "localclash-intent.json"))
			if err != nil {
				t.Fatal(err)
			}
			if intent.ProxyGroups["Unrelated-Direct"].Mode != "direct" {
				t.Fatal("compiled intent is missing the applied direct group")
			}
			group = findMCPProxyGroup(t, readMCPYAML(t, generated), "ChatGPT-available")
			if !reflect.DeepEqual(group["use"], []any{"localclash-qualified-chatgpt"}) {
				t.Fatalf("applied capability provider = %v", group["use"])
			}
		})
	}
}
