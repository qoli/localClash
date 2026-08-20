package routertakeover

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"localclash/internal/corerun"
	"localclash/internal/runtimeprofile"
)

const (
	defaultFWMark            = "0x6c63"
	defaultRouteTable        = "27747"
	defaultRulePref          = "1890"
	legacyFWMark             = "0x162"
	legacyRouteTable         = "0x162"
	defaultStateDir          = "/tmp/localclash/router-takeover"
	commandTimeout           = 75 * time.Second
	StatusObservationTimeout = 8 * time.Second
)

type Options struct {
	RuntimeProfile string
	ConfigPath     string
	RuntimeDir     string
	LogPath        string
	StateDir       string
	DNSPort        int
	RedirPort      int
	TunDevice      string
	IPv6           bool
	DryRun         bool
	OnStage        func(StageEvent) `json:"-"`
}

type StageEvent struct {
	Stage      string         `json:"stage"`
	Event      string         `json:"event"`
	DurationMS int64          `json:"duration_ms,omitempty"`
	Error      string         `json:"error,omitempty"`
	Fields     map[string]any `json:"fields,omitempty"`
}

type Check struct {
	ID      string `json:"id"`
	OK      bool   `json:"ok"`
	Summary string `json:"summary"`
	Error   string `json:"error,omitempty"`
}

type Result struct {
	ProfileMode           string   `json:"profile_mode"`
	RuntimeRunning        bool     `json:"runtime_running"`
	Effective             bool     `json:"effective"`
	Applied               bool     `json:"applied,omitempty"`
	Stopped               bool     `json:"stopped,omitempty"`
	DryRun                bool     `json:"dry_run,omitempty"`
	StateDir              string   `json:"state_dir"`
	DNSPort               int      `json:"dns_port"`
	RedirPort             int      `json:"redir_port"`
	TunDevice             string   `json:"tun_device"`
	LocalDNS              []string `json:"local_dns,omitempty"`
	LocalDomains          []string `json:"local_domains,omitempty"`
	Error                 string   `json:"error,omitempty"`
	Checks                []Check  `json:"checks"`
	Warnings              []string `json:"warnings,omitempty"`
	NextActions           []string `json:"next_actions,omitempty"`
	Script                string   `json:"script,omitempty"`
	ObservationDurationMS int64    `json:"observation_duration_ms,omitempty"`
}

type commandRunner func(context.Context, string) (string, error)

func Status(ctx context.Context, opts Options) (Result, error) {
	return statusWithRunner(ctx, opts, defaultRunner)
}

func statusWithRunner(ctx context.Context, opts Options, runner commandRunner) (Result, error) {
	opts = normalizeOptions(opts)
	stage := routerTakeoverStageEmitter(opts.OnStage)
	finish := stage("read_runtime_profile", map[string]any{"runtime_profile": opts.RuntimeProfile})
	status, err := runtimeprofile.StatusFor(opts.RuntimeProfile)
	if err != nil {
		finish(err, nil)
		return Result{}, err
	}
	finish(nil, map[string]any{"profile_mode": status.Mode})
	opts = mergeProfileDefaults(opts, status)
	result := baseResult(opts, status)
	finish = stage("runtime_status", map[string]any{"runtime_dir": opts.RuntimeDir, "config": opts.ConfigPath})
	runtimeStatus := corerun.Status(corerun.StatusOptions{
		ConfigPath: opts.ConfigPath,
		WorkDir:    opts.RuntimeDir,
		LogPath:    opts.LogPath,
	})
	result.RuntimeRunning = runtimeStatus.Running
	finish(nil, map[string]any{"running": runtimeStatus.Running, "pid": runtimeStatus.PID})
	result.Checks = append(result.Checks, check("profile_router", status.Mode == runtimeprofile.ModeRouter, fmt.Sprintf("active profile mode is %s", status.Mode), "router_takeover_* is only meaningful when runtime profile mode is router"))
	result.Checks = append(result.Checks, check("runtime_running", runtimeStatus.Running, "localClash Mihomo runtime is running", "call run_runtime before router_takeover_apply"))
	finish = stage("observe_router_state", map[string]any{"timeout_ms": StatusObservationTimeout.Milliseconds()})
	started := time.Now()
	observation, err := observeStatus(ctx, opts, runner)
	result.ObservationDurationMS = time.Since(started).Milliseconds()
	if err != nil {
		finish(err, map[string]any{"duration_ms": result.ObservationDurationMS})
		return Result{}, err
	}
	finish(nil, map[string]any{"duration_ms": result.ObservationDurationMS})
	checks := []struct {
		id   string
		ok   string
		fail string
	}{
		{"fw4_available", "Firewall4/fw4 is available", "fw4 is unavailable"},
		{"nft_available", "nft is available", "nft is unavailable"},
		{"fw4_table", "Firewall4 nft table inet fw4 is active", "Firewall4 nft table inet fw4 is not active"},
		{"fw4_base_chains", "Firewall4 base chains are available", "Firewall4 base chains are missing"},
		{"tun_interface", fmt.Sprintf("TUN device %s exists", opts.TunDevice), fmt.Sprintf("TUN device %s is missing", opts.TunDevice)},
		{"fwmark_route_v4", "IPv4 fwmark route points to TUN", "IPv4 fwmark route is missing"},
		{"nft_chains", "localClash nft takeover chains are installed", "localClash nft takeover chains are missing"},
		{"tcp_redirect", "localClash TCP redir-host redirect is installed", "localClash TCP redir-host redirect is missing"},
		{"udp_tun_mark", "localClash UDP/ICMP TUN mark is installed", "localClash UDP/ICMP TUN mark is missing"},
		{"dns_hijack", "localClash DNS hijack rule is installed", "localClash DNS hijack rule is missing"},
		{"local_dns_bypass", "localClash local DNS bypass is installed", "localClash local DNS bypass is missing"},
	}
	for _, item := range checks {
		checkResult := check(item.id, observation[item.id], item.ok, item.fail)
		result.Checks = append(result.Checks, checkResult)
	}
	finish = stage("check_local_dns_discovered", nil)
	localDNSOK := len(result.LocalDNS) > 0
	result.Checks = append(result.Checks, check("local_dns_discovered", localDNSOK, "local DNS bypass addresses were discovered", "local DNS bypass address discovery state is missing"))
	finish(nil, map[string]any{"ok": localDNSOK, "local_dns_count": len(result.LocalDNS)})
	result.Effective = allChecksOK(result.Checks)
	result.NextActions = nextActions(result)
	return result, nil
}

