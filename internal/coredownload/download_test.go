package coredownload

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestSmartDownloadSelectsForkRelease(t *testing.T) {
	for _, tt := range []struct {
		arch string
		want string
	}{
		{arch: "x86_64", want: "amd64-v1"},
		{arch: "aarch64", want: "arm64"},
		{arch: "mips", want: "mips-softfloat"},
		{arch: "mipsle", want: "mipsle-softfloat"},
		{arch: "unsupported"},
	} {
		t.Run(tt.arch, func(t *testing.T) {
			const base = "https://github.com/qoli/mihomo-Alpha/releases/download/Prerelease-Alpha/"
			rel := release{TagName: "Prerelease-Alpha"}
			for _, target := range []string{"amd64-v3", "amd64-v1-go123", "amd64-v1", "arm64", "mips-hardfloat", "mips-softfloat", "mipsle-hardfloat", "mipsle-softfloat"} {
				name := "mihomo-linux-" + target + "-alpha-smart-02c0eaf.gz"
				rel.Assets = append(rel.Assets, asset{Name: name, BrowserDownloadURL: base + name})
			}
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				if r.URL.Path != "/repos/qoli/mihomo-Alpha/releases/tags/Prerelease-Alpha" {
					t.Errorf("unexpected metadata path: %s", r.URL.Path)
				}
				json.NewEncoder(w).Encode(rel)
			}))
			defer server.Close()
			t.Setenv("LOCALCLASH_GITHUB_MIRROR", "")
			t.Setenv("LOCALCLASH_GITHUB_API_MIRRORS", server.URL)
			results, err := Download(context.Background(), Options{
				Flavor: FlavorSmart, Target: TargetRouter, TargetArch: tt.arch, DryRun: true,
				Repo: "example/meta", Version: "v1", SmartBranch: "other",
			})
			if requests != 1 {
				t.Fatalf("metadata requests = %d, want 1", requests)
			}
			if tt.want == "" {
				if err == nil || !strings.Contains(err.Error(), "no matching mihomo release asset") {
					t.Fatalf("expected missing asset error, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			wantName := "mihomo-linux-" + tt.want + "-alpha-smart-02c0eaf.gz"
			if len(results) != 1 || results[0].DownloadURL != base+wantName || results[0].Version != rel.TagName {
				t.Fatalf("unexpected Smart result: %+v", results)
			}
		})
	}
}

func TestSelectAssetPrefersDefaultVariant(t *testing.T) {
	assets := []asset{
		{Name: "mihomo-darwin-arm64-go124-v1.19.24.gz"},
		{Name: "mihomo-darwin-arm64-v1.19.24.gz", BrowserDownloadURL: "https://example.test/darwin-arm64"},
	}

	got, err := selectAsset(assets, "darwin", "arm64")
	if err != nil {
		t.Fatal(err)
	}
	if got.BrowserDownloadURL != "https://example.test/darwin-arm64" {
		t.Fatalf("selected %q, want default darwin arm64 asset", got.Name)
	}
}

func TestSelectAssetUsesConservativeLinuxAMD64V1(t *testing.T) {
	assets := []asset{
		{Name: "mihomo-linux-amd64-v3-v1.19.25.gz", BrowserDownloadURL: "https://example.test/linux-amd64-v3"},
		{Name: "mihomo-linux-amd64-v1.19.25.gz", BrowserDownloadURL: "https://example.test/linux-amd64"},
		{Name: "mihomo-linux-amd64-compatible-v1.19.25.gz", BrowserDownloadURL: "https://example.test/linux-amd64-compatible"},
		{Name: "mihomo-linux-amd64-v1-v1.19.25.gz", BrowserDownloadURL: "https://example.test/linux-amd64-v1"},
	}

	got, err := selectAsset(assets, "linux", "amd64")
	if err != nil {
		t.Fatal(err)
	}
	if got.BrowserDownloadURL != "https://example.test/linux-amd64-v1" {
		t.Fatalf("selected %q, want conservative linux amd64-v1 asset", got.Name)
	}
}

