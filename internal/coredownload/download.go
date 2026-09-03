package coredownload

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"localclash/internal/runtimeprofile"
)

type Options struct {
	Version     string
	Target      string
	TargetOS    string
	TargetArch  string
	OutputPath  string
	OutputDir   string
	Repo        string
	SmartBranch string
	Flavor      string
	Force       bool
	DryRun      bool
}

type Result struct {
	Version     string
	AssetName   string
	DownloadURL string
	OutputPath  string
	Flavor      string
	Target      string
}

type release struct {
	TagName string  `json:"tag_name"`
	Assets  []asset `json:"assets"`
}

type asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

type assetSelection struct {
	Asset        asset
	Target       string
	CPULevel     string
	Reason       string
	DetectedOS   string
	DetectedArch string
}

const (
	FlavorAll    = "all"
	FlavorMeta   = "meta"
	FlavorSmart  = "smart"
	TargetHost   = "host"
	TargetRouter = "router"
)

const (
	defaultGitHubReleaseMirrors = "https://gh-proxy.com/https://github.com https://ghproxy.imciel.com/https://github.com https://gitproxy.mrhjx.cn/https://github.com https://gh.jasonzeng.dev/https://github.com https://gh.monlor.com/https://github.com https://gh.noki.icu/https://github.com https://ghfast.top/https://github.com"
	defaultGitHubRawMirrors     = "https://gh-proxy.com/https://raw.githubusercontent.com https://ghproxy.imciel.com/https://raw.githubusercontent.com https://gitproxy.mrhjx.cn/https://raw.githubusercontent.com https://gh.jasonzeng.dev/https://raw.githubusercontent.com https://gh.monlor.com/https://raw.githubusercontent.com https://gh.noki.icu/https://raw.githubusercontent.com https://ghfast.top/https://raw.githubusercontent.com https://fastly.jsdelivr.net/gh"
)

func Download(ctx context.Context, opts Options) ([]Result, error) {
	opts = normalizeOptions(opts)
	if err := opts.validate(); err != nil {
		return nil, err
	}
	flavors := effectiveFlavors(opts)
	results := make([]Result, 0, len(flavors))
	for _, flavor := range flavors {
		result, err := downloadFlavor(ctx, opts, flavor)
		if err != nil {
			return nil, err
		}
		results = append(results, result)
	}
	return results, nil
}

func downloadFlavor(ctx context.Context, opts Options, flavor string) (Result, error) {
	if flavor == FlavorMeta && shouldUseOpenClashMeta(opts) {
		result, err := downloadOpenClashCore(ctx, opts, FlavorMeta)
		if err == nil {
			return result, nil
		}
		fmt.Fprintf(os.Stderr, "download: openclash meta core failed, falling back to github release: %v\n", err)
	}
	repo, version, targetArch := opts.Repo, opts.Version, opts.TargetArch
	if flavor == FlavorSmart {
		// The fork publishes a rolling prerelease, which /releases/latest excludes.
		repo, version = "qoli/mihomo-Alpha", "Prerelease-Alpha"
		if targetArch == "mips" || targetArch == "mipsle" {
			targetArch += "-softfloat"
		}
	}
	rel, err := fetchRelease(ctx, repo, version)
	if err != nil {
		return Result{}, err
	}

	selected, err := selectAssetForTarget(rel.Assets, opts.TargetOS, targetArch)
	if err != nil {
		return Result{}, fmt.Errorf("%w for %s/%s in %s", err, opts.TargetOS, opts.TargetArch, rel.TagName)
	}
	logSelection(selected)

	result := Result{
		Version:     rel.TagName,
		AssetName:   selected.Asset.Name,
		DownloadURL: selected.Asset.BrowserDownloadURL,
		OutputPath:  outputPath(opts, flavor),
		Flavor:      flavor,
		Target:      opts.Target,
	}
	if opts.DryRun {
		return result, nil
	}

	if err := ensureWritableOutput(result.OutputPath, opts.Force); err != nil {
		return Result{}, err
	}
	if err := os.MkdirAll(filepath.Dir(result.OutputPath), 0o755); err != nil {
		return Result{}, err
	}

	tmpPath := result.OutputPath + ".download"
	defer os.Remove(tmpPath)

	if err := downloadAsset(ctx, selected.Asset.BrowserDownloadURL, selected.Asset.Name, tmpPath); err != nil {
		return Result{}, err
	}
	if err := os.Chmod(tmpPath, 0o755); err != nil {
		return Result{}, err
	}
	if err := os.Rename(tmpPath, result.OutputPath); err != nil {
		return Result{}, err
	}

	return result, nil
}

