package chatgptavailable

import (
	"errors"
	"fmt"
	"net/netip"
	"os"
	"slices"
	"strings"

	"google.golang.org/protobuf/encoding/protowire"
)

// Use only the candidate's own server. A dialer is an intermediate hop.
// Do not resolve hostnames: the resolver used by the proxy may see other IPs.
func candidateAddress(candidate Candidate) (string, netip.Addr) {
	server := strings.TrimSpace(stringValue(candidate.Proxy["server"]))
	if server == "" {
		return "unknown", netip.Addr{}
	}
	addr, err := netip.ParseAddr(server)
	if err != nil {
		return "hostname", netip.Addr{}
	}
	return "ip", addr.Unmap()
}

func countryCode(code string) bool {
	return len(code) == 2 && code[0] >= 'A' && code[0] <= 'Z' && code[1] >= 'A' && code[1] <= 'Z'
}

// geoipCountries scans Mihomo's GeoIPList once per diagnostic run. Keep only
// matches for this subscription, rather than expanding every CIDR into a trie.
func geoipCountries(path string, candidates []Candidate) (map[netip.Addr]string, error) {
	var addresses []netip.Addr
	for _, candidate := range candidates {
		if _, addr := candidateAddress(candidate); addr.IsValid() {
			addresses = append(addresses, addr)
		}
	}
	if len(addresses) == 0 {
		return nil, nil
	}
	slices.SortFunc(addresses, netip.Addr.Compare)
	addresses = slices.Compact(addresses)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	found := make(map[netip.Addr]string)
	var countries int
	list := geoFields{remaining: data}
	for list.next() {
		if list.number != 1 || list.kind != protowire.BytesType {
			continue
		}
		// Protobuf permits fields in any order. Read the code and inverse flag
		// before interpreting this entry's repeated CIDRs.
		entry := geoFields{remaining: list.bytes}
		var code string
		var inverse bool
		for entry.next() {
			switch {
			case entry.number == 1 && entry.kind == protowire.BytesType:
				code = strings.ToUpper(string(entry.bytes))
			case entry.number == 3 && entry.kind == protowire.VarintType:
				inverse = entry.value != 0
			}
		}
		if entry.err != nil {
			return nil, entry.err
		}
		if inverse || !countryCode(code) {
			continue
		}
		countries++
		entry = geoFields{remaining: list.bytes}
		for entry.next() {
			if entry.number != 2 || entry.kind != protowire.BytesType {
				continue
			}
			prefix, err := geoPrefix(entry.bytes)
			if err != nil {
				return nil, err
			}
			start, _ := slices.BinarySearchFunc(addresses, prefix.Addr(), netip.Addr.Compare)
			for _, addr := range addresses[start:] {
				if !prefix.Contains(addr) {
					break
				}
				old, exists := found[addr]
				if exists && old != code {
					found[addr] = "" // Multiple country sets match; keep the prediction unknown.
				} else {
					found[addr] = code
				}
			}
		}
	}
	if list.err != nil {
		return nil, list.err
	}
	if countries == 0 {
		return nil, errors.New("geoip.dat contains no country entries")
	}
	return found, nil
}

func geoPrefix(data []byte) (netip.Prefix, error) {
	fields := geoFields{remaining: data}
	var addr netip.Addr
	var bits uint64
	for fields.next() {
		switch {
		case fields.number == 1 && fields.kind == protowire.BytesType:
			addr, _ = netip.AddrFromSlice(fields.bytes)
		case fields.number == 2 && fields.kind == protowire.VarintType:
			bits = fields.value
		}
	}
	if fields.err != nil {
		return netip.Prefix{}, fields.err
	}
	if !addr.IsValid() || bits > uint64(addr.BitLen()) {
		return netip.Prefix{}, fmt.Errorf("invalid CIDR in geoip.dat")
	}
	return netip.PrefixFrom(addr, int(bits)).Masked(), nil
}

// Wire fields of Mihomo's GeoIPList, GeoIP, and CIDR messages. Bytes alias the
// file buffer, so scanning the database doesn't allocate a Go object per prefix.
type geoFields struct {
	remaining []byte
	number    protowire.Number
	kind      protowire.Type
	bytes     []byte
	value     uint64
	err       error
}

func (f *geoFields) next() bool {
	if len(f.remaining) == 0 || f.err != nil {
		return false
	}
	number, kind, n := protowire.ConsumeTag(f.remaining)
	if n < 0 {
		f.err = protowire.ParseError(n)
		return false
	}
	f.number, f.kind = number, kind
	body := f.remaining[n:]
	switch kind {
	case protowire.BytesType:
		f.bytes, n = protowire.ConsumeBytes(body)
	case protowire.VarintType:
		f.value, n = protowire.ConsumeVarint(body)
	default:
		n = protowire.ConsumeFieldValue(number, kind, body)
	}
	if n < 0 {
		f.err = protowire.ParseError(n)
		return false
	}
	f.remaining = body[n:]
	return true
}