func TestSelectAssetNormalizesX8664ToLinuxAMD64V1(t *testing.T) {
	assets := []asset{
		{Name: "mihomo-linux-amd64-v3-v1.19.25.gz", BrowserDownloadURL: "https://example.test/linux-amd64-v3"},
		{Name: "mihomo-linux-amd64-v1-v1.19.25.gz", BrowserDownloadURL: "https://example.test/linux-amd64-v1"},
	}

	got, err := selectAssetForTarget(assets, "linux", "x86_64")
	if err != nil {
		t.Fatal(err)
	}
	if got.Target != "amd64-v1" {
		t.Fatalf("target = %q, want amd64-v1", got.Target)
	}
	if got.DetectedArch != "x86_64" {
		t.Fatalf("detected arch = %q, want x86_64", got.DetectedArch)
	}
	if got.Asset.BrowserDownloadURL != "https://example.test/linux-amd64-v1" {
		t.Fatalf("selected %q, want linux amd64-v1 asset", got.Asset.Name)
	}
}

func TestSelectAssetRequiresExactLinuxAMD64V1Token(t *testing.T) {
	assets := []asset{
		{Name: "mihomo-linux-amd64-v1.19.25.gz", BrowserDownloadURL: "https://example.test/linux-amd64"},
		{Name: "mihomo-linux-amd64-compatible-v1.19.25.gz", BrowserDownloadURL: "https://example.test/linux-amd64-compatible"},
		{Name: "mihomo-linux-amd64-v2-v1.19.25.gz", BrowserDownloadURL: "https://example.test/linux-amd64-v2"},
		{Name: "mihomo-linux-amd64-v3-v1.19.25.gz", BrowserDownloadURL: "https://example.test/linux-amd64-v3"},
	}

	_, err := selectAsset(assets, "linux", "amd64")
	if err == nil {
		t.Fatal("expected missing amd64-v1 asset to fail")
	}
	if !strings.Contains(err.Error(), `exact target "amd64-v1"`) {
		t.Fatalf("error = %q, want exact target amd64-v1", err)
	}
}

func TestSelectAssetChoosesLinuxAMD64V1WhenV3AppearsFirst(t *testing.T) {
	assets := []asset{
		{Name: "mihomo-linux-amd64-v3-v1.19.25.gz", BrowserDownloadURL: "https://example.test/linux-amd64-v3"},
		{Name: "mihomo-linux-amd64-v1-v1.19.25.gz", BrowserDownloadURL: "https://example.test/linux-amd64-v1"},
	}

	got, err := selectAsset(assets, "linux", "amd64")
	if err != nil {
		t.Fatal(err)
	}
	if got.BrowserDownloadURL != "https://example.test/linux-amd64-v1" {
		t.Fatalf("selected %q, want linux amd64-v1 asset", got.Name)
	}
}

func TestSelectAssetNormalizesAarch64(t *testing.T) {
	assets := []asset{
		{Name: "mihomo-linux-arm64-v1.19.24.gz", BrowserDownloadURL: "https://example.test/linux-arm64"},
	}

	got, err := selectAsset(assets, "linux", "aarch64")
	if err != nil {
		t.Fatal(err)
	}
	if got.BrowserDownloadURL != "https://example.test/linux-arm64" {
		t.Fatalf("selected %q, want linux arm64 asset", got.Name)
	}
}

func TestSelectAssetKeepsNonX86LinuxTargets(t *testing.T) {
	tests := []struct {
		name       string
		targetArch string
		assetName  string
	}{
		{name: "arm64", targetArch: "arm64", assetName: "mihomo-linux-arm64-v1.19.25.gz"},
		{name: "mips64", targetArch: "mips64", assetName: "mihomo-linux-mips64-v1.19.25.gz"},
		{name: "riscv64", targetArch: "riscv64", assetName: "mihomo-linux-riscv64-v1.19.25.gz"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := selectAssetForTarget([]asset{{Name: tt.assetName, BrowserDownloadURL: "https://example.test/" + tt.targetArch}}, "linux", tt.targetArch)
			if err != nil {
				t.Fatal(err)
			}
			if got.Target != tt.targetArch {
				t.Fatalf("target = %q, want %q", got.Target, tt.targetArch)
			}
			if got.Asset.Name != tt.assetName {
				t.Fatalf("selected %q, want %q", got.Asset.Name, tt.assetName)
			}
		})
	}
}

