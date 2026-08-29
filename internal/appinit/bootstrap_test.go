package appinit

import (
	"context"
	"encoding/gob"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"localclash/internal/rules"

	"gopkg.in/yaml.v3"
)

func TestBootstrapBuildsRuntimeStateFromLocalArtifacts(t *testing.T) {
	dir := t.TempDir()
	core := filepath.Join(dir, "bin", "mihomo")
	writeAppinitFile(t, core, "#!/bin/sh\nif [ \"$1\" = \"-v\" ]; then echo Mihomo test; exit 0; fi\nexit 0\n", 0o755)
	subscription := filepath.Join(dir, "subscription.gob")
	writeAppinitFile(t, subscription, `proxies:
  - name: SG 01
    type: ss
    server: sg.example.com
    password: secret
`, 0o644)
	cacheDir := filepath.Join(dir, ".runtime", "rules", "packs")
	writeAppinitPackIndex(t, cacheDir)

	state := Bootstrap(context.Background(), Options{
		RuntimeRoot:        filepath.Join(dir, ".runtime"),
		RuleSourcesDir:     filepath.Join(dir, "rule-sources"),
		RulesCacheDir:      cacheDir,
		GeneratedConfig:    filepath.Join(dir, "generated", "mihomo.yaml"),
		SubscriptionPath:   subscription,
		MihomoRuntimeDir:   filepath.Join(dir, ".runtime", "mihomo"),
		CorePath:           core,
		RuntimeProfilePath: filepath.Join(dir, "localclash-runtime.json"),
	})

	if !state.Core.Exists || !strings.Contains(state.Core.Version, "Mihomo test") {
		t.Fatalf("core = %+v, want version", state.Core)
	}
	if !state.Subscription.Available {
		t.Fatalf("subscription = %+v, want available", state.Subscription)
	}
	if !state.Rules.CatalogAvailable || len(state.Rules.Packs) != 1 {
		t.Fatalf("rules = %+v, want one pack catalog", state.Rules)
	}
	if state.Config.Rendered || state.Config.Available {
		t.Fatalf("config = %+v, bootstrap should not render generated config", state.Config)
	}
	if _, err := os.Stat(state.Paths.GeneratedConfig); !os.IsNotExist(err) {
		t.Fatalf("generated config should not be created by bootstrap, err=%v", err)
	}
}

func TestBootstrapRecordsDiagnosticsWithoutFailingProcess(t *testing.T) {
	dir := t.TempDir()
	state := Bootstrap(context.Background(), Options{
		RuntimeRoot:        filepath.Join(dir, ".runtime"),
		RuleSourcesDir:     filepath.Join(dir, "missing-rule-sources"),
		RulesCacheDir:      filepath.Join(dir, ".runtime", "rules", "packs"),
		GeneratedConfig:    filepath.Join(dir, "generated", "mihomo.yaml"),
		SubscriptionPath:   filepath.Join(dir, "subscription.gob"),
		MihomoRuntimeDir:   filepath.Join(dir, ".runtime", "mihomo"),
		CorePath:           filepath.Join(dir, "bin", "mihomo"),
		RuntimeProfilePath: filepath.Join(dir, "localclash-runtime.json"),
	})

	if state.Core.Exists {
		t.Fatal("core should be missing")
	}
	if state.Subscription.Available {
		t.Fatal("subscription should be unavailable")
	}
	if state.Rules.CatalogAvailable {
		t.Fatal("rules catalog should be unavailable")
	}
	if len(state.Diagnostics) == 0 {
		t.Fatal("expected bootstrap diagnostics")
	}
	if _, err := os.Stat(state.Paths.RulesCacheDir); err != nil {
		t.Fatalf("rules cache dir should be created: %v", err)
	}
}

func TestBootstrapDoesNotRenderGeneratedConfigOnStartup(t *testing.T) {
	dir := t.TempDir()
	core := filepath.Join(dir, "bin", "mihomo")
	writeAppinitFile(t, core, "#!/bin/sh\nif [ \"$1\" = \"-v\" ]; then echo Mihomo test; exit 0; fi\nexit 0\n", 0o755)
	subscription := filepath.Join(dir, "subscription.gob")
	writeAppinitFile(t, subscription, `proxies:
  - name: SG 01
    type: ss
    server: sg.example.com
    password: secret
`, 0o644)
	generated := filepath.Join(dir, "generated", "mihomo.yaml")

	state := Bootstrap(context.Background(), Options{
		RuntimeRoot:        filepath.Join(dir, ".runtime"),
		RuleSourcesDir:     filepath.Join(dir, "missing-rule-sources"),
		RulesCacheDir:      filepath.Join(dir, ".runtime", "rules", "packs"),
		GeneratedConfig:    generated,
		SubscriptionPath:   subscription,
		MihomoRuntimeDir:   filepath.Join(dir, ".runtime", "mihomo"),
		CorePath:           core,
		RuntimeProfilePath: filepath.Join(dir, "localclash-runtime.json"),
	})

	if state.Config.Rendered {
		t.Fatalf("config = %+v, want render skipped", state.Config)
	}
	if state.Config.Available {
		t.Fatalf("config = %+v, generated config should not be available", state.Config)
	}
	if _, err := os.Stat(generated); !os.IsNotExist(err) {
		t.Fatalf("generated config should not be created, err=%v", err)
	}
}