var statusObservationIDs = []string{
	"fw4_available",
	"nft_available",
	"fw4_table",
	"fw4_base_chains",
	"tun_interface",
	"fwmark_route_v4",
	"nft_chains",
	"tcp_redirect",
	"udp_tun_mark",
	"dns_hijack",
	"local_dns_bypass",
}

func observeStatus(ctx context.Context, opts Options, runner commandRunner) (map[string]bool, error) {
	observationCtx, cancel := context.WithTimeout(ctx, StatusObservationTimeout)
	defer cancel()
	output, err := runner(observationCtx, statusObservationScript(opts))
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(observationCtx.Err(), context.DeadlineExceeded) {
			return nil, fmt.Errorf("router takeover status observation timed out after %s: %w", StatusObservationTimeout, context.DeadlineExceeded)
		}
		return nil, fmt.Errorf("router takeover status observation failed: %w", err)
	}
	return parseStatusObservation(output)
}

func parseStatusObservation(output string) (map[string]bool, error) {
	wanted := make(map[string]bool, len(statusObservationIDs))
	for _, id := range statusObservationIDs {
		wanted[id] = true
	}
	result := make(map[string]bool, len(statusObservationIDs))
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.SplitN(strings.TrimSpace(line), "=", 2)
		if len(parts) != 2 || !wanted[parts[0]] || (parts[1] != "0" && parts[1] != "1") {
			return nil, fmt.Errorf("invalid router takeover status observation line %q", line)
		}
		if _, exists := result[parts[0]]; exists {
			return nil, fmt.Errorf("duplicate router takeover status observation %q", parts[0])
		}
		result[parts[0]] = parts[1] == "1"
	}
	for _, id := range statusObservationIDs {
		if _, ok := result[id]; !ok {
			return nil, fmt.Errorf("router takeover status observation missing %q", id)
		}
	}
	return result, nil
}

func statusObservationScript(opts Options) string {
	return fmt.Sprintf(`TUN_DEVICE=%s
ROUTE_TABLE=%s
contains() {
	case "$1" in
		*"$2"*) return 0 ;;
		*) return 1 ;;
	esac
}
fw4_available=0
nft_available=0
command -v fw4 >/dev/null 2>&1 && fw4_available=1
command -v nft >/dev/null 2>&1 && nft_available=1
tables="$(nft list tables 2>/dev/null)"; tables_rc=$?
dstnat="$(nft list chain inet fw4 dstnat 2>/dev/null)"; dstnat_rc=$?
mangle_prerouting="$(nft list chain inet fw4 mangle_prerouting 2>/dev/null)"; mangle_prerouting_rc=$?
forward="$(nft list chain inet fw4 forward 2>/dev/null)"; forward_rc=$?
input="$(nft list chain inet fw4 input 2>/dev/null)"; input_rc=$?
srcnat="$(nft list chain inet fw4 srcnat 2>/dev/null)"; srcnat_rc=$?
localclash="$(nft list chain inet fw4 localclash 2>/dev/null)"; localclash_rc=$?
localclash_mangle="$(nft list chain inet fw4 localclash_mangle 2>/dev/null)"; localclash_mangle_rc=$?
localclash_dns="$(nft list chain inet fw4 localclash_dns_redirect 2>/dev/null)"; localclash_dns_rc=$?
ip_link="$(ip link show "$TUN_DEVICE" 2>/dev/null)"; ip_link_rc=$?
ip_rules="$(ip rule show 2>/dev/null)"; ip_rules_rc=$?
ip_routes="$(ip route show table "$ROUTE_TABLE" 2>/dev/null)"; ip_routes_rc=$?
fw4_table=0
[ "$tables_rc" -eq 0 ] && contains "$tables" "table inet fw4" && fw4_table=1
fw4_base_chains=0
[ "$dstnat_rc" -eq 0 ] && [ "$mangle_prerouting_rc" -eq 0 ] && [ "$forward_rc" -eq 0 ] && [ "$input_rc" -eq 0 ] && [ "$srcnat_rc" -eq 0 ] && fw4_base_chains=1
tun_interface=0
[ "$ip_link_rc" -eq 0 ] && tun_interface=1
fwmark_route_v4=0
[ "$ip_rules_rc" -eq 0 ] && [ "$ip_routes_rc" -eq 0 ] && contains "$ip_rules" "fwmark %s" && contains "$ip_routes" "$TUN_DEVICE" && fwmark_route_v4=1
nft_chains=0
[ "$localclash_rc" -eq 0 ] && [ "$localclash_mangle_rc" -eq 0 ] && nft_chains=1
tcp_redirect=0
contains "$dstnat" "localClash TCP redirect" && tcp_redirect=1
udp_tun_mark=0
contains "$mangle_prerouting" "localClash TUN mark" && udp_tun_mark=1
dns_hijack=0
[ "$localclash_dns_rc" -eq 0 ] && contains "$localclash_dns" "localClash DNS hijack" && dns_hijack=1
local_dns_bypass=0
[ "$localclash_dns_rc" -eq 0 ] && contains "$localclash_dns" "localClash local DNS bypass" && local_dns_bypass=1
for id in %s; do
	eval "value=\${$id}"
	printf '%%s=%%s\n' "$id" "$value"
done
`, shellQuote(opts.TunDevice), shellQuote(defaultRouteTable), defaultFWMark, strings.Join(statusObservationIDs, " "))
}

