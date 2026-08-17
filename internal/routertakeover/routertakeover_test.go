package routertakeover

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"localclash/internal/runtimeprofile"
)

func TestStatusUsesOneBoundedExactChainObservation(t *testing.T) {
	dir := t.TempDir()
	profilePath := filepath.Join(dir, runtimeprofile.DefaultPath)
	if _, err := runtimeprofile.Configure(profilePath, runtimeprofile.ModeRouter, runtimeprofile.CoreSmart); err != nil {
		t.Fatal(err)
	}
	calls := 0
	var script string
	runner := func(_ context.Context, command string) (string, error) {
		calls++
		script = command
		lines := make([]string, 0, len(statusObservationIDs))
		for _, id := range statusObservationIDs {
			lines = append(lines, id+"=1")
		}
		return strings.Join(lines, "\n"), nil
	}

	result, err := statusWithRunner(context.Background(), Options{RuntimeProfile: profilePath}, runner)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("status observation runner calls = %d, want 1", calls)
	}
	if strings.Contains(script, "nft list ruleset") {
		t.Fatalf("status observation must not scan the global nft ruleset:\n%s", script)
	}
	for _, want := range []string{
		"nft list chain inet fw4 dstnat",
		"nft list chain inet fw4 mangle_prerouting",
		"nft list chain inet fw4 localclash_dns_redirect",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("status observation missing exact-chain probe %q:\n%s", want, script)
		}
	}
	seen := map[string]bool{}
	for _, item := range result.Checks {
		seen[item.ID] = item.OK
	}
	for _, id := range statusObservationIDs {
		if !seen[id] {
			t.Fatalf("check %q not populated from observation: %+v", id, result.Checks)
		}
	}
}

func TestParseStatusObservationRejectsIncompleteOrMalformedOutput(t *testing.T) {
	validLines := make([]string, 0, len(statusObservationIDs))
	for _, id := range statusObservationIDs {
		validLines = append(validLines, id+"=1")
	}
	tests := []struct {
		name   string
		output string
	}{
		{name: "missing", output: strings.Join(validLines[:len(validLines)-1], "\n")},
		{name: "unknown", output: strings.Join(append(append([]string{}, validLines...), "other=1"), "\n")},
		{name: "bad value", output: strings.Replace(strings.Join(validLines, "\n"), "fw4_available=1", "fw4_available=yes", 1)},
		{name: "duplicate", output: strings.Join(append(append([]string{}, validLines...), validLines[0]), "\n")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := parseStatusObservation(test.output); err == nil {
				t.Fatalf("parseStatusObservation(%q) succeeded, want error", test.output)
			}
		})
	}
}

func TestStatusObservationHonorsCallerDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err := observeStatus(ctx, Options{TunDevice: "utun"}, func(ctx context.Context, _ string) (string, error) {
		<-ctx.Done()
		return "", ctx.Err()
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("observeStatus error = %v, want context deadline exceeded", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("caller deadline was not honored promptly: %s", elapsed)
	}
}

func TestDefaultRunnerPreservesDeadlineCause(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := defaultRunner(ctx, "sleep 2")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("defaultRunner error = %v, want context deadline exceeded", err)
	}
}

func TestStatusObservationScriptHasShellSyntax(t *testing.T) {
	cmd := exec.Command("/bin/sh", "-n")
	cmd.Stdin = strings.NewReader(statusObservationScript(Options{TunDevice: "utun"}))
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("status observation script has syntax error: %v\n%s", err, output)
	}
}

