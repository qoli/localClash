package chatgptavailable

import (
	"encoding/json"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"google.golang.org/protobuf/encoding/protowire"
)

func testGeoEntry(code, prefix string, inverse bool) []byte {
	p := netip.MustParsePrefix(prefix)
	cidr := protowire.AppendBytes(protowire.AppendTag(nil, 1, protowire.BytesType), p.Addr().AsSlice())
	cidr = protowire.AppendVarint(protowire.AppendTag(cidr, 2, protowire.VarintType), uint64(p.Bits()))
	// Place country and reverse flag after the CIDR to exercise arbitrary field order.
	entry := protowire.AppendBytes(protowire.AppendTag(nil, 2, protowire.BytesType), cidr)
	entry = protowire.AppendString(protowire.AppendTag(entry, 1, protowire.BytesType), code)
	if inverse {
		entry = protowire.AppendVarint(protowire.AppendTag(entry, 3, protowire.VarintType), 1)
	}
	return protowire.AppendBytes(protowire.AppendTag(nil, 1, protowire.BytesType), entry)
}

func TestGeoIPShadowUsesFinalLiteralWithoutChangingMeasuredCountry(t *testing.T) {
	var data []byte
	for _, e := range [][]byte{
		testGeoEntry("ru", "192.0.2.0/24", false), testGeoEntry("SE", "192.0.2.128/25", false),
		testGeoEntry("SE", "2001:db8::/32", false), testGeoEntry("TEST", "192.0.2.0/24", false),
		testGeoEntry("US", "192.0.2.0/24", true),
	} {
		data = append(data, e...)
	}
	candidates := []Candidate{
		{Proxy: map[string]any{"server": "192.0.2.1", "dialer-proxy": "private-dialer"}},
		{Proxy: map[string]any{"server": "192.0.2.128"}},
		{Proxy: map[string]any{"server": "2001:db8::1"}},
		{Proxy: map[string]any{"server": "private-host.invalid"}},
		{}, {Proxy: map[string]any{"server": "198.51.100.1"}},
	}
	path := filepath.Join(t.TempDir(), "geoip.dat")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	countries, err := geoipCountries(path, candidates)
	if err != nil {
		t.Fatal(err)
	}
	for i, want := range []struct{ kind, code, prediction string }{
		{"ip", "RU", "unsupported"}, {"ip", "", "unknown"}, {"ip", "SE", "supported"},
		{"hostname", "", "unknown"}, {"unknown", "", "unknown"}, {"ip", "", "unknown"},
	} {
		ev := candidateTiming(candidates[i], countries, Observation{Available: true, StatsigCountry: "SE"})
		if ev.AddressKind != want.kind || ev.PredictedCountry != want.code || ev.GeoIPPrediction != want.prediction ||
			ev.Chained != (i == 0) || ev.StatsigCountry != "SE" || ev.Outcome != "qualified" {
			t.Errorf("candidate %d: %+v", i, ev)
		}
		raw, err := json.Marshal(ev)
		if err != nil || strings.Contains(string(raw), "private-") || strings.Contains(string(raw), "192.0.2.") {
			t.Fatalf("shadow event contains a raw identifier: %v", err)
		}
	}
	// A damaged update must discard any partial matches.
	if err := os.WriteFile(path, append(data, 0xff), 0600); err != nil {
		t.Fatal(err)
	}
	if partial, err := geoipCountries(path, candidates); err == nil || partial != nil {
		t.Fatalf("invalid database returned %v, %v", partial, err)
	}
}