func Apply(ctx context.Context, opts Options) (Result, error) {
	opts = normalizeOptions(opts)
	stage := routerTakeoverStageEmitter(opts.OnStage)
	finish := stage("read_runtime_profile", map[string]any{"runtime_profile": opts.RuntimeProfile})
	status, err := runtimeprofile.StatusFor(opts.RuntimeProfile)
	if err != nil {
		finish(err, nil)
		return Result{}, err
	}
	finish(nil, map[string]any{"profile_mode": status.Mode})
	opts = mergeProfileDefaults(opts, status)
	result := baseResult(opts, status)
	if status.Mode != runtimeprofile.ModeRouter {
		message := "call config_configure with runtime_profile=router before router_takeover_apply"
		result.Error = message
		result.Checks = append(result.Checks, check("profile_router", false, fmt.Sprintf("active profile mode is %s", status.Mode), message))
		result.NextActions = []string{"call config_configure with runtime_profile=router", "call config_render", "call run_runtime", "call router_takeover_apply again"}
		return result, nil
	}
	finish = stage("runtime_status", map[string]any{"runtime_dir": opts.RuntimeDir, "config": opts.ConfigPath})
	runtimeStatus := corerun.Status(corerun.StatusOptions{ConfigPath: opts.ConfigPath, WorkDir: opts.RuntimeDir, LogPath: opts.LogPath})
	result.RuntimeRunning = runtimeStatus.Running
	finish(nil, map[string]any{"running": runtimeStatus.Running, "pid": runtimeStatus.PID})
	script := applyScript(opts)
	result.Script = script
	if opts.DryRun {
		result.DryRun = true
		result.Checks = append(result.Checks, check("runtime_running", runtimeStatus.Running, "localClash Mihomo runtime is running", "call run_runtime before applying router takeover"))
		if runtimeStatus.Running {
			result.NextActions = []string{"review the script", "call router_takeover_apply without dry_run after user confirmation"}
		} else {
			result.NextActions = []string{"review the script", "call run_runtime after user confirmation", "call router_takeover_apply without dry_run after user confirmation"}
		}
		return result, nil
	}
	if !runtimeStatus.Running {
		message := "call run_runtime before router_takeover_apply"
		result.Error = message
		result.Checks = append(result.Checks, check("runtime_running", false, "localClash Mihomo runtime is not running", message))
		result.NextActions = []string{"call run_runtime after user confirmation", "call router_takeover_apply again"}
		return result, nil
	}
	finish = stage("apply_script", map[string]any{"state_dir": opts.StateDir, "tun_device": opts.TunDevice})
	if _, err := defaultRunner(ctx, script); err != nil {
		finish(err, map[string]any{"recovery": "runtime takeover state is non-persistent; reboot clears localClash-owned rules"})
		result.Error = err.Error()
		result.Checks = append(result.Checks, check("apply_script", false, "router takeover script applied", err.Error()))
		result.NextActions = takeoverFailureNextActions("apply", err)
		return result, nil
	}
	finish(nil, nil)
	finish = stage("verify_takeover_status", nil)
	statusOpts := opts
	statusOpts.OnStage = nil
	result, err = Status(ctx, statusOpts)
	if err != nil {
		finish(err, nil)
		return Result{}, err
	}
	finish(nil, map[string]any{"effective": result.Effective})
	if !result.Effective {
		result.Error = "router takeover verification failed after apply"
		result.NextActions = takeoverFailureNextActions("apply", errors.New(result.Error))
		return result, nil
	}
	result.Applied = true
	return result, nil
}

func Stop(ctx context.Context, opts Options) (Result, error) {
	opts = normalizeOptions(opts)
	stage := routerTakeoverStageEmitter(opts.OnStage)
	finish := stage("read_runtime_profile", map[string]any{"runtime_profile": opts.RuntimeProfile})
	status, err := runtimeprofile.StatusFor(opts.RuntimeProfile)
	if err != nil {
		finish(err, nil)
		return Result{}, err
	}
	finish(nil, map[string]any{"profile_mode": status.Mode})
	opts = mergeProfileDefaults(opts, status)
	result := baseResult(opts, status)
	script := stopScript(opts)
	result.Script = script
	if opts.DryRun {
		result.DryRun = true
		result.NextActions = []string{"review the cleanup script", "call router_takeover_stop without dry_run after user confirmation"}
		return result, nil
	}
	finish = stage("stop_script", map[string]any{"state_dir": opts.StateDir, "tun_device": opts.TunDevice})
	if _, err := defaultRunner(ctx, script); err != nil {
		finish(err, map[string]any{"recovery": "runtime takeover state is non-persistent; reboot clears localClash-owned rules"})
		result.Error = err.Error()
		result.Checks = append(result.Checks, check("stop_script", false, "router takeover cleanup script ran", err.Error()))
		result.NextActions = takeoverFailureNextActions("stop", err)
		return result, nil
	}
	finish(nil, nil)
	finish = stage("verify_takeover_status", nil)
	statusOpts := opts
	statusOpts.OnStage = nil
	result, err = Status(ctx, statusOpts)
	if err != nil {
		finish(err, nil)
		return Result{}, err
	}
	finish(nil, map[string]any{"effective": result.Effective})
	result.Stopped = true
	return result, nil
}

func routerTakeoverStageEmitter(callback func(StageEvent)) func(string, map[string]any) func(error, map[string]any) {
	return func(stage string, fields map[string]any) func(error, map[string]any) {
		if callback == nil {
			return func(error, map[string]any) {}
		}
		started := time.Now()
		callback(StageEvent{Stage: stage, Event: "started", Fields: fields})
		return func(err error, doneFields map[string]any) {
			event := StageEvent{
				Stage:      stage,
				Event:      "done",
				DurationMS: time.Since(started).Milliseconds(),
				Fields:     doneFields,
			}
			if err != nil {
				event.Event = "error"
				event.Error = err.Error()
			}
			callback(event)
		}
	}
}