func TestBootstrapDefaultsToDetectedRouterWorkDir(t *testing.T) {
	wrongDir := t.TempDir()
	routerDir := t.TempDir()
	t.Chdir(wrongDir)
	writeAppinitFile(t, filepath.Join(routerDir, ".runtime", "mihomo", "config.yaml"), "mixed-port: 7890\n", 0o644)
	oldCandidates := defaultWorkDirCandidates
	defaultWorkDirCandidates = []string{routerDir}
	t.Cleanup(func() {
		defaultWorkDirCandidates = oldCandidates
	})

	state := Bootstrap(context.Background(), Options{})

	if state.Paths.MihomoRuntimeDir != filepath.Join(routerDir, ".runtime", "mihomo") {
		t.Fatalf("mihomo runtime dir = %q, want detected router workdir", state.Paths.MihomoRuntimeDir)
	}
	if state.Paths.GeneratedConfig != filepath.Join(routerDir, ".runtime", "mihomo", "config.yaml") {
		t.Fatalf("generated config = %q, want detected router workdir", state.Paths.GeneratedConfig)
	}
	if got := defaultWorkDirPath(state.Paths.RuntimeRoot, "localclash-intent.json"); got != filepath.Join(routerDir, "localclash-intent.json") {
		t.Fatalf("localclash config path = %q, want detected router workdir", got)
	}
	if state.Paths.CustomSitesProxy != filepath.Join(routerDir, "custom-sites", "proxy.json") || state.Paths.CustomSitesDirect != filepath.Join(routerDir, "custom-sites", "direct.json") {
		t.Fatalf("custom site paths = %q/%q, want detected router workdir", state.Paths.CustomSitesProxy, state.Paths.CustomSitesDirect)
	}
	if _, err := os.Stat(filepath.Join(wrongDir, ".runtime")); !os.IsNotExist(err) {
		t.Fatalf("bootstrap should not create runtime dir in wrong cwd, err=%v", err)
	}
}

func TestBootstrapUsesCoreVersionCacheWithoutExecutingCore(t *testing.T) {
	dir := t.TempDir()
	core := filepath.Join(dir, "bin", "mihomo")
	writeAppinitFile(t, core, "#!/bin/sh\nif [ \"$1\" = \"-v\" ]; then exit 23; fi\nexit 0\n", 0o755)
	cachePath := CoreVersionCachePath(filepath.Join(dir, ".runtime"))
	if err := writeCoreVersionCache(cachePath, CoreState{Path: core, Exists: true, Version: "Mihomo cached smart", SmartSupported: true}, fixedAppinitCacheNow()); err != nil {
		t.Fatal(err)
	}

	state := Bootstrap(context.Background(), Options{
		RuntimeRoot:        filepath.Join(dir, ".runtime"),
		RuleSourcesDir:     filepath.Join(dir, "missing-rule-sources"),
		RulesCacheDir:      filepath.Join(dir, ".runtime", "rules", "packs"),
		GeneratedConfig:    filepath.Join(dir, "generated", "mihomo.yaml"),
		SubscriptionPath:   filepath.Join(dir, "subscription.gob"),
		MihomoRuntimeDir:   filepath.Join(dir, ".runtime", "mihomo"),
		CorePath:           core,
		RuntimeProfilePath: filepath.Join(dir, "localclash-runtime.json"),
	})

	if state.Core.Version != "Mihomo cached smart" || !state.Core.SmartSupported {
		t.Fatalf("core = %+v, want cached version", state.Core)
	}
}

func TestBootstrapWritesCoreVersionCacheOnMiss(t *testing.T) {
	dir := t.TempDir()
	countPath := filepath.Join(dir, "count")
	core := filepath.Join(dir, "bin", "mihomo")
	writeCountingCore(t, core, countPath, "Mihomo live smart")

	state := Bootstrap(context.Background(), Options{
		RuntimeRoot:        filepath.Join(dir, ".runtime"),
		RuleSourcesDir:     filepath.Join(dir, "missing-rule-sources"),
		RulesCacheDir:      filepath.Join(dir, ".runtime", "rules", "packs"),
		GeneratedConfig:    filepath.Join(dir, "generated", "mihomo.yaml"),
		SubscriptionPath:   filepath.Join(dir, "subscription.gob"),
		MihomoRuntimeDir:   filepath.Join(dir, ".runtime", "mihomo"),
		CorePath:           core,
		RuntimeProfilePath: filepath.Join(dir, "localclash-runtime.json"),
	})

	if state.Core.Version != "Mihomo live smart" || !state.Core.SmartSupported {
		t.Fatalf("core = %+v, want live smart version", state.Core)
	}
	if got := readCount(t, countPath); got != 1 {
		t.Fatalf("core -v count = %d, want 1", got)
	}
	cached, ok := readCoreVersionCache(CoreVersionCachePath(filepath.Join(dir, ".runtime")), core)
	if !ok || cached.Version != "Mihomo live smart" {
		t.Fatalf("cached = %+v ok=%v, want live version", cached, ok)
	}
}