func TestNormalizeOptionsUsesCurrentPlatformDefaults(t *testing.T) {
	got := normalizeOptions(Options{})
	if got.TargetOS == "" {
		t.Fatal("TargetOS should default to runtime OS")
	}
	if got.TargetArch == "" {
		t.Fatal("TargetArch should default to runtime arch")
	}
	if got.OutputDir != "bin" {
		t.Fatalf("OutputDir = %q, want bin", got.OutputDir)
	}
	if got.Flavor != FlavorAll {
		t.Fatalf("Flavor = %q, want all", got.Flavor)
	}
	if got.Target != TargetHost {
		t.Fatalf("Target = %q, want host", got.Target)
	}
}

func TestNormalizeOptionsUsesWindowsExeDefault(t *testing.T) {
	got := normalizeOptions(Options{TargetOS: "Windows", TargetArch: "amd64", Flavor: FlavorMeta})
	want := filepath.Join("bin", "windows-amd64", "lc-mihomo-meta.exe")
	if path := outputPath(got, FlavorMeta); path != want {
		t.Fatalf("OutputPath = %q, want %q", path, want)
	}
}

func TestOutputPathUsesPlatformFlavorNames(t *testing.T) {
	opts := normalizeOptions(Options{TargetOS: "linux", TargetArch: "arm64"})
	if got := outputPath(opts, FlavorMeta); got != filepath.Join("bin", "linux-arm64", "lc-mihomo-meta") {
		t.Fatalf("meta output path = %q", got)
	}
	if got := outputPath(opts, FlavorSmart); got != filepath.Join("bin", "linux-arm64", "lc-mihomo-smart") {
		t.Fatalf("smart output path = %q", got)
	}
}

func TestEffectiveFlavorsDefaultsHostToMetaOnly(t *testing.T) {
	opts := normalizeOptions(Options{Target: TargetHost, TargetOS: "darwin", TargetArch: "arm64"})
	got := effectiveFlavors(opts)
	if len(got) != 1 || got[0] != FlavorMeta {
		t.Fatalf("flavors = %+v, want host meta only", got)
	}
}

func TestEffectiveFlavorsDefaultsRouterToMetaAndSmart(t *testing.T) {
	opts := normalizeOptions(Options{Target: TargetRouter, TargetArch: "arm64"})
	got := effectiveFlavors(opts)
	if opts.TargetOS != "linux" {
		t.Fatalf("router OS = %q, want linux", opts.TargetOS)
	}
	if len(got) != 2 || got[0] != FlavorMeta || got[1] != FlavorSmart {
		t.Fatalf("flavors = %+v, want router meta and smart", got)
	}
}

func TestOpenClashCoreAssetNameRejectsNonLinuxSmart(t *testing.T) {
	if _, err := openClashCoreAssetName("darwin", "arm64"); err == nil {
		t.Fatal("expected non-linux smart core error")
	}
}

func TestOpenClashCoreAssetNameUsesLinuxArm64(t *testing.T) {
	got, err := openClashCoreAssetName("linux", "aarch64")
	if err != nil {
		t.Fatal(err)
	}
	if got != "clash-linux-arm64.tar.gz" {
		t.Fatalf("asset = %q, want clash-linux-arm64.tar.gz", got)
	}
}

func TestRouterMetaLinuxAMD64UsesGitHubReleaseSelection(t *testing.T) {
	opts := normalizeOptions(Options{Target: TargetRouter, TargetArch: "x86_64", Flavor: FlavorMeta, Version: "latest", Repo: "MetaCubeX/mihomo"})
	if shouldUseOpenClashMeta(opts) {
		t.Fatal("router meta linux amd64 should use exact GitHub amd64-v1 release selection, not OpenClash amd64 asset")
	}
}