func normalizeOptions(opts Options) Options {
	if strings.TrimSpace(opts.RuntimeProfile) == "" {
		opts.RuntimeProfile = runtimeprofile.DefaultPath
	}
	if strings.TrimSpace(opts.ConfigPath) == "" {
		opts.ConfigPath = filepath.Join(".runtime", "mihomo", "config.yaml")
	}
	if strings.TrimSpace(opts.RuntimeDir) == "" {
		opts.RuntimeDir = filepath.Join(".runtime", "mihomo")
	}
	if strings.TrimSpace(opts.LogPath) == "" {
		opts.LogPath = filepath.Join(opts.RuntimeDir, "mihomo.log")
	}
	if strings.TrimSpace(opts.StateDir) == "" {
		opts.StateDir = defaultStateDir
	}
	if opts.DNSPort == 0 {
		opts.DNSPort = 7874
	}
	if opts.RedirPort == 0 {
		opts.RedirPort = 7892
	}
	if strings.TrimSpace(opts.TunDevice) == "" {
		opts.TunDevice = "utun"
	}
	return opts
}

func mergeProfileDefaults(opts Options, status runtimeprofile.Status) Options {
	if status.Mode != runtimeprofile.ModeRouter {
		return opts
	}
	if value, ok := intFromSummary(status.Summary, "redir-port"); ok && opts.RedirPort == 7892 {
		opts.RedirPort = value
	}
	if dns, ok := status.Summary["dns"].(map[string]any); ok && opts.DNSPort == 7874 {
		if listen, ok := dns["listen"].(string); ok {
			if port := portFromListen(listen); port != 0 {
				opts.DNSPort = port
			}
		}
	}
	if tun, ok := status.Summary["tun"].(map[string]any); ok {
		if device, ok := tun["device"].(string); ok && strings.TrimSpace(device) != "" && opts.TunDevice == "utun" {
			opts.TunDevice = strings.TrimSpace(device)
		}
	}
	if ipv6, ok := status.Summary["ipv6"].(bool); ok {
		opts.IPv6 = ipv6
	}
	return opts
}

func intFromSummary(summary map[string]any, key string) (int, bool) {
	switch value := summary[key].(type) {
	case int:
		return value, true
	case int64:
		return int(value), true
	case float64:
		return int(value), true
	case string:
		parsed, err := strconv.Atoi(value)
		return parsed, err == nil
	default:
		return 0, false
	}
}

func portFromListen(listen string) int {
	parts := strings.Split(listen, ":")
	if len(parts) == 0 {
		return 0
	}
	port, err := strconv.Atoi(parts[len(parts)-1])
	if err != nil {
		return 0
	}
	return port
}

func baseResult(opts Options, status runtimeprofile.Status) Result {
	return Result{
		ProfileMode:  status.Mode,
		StateDir:     opts.StateDir,
		DNSPort:      opts.DNSPort,
		RedirPort:    opts.RedirPort,
		TunDevice:    opts.TunDevice,
		LocalDNS:     stateLines(filepath.Join(opts.StateDir, "local_dns4"), filepath.Join(opts.StateDir, "local_dns6")),
		LocalDomains: stateLines(filepath.Join(opts.StateDir, "local_domains")),
		Warnings: []string{
			"router_takeover_* applies runtime-only OpenWrt firewall, DNS, and policy-routing state and may interrupt network connectivity.",
			"router_takeover_* follows localClash router redir-host-mix behavior: TCP redir-host, DNS hijack with local resolver bypass, and UDP/ICMP TUN marking.",
			"router_takeover_* must not write persistent firewall configuration; reboot clears the runtime takeover state.",
			"router_takeover_* manages only localClash-owned rules and state.",
		},
	}
}

func stateLines(paths ...string) []string {
	seen := map[string]bool{}
	var out []string
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || seen[line] {
				continue
			}
			seen[line] = true
			out = append(out, line)
		}
	}
	return out
}

func check(id string, ok bool, summary, errText string) Check {
	item := Check{ID: id, OK: ok}
	if ok {
		item.Summary = summary
	} else {
		item.Summary = errText
		item.Error = errText
	}
	return item
}

func allChecksOK(checks []Check) bool {
	if len(checks) == 0 {
		return false
	}
	for _, item := range checks {
		if !item.OK {
			return false
		}
	}
	return true
}

func nextActions(result Result) []string {
	if result.Effective {
		return []string{"router takeover is installed; use router_takeover_status to verify later", "use router_takeover_stop to remove localClash-owned takeover rules"}
	}
	if result.ProfileMode != runtimeprofile.ModeRouter {
		return []string{"call config_configure with runtime_profile=router", "call config_render", "call run_runtime", "call router_takeover_apply"}
	}
	if !result.RuntimeRunning {
		return []string{"call run_runtime after user confirmation", "call router_takeover_apply"}
	}
	return []string{"call router_takeover_apply after user confirmation"}
}

func takeoverFailureNextActions(action string, err error) []string {
	actions := []string{
		"inspect the MCP task log stage_error entry for the failing command output",
		"call router_takeover_status to see which runtime-only checks are still effective",
		"retry after fixing the reported OpenWrt prerequisite or Mihomo runtime state",
		"rebooting the router clears localClash runtime takeover state because no persistent firewall config is written",
	}
	if action == "apply" {
		actions = append(actions, "call router_takeover_stop after user confirmation if status shows partially installed localClash rules")
	}
	if err != nil {
		actions = append([]string{"failure: " + err.Error()}, actions...)
	}
	return actions
}

func defaultRunner(ctx context.Context, command string) (string, error) {
	runCtx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()
	cmd := exec.CommandContext(runCtx, "/bin/sh", "-c", command)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		text := strings.TrimSpace(out.String())
		if runCtx.Err() != nil {
			if text != "" {
				return text, fmt.Errorf("%w: %s", runCtx.Err(), text)
			}
			return "", runCtx.Err()
		}
		if text != "" {
			return text, fmt.Errorf("%w: %s", err, text)
		}
		return "", err
	}
	return strings.TrimSpace(out.String()), nil
}

type shellScriptBuilder struct {
	lines []string
}

func (builder *shellScriptBuilder) line(line string) {
	builder.lines = append(builder.lines, line)
}

func (builder *shellScriptBuilder) raw(text string) {
	text = strings.Trim(text, "\n")
	if text == "" {
		return
	}
	builder.lines = append(builder.lines, strings.Split(text, "\n")...)
}