// NOTE: These Go tests are not a functional acceptance gate for router takeover.
// Any behavior change in this package must also be exercised in the Docker
// OpenWrt environment; do not treat go test alone as enough validation.
func TestApplyDryRunBuildsLocalClashOwnedScript(t *testing.T) {
	dir := t.TempDir()
	profilePath := filepath.Join(dir, runtimeprofile.DefaultPath)
	if _, err := runtimeprofile.Configure(profilePath, runtimeprofile.ModeRouter, runtimeprofile.CoreSmart); err != nil {
		t.Fatal(err)
	}

	result, err := Apply(context.Background(), Options{
		RuntimeProfile: profilePath,
		DryRun:         true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.DryRun {
		t.Fatal("dry-run apply should not execute takeover")
	}
	for _, want := range []string{"localclash_mangle", "localClash DNS hijack", "ip rule add fwmark", "router_takeover_apply without dry_run"} {
		if !strings.Contains(result.Script+strings.Join(result.NextActions, " "), want) {
			t.Fatalf("dry-run output missing %q:\n%s\n%v", want, result.Script, result.NextActions)
		}
	}
	if !strings.Contains(result.Script, `comment "localClash TCP redirect"`) {
		t.Fatalf("nft comments must be quoted for nft parser, got:\n%s", result.Script)
	}
	for _, forbidden := range []string{"uci set", "uci add", "uci delete", "uci commit", "/etc/config/firewall", "/var/etc/localclash", "fw4 reload"} {
		if strings.Contains(result.Script, forbidden) {
			t.Fatalf("router takeover must not persist firewall config; found %q in:\n%s", forbidden, result.Script)
		}
	}
	for _, want := range []string{"STATE_DIR='/tmp/localclash/router-takeover'", "wait_tun_ready()", "trap 'cleanup_localclash_state' ERR", "nft -f - <<EOF_NFT"} {
		if !strings.Contains(result.Script, want) {
			t.Fatalf("runtime takeover script missing %q:\n%s", want, result.Script)
		}
	}
	for _, want := range []string{"check_fw4_ready()", "firewall table inet fw4 is not active", "firewall chain inet fw4 $chain is missing"} {
		if !strings.Contains(result.Script, want) {
			t.Fatalf("runtime takeover script missing firewall preflight %q:\n%s", want, result.Script)
		}
	}
	for _, want := range []string{"add_dynamic_localnetwork4()", "add_dynamic_localnetwork6()", "ip -o -4 addr show scope global", "ip -o -6 addr show scope global"} {
		if !strings.Contains(result.Script, want) {
			t.Fatalf("runtime takeover script missing dynamic localnetwork refresh %q:\n%s", want, result.Script)
		}
	}
	for _, want := range []string{"discover_lan_networks()", "discover_lan_domains()", "add_dynamic_localdns4()", "add_dynamic_localdns6()", "localclash_dns_redirect", "localClash local DNS bypass"} {
		if !strings.Contains(result.Script, want) {
			t.Fatalf("runtime takeover script missing local DNS preservation %q:\n%s", want, result.Script)
		}
	}
	dnsBypass := strings.Index(result.Script, `comment "localClash local DNS bypass"`)
	dnsRedirect := strings.Index(result.Script, `redirect to $DNS_PORT comment "localClash DNS hijack"`)
	if dnsBypass < 0 || dnsRedirect < 0 || dnsBypass > dnsRedirect {
		t.Fatalf("local DNS bypass must be installed before DNS hijack redirect:\n%s", result.Script)
	}
	for _, line := range strings.Split(result.Script, "\n") {
		if strings.Contains(line, "localclash_dns_redirect") && strings.Contains(line, "redirect to $DNS_PORT") {
			if !strings.Contains(line, "meta l4proto") || !strings.Contains(line, "th dport 53") {
				t.Fatalf("DNS redirect chain rule must carry its own transport match for nft type checking:\n%s", line)
			}
		}
	}
	if strings.Contains(result.Script, "OpenClash") {
		t.Fatalf("router takeover script should not special-case OpenClash:\n%s", result.Script)
	}
}

func TestApplyDryRunScriptHasShellSyntax(t *testing.T) {
	dir := t.TempDir()
	profilePath := filepath.Join(dir, runtimeprofile.DefaultPath)
	if _, err := runtimeprofile.Configure(profilePath, runtimeprofile.ModeRouter, runtimeprofile.CoreSmart); err != nil {
		t.Fatal(err)
	}
	result, err := Apply(context.Background(), Options{
		RuntimeProfile: profilePath,
		DryRun:         true,
	})
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("/bin/sh", "-n")
	cmd.Stdin = strings.NewReader(result.Script)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated shell script has syntax error: %v\n%s\nscript:\n%s", err, output, result.Script)
	}
}

func TestBaseResultReportsLocalDNSState(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "local_dns4"), []byte("192.168.6.1\n192.168.6.1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "local_dns6"), []byte("fd00::1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "local_domains"), []byte("lan\nlocal\nlan\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	result := baseResult(Options{
		StateDir:  dir,
		DNSPort:   7874,
		RedirPort: 7892,
		TunDevice: "utun",
	}, runtimeprofile.Status{Mode: runtimeprofile.ModeRouter})

	if got := strings.Join(result.LocalDNS, ","); got != "192.168.6.1,fd00::1" {
		t.Fatalf("LocalDNS = %q", got)
	}
	if got := strings.Join(result.LocalDomains, ","); got != "lan,local" {
		t.Fatalf("LocalDomains = %q", got)
	}
}

func TestApplyRejectsNormalProfileBeforeSystemChanges(t *testing.T) {
	dir := t.TempDir()
	profilePath := filepath.Join(dir, runtimeprofile.DefaultPath)
	if _, err := runtimeprofile.Configure(profilePath, runtimeprofile.ModeNormal, runtimeprofile.CoreMeta); err != nil {
		t.Fatal(err)
	}

	result, err := Apply(context.Background(), Options{RuntimeProfile: profilePath})
	if err != nil {
		t.Fatal(err)
	}
	if result.Applied {
		t.Fatal("normal profile must not apply router takeover")
	}
	if len(result.Checks) == 0 || result.Checks[0].ID != "profile_router" || result.Checks[0].OK {
		t.Fatalf("checks = %+v, want profile_router failure", result.Checks)
	}
}

func TestApplyDryRunRejectsNormalProfile(t *testing.T) {
	dir := t.TempDir()
	profilePath := filepath.Join(dir, runtimeprofile.DefaultPath)
	if _, err := runtimeprofile.Configure(profilePath, runtimeprofile.ModeNormal, runtimeprofile.CoreMeta); err != nil {
		t.Fatal(err)
	}

	result, err := Apply(context.Background(), Options{
		RuntimeProfile: profilePath,
		DryRun:         true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Script != "" {
		t.Fatalf("normal profile dry-run should not expose takeover script:\n%s", result.Script)
	}
	if len(result.Checks) == 0 || result.Checks[0].ID != "profile_router" || result.Checks[0].OK {
		t.Fatalf("checks = %+v, want profile_router failure", result.Checks)
	}
}