func TestBootstrapReplacesCoreVersionCacheWhenCorePathChanges(t *testing.T) {
	dir := t.TempDir()
	oldCore := filepath.Join(dir, "bin", "old-mihomo")
	core := filepath.Join(dir, "bin", "mihomo")
	countPath := filepath.Join(dir, "count")
	writeAppinitFile(t, oldCore, "#!/bin/sh\nexit 0\n", 0o755)
	writeCountingCore(t, core, countPath, "Mihomo replacement")
	if err := writeCoreVersionCache(CoreVersionCachePath(filepath.Join(dir, ".runtime")), CoreState{Path: oldCore, Exists: true, Version: "Mihomo old", SmartSupported: false}, fixedAppinitCacheNow()); err != nil {
		t.Fatal(err)
	}

	state := Bootstrap(context.Background(), Options{
		RuntimeRoot:        filepath.Join(dir, ".runtime"),
		RuleSourcesDir:     filepath.Join(dir, "missing-rule-sources"),
		RulesCacheDir:      filepath.Join(dir, ".runtime", "rules", "packs"),
		GeneratedConfig:    filepath.Join(dir, "generated", "mihomo.yaml"),
		SubscriptionPath:   filepath.Join(dir, "subscription.gob"),
		MihomoRuntimeDir:   filepath.Join(dir, ".runtime", "mihomo"),
		CorePath:           core,
		RuntimeProfilePath: filepath.Join(dir, "localclash-runtime.json"),
	})

	if state.Core.Version != "Mihomo replacement" {
		t.Fatalf("core = %+v, want replacement version", state.Core)
	}
	if got := readCount(t, countPath); got != 1 {
		t.Fatalf("core -v count = %d, want 1", got)
	}
	cached, ok := readCoreVersionCache(CoreVersionCachePath(filepath.Join(dir, ".runtime")), core)
	if !ok || cached.Path != core || cached.Version != "Mihomo replacement" {
		t.Fatalf("cached = %+v ok=%v, want replacement core", cached, ok)
	}
}

func TestBootstrapFallsBackWhenCoreVersionCacheIsCorrupt(t *testing.T) {
	dir := t.TempDir()
	countPath := filepath.Join(dir, "count")
	core := filepath.Join(dir, "bin", "mihomo")
	writeCountingCore(t, core, countPath, "Mihomo after corrupt cache")
	cachePath := CoreVersionCachePath(filepath.Join(dir, ".runtime"))
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cachePath, []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}

	state := Bootstrap(context.Background(), Options{
		RuntimeRoot:        filepath.Join(dir, ".runtime"),
		RuleSourcesDir:     filepath.Join(dir, "missing-rule-sources"),
		RulesCacheDir:      filepath.Join(dir, ".runtime", "rules", "packs"),
		GeneratedConfig:    filepath.Join(dir, "generated", "mihomo.yaml"),
		SubscriptionPath:   filepath.Join(dir, "subscription.gob"),
		MihomoRuntimeDir:   filepath.Join(dir, ".runtime", "mihomo"),
		CorePath:           core,
		RuntimeProfilePath: filepath.Join(dir, "localclash-runtime.json"),
	})

	if state.Core.Version != "Mihomo after corrupt cache" {
		t.Fatalf("core = %+v, want live version after corrupt cache", state.Core)
	}
	if got := readCount(t, countPath); got != 1 {
		t.Fatalf("core -v count = %d, want 1", got)
	}
}

func writeAppinitFile(t *testing.T, path, content string, mode os.FileMode) {
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
		file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
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
	if err := os.WriteFile(path, data, mode); err != nil {
		t.Fatal(err)
	}
}

func writeCountingCore(t *testing.T, path, countPath, version string) {
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
		"exit 0\n"
	writeAppinitFile(t, path, script, 0o755)
}

func readCount(t *testing.T, path string) int {
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

func fixedAppinitCacheNow() time.Time {
	return time.Date(2026, 5, 28, 9, 0, 0, 0, time.UTC)
}

func writeAppinitPackIndex(t *testing.T, cacheDir string) {
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