func (builder *shellScriptBuilder) nft(lines []string) {
	builder.line("nft -f - <<EOF_NFT")
	builder.lines = append(builder.lines, lines...)
	builder.line("EOF_NFT")
}

func (builder shellScriptBuilder) String() string {
	return strings.Join(builder.lines, "\n") + "\n"
}

func applyScript(opts Options) string {
	var script shellScriptBuilder
	script.line("set -eu")
	script.line("STATE_DIR=" + shellQuote(opts.StateDir))
	script.line("DNS_PORT=" + strconv.Itoa(opts.DNSPort))
	script.line("REDIR_PORT=" + strconv.Itoa(opts.RedirPort))
	script.line("TUN_DEVICE=" + shellQuote(opts.TunDevice))
	script.line("FWMARK=" + shellQuote(defaultFWMark))
	script.line("ROUTE_TABLE=" + shellQuote(defaultRouteTable))
	script.line("RULE_PREF=" + shellQuote(defaultRulePref))
	script.line("LEGACY_FWMARK=" + shellQuote(legacyFWMark))
	script.line("LEGACY_ROUTE_TABLE=" + shellQuote(legacyRouteTable))
	script.line("TUN_WAIT_SECONDS=30")
	script.raw(applyShellLibrary)
	script.raw(applyRoutingSetup)
	script.nft(applyBaseNftRules())
	script.raw(`
add_dynamic_localdns4
add_dynamic_localdns6
`)
	script.nft(applyTakeoverNftRules())
	script.raw(applyPostNftScript)
	return script.String()
}

