package wandns

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiscoverPreservesInterfaceProvenanceAndOrder(t *testing.T) {
	path := writeResolvFile(t, `# Interface wan
nameserver 202.96.134.133
nameserver 202.96.128.166
# Interface wan6
nameserver 240e:1f:1::1
nameserver 202.96.134.133
`)
	got, err := Discover(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 {
		t.Fatalf("resolvers = %+v, want four interface-scoped entries", got)
	}
	if got[0].Address != "202.96.134.133" || got[0].Interface != "wan" {
		t.Fatalf("first resolver = %+v, want wan/202.96.134.133", got[0])
	}
	if got[2].Address != "240e:1f:1::1" || got[2].Interface != "wan6" {
		t.Fatalf("third resolver = %+v, want wan6/240e:1f:1::1", got[2])
	}
}

func TestDiscoverRejectsLoopbackResolver(t *testing.T) {
	path := writeResolvFile(t, "nameserver 127.0.0.1\n")
	_, err := Discover(path)
	if err == nil || !strings.Contains(err.Error(), `invalid WAN nameserver "127.0.0.1"`) {
		t.Fatalf("Discover error = %v, want explicit loopback rejection", err)
	}
}

func TestDiscoverRejectsMissingNameserver(t *testing.T) {
	path := writeResolvFile(t, "# Interface wan\nsearch lan\n")
	_, err := Discover(path)
	if err == nil || !strings.Contains(err.Error(), "no WAN-interface nameservers found") {
		t.Fatalf("Discover error = %v, want explicit missing nameserver failure", err)
	}
}

func TestSelectUsesConfirmedResponsiveWANResolvers(t *testing.T) {
	path := writeResolvFile(t, "# Interface wan\nnameserver 192.0.2.53\n# Interface wan6\nnameserver 2001:db8::53\n")
	selection := Select(path, func(address string) error {
		if address == "2001:db8::53" {
			return os.ErrDeadlineExceeded
		}
		return nil
	})
	if selection.Mode != ModeWAN || len(selection.Endpoints) != 1 || selection.Endpoints[0] != "192.0.2.53" {
		t.Fatalf("selection = %+v, want responsive WAN resolver only", selection)
	}
	if selection.FallbackReason != "" {
		t.Fatalf("WAN selection has fallback reason %q", selection.FallbackReason)
	}
}

func TestSelectFallsBackToAliDNSOnlyWhenWANCannotBeConfirmedOrUsed(t *testing.T) {
	unconfirmed := writeResolvFile(t, "nameserver 192.0.2.53\n")
	selection := Select(unconfirmed, func(string) error { return nil })
	if selection.Mode != ModeAliDNSFallback || len(selection.Endpoints) != 1 || selection.Endpoints[0] != AliDNSEndpoint {
		t.Fatalf("unconfirmed selection = %+v", selection)
	}
	if !strings.Contains(selection.FallbackReason, "missing interface provenance") {
		t.Fatalf("fallback reason = %q", selection.FallbackReason)
	}

	unresponsive := writeResolvFile(t, "# Interface wan\nnameserver 192.0.2.53\n")
	selection = Select(unresponsive, func(string) error { return os.ErrDeadlineExceeded })
	if selection.Mode != ModeAliDNSFallback || !strings.Contains(selection.FallbackReason, "timeout") {
		t.Fatalf("unresponsive selection = %+v", selection)
	}
}

func writeResolvFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "resolv.conf.auto")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}
