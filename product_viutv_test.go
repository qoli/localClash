package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"localclash/internal/configpatch"
	"localclash/internal/customsites"
	"localclash/internal/localconfig"
	"localclash/internal/policytemplate"
)

func TestRunProductDefaultTemplateAddsViuTVWithoutOrderCollision(t *testing.T) {
	templates, err := filepath.Abs(policytemplate.DefaultDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"fresh", "reapply_pre_viutv", "sync_pre_viutv"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("LOCALCLASH_WORKDIR", dir)
			t.Chdir(dir)
			templatesDir := filepath.Join(dir, policytemplate.DefaultDir)
			if err := os.CopyFS(templatesDir, os.DirFS(templates)); err != nil {
				t.Fatal(err)
			}
			manifestPath := filepath.Join(templatesDir, "localclash-default.json")
			currentManifest, err := os.ReadFile(manifestPath)
			if err != nil {
				t.Fatal(err)
			}
			input := filepath.Join(dir, "template-input.json")
			apply := func(refresh bool) {
				t.Helper()
				request := configInput{
					Version: 1, Template: policytemplate.TemplateLocalClashDefault,
					RuntimeProfile: "router", Core: "meta", RefreshPolicyTemplatePatches: refresh,
				}
				data, err := json.Marshal(request)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(input, data, 0o600); err != nil {
					t.Fatal(err)
				}
				output, err := captureStdoutAllowError(t, func() error {
					return run([]string{"config", "apply-template", "--input", input, "--json"})
				})
				if err != nil {
					t.Fatalf("formal template apply failed: %v; response=%s", err, output)
				}
				var result struct {
					OK bool `json:"ok"`
				}
				if err := json.Unmarshal([]byte(output), &result); err != nil || !result.OK {
					t.Fatalf("formal template response=%s decode error=%v", output, err)
				}
			}
			registryDir := filepath.Join(dir, configpatch.RegistryDirName)
			previousOrders := map[string]string{}
			if mode != "fresh" {
				// A synthetic pre-ViuTV inventory exercises the actual persisted
				// registry path; real old-release upgrade acceptance remains in QEMU.
				var manifest policytemplate.File
				if err := json.Unmarshal(currentManifest, &manifest); err != nil {
					t.Fatal(err)
				}
				var previous []policytemplate.PatchRef
				for _, patch := range manifest.Patches {
					if patch.ID != "default.viutv.v1" {
						previous = append(previous, patch)
					}
				}
				manifest.Patches = previous
				data, err := json.Marshal(manifest)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(manifestPath, data, 0o600); err != nil {
					t.Fatal(err)
				}
				apply(false)
				before, err := configpatch.Load(registryDir)
				if err != nil {
					t.Fatal(err)
				}
				if _, exists := before.ByID["default.viutv.v1"]; exists || len(before.Records) != len(previous) {
					t.Fatal("synthetic pre-ViuTV registry was not established")
				}
				for _, record := range before.Records {
					previousOrders[record.Patch.PatchID] = record.Patch.OrderID
				}
				if err := os.WriteFile(manifestPath, currentManifest, 0o600); err != nil {
					t.Fatal(err)
				}
			}

			sites := customsites.DefaultPaths(dir)
			pair := customsites.EmptyPair()
			for route, pattern := range map[string]string{customsites.RouteDirect: "keep.example.invalid", customsites.RouteProxy: "proxy.example.invalid"} {
				pair, _, err = customsites.Add(pair, route, pattern, time.Unix(1, 0).UTC())
				if err != nil {
					t.Fatal(err)
				}
			}
			if err := os.MkdirAll(filepath.Dir(sites.Proxy), 0o700); err != nil {
				t.Fatal(err)
			}
			beforeSiteFiles := map[string][]byte{}
			for path, doc := range map[string]customsites.Document{sites.Proxy: pair.Proxy, sites.Direct: pair.Direct} {
				data, err := customsites.MarshalDocument(doc)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, data, 0o600); err != nil {
					t.Fatal(err)
				}
				beforeSiteFiles[path] = data
			}
			beforeSites, err := customsites.SnapshotChecked(pair)
			if err != nil {
				t.Fatal(err)
			}
			apply(mode == "sync_pre_viutv")
			registry, err := configpatch.Load(registryDir)
			if err != nil {
				t.Fatal(err)
			}
			viu := registry.ByID["default.viutv.v1"].Patch
			if viu.OrderID != "0650.000000" || viu.Status != configpatch.StatusEnabled {
				t.Fatalf("imported ViuTV patch = %+v, want enabled at 0650.000000", viu)
			}
			for id, order := range previousOrders {
				if got := registry.ByID[id].Patch.OrderID; got != order {
					t.Fatalf("existing patch %s changed order from %s to %s", id, order, got)
				}
			}
			orders := map[string]string{}
			for _, record := range registry.Records {
				patch := record.Patch
				if prior, exists := orders[patch.OrderID]; exists {
					t.Fatalf("duplicate order %s for %s and %s", patch.OrderID, prior, patch.PatchID)
				}
				orders[patch.OrderID] = patch.PatchID
			}
			if platform := registry.ByID["default.platform-media.v1"].Patch.OrderID; platform >= viu.OrderID {
				t.Fatalf("ViuTV order %s must follow platform media %s", viu.OrderID, platform)
			}
			for _, id := range []string{"default.games.v1", "default.cloudflare-geoip.v1", "default.tail-fallback.v1"} {
				if later := registry.ByID[id].Patch.OrderID; later <= viu.OrderID {
					t.Fatalf("ViuTV order %s must precede %s at %s", viu.OrderID, id, later)
				}
			}
			intent, err := localconfig.Load(filepath.Join(dir, "localclash-intent.json"))
			if err != nil {
				t.Fatal(err)
			}
			viuIndex, cloudflareIndex := -1, -1
			for index, rule := range intent.CustomRules {
				if rule.ID == "viutv-playback-hosts" {
					viuIndex = index
				}
				if rule.ID == "cloudflare-geoip" {
					cloudflareIndex = index
				}
			}
			if viuIndex < 0 || cloudflareIndex <= viuIndex || intent.Generated == nil || intent.Generated.RegistryHash != registry.Hash(policytemplate.TemplateLocalClashDefault) {
				t.Fatalf("compiled intent ordering/provenance invalid: ViuTV=%d Cloudflare=%d generated=%+v", viuIndex, cloudflareIndex, intent.Generated)
			}
			afterPair, err := customsites.Load(sites)
			if err != nil {
				t.Fatal(err)
			}
			afterSites, err := customsites.SnapshotChecked(afterPair)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(beforeSites, afterSites) {
				t.Fatalf("template import changed custom-site state: before=%+v after=%+v", beforeSites, afterSites)
			}
			for path, before := range beforeSiteFiles {
				after, err := os.ReadFile(path)
				if err != nil || !reflect.DeepEqual(before, after) {
					t.Fatalf("template import changed custom-site file %s: %v", path, err)
				}
			}
		})
	}
}