const applyShellLibrary = `
command -v fw4 >/dev/null 2>&1
command -v nft >/dev/null 2>&1
mkdir -p "$STATE_DIR"
modprobe tun >/dev/null 2>&1 || true
modprobe nft_tproxy >/dev/null 2>&1 || true

cleanup_localclash_nft() {
  for chain in dstnat nat_output mangle_prerouting mangle_output forward input srcnat; do
    nft -a list chain inet fw4 "$chain" 2>/dev/null | awk '/localClash/{print $NF}' | sort -rn | while read -r handle; do
      [ -n "$handle" ] && nft delete rule inet fw4 "$chain" handle "$handle" 2>/dev/null || true
    done
  done
  for chain in localclash localclash_output localclash_mangle localclash_mangle_output localclash_v6 localclash_mangle_v6 localclash_dns_redirect; do
    nft flush chain inet fw4 "$chain" >/dev/null 2>&1 || true
    nft delete chain inet fw4 "$chain" >/dev/null 2>&1 || true
  done
  for set_name in localclash_localnetwork localclash_localnetwork6 localclash_localdns4 localclash_localdns6; do
    nft flush set inet fw4 "$set_name" >/dev/null 2>&1 || true
    nft delete set inet fw4 "$set_name" >/dev/null 2>&1 || true
  done
}

cleanup_localclash_state() {
  cleanup_localclash_nft
  while ip rule del fwmark "$FWMARK" table "$ROUTE_TABLE" >/dev/null 2>&1; do :; done
  ip route del default table "$ROUTE_TABLE" >/dev/null 2>&1 || true
  while ip -6 rule del fwmark "$FWMARK" table "$ROUTE_TABLE" >/dev/null 2>&1; do :; done
  ip -6 route del default table "$ROUTE_TABLE" >/dev/null 2>&1 || true
  rm -f "$STATE_DIR/status" "$STATE_DIR/local_dns4" "$STATE_DIR/local_dns6" "$STATE_DIR/local_domains" "$STATE_DIR/local_dns4.tmp" "$STATE_DIR/local_dns6.tmp" "$STATE_DIR/local_domains.tmp" >/dev/null 2>&1 || true
}

legacy_localclash_route_identity_present() {
  ip rule show 2>/dev/null | grep -F "fwmark $LEGACY_FWMARK" >/dev/null 2>&1 && return 0
  ip route show table "$LEGACY_ROUTE_TABLE" 2>/dev/null | grep -q . && return 0
  ip -6 rule show 2>/dev/null | grep -F "fwmark $LEGACY_FWMARK" >/dev/null 2>&1 && return 0
  ip -6 route show table "$LEGACY_ROUTE_TABLE" 2>/dev/null | grep -q . && return 0
  return 1
}

cleanup_legacy_localclash_route_identity() {
  legacy_localclash_route_identity_present || return 0
  if [ "$(cat "$STATE_DIR/status" 2>/dev/null || true)" != "applied" ]; then
    echo "legacy localClash route identity $LEGACY_FWMARK/$LEGACY_ROUTE_TABLE exists without applied ownership state; refusing migration" >&2
    return 1
  fi
  if ! nft list chain inet fw4 localclash >/dev/null 2>&1 || ! nft list chain inet fw4 localclash_mangle >/dev/null 2>&1; then
    echo "legacy localClash route identity $LEGACY_FWMARK/$LEGACY_ROUTE_TABLE exists without localClash nft ownership evidence; refusing migration" >&2
    return 1
  fi
  while ip rule del fwmark "$LEGACY_FWMARK" table "$LEGACY_ROUTE_TABLE" >/dev/null 2>&1; do :; done
  ip route del default table "$LEGACY_ROUTE_TABLE" >/dev/null 2>&1 || true
  while ip -6 rule del fwmark "$LEGACY_FWMARK" table "$LEGACY_ROUTE_TABLE" >/dev/null 2>&1; do :; done
  ip -6 route del default table "$LEGACY_ROUTE_TABLE" >/dev/null 2>&1 || true
}

check_fw4_ready() {
  if ! nft list table inet fw4 >/dev/null 2>&1; then
    echo "OpenWrt firewall table inet fw4 is not active; start or reload the firewall service, then retry router_takeover_apply" >&2
    return 1
  fi
  for chain in dstnat mangle_prerouting forward input srcnat; do
    if ! nft list chain inet fw4 "$chain" >/dev/null 2>&1; then
      echo "OpenWrt firewall chain inet fw4 $chain is missing; start or reload the firewall service, then retry router_takeover_apply" >&2
      return 1
    fi
  done
}

wait_tun_ready() {
  i=0
  while [ "$i" -lt "$TUN_WAIT_SECONDS" ]; do
    if ip link show "$TUN_DEVICE" >/dev/null 2>&1; then
      ip link set "$TUN_DEVICE" up >/dev/null 2>&1 || true
      return 0
    fi
    i=$((i + 1))
    sleep 1
  done
  echo "TUN device $TUN_DEVICE is not ready after ${TUN_WAIT_SECONDS}s; call run_runtime and retry router_takeover_apply" >&2
  return 1
}

add_dynamic_localnetwork4() {
  ip -o -4 addr show scope global 2>/dev/null | awk '{print $4}' | sort -u | while read -r addr; do
    [ -n "$addr" ] && nft add element inet fw4 localclash_localnetwork { "$addr" } >/dev/null 2>&1 || true
  done
}

add_dynamic_localnetwork6() {
  ip -o -6 addr show scope global 2>/dev/null | awk '{print $4}' | sort -u | while read -r addr; do
    [ -n "$addr" ] && nft add element inet fw4 localclash_localnetwork6 { "$addr" } >/dev/null 2>&1 || true
  done
}

discover_lan_networks() {
  echo lan
  if command -v uci >/dev/null 2>&1; then
    i=0
    while zone_name=$(uci -q get "firewall.@zone[$i].name" 2>/dev/null); do
      if [ "$zone_name" = "lan" ]; then
        uci -q get "firewall.@zone[$i].network" 2>/dev/null || true
      fi
      i=$((i + 1))
    done
  fi
}

add_lan_domain() {
  domain=$(echo "$1" | sed 's#^/##; s#/$##; s#^\.*##')
  [ -n "$domain" ] && echo "$domain" >> "$STATE_DIR/local_domains"
}

discover_lan_domains() {
  : > "$STATE_DIR/local_domains"
  if command -v uci >/dev/null 2>&1; then
    add_lan_domain "$(uci -q get "dhcp.@dnsmasq[0].domain" 2>/dev/null || true)"
    add_lan_domain "$(uci -q get "dhcp.@dnsmasq[0].local" 2>/dev/null || true)"
  fi
  for file in /etc/resolv.conf /tmp/resolv.conf /tmp/resolv.conf.d/resolv.conf.auto; do
    [ -f "$file" ] || continue
    awk '/^search[[:space:]]/ { for (i = 2; i <= NF; i++) print $i }' "$file" 2>/dev/null | while read -r domain; do
      add_lan_domain "$domain"
    done
  done
  sort -u "$STATE_DIR/local_domains" > "$STATE_DIR/local_domains.tmp" 2>/dev/null && mv "$STATE_DIR/local_domains.tmp" "$STATE_DIR/local_domains" || true
}

add_localdns4_addr() {
  addr=${1%%/*}
  case "$addr" in
    ""|0.0.0.0|127.*|169.254.*) return 0 ;;
  esac
  nft add element inet fw4 localclash_localdns4 { "$addr" } >/dev/null 2>&1 || true
  echo "$addr" >> "$STATE_DIR/local_dns4"
}

add_localdns6_addr() {
  addr=${1%%/*}
  case "$addr" in
    ""|"::"|::1) return 0 ;;
  esac
  nft add element inet fw4 localclash_localdns6 { "$addr" } >/dev/null 2>&1 || true
  echo "$addr" >> "$STATE_DIR/local_dns6"
}

is_private_or_router_scope4() {
  case "$1" in
    10.*|192.168.*|172.1[6-9].*|172.2[0-9].*|172.3[0-1].*|100.6[4-9].*|100.[7-9][0-9].*|100.1[0-1][0-9].*|100.12[0-7].*|198.18.*|198.19.*) return 0 ;;
  esac
  return 1
}

is_router_scope6() {
  case "$1" in
    fc*|fd*|fe80:*) return 0 ;;
  esac
  return 1
}

add_network_interface_dns4_addresses() {
  net="$1"
  if command -v uci >/dev/null 2>&1; then
    for addr in $(uci -q get "network.$net.ipaddr" 2>/dev/null || true); do
      add_localdns4_addr "$addr"
    done
    for dev in $(uci -q get "network.$net.device" 2>/dev/null || true) $(uci -q get "network.$net.ifname" 2>/dev/null || true); do
      [ -n "$dev" ] || continue
      ip -o -4 addr show dev "$dev" scope global 2>/dev/null | awk '{print $4}' | while read -r addr; do
        add_localdns4_addr "$addr"
      done
    done
  fi
}

add_network_interface_dns6_addresses() {
  net="$1"
  if command -v uci >/dev/null 2>&1; then
    for addr in $(uci -q get "network.$net.ip6addr" 2>/dev/null || true); do
      add_localdns6_addr "$addr"
    done
    for dev in $(uci -q get "network.$net.device" 2>/dev/null || true) $(uci -q get "network.$net.ifname" 2>/dev/null || true); do
      [ -n "$dev" ] || continue
      ip -o -6 addr show dev "$dev" 2>/dev/null | awk '{print $4}' | while read -r addr; do
        add_localdns6_addr "$addr"
      done
    done
  fi
}

add_dynamic_localdns4() {
  : > "$STATE_DIR/local_dns4"
  for net in $(discover_lan_networks | sort -u); do
    add_network_interface_dns4_addresses "$net"
  done
  ip -o -4 addr show scope global 2>/dev/null | awk '{print $4}' | while read -r addr; do
    host=${addr%%/*}
    if is_private_or_router_scope4 "$host"; then
      add_localdns4_addr "$host"
    fi
  done
  sort -u "$STATE_DIR/local_dns4" > "$STATE_DIR/local_dns4.tmp" 2>/dev/null && mv "$STATE_DIR/local_dns4.tmp" "$STATE_DIR/local_dns4" || true
}

add_dynamic_localdns6() {
  : > "$STATE_DIR/local_dns6"
  for net in $(discover_lan_networks | sort -u); do
    add_network_interface_dns6_addresses "$net"
  done
  ip -o -6 addr show 2>/dev/null | awk '{print $4}' | while read -r addr; do
    host=${addr%%/*}
    if is_router_scope6 "$host"; then
      add_localdns6_addr "$host"
    fi
  done
  sort -u "$STATE_DIR/local_dns6" > "$STATE_DIR/local_dns6.tmp" 2>/dev/null && mv "$STATE_DIR/local_dns6.tmp" "$STATE_DIR/local_dns6" || true
}
`

