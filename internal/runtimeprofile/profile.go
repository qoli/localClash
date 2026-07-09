package runtimeprofile

import (
	"bytes"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

//go:embed profiles/*.default.json
var defaultProfileFS embed.FS

const (
	DefaultPath = "localclash-runtime.json"
	UserPath    = "localclash-user.json"

	ModeNormal = "normal"
	ModeRouter = "router"
	CoreMeta   = "meta"
	CoreSmart  = "smart"

	RuntimeSourceBuiltin = "builtin"
	RuntimeSourceUser    = "user"

	ManagedMetaCoreName  = "lc-mihomo-meta"
	ManagedSmartCoreName = "lc-mihomo-smart"
)

var (
	MetaCorePath  = filepath.Join("bin", runtime.GOOS+"-"+runtime.GOARCH, ManagedMetaCoreName)
	SmartCorePath = filepath.Join("bin", "linux-"+runtime.GOARCH, ManagedSmartCoreName)
)

var dynamicConfigKeys = map[string]bool{
	"proxies":         true,
	"proxy-groups":    true,
	"proxy-providers": true,
	"rule-providers":  true,
	"rules":           true,
}

type File struct {
	Version  int                `yaml:"version" json:"version"`
	Mode     string             `yaml:"mode" json:"mode"`
	Core     string             `yaml:"core" json:"core"`
	Profiles map[string]Profile `yaml:"-" json:"-"`
	Cores    map[string]Core    `yaml:"-" json:"-"`
	Smart    SmartOptions       `yaml:"-" json:"-"`
}

type Profile struct {
	Path        string         `yaml:"path,omitempty" json:"path,omitempty"`
	Description string         `yaml:"description,omitempty" json:"description,omitempty"`
	Mihomo      map[string]any `yaml:"mihomo" json:"mihomo"`
	Deploy      map[string]any `yaml:"deploy,omitempty" json:"deploy,omitempty"`
}

type diskFile struct {
	Version int    `yaml:"version" json:"version"`
	Mode    string `yaml:"mode" json:"mode"`
	Core    string `yaml:"core" json:"core"`
}

type Core struct {
	Path string `yaml:"path" json:"path"`
}

type SmartOptions struct {
	UseLightGBM    bool    `yaml:"uselightgbm" json:"uselightgbm"`
	PreferASN      bool    `yaml:"prefer-asn" json:"prefer_asn"`
	CollectData    bool    `yaml:"collectdata,omitempty" json:"collectdata,omitempty"`
	SampleRate     float64 `yaml:"sample-rate,omitempty" json:"sample_rate,omitempty"`
	PolicyPriority string  `yaml:"policy-priority,omitempty" json:"policy_priority,omitempty"`
	URL            string  `yaml:"url,omitempty" json:"url,omitempty"`
	Interval       int     `yaml:"interval,omitempty" json:"interval,omitempty"`
}

type Status struct {
	Path                   string         `json:"path"`
	Exists                 bool           `json:"exists"`
	Mode                   string         `json:"mode"`
	Core                   string         `json:"core"`
	CorePath               string         `json:"core_path"`
	RuntimeSource          string         `json:"runtime_source"`
	UserProfilePath        string         `json:"user_profile_path"`
	UserProfileExists      bool           `json:"user_profile_exists"`
	AvailableModes         []string       `json:"available_modes"`
	AvailableCores         []string       `json:"available_cores"`
	Summary                map[string]any `json:"summary"`
	SmartGroupDefault      SmartOptions   `json:"smart_group_default"`
	RouterTakeoverRequired bool           `json:"router_takeover_required,omitempty"`
	NextActions            []string       `json:"next_actions,omitempty"`
}

func Load(path string) (File, bool, error) {
	path = normalizePath(path)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		file := DefaultFile()
		if err := write(path, file); err != nil {
			return File{}, false, err
		}
		return file, true, nil
	}
	if err != nil {
		return File{}, false, err
	}
	file, err := parseFile(data)
	if err != nil {
		return File{}, true, err
	}
	return file, true, nil
}

func parseFile(data []byte) (File, error) {
	var disk diskFile
	if err := decodeJSON(data, &disk); err != nil {
		return File{}, err
	}
	file := normalizeDiskFile(disk)
	if err := validate(file); err != nil {
		return File{}, err
	}
	return file, nil
}

