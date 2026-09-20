package baseassets

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

const (
	defaultManifestURL          = "https://github.com/qoli/localClash/releases/latest/download/localclash-release-manifest.json"
	defaultGitHubReleaseMirrors = "https://gh-proxy.com/https://github.com https://proxy.vvvv.ee/https://github.com https://cors.isteed.cc/https://github.com https://gh.ddlc.top/https://github.com https://gh.xmly.dev/https://github.com https://ghproxy.net/https://github.com https://ghfast.top/https://github.com"
)

type Options struct {
	ManifestURL string
	OutputDir   string
	Force       bool
}

type Result struct {
	Version      string `json:"version"`
	Filename     string `json:"filename"`
	URL          string `json:"url"`
	SHA256       string `json:"sha256"`
	OutputDir    string `json:"output_dir"`
	Extracted    int    `json:"extracted"`
	AlreadyReady bool   `json:"already_ready"`
}

type StatusResult struct {
	Installed               bool     `json:"installed"`
	Path                    string   `json:"path"`
	Missing                 []string `json:"missing,omitempty"`
	Default                 string   `json:"default_template,omitempty"`
	DefaultPatchCount       int      `json:"default_patch_count,omitempty"`
	DefaultPatchesInstalled bool     `json:"default_patches_installed,omitempty"`
	RuleSourceDir           string   `json:"rule_source_dir,omitempty"`
}

type manifest struct {
	Version   string    `json:"version"`
	BaseAsset baseAsset `json:"base_assets"`
}

type baseAsset struct {
	Filename string `json:"filename"`
	URL      string `json:"url"`
	SHA256   string `json:"sha256"`
	Size     int64  `json:"size"`
}

func Install(ctx context.Context, opts Options) (Result, error) {
	opts = normalizeOptions(opts)
	if err := validateOptions(opts); err != nil {
		return Result{}, err
	}
	status := Status(opts.OutputDir)
	if status.Installed && !opts.Force {
		return Result{OutputDir: opts.OutputDir, AlreadyReady: true}, nil
	}

	manifest, err := fetchManifest(ctx, opts.ManifestURL)
	if err != nil {
		return Result{}, err
	}
	if strings.TrimSpace(manifest.BaseAsset.URL) == "" || strings.TrimSpace(manifest.BaseAsset.SHA256) == "" {
		return Result{}, errors.New("release manifest does not include base_assets url and sha256")
	}

	tmp, err := os.CreateTemp("", "localclash-base-assets-*.tar.gz")
	if err != nil {
		return Result{}, err
	}
	tmpPath := tmp.Name()
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return Result{}, err
	}
	defer os.Remove(tmpPath)

	if err := downloadFile(ctx, manifest.BaseAsset.URL, tmpPath); err != nil {
		return Result{}, err
	}
	if err := verifySHA256(tmpPath, manifest.BaseAsset.SHA256); err != nil {
		return Result{}, err
	}
	if err := os.MkdirAll(opts.OutputDir, 0o755); err != nil {
		return Result{}, err
	}
	count, err := extractTarGz(tmpPath, opts.OutputDir)
	if err != nil {
		return Result{}, err
	}
	status = Status(opts.OutputDir)
	if !status.Installed {
		return Result{}, fmt.Errorf("base assets incomplete after extraction: missing %s", strings.Join(status.Missing, ", "))
	}
	return Result{
		Version:   manifest.Version,
		Filename:  manifest.BaseAsset.Filename,
		URL:       manifest.BaseAsset.URL,
		SHA256:    strings.ToLower(manifest.BaseAsset.SHA256),
		OutputDir: opts.OutputDir,
		Extracted: count,
	}, nil
}

func Status(outputDir string) StatusResult {
	outputDir = strings.TrimSpace(outputDir)
	if outputDir == "" {
		outputDir = "."
	}
	result := StatusResult{
		Path:          outputDir,
		Default:       filepath.Join(outputDir, "policy-templates", "localclash-default.json"),
		RuleSourceDir: filepath.Join(outputDir, "rule-sources"),
	}
	required := []string{
		result.Default,
		filepath.Join(outputDir, ".runtime", "mihomo", "Country.mmdb"),
		filepath.Join(outputDir, ".runtime", "mihomo", "geoip.dat"),
		filepath.Join(outputDir, ".runtime", "mihomo", "geosite.dat"),
		filepath.Join(outputDir, ".runtime", "mihomo", "ASN.mmdb"),
	}
	for _, path := range required {
		if !regularFile(path) {
			result.Missing = append(result.Missing, rel(outputDir, path))
		}
	}
	if !hasJSONFile(result.RuleSourceDir) {
		result.Missing = append(result.Missing, "rule-sources/*.json")
	}
	defaultPatchesInstalled := true
	patches := defaultPatchPaths(result.Default)
	result.DefaultPatchCount = len(patches)
	for _, patch := range patches {
		path := filepath.Join(outputDir, "policy-templates", patch)
		if !regularFile(path) {
			defaultPatchesInstalled = false
			result.Missing = append(result.Missing, rel(outputDir, path))
		}
	}
	result.Installed = len(result.Missing) == 0
	result.DefaultPatchesInstalled = result.Installed && result.DefaultPatchCount > 0 && defaultPatchesInstalled
	return result
}