func TestRouterMetaNonX86KeepsOpenClashSelection(t *testing.T) {
	opts := normalizeOptions(Options{Target: TargetRouter, TargetArch: "arm64", Flavor: FlavorMeta, Version: "latest", Repo: "MetaCubeX/mihomo"})
	if !shouldUseOpenClashMeta(opts) {
		t.Fatal("router meta non-x86 target should keep existing OpenClash selection")
	}
}

func TestValidateRejectsHostSmartOnNonLinux(t *testing.T) {
	opts := normalizeOptions(Options{Flavor: FlavorSmart, TargetOS: "darwin", TargetArch: "arm64"})
	if err := opts.validate(); err == nil {
		t.Fatal("expected non-linux smart validation error")
	}
}

func TestDefaultHostOutputPathIncludesCurrentPlatform(t *testing.T) {
	opts := normalizeOptions(Options{})
	want := filepath.Join("bin", runtime.GOOS+"-"+runtime.GOARCH, "lc-mihomo-meta")
	if runtime.GOOS == "windows" {
		want += ".exe"
	}
	if got := outputPath(opts, FlavorMeta); got != want {
		t.Fatalf("host output path = %q, want %q", got, want)
	}
}

func TestDownloadCandidatesUsesDefaultGitHubReleaseMirrors(t *testing.T) {
	t.Setenv("LOCALCLASH_GITHUB_RELEASE_MIRRORS", "")
	t.Setenv("LOCALCLASH_GITHUB_MIRROR", "")

	got := downloadCandidates("https://github.com/MetaCubeX/mihomo/releases/download/v1/mihomo.gz")
	want := []string{
		"https://gh-proxy.com/https://github.com/MetaCubeX/mihomo/releases/download/v1/mihomo.gz",
		"https://ghproxy.imciel.com/https://github.com/MetaCubeX/mihomo/releases/download/v1/mihomo.gz",
		"https://gitproxy.mrhjx.cn/https://github.com/MetaCubeX/mihomo/releases/download/v1/mihomo.gz",
		"https://gh.jasonzeng.dev/https://github.com/MetaCubeX/mihomo/releases/download/v1/mihomo.gz",
		"https://gh.monlor.com/https://github.com/MetaCubeX/mihomo/releases/download/v1/mihomo.gz",
		"https://gh.noki.icu/https://github.com/MetaCubeX/mihomo/releases/download/v1/mihomo.gz",
		"https://ghfast.top/https://github.com/MetaCubeX/mihomo/releases/download/v1/mihomo.gz",
		"https://github.com/MetaCubeX/mihomo/releases/download/v1/mihomo.gz",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("candidates = %#v, want %#v", got, want)
	}
	assertNoOldDefaultMirrors(t, got)
}

func TestDownloadCandidatesUsesDefaultGitHubAPIMirrors(t *testing.T) {
	t.Setenv("LOCALCLASH_GITHUB_API_MIRRORS", "")
	t.Setenv("LOCALCLASH_GITHUB_MIRROR", "")

	got := downloadCandidates("https://api.github.com/repos/MetaCubeX/mihomo/releases/latest")
	want := []string{
		"https://gh-proxy.com/https://api.github.com/repos/MetaCubeX/mihomo/releases/latest",
		"https://ghproxy.imciel.com/https://api.github.com/repos/MetaCubeX/mihomo/releases/latest",
		"https://gitproxy.mrhjx.cn/https://api.github.com/repos/MetaCubeX/mihomo/releases/latest",
		"https://gh.jasonzeng.dev/https://api.github.com/repos/MetaCubeX/mihomo/releases/latest",
		"https://gh.monlor.com/https://api.github.com/repos/MetaCubeX/mihomo/releases/latest",
		"https://gh.noki.icu/https://api.github.com/repos/MetaCubeX/mihomo/releases/latest",
		"https://ghfast.top/https://api.github.com/repos/MetaCubeX/mihomo/releases/latest",
		"https://api.github.com/repos/MetaCubeX/mihomo/releases/latest",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("candidates = %#v, want %#v", got, want)
	}
	assertNoOldDefaultMirrors(t, got)
}