func StatusFor(path string) (Status, error) {
	path = normalizePath(path)
	file, exists, err := Load(path)
	if err != nil {
		return Status{}, err
	}
	profile, source, err := activeProfile(path, file)
	if err != nil {
		return Status{}, err
	}
	userPath := userProfilePath(path)
	userExists := fileExists(userPath)
	status := Status{
		Path:              path,
		Exists:            exists,
		Mode:              file.Mode,
		Core:              file.Core,
		CorePath:          ActiveCorePathFromFile(file),
		RuntimeSource:     source,
		UserProfilePath:   userPath,
		UserProfileExists: userExists,
		AvailableModes:    []string{ModeNormal, ModeRouter},
		AvailableCores:    []string{CoreMeta, CoreSmart},
		Summary:           summarize(profile),
		SmartGroupDefault: file.Smart,
	}
	sort.Strings(status.AvailableModes)
	sort.Strings(status.AvailableCores)
	if file.Mode == ModeRouter {
		status.RouterTakeoverRequired = true
		status.NextActions = []string{
			"call config_render when .runtime/mihomo/config.yaml is missing or stale",
			"call run_runtime after user confirmation to start Mihomo",
			"call router_takeover_apply after user confirmation to capture router traffic",
		}
	} else {
		status.NextActions = []string{
			"call config_render when .runtime/mihomo/config.yaml is missing or stale",
			"call run_runtime after user confirmation to start Mihomo",
		}
	}
	return status, nil
}

func Configure(path, mode, core string) (Status, error) {
	path = normalizePath(path)
	mode = strings.TrimSpace(mode)
	core = strings.TrimSpace(core)
	file, _, err := Load(path)
	if err != nil {
		return Status{}, err
	}
	if mode != "" {
		if mode != ModeNormal && mode != ModeRouter {
			return Status{}, fmt.Errorf("runtime mode %q is not supported; use %q or %q", mode, ModeNormal, ModeRouter)
		}
		file.Mode = mode
	}
	if core != "" {
		if core != CoreMeta && core != CoreSmart {
			return Status{}, fmt.Errorf("runtime core %q is not supported; use %q or %q", core, CoreMeta, CoreSmart)
		}
		file.Core = core
	}
	if mode == "" && core == "" {
		return Status{}, errors.New("runtime profile configure requires mode or core")
	}
	if err := validate(file); err != nil {
		return Status{}, err
	}
	if err := write(path, file); err != nil {
		return Status{}, err
	}
	return StatusFor(path)
}

func ActiveProfile(path string) (File, Profile, bool, error) {
	path = normalizePath(path)
	file, exists, err := Load(path)
	if err != nil {
		return File{}, Profile{}, exists, err
	}
	profile, _, err := activeProfile(path, file)
	if err != nil {
		return File{}, Profile{}, exists, err
	}
	return file, profile, exists, nil
}

func ValidateUserProfileForRuntime(path string) (bool, error) {
	userPath := userProfilePath(normalizePath(path))
	if !fileExists(userPath) {
		return false, nil
	}
	_, err := readUserProfileFile(userPath)
	return true, err
}

func activeProfile(runtimePath string, file File) (Profile, string, error) {
	userPath := userProfilePath(runtimePath)
	if fileExists(userPath) {
		profile, err := readUserProfileFile(userPath)
		if err != nil {
			return Profile{}, RuntimeSourceUser, err
		}
		return profile, RuntimeSourceUser, nil
	}
	profile, err := readEmbeddedDefaultProfile(file.Mode)
	if err != nil {
		return Profile{}, RuntimeSourceBuiltin, err
	}
	profile.Path = "builtin:" + file.Mode
	return profile, RuntimeSourceBuiltin, nil
}

func ActiveCorePath(path string) (string, error) {
	file, _, err := Load(path)
	if err != nil {
		return "", err
	}
	return ActiveCorePathFromFile(file), nil
}

func ActiveCorePathFromFile(file File) string {
	switch file.Core {
	case CoreMeta:
		return MetaCorePath
	case CoreSmart:
		return SmartCorePath
	default:
		return ""
	}
}