const applyRoutingSetup = `
check_fw4_ready
cleanup_legacy_localclash_route_identity
trap 'cleanup_localclash_state' ERR
cleanup_localclash_state
wait_tun_ready
discover_lan_domains

while ip rule del fwmark "$FWMARK" table "$ROUTE_TABLE" >/dev/null 2>&1; do :; done
ip rule add fwmark "$FWMARK" table "$ROUTE_TABLE" pref "$RULE_PREF"
ip route replace default dev "$TUN_DEVICE" table "$ROUTE_TABLE"
while ip -6 rule del fwmark "$FWMARK" table "$ROUTE_TABLE" >/dev/null 2>&1; do :; done
ip -6 rule add fwmark "$FWMARK" table "$ROUTE_TABLE" pref "$RULE_PREF" >/dev/null 2>&1 || true
ip -6 route replace default dev "$TUN_DEVICE" table "$ROUTE_TABLE" >/dev/null 2>&1 || true
`

func applyBaseNftRules() []string {
	return []string{
		"add set inet fw4 localclash_localnetwork { type ipv4_addr; flags interval; auto-merge; }",
		"add element inet fw4 localclash_localnetwork { 0.0.0.0/8, 10.0.0.0/8, 100.64.0.0/10, 127.0.0.0/8, 169.254.0.0/16, 172.16.0.0/12, 192.168.0.0/16, 224.0.0.0/4, 240.0.0.0/4 }",
		"add set inet fw4 localclash_localdns4 { type ipv4_addr; flags interval; auto-merge; }",
		"add set inet fw4 localclash_localdns6 { type ipv6_addr; flags interval; auto-merge; }",
	}
}

func applyTakeoverNftRules() []string {
	return []string{
		"add chain inet fw4 localclash",
		"add rule inet fw4 localclash ip daddr @localclash_localnetwork counter return",
		"add rule inet fw4 localclash ct direction reply counter return",
		"add rule inet fw4 localclash ip protocol tcp counter redirect to $REDIR_PORT",
		"insert rule inet fw4 dstnat position 0 meta nfproto ipv4 ip protocol tcp counter jump localclash comment \"localClash TCP redirect\"",
		"add chain inet fw4 localclash_mangle",
		"add rule inet fw4 localclash_mangle meta l4proto { tcp, udp } iifname \"$TUN_DEVICE\" counter return",
		"add rule inet fw4 localclash_mangle ip daddr @localclash_localnetwork counter return",
		"add rule inet fw4 localclash_mangle ct direction reply counter return",
		"add rule inet fw4 localclash_mangle ip protocol udp mark set $FWMARK counter accept",
		"add rule inet fw4 localclash_mangle ip protocol icmp icmp type echo-request mark set $FWMARK counter accept comment \"localClash ICMP mark\"",
		"insert rule inet fw4 mangle_prerouting position 0 meta nfproto ipv4 counter jump localclash_mangle comment \"localClash TUN mark\"",
		"add chain inet fw4 localclash_dns_redirect",
		"add rule inet fw4 localclash_dns_redirect ip daddr @localclash_localdns4 counter return comment \"localClash local DNS bypass\"",
		"add rule inet fw4 localclash_dns_redirect ip6 daddr @localclash_localdns6 counter return comment \"localClash local DNS bypass\"",
		"add rule inet fw4 localclash_dns_redirect meta l4proto { tcp, udp } th dport 53 counter redirect to $DNS_PORT comment \"localClash DNS hijack\"",
		"insert rule inet fw4 dstnat position 0 meta l4proto { tcp, udp } th dport 53 counter jump localclash_dns_redirect comment \"localClash DNS hijack\"",
		"insert rule inet fw4 forward position 0 meta nfproto ipv4 oifname \"$TUN_DEVICE\" counter accept comment \"localClash TUN forward\"",
		"insert rule inet fw4 forward position 0 meta nfproto ipv4 iifname \"$TUN_DEVICE\" counter accept comment \"localClash TUN forward\"",
		"insert rule inet fw4 input position 0 meta nfproto ipv4 iifname \"$TUN_DEVICE\" counter accept comment \"localClash TUN input\"",
		"insert rule inet fw4 srcnat position 0 meta nfproto ipv4 oifname \"$TUN_DEVICE\" counter return comment \"localClash TUN postrouting\"",
	}
}

