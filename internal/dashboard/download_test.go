package dashboard

import (
	"archive/zip"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestSelectAsset(t *testing.T) {
	got, err := selectAsset([]asset{
		{Name: "dist-no-fonts.zip"},
		{Name: "dist.zip", BrowserDownloadURL: "https://example.test/dist.zip"},
	}, "dist.zip")
	if err != nil {
		t.Fatal(err)
	}
	if got.BrowserDownloadURL != "https://example.test/dist.zip" {
		t.Fatalf("selected asset = %+v", got)
	}
}

func TestExtractZipRejectsPathTraversal(t *testing.T) {
	zipPath := filepath.Join(t.TempDir(), "bad.zip")
	if err := writeZip(zipPath, map[string]string{"../escape.txt": "bad"}); err != nil {
		t.Fatal(err)
	}
	err := extractZip(zipPath, t.TempDir())
	if err == nil {
		t.Fatal("expected path traversal error")
	}
}

func TestExtractZipAndVerifyDashboard(t *testing.T) {
	zipPath := filepath.Join(t.TempDir(), "dash.zip")
	if err := writeZip(zipPath, map[string]string{
		"index.html":       "<html></html>",
		"assets/app.js":    "console.log(1)",
		"assets/style.css": "body{}",
	}); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "dashboard")
	if err := os.MkdirAll(output, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := extractZip(zipPath, output); err != nil {
		t.Fatal(err)
	}
	if err := verifyDashboard(output); err != nil {
		t.Fatal(err)
	}
}

func TestExtractZipStripsSingleTopLevelDirectory(t *testing.T) {
	zipPath := filepath.Join(t.TempDir(), "dash.zip")
	if err := writeZip(zipPath, map[string]string{
		"dist/index.html":    "<html></html>",
		"dist/assets/app.js": "console.log(1)",
	}); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "dashboard")
	if err := os.MkdirAll(output, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := extractZip(zipPath, output); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(output, "index.html")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(output, "dist", "index.html")); !os.IsNotExist(err) {
		t.Fatalf("expected dist prefix to be stripped, err=%v", err)
	}
}

func TestInstallDashboardZipKeepsExistingDashboardWhenReplacementIsInvalid(t *testing.T) {
	output := filepath.Join(t.TempDir(), "dashboard")
	if err := os.MkdirAll(output, 0o755); err != nil {
		t.Fatal(err)
	}
	oldIndex := filepath.Join(output, "index.html")
	if err := os.WriteFile(oldIndex, []byte("old dashboard"), 0o644); err != nil {
		t.Fatal(err)
	}
	zipPath := filepath.Join(t.TempDir(), "invalid.zip")
	if err := writeZip(zipPath, map[string]string{"assets/app.js": "broken"}); err != nil {
		t.Fatal(err)
	}

	err := installDashboardZip(zipPath, output, true)
	if err == nil || !strings.Contains(err.Error(), "index.html") {
		t.Fatalf("error = %v, want missing index.html", err)
	}
	content, readErr := os.ReadFile(oldIndex)
	if readErr != nil || string(content) != "old dashboard" {
		t.Fatalf("existing dashboard changed after failed replacement: content=%q error=%v", content, readErr)
	}
}

func TestInstallDashboardZipAtomicallyReplacesExistingDashboard(t *testing.T) {
	output := filepath.Join(t.TempDir(), "dashboard")
	if err := os.MkdirAll(output, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(output, "index.html"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	zipPath := filepath.Join(t.TempDir(), "valid.zip")
	if err := writeZip(zipPath, map[string]string{"index.html": "new", "assets/app.js": "ok"}); err != nil {
		t.Fatal(err)
	}

	if err := installDashboardZip(zipPath, output, true); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(output, "index.html"))
	if err != nil || string(content) != "new" {
		t.Fatalf("installed index = %q, error=%v", content, err)
	}
}

func TestDownloadCandidatesUsesDefaultGitHubReleaseMirrors(t *testing.T) {
	t.Setenv("LOCALCLASH_GITHUB_RELEASE_MIRRORS", "")
	t.Setenv("LOCALCLASH_GITHUB_MIRROR", "")

	got := downloadCandidates("https://github.com/Zephyruso/zashboard/releases/download/v1/dist.zip")
	want := []string{
		"https://gh-proxy.com/https://github.com/Zephyruso/zashboard/releases/download/v1/dist.zip",
		"https://ghproxy.imciel.com/https://github.com/Zephyruso/zashboard/releases/download/v1/dist.zip",
		"https://gitproxy.mrhjx.cn/https://github.com/Zephyruso/zashboard/releases/download/v1/dist.zip",
		"https://gh.jasonzeng.dev/https://github.com/Zephyruso/zashboard/releases/download/v1/dist.zip",
		"https://github.com/Zephyruso/zashboard/releases/download/v1/dist.zip",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("candidates = %#v, want %#v", got, want)
	}
	assertNoOldDefaultMirrors(t, got)
}

func TestDownloadCandidatesMirrorsGitHubBeforeDirect(t *testing.T) {
	t.Setenv("LOCALCLASH_GITHUB_RELEASE_MIRRORS", "https://mirror.example/https://github.com")
	t.Setenv("LOCALCLASH_GITHUB_MIRROR", "")

	got := downloadCandidates("https://github.com/Zephyruso/zashboard/releases/download/v1/dist.zip")
	want := []string{
		"https://mirror.example/https://github.com/Zephyruso/zashboard/releases/download/v1/dist.zip",
		"https://github.com/Zephyruso/zashboard/releases/download/v1/dist.zip",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("candidates = %#v, want %#v", got, want)
	}
}

func TestDownloadCandidatesCanDisableMirrors(t *testing.T) {
	t.Setenv("LOCALCLASH_GITHUB_RELEASE_MIRRORS", "https://mirror.example/https://github.com")
	t.Setenv("LOCALCLASH_GITHUB_MIRROR", "off")

	got := downloadCandidates("https://github.com/Zephyruso/zashboard/releases/download/v1/dist.zip")
	want := []string{"https://github.com/Zephyruso/zashboard/releases/download/v1/dist.zip"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("candidates = %#v, want %#v", got, want)
	}
}

func TestDownloadAssetDoesNotConvertCancellationToOptionalUnavailability(t *testing.T) {
	t.Setenv("LOCALCLASH_GITHUB_MIRROR", "off")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := downloadAsset(ctx, "https://github.com/Zephyruso/zashboard/releases/latest/download/dist.zip")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context cancellation", err)
	}
	if IsUnavailable(err) {
		t.Fatalf("cancellation was converted to optional unavailability: %v", err)
	}
}

func TestDownloadAssetURLDoesNotExposeRedirectCredentials(t *testing.T) {
	const secret = "signed-download-token"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://127.0.0.1:1/asset.zip?token="+secret, http.StatusFound)
	}))
	defer server.Close()

	_, err := downloadAssetURL(context.Background(), server.URL+"/download")
	if err == nil {
		t.Fatal("expected redirected download to fail")
	}
	if strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), "token=") {
		t.Fatalf("download error exposed redirect credentials: %v", err)
	}
}

func writeZip(path string, files map[string]string) error {
	out, err := os.Create(path)
	if err != nil {
		return err
	}
	zw := zip.NewWriter(out)
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			out.Close()
			return err
		}
		if _, err := w.Write([]byte(body)); err != nil {
			out.Close()
			return err
		}
	}
	if err := zw.Close(); err != nil {
		out.Close()
		return err
	}
	return out.Close()
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
