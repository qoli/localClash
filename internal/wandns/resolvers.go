package wandns

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"time"
)

const (
	DefaultResolvPath  = "/tmp/resolv.conf.d/resolv.conf.auto"
	ModeWAN            = "wan"
	ModeAliDNSFallback = "alidns_fallback"
	AliDNSEndpoint     = "https://dns.alidns.com/dns-query"
)

type Resolver struct {
	Address   string `json:"address"`
	Interface string `json:"interface"`
}

type Selection struct {
	Mode           string     `json:"mode"`
	Scope          string     `json:"scope"`
	Endpoints      []string   `json:"endpoints"`
	WANResolvers   []Resolver `json:"wan_resolvers,omitempty"`
	FallbackReason string     `json:"fallback_reason,omitempty"`
	ResolvPath     string     `json:"resolv_path"`
}

type ProbeFunc func(address string) error

func Discover(path string) ([]Resolver, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		path = DefaultResolvPath
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("read WAN resolver provenance %s: %w", path, err)
	}
	defer file.Close()

	currentInterface := ""
	resolvers := []Resolver{}
	seen := map[string]bool{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "# Interface ") {
			currentInterface = strings.TrimSpace(strings.TrimPrefix(line, "# Interface "))
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 2 || fields[0] != "nameserver" {
			continue
		}
		ip := net.ParseIP(fields[1])
		if ip == nil || ip.IsUnspecified() || ip.IsLoopback() || ip.IsMulticast() || ip.IsLinkLocalUnicast() {
			return nil, fmt.Errorf("invalid WAN nameserver %q in %s", fields[1], path)
		}
		key := currentInterface + "|" + ip.String()
		if seen[key] {
			continue
		}
		seen[key] = true
		resolvers = append(resolvers, Resolver{
			Address:   ip.String(),
			Interface: currentInterface,
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan WAN resolver provenance %s: %w", path, err)
	}
	if len(resolvers) == 0 {
		return nil, fmt.Errorf("no WAN-interface nameservers found in %s", path)
	}
	return resolvers, nil
}

// Select implements the explicitly approved base contract: confirmed, responsive
// WAN resolvers are preferred; AliDNS is used only when no WAN resolver can be
// confirmed and complete a basic DNS exchange.
func Select(path string, probe ProbeFunc) Selection {
	path = strings.TrimSpace(path)
	if path == "" {
		path = DefaultResolvPath
	}
	resolvers, err := Discover(path)
	if err != nil {
		return aliDNSFallback(path, err)
	}
	if probe == nil {
		probe = Probe
	}
	confirmed := make([]Resolver, 0, len(resolvers))
	failures := make([]string, 0, len(resolvers))
	for _, resolver := range resolvers {
		if strings.TrimSpace(resolver.Interface) == "" {
			failures = append(failures, resolver.Address+": missing interface provenance")
			continue
		}
		if err := probe(resolver.Address); err != nil {
			failures = append(failures, resolver.Interface+"/"+resolver.Address+": "+err.Error())
			continue
		}
		confirmed = append(confirmed, resolver)
	}
	if len(confirmed) == 0 {
		reason := "no confirmed responsive WAN resolver"
		if len(failures) > 0 {
			reason += ": " + strings.Join(failures, "; ")
		}
		return aliDNSFallback(path, errors.New(reason))
	}
	endpoints := make([]string, 0, len(confirmed))
	seen := map[string]bool{}
	for _, resolver := range confirmed {
		if !seen[resolver.Address] {
			seen[resolver.Address] = true
			endpoints = append(endpoints, resolver.Address)
		}
	}
	return Selection{
		Mode: ModeWAN, Scope: "geosite:cn", Endpoints: endpoints,
		WANResolvers: confirmed, ResolvPath: path,
	}
}

func Probe(address string) error {
	ip := net.ParseIP(strings.TrimSpace(address))
	if ip == nil {
		return fmt.Errorf("invalid resolver address %q", address)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	resolver := &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var dialer net.Dialer
			return dialer.DialContext(ctx, "udp", net.JoinHostPort(ip.String(), "53"))
		},
	}
	addresses, err := resolver.LookupHost(ctx, "www.qq.com")
	if err != nil {
		return fmt.Errorf("basic DNS exchange failed: %w", err)
	}
	if len(addresses) == 0 {
		return errors.New("basic DNS exchange returned no addresses")
	}
	return nil
}

func aliDNSFallback(path string, cause error) Selection {
	return Selection{
		Mode: ModeAliDNSFallback, Scope: "geosite:cn",
		Endpoints: []string{AliDNSEndpoint}, FallbackReason: cause.Error(),
		ResolvPath: path,
	}
}