const applyPostNftScript = `
add_dynamic_localnetwork4

nft 'add set inet fw4 localclash_localnetwork6 { type ipv6_addr; flags interval; auto-merge; }' >/dev/null 2>&1 || true
nft 'add element inet fw4 localclash_localnetwork6 { ::/128, ::1/128, ::ffff:0:0/96, 64:ff9b::/96, 100::/64, 2001:db8::/32, fe80::/10, ff00::/8 }' >/dev/null 2>&1 || true
add_dynamic_localnetwork6
nft 'add chain inet fw4 nat_output { type nat hook output priority -1; }' >/dev/null 2>&1 || true
nft "insert rule inet fw4 nat_output position 0 meta l4proto { tcp, udp } th dport 53 ip daddr 127.0.0.1 counter redirect to $DNS_PORT comment \"localClash DNS hijack\""

nft 'add chain inet fw4 localclash_v6' >/dev/null 2>&1 || true
nft 'add rule inet fw4 localclash_v6 ip6 daddr @localclash_localnetwork6 counter return' >/dev/null 2>&1 || true
nft 'add rule inet fw4 localclash_v6 ct direction reply counter return' >/dev/null 2>&1 || true
nft add rule inet fw4 localclash_v6 ip6 nexthdr tcp counter redirect to "$REDIR_PORT" >/dev/null 2>&1 || true
nft "insert rule inet fw4 dstnat position 0 meta nfproto ipv6 ip6 nexthdr tcp counter jump localclash_v6 comment \"localClash IPv6 TCP redirect\"" >/dev/null 2>&1 || true

nft 'add chain inet fw4 localclash_mangle_v6' >/dev/null 2>&1 || true
nft add rule inet fw4 localclash_mangle_v6 meta l4proto { tcp, udp } iifname "$TUN_DEVICE" counter return >/dev/null 2>&1 || true
nft 'add rule inet fw4 localclash_mangle_v6 ip6 daddr @localclash_localnetwork6 counter return' >/dev/null 2>&1 || true
nft 'add rule inet fw4 localclash_mangle_v6 ct direction reply counter return' >/dev/null 2>&1 || true
nft add rule inet fw4 localclash_mangle_v6 ip6 nexthdr udp mark set "$FWMARK" counter accept >/dev/null 2>&1 || true
nft "add rule inet fw4 localclash_mangle_v6 ip6 nexthdr ipv6-icmp icmpv6 type echo-request mark set $FWMARK counter accept comment \"localClash ICMPv6 mark\"" >/dev/null 2>&1 || true
nft "insert rule inet fw4 mangle_prerouting position 0 meta nfproto ipv6 counter jump localclash_mangle_v6 comment \"localClash IPv6 TUN mark\"" >/dev/null 2>&1 || true
nft "insert rule inet fw4 forward position 0 meta nfproto ipv6 oifname \"$TUN_DEVICE\" counter accept comment \"localClash IPv6 TUN forward\"" >/dev/null 2>&1 || true
nft "insert rule inet fw4 forward position 0 meta nfproto ipv6 iifname \"$TUN_DEVICE\" counter accept comment \"localClash IPv6 TUN forward\"" >/dev/null 2>&1 || true
nft "insert rule inet fw4 input position 0 meta nfproto ipv6 iifname \"$TUN_DEVICE\" counter accept comment \"localClash IPv6 TUN input\"" >/dev/null 2>&1 || true
nft "insert rule inet fw4 srcnat position 0 meta nfproto ipv6 oifname \"$TUN_DEVICE\" counter return comment \"localClash IPv6 TUN postrouting\"" >/dev/null 2>&1 || true

printf 'applied\n' > "$STATE_DIR/status"
trap - ERR
`

func stopScript(opts Options) string {
	var script shellScriptBuilder
	script.line("set -eu")
	script.line("STATE_DIR=" + shellQuote(opts.StateDir))
	script.line("FWMARK=" + shellQuote(defaultFWMark))
	script.line("ROUTE_TABLE=" + shellQuote(defaultRouteTable))
	script.line("LEGACY_FWMARK=" + shellQuote(legacyFWMark))
	script.line("LEGACY_ROUTE_TABLE=" + shellQuote(legacyRouteTable))
	script.raw(stopShellBody)
	return script.String()
}

const stopShellBody = `
legacy_rules="$(ip rule show 2>/dev/null || true)"
legacy_routes="$(ip route show table "$LEGACY_ROUTE_TABLE" 2>/dev/null || true)"
legacy_rules6="$(ip -6 rule show 2>/dev/null || true)"
legacy_routes6="$(ip -6 route show table "$LEGACY_ROUTE_TABLE" 2>/dev/null || true)"
if printf '%s\n' "$legacy_rules" "$legacy_rules6" | grep -F "fwmark $LEGACY_FWMARK" >/dev/null 2>&1 || [ -n "$legacy_routes" ] || [ -n "$legacy_routes6" ]; then
  if [ "$(cat "$STATE_DIR/status" 2>/dev/null || true)" != "applied" ]; then
    echo "legacy localClash route identity $LEGACY_FWMARK/$LEGACY_ROUTE_TABLE exists without applied ownership state; refusing cleanup" >&2
    exit 1
  fi
  while ip rule del fwmark "$LEGACY_FWMARK" table "$LEGACY_ROUTE_TABLE" >/dev/null 2>&1; do :; done
  ip route del default table "$LEGACY_ROUTE_TABLE" >/dev/null 2>&1 || true
  while ip -6 rule del fwmark "$LEGACY_FWMARK" table "$LEGACY_ROUTE_TABLE" >/dev/null 2>&1; do :; done
  ip -6 route del default table "$LEGACY_ROUTE_TABLE" >/dev/null 2>&1 || true
fi
for chain in dstnat nat_output mangle_prerouting mangle_output forward input srcnat; do
  nft -a list chain inet fw4 "$chain" 2>/dev/null | awk '/localClash/{print $NF}' | sort -rn | while read -r handle; do
    [ -n "$handle" ] && nft delete rule inet fw4 "$chain" handle "$handle" 2>/dev/null || true
  done
done
for chain in localclash localclash_output localclash_mangle localclash_mangle_output localclash_v6 localclash_mangle_v6 localclash_dns_redirect; do
  nft flush chain inet fw4 "$chain" >/dev/null 2>&1 || true
  nft delete chain inet fw4 "$chain" >/dev/null 2>&1 || true
done
for set_name in localclash_localnetwork localclash_localnetwork6 localclash_localdns4 localclash_localdns6; do
  nft flush set inet fw4 "$set_name" >/dev/null 2>&1 || true
  nft delete set inet fw4 "$set_name" >/dev/null 2>&1 || true
done
while ip rule del fwmark "$FWMARK" table "$ROUTE_TABLE" >/dev/null 2>&1; do :; done
ip route del default table "$ROUTE_TABLE" >/dev/null 2>&1 || true
while ip -6 rule del fwmark "$FWMARK" table "$ROUTE_TABLE" >/dev/null 2>&1; do :; done
ip -6 route del default table "$ROUTE_TABLE" >/dev/null 2>&1 || true
rm -f "$STATE_DIR/status" "$STATE_DIR/local_dns4" "$STATE_DIR/local_dns6" "$STATE_DIR/local_domains" "$STATE_DIR/local_dns4.tmp" "$STATE_DIR/local_dns6.tmp" "$STATE_DIR/local_domains.tmp" >/dev/null 2>&1 || true
`

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}