func shouldUseOpenClashMeta(opts Options) bool {
	return opts.Target == TargetRouter &&
		opts.TargetOS == "linux" &&
		normalizeArch(opts.TargetArch) != "amd64" &&
		opts.Version == "latest" &&
		opts.Repo == "MetaCubeX/mihomo"
}

func downloadOpenClashCore(ctx context.Context, opts Options, flavor string) (Result, error) {
	name, err := openClashCoreAssetName(opts.TargetOS, opts.TargetArch)
	if err != nil {
		return Result{}, err
	}
	version, err := fetchOpenClashCoreVersion(ctx, opts.SmartBranch, flavor)
	if err != nil {
		return Result{}, err
	}
	url := fmt.Sprintf("https://raw.githubusercontent.com/vernesong/OpenClash/core/%s/%s/%s", opts.SmartBranch, flavor, name)
	logSelection(openClashSelection(opts.TargetOS, opts.TargetArch, name))
	result := Result{
		Version:     version,
		AssetName:   name,
		DownloadURL: url,
		OutputPath:  outputPath(opts, flavor),
		Flavor:      flavor,
		Target:      opts.Target,
	}
	if opts.DryRun {
		return result, nil
	}
	if err := ensureWritableOutput(result.OutputPath, opts.Force); err != nil {
		return Result{}, err
	}
	if err := os.MkdirAll(filepath.Dir(result.OutputPath), 0o755); err != nil {
		return Result{}, err
	}
	tmpPath := result.OutputPath + ".download"
	defer os.Remove(tmpPath)
	if err := downloadAsset(ctx, url, name, tmpPath); err != nil {
		return Result{}, err
	}
	if err := os.Chmod(tmpPath, 0o755); err != nil {
		return Result{}, err
	}
	if err := os.Rename(tmpPath, result.OutputPath); err != nil {
		return Result{}, err
	}
	return result, nil
}

func normalizeOptions(opts Options) Options {
	if opts.Version == "" {
		opts.Version = "latest"
	}
	if opts.Target == "" {
		opts.Target = TargetHost
	}
	opts.Target = strings.ToLower(strings.TrimSpace(opts.Target))
	if opts.TargetOS == "" {
		if opts.Target == TargetRouter {
			opts.TargetOS = "linux"
		} else {
			opts.TargetOS = runtime.GOOS
		}
	}
	if opts.TargetArch == "" {
		opts.TargetArch = runtime.GOARCH
	}
	if opts.Flavor == "" {
		opts.Flavor = FlavorAll
	}
	opts.Flavor = strings.ToLower(strings.TrimSpace(opts.Flavor))
	opts.TargetOS = strings.ToLower(opts.TargetOS)
	opts.TargetArch = normalizeArch(opts.TargetArch)
	if opts.OutputDir == "" {
		opts.OutputDir = "bin"
	}
	if opts.Repo == "" {
		opts.Repo = "MetaCubeX/mihomo"
	}
	if opts.SmartBranch == "" {
		opts.SmartBranch = "master"
	}
	return opts
}

func (opts Options) validate() error {
	if !strings.Contains(opts.Repo, "/") {
		return fmt.Errorf("repo must be owner/name, got %q", opts.Repo)
	}
	switch opts.Flavor {
	case FlavorAll, FlavorMeta, FlavorSmart:
	default:
		return fmt.Errorf("flavor must be %q, %q, or %q, got %q", FlavorAll, FlavorMeta, FlavorSmart, opts.Flavor)
	}
	switch opts.Target {
	case TargetHost, TargetRouter:
	default:
		return fmt.Errorf("target must be %q or %q, got %q", TargetHost, TargetRouter, opts.Target)
	}
	if opts.Target == TargetRouter && opts.TargetOS != "linux" {
		return fmt.Errorf("router target requires linux OS, got %s/%s", opts.TargetOS, opts.TargetArch)
	}
	if opts.Flavor == FlavorSmart && opts.TargetOS != "linux" {
		return fmt.Errorf("smart core is available only for linux/router targets, got %s/%s; use --target router --arch %s", opts.TargetOS, opts.TargetArch, opts.TargetArch)
	}
	if opts.Flavor == FlavorAll && strings.TrimSpace(opts.OutputPath) != "" {
		return errors.New("output path is only valid with --flavor meta or --flavor smart; use --output-dir with --flavor all")
	}
	if opts.Flavor != FlavorAll && opts.OutputPath == "." {
		return errors.New("output path must be a file path")
	}
	if opts.OutputDir == "." || strings.TrimSpace(opts.OutputDir) == "" {
		return errors.New("output dir must be a directory path")
	}
	return nil
}