func BuildConfig(profile Profile, dynamic map[string]any) map[string]any {
	config := cloneMap(profile.Mihomo)
	for key, value := range dynamic {
		if IsDynamicConfigKey(key) {
			config[key] = cloneValue(value)
		}
	}
	return config
}

func ApplyToConfig(config map[string]any, profile Profile) {
	dynamic := map[string]any{}
	for key, value := range config {
		if IsDynamicConfigKey(key) {
			dynamic[key] = value
		}
	}
	merged := BuildConfig(profile, dynamic)
	for key := range config {
		delete(config, key)
	}
	for key, value := range merged {
		config[key] = value
	}
}

func IsDynamicConfigKey(key string) bool {
	return dynamicConfigKeys[key] || strings.HasPrefix(key, "x-localclash")
}

func DefaultFile() File {
	file := File{
		Version:  2,
		Mode:     ModeNormal,
		Core:     CoreMeta,
		Cores:    defaultCores(),
		Smart:    defaultSmartOptions(),
		Profiles: map[string]Profile{},
	}
	for _, mode := range []string{ModeNormal, ModeRouter} {
		profile, err := readEmbeddedDefaultProfile(mode)
		if err != nil {
			panic(fmt.Sprintf("invalid embedded %s default profile: %v", mode, err))
		}
		profile.Path = "builtin:" + mode
		file.Profiles[mode] = profile
	}
	return file
}

func normalizePath(path string) string {
	if strings.TrimSpace(path) == "" {
		return DefaultPath
	}
	return path
}

func normalizeDiskFile(file diskFile) File {
	if file.Version == 0 {
		file.Version = 2
	}
	if file.Mode == "" {
		file.Mode = ModeNormal
	}
	if file.Core == "" {
		file.Core = CoreMeta
	}
	out := DefaultFile()
	out.Version = file.Version
	out.Mode = file.Mode
	out.Core = file.Core
	return out
}

func defaultCores() map[string]Core {
	return map[string]Core{
		CoreMeta:  {Path: MetaCorePath},
		CoreSmart: {Path: SmartCorePath},
	}
}

// defaultSmartOptions applies only when a smart group lacks these fields.
// Built-in auto groups already carry the auto health-check defaults before they
// are converted to smart groups for the Smart core, so this interval does not
// control the default-template auto-group cadence.
func defaultSmartOptions() SmartOptions {
	return SmartOptions{
		UseLightGBM: true,
		PreferASN:   true,
		URL:         "https://cp.cloudflare.com/generate_204",
		Interval:    600,
	}
}

func defaultProfileBytes(mode string) []byte {
	data, err := defaultProfileFS.ReadFile(filepath.ToSlash(defaultProfileRelPath(mode)))
	if err != nil {
		panic(fmt.Sprintf("missing embedded %s default profile: %v", mode, err))
	}
	return []byte(expandRuntimePlaceholders(string(data)))
}

func readEmbeddedDefaultProfile(mode string) (Profile, error) {
	var profile Profile
	if err := decodeJSON(defaultProfileBytes(mode), &profile); err != nil {
		return Profile{}, err
	}
	if len(profile.Mihomo) == 0 {
		return Profile{}, fmt.Errorf("embedded runtime profile %q has no mihomo config", mode)
	}
	return profile, nil
}

func readUserProfileFile(path string) (Profile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Profile{}, err
	}
	var mihomo map[string]any
	if err := decodeJSON(data, &mihomo); err != nil {
		return Profile{}, err
	}
	if mihomo == nil {
		mihomo = map[string]any{}
	}
	if err := validateUserProfile(path, mihomo); err != nil {
		return Profile{}, err
	}
	return Profile{
		Path:        UserPath,
		Description: "User-authored Mihomo runtime fragment",
		Mihomo:      mihomo,
	}, nil
}

func validateUserProfile(path string, mihomo map[string]any) error {
	for key := range mihomo {
		if IsDynamicConfigKey(key) {
			return fmt.Errorf("%s contains localClash-owned top-level key %q; remove it from %s", filepath.Base(path), key, UserPath)
		}
	}
	return nil
}

func decodeJSON(data []byte, out any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(out); err != nil {
		return err
	}
	normalizeJSONNumbers(out)
	return nil
}