func normalizeOptions(opts Options) Options {
	opts.ManifestURL = strings.TrimSpace(opts.ManifestURL)
	opts.OutputDir = strings.TrimSpace(opts.OutputDir)
	if opts.ManifestURL == "" {
		opts.ManifestURL = strings.TrimSpace(os.Getenv("LOCALCLASH_RELEASE_MANIFEST"))
	}
	if opts.ManifestURL == "" {
		opts.ManifestURL = defaultManifestURL
	}
	if opts.OutputDir == "" {
		opts.OutputDir = "."
	}
	return opts
}

func validateOptions(opts Options) error {
	clean := filepath.Clean(opts.OutputDir)
	if clean == string(filepath.Separator) {
		return errors.New("output directory is too broad")
	}
	return nil
}

func fetchManifest(ctx context.Context, url string) (manifest, error) {
	var lastErr error
	for _, candidate := range downloadCandidates(url) {
		var out manifest
		err := fetchJSON(ctx, candidate, &out)
		if err == nil {
			return out, nil
		}
		lastErr = err
		fmt.Fprintf(os.Stderr, "download: base assets manifest failed from %s: %v\n", candidate, err)
	}
	return manifest{}, lastErr
}

func fetchJSON(ctx context.Context, url string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "localclash-base-assets")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("manifest request failed: %s: %s", resp.Status, shortHTTPBody(body))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func downloadFile(ctx context.Context, url, output string) error {
	var lastErr error
	for _, candidate := range downloadCandidates(url) {
		err := downloadURL(ctx, candidate, output)
		if err == nil {
			return nil
		}
		lastErr = err
		fmt.Fprintf(os.Stderr, "download: base assets archive failed from %s: %v\n", candidate, err)
		_ = os.Remove(output)
	}
	return lastErr
}

func downloadURL(ctx context.Context, url, output string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "localclash-base-assets")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("download failed: %s", resp.Status)
	}
	out, err := os.OpenFile(output, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, resp.Body)
	return err
}

func verifySHA256(path, expected string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return err
	}
	got := hex.EncodeToString(hash.Sum(nil))
	expected = strings.ToLower(strings.TrimSpace(expected))
	if got != expected {
		return fmt.Errorf("sha256 mismatch: expected %s, got %s", expected, got)
	}
	return nil
}

func extractTarGz(path, outputDir string) (int, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer file.Close()
	gz, err := gzip.NewReader(file)
	if err != nil {
		return 0, err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	count := 0
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return count, nil
		}
		if err != nil {
			return count, err
		}
		target, err := safeJoin(outputDir, header.Name)
		if err != nil {
			return count, err
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return count, err
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return count, err
			}
			mode := header.FileInfo().Mode().Perm()
			if mode == 0 {
				mode = 0o644
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
			if err != nil {
				return count, err
			}
			_, copyErr := io.Copy(out, tr)
			closeErr := out.Close()
			if copyErr != nil {
				return count, copyErr
			}
			if closeErr != nil {
				return count, closeErr
			}
			count++
		}
	}
}

func safeJoin(base, name string) (string, error) {
	cleanName := filepath.Clean(name)
	if cleanName == "." || strings.HasPrefix(cleanName, ".."+string(filepath.Separator)) || filepath.IsAbs(cleanName) {
		return "", fmt.Errorf("unsafe archive path %q", name)
	}
	target := filepath.Join(base, cleanName)
	relPath, err := filepath.Rel(base, target)
	if err != nil {
		return "", err
	}
	if relPath == ".." || strings.HasPrefix(relPath, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("unsafe archive path %q", name)
	}
	return target, nil
}

func downloadCandidates(url string) []string {
	if mirrorModeDisabled() {
		return []string{url}
	}
	candidates := append(mirroredURLs(url), url)
	return uniqueStrings(candidates)
}

func mirrorModeDisabled() bool {
	mode := strings.ToLower(strings.TrimSpace(os.Getenv("LOCALCLASH_GITHUB_MIRROR")))
	return mode == "off" || mode == "none" || mode == "direct"
}

func mirroredURLs(url string) []string {
	if !strings.HasPrefix(url, "https://github.com/") {
		return nil
	}
	return mirrorByPrefix(url, "https://github.com", envWords("LOCALCLASH_GITHUB_RELEASE_MIRRORS", defaultGitHubReleaseMirrors))
}

func mirrorByPrefix(url, upstreamPrefix string, mirrors []string) []string {
	out := make([]string, 0, len(mirrors))
	suffix := strings.TrimPrefix(url, upstreamPrefix)
	for _, mirror := range mirrors {
		mirror = strings.TrimRight(strings.TrimSpace(mirror), "/")
		if mirror == "" {
			continue
		}
		out = append(out, mirror+suffix)
	}
	return out
}

func envWords(name, fallback string) []string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		value = fallback
	}
	return strings.Fields(value)
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func regularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func hasJSONFile(dir string) bool {
	matches, err := filepath.Glob(filepath.Join(dir, "*.json"))
	return err == nil && len(matches) > 0
}

func defaultPatchPaths(path string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var doc struct {
		Patches []struct {
			Path string `json:"path"`
		} `json:"patches"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil
	}
	out := make([]string, 0, len(doc.Patches))
	for _, patch := range doc.Patches {
		p := strings.TrimSpace(patch.Path)
		if p == "" || filepath.IsAbs(p) || strings.Contains(p, "..") {
			continue
		}
		out = append(out, p)
	}
	return out
}

func rel(base, path string) string {
	relPath, err := filepath.Rel(base, path)
	if err != nil {
		return path
	}
	return relPath
}

func shortHTTPBody(body []byte) string {
	text := strings.Join(strings.Fields(strings.TrimSpace(string(body))), " ")
	if len(text) > 240 {
		return text[:240] + "..."
	}
	return text
}