func outputPath(opts Options, flavor string) string {
	if opts.OutputPath != "" && opts.Flavor != FlavorAll {
		return opts.OutputPath
	}
	name := managedCoreName(flavor)
	if opts.TargetOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(opts.OutputDir, opts.TargetOS+"-"+opts.TargetArch, name)
}

func managedCoreName(flavor string) string {
	switch flavor {
	case FlavorSmart:
		return runtimeprofile.ManagedSmartCoreName
	default:
		return runtimeprofile.ManagedMetaCoreName
	}
}

func effectiveFlavors(opts Options) []string {
	if opts.Flavor != FlavorAll {
		return []string{opts.Flavor}
	}
	if opts.Target == TargetRouter {
		return []string{FlavorMeta, FlavorSmart}
	}
	return []string{FlavorMeta}
}

func fetchRelease(ctx context.Context, repo, version string) (release, error) {
	endpoint := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", repo)
	if version != "latest" {
		endpoint = fmt.Sprintf("https://api.github.com/repos/%s/releases/tags/%s", repo, version)
	}

	var lastErr error
	for _, candidate := range downloadCandidates(endpoint) {
		rel, err := fetchReleaseURL(ctx, candidate)
		if err == nil {
			return rel, nil
		}
		lastErr = err
		fmt.Fprintf(os.Stderr, "download: release metadata failed from %s: %v\n", candidate, err)
	}
	return release{}, lastErr
}

func fetchReleaseURL(ctx context.Context, endpoint string) (release, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return release{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "localclash-core-downloader")
	fmt.Fprintf(os.Stderr, "download: requesting release metadata %s\n", endpoint)

	resp, err := httpClient().Do(req)
	if err != nil {
		return release{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return release{}, fmt.Errorf("github release request failed: %s: %s", resp.Status, shortHTTPBody(body))
	}

	var rel release
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return release{}, err
	}
	if rel.TagName == "" {
		return release{}, errors.New("github release response did not include tag_name")
	}
	return rel, nil
}

func fetchOpenClashCoreVersion(ctx context.Context, branch, flavor string) (string, error) {
	endpoint := fmt.Sprintf("https://raw.githubusercontent.com/vernesong/OpenClash/core/%s/core_version", branch)
	var lastErr error
	for _, candidate := range downloadCandidates(endpoint) {
		version, err := fetchOpenClashCoreVersionURL(ctx, candidate, flavor)
		if err == nil {
			return version, nil
		}
		lastErr = err
		fmt.Fprintf(os.Stderr, "download: %s version failed from %s: %v\n", flavor, candidate, err)
	}
	return "", lastErr
}

func fetchOpenClashCoreVersionURL(ctx context.Context, endpoint, flavor string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "localclash-core-downloader")
	fmt.Fprintf(os.Stderr, "download: requesting %s core version %s\n", flavor, endpoint)
	resp, err := httpClient().Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("openclash core version request failed: %s: %s", resp.Status, shortHTTPBody(body))
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 8192))
	if err != nil {
		return "", err
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	index := 0
	if flavor == FlavorSmart {
		index = 1
	}
	if len(lines) <= index || strings.TrimSpace(lines[index]) == "" {
		return "", fmt.Errorf("openclash core_version did not include %s version", flavor)
	}
	return strings.TrimSpace(lines[index]), nil
}

func openClashCoreAssetName(targetOS, targetArch string) (string, error) {
	if targetOS != "linux" {
		return "", fmt.Errorf("smart core is available from OpenClash only for linux targets, got %s/%s", targetOS, targetArch)
	}
	switch normalizeArch(targetArch) {
	case "amd64", "386", "arm64", "armv5", "armv6", "armv7", "loong64-abi1", "loong64-abi2", "mips64", "mips64le", "riscv64", "s390x":
		return fmt.Sprintf("clash-linux-%s.tar.gz", normalizeArch(targetArch)), nil
	case "mips", "mipsle":
		return fmt.Sprintf("clash-linux-%s-softfloat.tar.gz", normalizeArch(targetArch)), nil
	default:
		return "", fmt.Errorf("unsupported OpenClash smart core arch %q", targetArch)
	}
}

func selectAsset(assets []asset, targetOS, targetArch string) (asset, error) {
	selected, err := selectAssetForTarget(assets, targetOS, targetArch)
	if err != nil {
		return asset{}, err
	}
	return selected.Asset, nil
}