func TestDownloadCandidatesMirrorsGitHubReleaseBeforeDirect(t *testing.T) {
	t.Setenv("LOCALCLASH_GITHUB_RELEASE_MIRRORS", "https://mirror.example/https://github.com")
	t.Setenv("LOCALCLASH_GITHUB_MIRROR", "")

	got := downloadCandidates("https://github.com/MetaCubeX/mihomo/releases/download/v1/mihomo.gz")
	want := []string{
		"https://mirror.example/https://github.com/MetaCubeX/mihomo/releases/download/v1/mihomo.gz",
		"https://github.com/MetaCubeX/mihomo/releases/download/v1/mihomo.gz",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("candidates = %#v, want %#v", got, want)
	}
}

func TestDownloadCandidatesCanDisableMirrors(t *testing.T) {
	t.Setenv("LOCALCLASH_GITHUB_RELEASE_MIRRORS", "https://mirror.example/https://github.com")
	t.Setenv("LOCALCLASH_GITHUB_MIRROR", "direct")

	got := downloadCandidates("https://github.com/MetaCubeX/mihomo/releases/download/v1/mihomo.gz")
	want := []string{"https://github.com/MetaCubeX/mihomo/releases/download/v1/mihomo.gz"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("candidates = %#v, want %#v", got, want)
	}
}

func TestRawMirrorCandidatesUseDefaultMirrorsAndJsdelivr(t *testing.T) {
	t.Setenv("LOCALCLASH_GITHUB_RAW_MIRRORS", "")
	t.Setenv("LOCALCLASH_GITHUB_MIRROR", "")

	got := downloadCandidates("https://raw.githubusercontent.com/vernesong/OpenClash/core/master/core_version")
	want := []string{
		"https://gh-proxy.com/https://raw.githubusercontent.com/vernesong/OpenClash/core/master/core_version",
		"https://ghproxy.imciel.com/https://raw.githubusercontent.com/vernesong/OpenClash/core/master/core_version",
		"https://gitproxy.mrhjx.cn/https://raw.githubusercontent.com/vernesong/OpenClash/core/master/core_version",
		"https://gh.jasonzeng.dev/https://raw.githubusercontent.com/vernesong/OpenClash/core/master/core_version",
		"https://gh.monlor.com/https://raw.githubusercontent.com/vernesong/OpenClash/core/master/core_version",
		"https://gh.noki.icu/https://raw.githubusercontent.com/vernesong/OpenClash/core/master/core_version",
		"https://ghfast.top/https://raw.githubusercontent.com/vernesong/OpenClash/core/master/core_version",
		"https://fastly.jsdelivr.net/gh/vernesong/OpenClash@core/master/core_version",
		"https://raw.githubusercontent.com/vernesong/OpenClash/core/master/core_version",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("candidates = %#v, want %#v", got, want)
	}
	assertNoOldDefaultMirrors(t, got)
}

func TestRawMirrorCandidatesIncludeJsdelivrShape(t *testing.T) {
	t.Setenv("LOCALCLASH_GITHUB_RAW_MIRRORS", "https://fastly.jsdelivr.net/gh")
	t.Setenv("LOCALCLASH_GITHUB_MIRROR", "")

	got := downloadCandidates("https://raw.githubusercontent.com/vernesong/OpenClash/core/master/core_version")
	want := []string{
		"https://fastly.jsdelivr.net/gh/vernesong/OpenClash@core/master/core_version",
		"https://raw.githubusercontent.com/vernesong/OpenClash/core/master/core_version",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("candidates = %#v, want %#v", got, want)
	}
}

func assertNoOldDefaultMirrors(t *testing.T, candidates []string) {
	t.Helper()
	for _, candidate := range candidates {
		for _, oldMirror := range []string{"gh.llkk.cc", "v1.ax", "ghp.xptvhelper.link"} {
			if strings.Contains(candidate, oldMirror) {
				t.Fatalf("candidate %q contains old mirror %q", candidate, oldMirror)
			}
		}
	}
}