func normalizeJSONNumbers(value any) any {
	switch typed := value.(type) {
	case *Profile:
		typed.Mihomo = normalizeOptionalMap(typed.Mihomo)
		typed.Deploy = normalizeOptionalMap(typed.Deploy)
	case *File:
		for key, profile := range typed.Profiles {
			profile.Mihomo = normalizeOptionalMap(profile.Mihomo)
			profile.Deploy = normalizeOptionalMap(profile.Deploy)
			typed.Profiles[key] = profile
		}
	case *diskFile:
		return typed
	case *map[string]any:
		*typed = normalizeOptionalMap(*typed)
	case map[string]any:
		for key, item := range typed {
			typed[key] = normalizeJSONNumbers(item)
		}
		return typed
	case []any:
		for i, item := range typed {
			typed[i] = normalizeJSONNumbers(item)
		}
		return typed
	case json.Number:
		if i, err := typed.Int64(); err == nil {
			return int(i)
		}
		if f, err := typed.Float64(); err == nil {
			return f
		}
		return typed.String()
	}
	return value
}

func normalizeOptionalMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	return normalizeJSONNumbers(in).(map[string]any)
}

func defaultProfileRelPath(mode string) string {
	return filepath.Join("profiles", mode+".default.json")
}

func expandRuntimePlaceholders(value string) string {
	return strings.NewReplacer(
		"${LOCALCLASH_HOST_OS}", runtime.GOOS,
		"${LOCALCLASH_HOST_ARCH}", runtime.GOARCH,
		"${LOCALCLASH_HOST_PLATFORM}", runtime.GOOS+"-"+runtime.GOARCH,
	).Replace(value)
}

func userProfilePath(runtimePath string) string {
	return resolveRuntimePath(filepath.Dir(normalizePath(runtimePath)), UserPath)
}

func resolveRuntimePath(baseDir, path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	if strings.TrimSpace(baseDir) == "" || baseDir == "." {
		return path
	}
	return filepath.Join(baseDir, path)
}

func validate(file File) error {
	if file.Version != 2 {
		return fmt.Errorf("unsupported runtime profile version %d; expected 2", file.Version)
	}
	if file.Mode != ModeNormal && file.Mode != ModeRouter {
		return fmt.Errorf("runtime mode %q is not supported; use %q or %q", file.Mode, ModeNormal, ModeRouter)
	}
	if file.Core != CoreMeta && file.Core != CoreSmart {
		return fmt.Errorf("runtime core %q is not supported; use %q or %q", file.Core, CoreMeta, CoreSmart)
	}
	if ActiveCorePathFromFile(file) == "" {
		return fmt.Errorf("runtime core %q has no path", file.Core)
	}
	return nil
}

func write(path string, file File) error {
	dir := filepath.Dir(path)
	if dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	disk := diskFile{
		Version: file.Version,
		Mode:    file.Mode,
		Core:    file.Core,
	}
	data, err := json.MarshalIndent(disk, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}

func cloneMap(in map[string]any) map[string]any {
	out := map[string]any{}
	for key, value := range in {
		out[key] = cloneValue(value)
	}
	return out
}

func cloneValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := map[string]any{}
		for key, item := range typed {
			out[key] = cloneValue(item)
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for i, item := range typed {
			out[i] = cloneValue(item)
		}
		return out
	case []string:
		return append([]string(nil), typed...)
	default:
		return value
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func summarize(profile Profile) map[string]any {
	m := profile.Mihomo
	summary := map[string]any{}
	for _, key := range []string{"mixed-port", "redir-port", "tproxy-port", "port", "socks-port", "allow-lan", "bind-address", "external-controller", "ipv6", "mode"} {
		if value, ok := m[key]; ok {
			summary[key] = value
		}
	}
	if dns, ok := m["dns"].(map[string]any); ok {
		summary["dns"] = map[string]any{
			"enable":        dns["enable"],
			"listen":        dns["listen"],
			"enhanced-mode": dns["enhanced-mode"],
			"respect-rules": dns["respect-rules"],
		}
	}
	if tun, ok := m["tun"].(map[string]any); ok {
		summary["tun"] = map[string]any{
			"enable":        tun["enable"],
			"stack":         tun["stack"],
			"device":        tun["device"],
			"auto-route":    tun["auto-route"],
			"auto-redirect": tun["auto-redirect"],
		}
	}
	return summary
}