func selectAssetForTarget(assets []asset, targetOS, targetArch string) (assetSelection, error) {
	targetOS = strings.ToLower(strings.TrimSpace(targetOS))
	targetArch = normalizeArch(targetArch)
	mihomoTarget, cpuLevel, reason := mihomoReleaseTarget(targetOS, targetArch)
	exact := fmt.Sprintf("mihomo-%s-%s-", targetOS, mihomoTarget)
	for _, candidate := range assets {
		name := candidate.Name
		if strings.HasPrefix(name, exact) && (strings.HasSuffix(name, ".gz") || strings.HasSuffix(name, ".zip")) {
			if isSpecialVariant(name, targetOS, mihomoTarget) {
				continue
			}
			return assetSelection{
				Asset:        candidate,
				Target:       mihomoTarget,
				CPULevel:     cpuLevel,
				Reason:       reason,
				DetectedOS:   targetOS,
				DetectedArch: detectedArchForLog(targetOS, targetArch),
			}, nil
		}
	}
	return assetSelection{}, fmt.Errorf("no matching mihomo release asset for exact target %q", mihomoTarget)
}

func normalizeArch(arch string) string {
	switch strings.ToLower(arch) {
	case "aarch64":
		return "arm64"
	case "x86_64", "x64":
		return "amd64"
	default:
		return strings.ToLower(arch)
	}
}

func mihomoReleaseTarget(targetOS, targetArch string) (target, cpuLevel, reason string) {
	if targetOS == "linux" && targetArch == "amd64" {
		return "amd64-v1", "unknown", "x86_64 OpenWrt/iStoreOS environment uses conservative amd64-v1 target unless higher CPU level is explicitly verified"
	}
	return targetArch, "unknown", fmt.Sprintf("%s/%s maps directly to Mihomo release target %s", targetOS, targetArch, targetArch)
}

func detectedArchForLog(targetOS, targetArch string) string {
	if targetOS == "linux" && targetArch == "amd64" {
		return "x86_64"
	}
	return targetArch
}

func openClashSelection(targetOS, targetArch, name string) assetSelection {
	targetOS = strings.ToLower(strings.TrimSpace(targetOS))
	targetArch = normalizeArch(targetArch)
	return assetSelection{
		Asset:        asset{Name: name},
		Target:       targetArch,
		CPULevel:     "unknown",
		Reason:       fmt.Sprintf("OpenClash %s/%s core asset naming maps directly to %s", targetOS, targetArch, name),
		DetectedOS:   targetOS,
		DetectedArch: detectedArchForLog(targetOS, targetArch),
	}
}

func logSelection(selection assetSelection) {
	fmt.Fprintf(os.Stderr, "Detected OS: %s\n", selection.DetectedOS)
	fmt.Fprintf(os.Stderr, "Detected arch: %s\n", selection.DetectedArch)
	fmt.Fprintf(os.Stderr, "Detected CPU level: %s\n", selection.CPULevel)
	fmt.Fprintf(os.Stderr, "Selected mihomo target: %s\n", selection.Target)
	fmt.Fprintf(os.Stderr, "Selected mihomo asset: %s\n", selection.Asset.Name)
	fmt.Fprintf(os.Stderr, "Selection reason: %s\n", selection.Reason)
}

func isSpecialVariant(name, targetOS, targetArch string) bool {
	prefix := fmt.Sprintf("mihomo-%s-%s-", targetOS, targetArch)
	remainder := strings.TrimPrefix(name, prefix)
	for _, marker := range []string{"compatible-", "softfloat-", "hardfloat-", "go120-", "go121-", "go122-", "go123-", "go124-", "go125-"} {
		if strings.HasPrefix(remainder, marker) {
			return true
		}
	}
	if targetArch == "amd64" {
		for _, marker := range []string{"v1-", "v2-", "v3-"} {
			if strings.HasPrefix(remainder, marker) {
				return true
			}
		}
	}
	return false
}

func ensureWritableOutput(path string, force bool) error {
	info, err := os.Stat(path)
	if err == nil {
		if info.IsDir() {
			return fmt.Errorf("output path %q is a directory", path)
		}
		if !force {
			return fmt.Errorf("output path %q already exists; pass --force to overwrite", path)
		}
		return nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func downloadAsset(ctx context.Context, url, name, outputPath string) error {
	var lastErr error
	for _, candidate := range downloadCandidates(url) {
		if err := downloadAssetURL(ctx, candidate, name, outputPath); err != nil {
			lastErr = err
			fmt.Fprintf(os.Stderr, "download: asset failed from %s: %v\n", candidate, err)
			_ = os.Remove(outputPath)
			continue
		}
		return nil
	}
	return lastErr
}

func downloadAssetURL(ctx context.Context, url, name, outputPath string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "localclash-core-downloader")
	fmt.Fprintf(os.Stderr, "download: requesting asset %s\n", url)

	resp, err := httpClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("download failed: %s", resp.Status)
	}

	out, err := os.OpenFile(outputPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	defer out.Close()

	switch {
	case strings.HasSuffix(name, ".tar.gz"):
		return extractFirstTarGzFile(resp.Body, out)
	case strings.HasSuffix(name, ".gz"):
		gz, err := gzip.NewReader(resp.Body)
		if err != nil {
			return err
		}
		defer gz.Close()
		_, err = io.Copy(out, gz)
		return err
	case strings.HasSuffix(name, ".zip"):
		return extractFirstZipFile(resp.Body, out)
	default:
		_, err = io.Copy(out, resp.Body)
		return err
	}
}

func downloadCandidates(url string) []string {
	if mirrorModeDisabled() {
		return []string{url}
	}

	candidates := make([]string, 0, 6)
	candidates = append(candidates, mirroredURLs(url)...)
	candidates = append(candidates, url)
	return uniqueStrings(candidates)
}

func mirrorModeDisabled() bool {
	mode := strings.ToLower(strings.TrimSpace(os.Getenv("LOCALCLASH_GITHUB_MIRROR")))
	return mode == "off" || mode == "none" || mode == "direct"
}

func mirroredURLs(url string) []string {
	switch {
	case strings.HasPrefix(url, "https://github.com/"):
		return mirrorByPrefix(url, "https://github.com", envWords("LOCALCLASH_GITHUB_RELEASE_MIRRORS", defaultGitHubReleaseMirrors))
	case strings.HasPrefix(url, "https://api.github.com/"):
		return mirrorByPrefix(url, "https://api.github.com", envWords("LOCALCLASH_GITHUB_API_MIRRORS", strings.ReplaceAll(defaultGitHubReleaseMirrors, "https://github.com", "https://api.github.com")))
	case strings.HasPrefix(url, "https://raw.githubusercontent.com/"):
		return rawMirrorURLs(url)
	default:
		return nil
	}
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

func rawMirrorURLs(url string) []string {
	mirrors := envWords("LOCALCLASH_GITHUB_RAW_MIRRORS", defaultGitHubRawMirrors)
	out := make([]string, 0, len(mirrors))
	const rawPrefix = "https://raw.githubusercontent.com/"
	rawPath := strings.TrimPrefix(url, rawPrefix)
	for _, mirror := range mirrors {
		mirror = strings.TrimRight(strings.TrimSpace(mirror), "/")
		if mirror == "" {
			continue
		}
		if strings.Contains(mirror, "jsdelivr.net/gh") {
			out = append(out, jsdelivrRawURL(mirror, rawPath))
			continue
		}
		out = append(out, mirror+"/"+rawPath)
	}
	return out
}

func jsdelivrRawURL(mirror, rawPath string) string {
	parts := strings.SplitN(rawPath, "/", 4)
	if len(parts) < 4 {
		return mirror + "/" + rawPath
	}
	return fmt.Sprintf("%s/%s/%s@%s/%s", mirror, parts[0], parts[1], parts[2], parts[3])
}

func envWords(name, fallback string) []string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		value = fallback
	}
	return strings.Fields(value)
}

func httpClient() *http.Client {
	return &http.Client{Timeout: httpTimeout()}
}

func httpTimeout() time.Duration {
	value := strings.TrimSpace(os.Getenv("LOCALCLASH_HTTP_TIMEOUT"))
	if value == "" {
		return 45 * time.Second
	}
	seconds, err := strconv.Atoi(value)
	if err != nil || seconds <= 0 {
		return 45 * time.Second
	}
	return time.Duration(seconds) * time.Second
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

func shortHTTPBody(body []byte) string {
	text := strings.Join(strings.Fields(strings.TrimSpace(string(body))), " ")
	if len(text) > 240 {
		return text[:240] + "..."
	}
	return text
}

func extractFirstTarGzFile(in io.Reader, out io.Writer) error {
	gz, err := gzip.NewReader(in)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return errors.New("tar.gz archive did not contain a file")
		}
		if err != nil {
			return err
		}
		if header.Typeflag == tar.TypeReg {
			_, err = io.Copy(out, tr)
			return err
		}
	}
}

func extractFirstZipFile(r io.Reader, out io.Writer) error {
	tmp, err := os.CreateTemp("", "localclash-mihomo-*.zip")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if _, err := io.Copy(tmp, r); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}

	zr, err := zip.OpenReader(tmpPath)
	if err != nil {
		return err
	}
	defer zr.Close()

	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(out, rc)
		closeErr := rc.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	}
	return errors.New("zip asset did not contain a file")
}
