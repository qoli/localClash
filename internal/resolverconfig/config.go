package resolverconfig

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	Version            = 2
	DefaultName        = "dnsqualify.json"
	ModeDNSQualify     = "qualified_ecs"
	ScopeTypeDomains   = "domains"
	ECSSTUNMainland    = "stun_xor_mapped_address_mainland"
	ECSHTTPSIPAPIIS    = "https_json_ipapi_is"
	MainlandSTUNServer = "stun.chat.bilibili.com:3478"
	IPAPIISServer      = "api.ipapi.is:443"
	DNSProxyGroup      = "DNSProxy"
)

var ErrExpired = errors.New("resolver qualification expired")

type Status struct {
	Enabled   bool   `json:"enabled"`
	State     string `json:"state"`
	Reason    string `json:"reason,omitempty"`
	ExpiresAt string `json:"expires_at,omitempty"`
}

type LoadResult struct {
	Config *Config
	Status Status
}

type Config struct {
	Version     int         `json:"version"`
	Scope       Scope       `json:"scope"`
	Resolver    Resolver    `json:"resolver"`
	ECS         ECS         `json:"ecs"`
	Measurement Measurement `json:"measurement"`
}

type Scope struct {
	Type         string   `json:"type"`
	ID           string   `json:"id"`
	Domains      []string `json:"domains"`
	DomainSHA256 string   `json:"domain_sha256"`
}

type Resolver struct {
	CandidateID string `json:"candidate_id"`
	Source      string `json:"source"`
	Transport   string `json:"transport"`
	Endpoint    string `json:"endpoint"`
	Proxy       string `json:"proxy"`
	Interface   string `json:"interface,omitempty"`
}

type ECS struct {
	Prefix      string `json:"prefix"`
	Source      string `json:"source"`
	Interface   string `json:"interface"`
	Server      string `json:"server"`
	ServerIP    string `json:"server_ip"`
	CountryCode string `json:"country_code,omitempty"`
}

type Measurement struct {
	ReportSHA256     string `json:"report_sha256"`
	ReportFinishedAt string `json:"report_finished_at"`
	ResolvPath       string `json:"resolv_path"`
	GeneratedAt      string `json:"generated_at"`
	ExpiresAt        string `json:"expires_at"`
}

func DefaultPath(runtimeProfilePath string) string {
	return filepath.Join(filepath.Dir(runtimeProfilePath), DefaultName)
}

// Load treats a missing file as the optional optimization being disabled. An
// existing file is authoritative and must satisfy the complete v2 contract.
func Load(path string) (LoadResult, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return LoadResult{Status: Status{State: "disabled", Reason: "missing"}}, nil
	}
	if err != nil {
		return LoadResult{}, fmt.Errorf("read resolver config %s: %w", path, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var config Config
	if err := decoder.Decode(&config); err != nil {
		return LoadResult{}, fmt.Errorf("decode resolver config %s: %w", path, err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return LoadResult{}, fmt.Errorf("decode resolver config %s: unexpected trailing JSON value", path)
		}
		return LoadResult{}, fmt.Errorf("decode resolver config %s trailing data: %w", path, err)
	}
	if err := ValidateAt(config, time.Now()); err != nil {
		if errors.Is(err, ErrExpired) {
			return LoadResult{Status: Status{State: "disabled", Reason: "expired", ExpiresAt: config.Measurement.ExpiresAt}}, nil
		}
		return LoadResult{}, fmt.Errorf("validate resolver config %s: %w", path, err)
	}
	return LoadResult{Config: &config, Status: Status{Enabled: true, State: "active", ExpiresAt: config.Measurement.ExpiresAt}}, nil
}

func Validate(config Config) error {
	return ValidateAt(config, time.Now())
}

func ValidateAt(config Config, now time.Time) error {
	if config.Version != Version {
		return fmt.Errorf("unsupported resolver config version %d; want %d", config.Version, Version)
	}
	if config.Scope.Type != ScopeTypeDomains || strings.TrimSpace(config.Scope.ID) == "" {
		return errors.New("resolver scope type must be domains and id is required")
	}
	domains, err := canonicalDomains(config.Scope.Domains)
	if err != nil {
		return err
	}
	if !equalStrings(domains, config.Scope.Domains) {
		return errors.New("resolver scope domains must be lowercase, unique, and sorted")
	}
	if hashDomains(domains) != strings.ToLower(config.Scope.DomainSHA256) {
		return errors.New("resolver scope domain_sha256 does not match domains")
	}
	if strings.TrimSpace(config.Resolver.CandidateID) == "" || strings.TrimSpace(config.Resolver.Source) == "" {
		return errors.New("resolver candidate_id and source are required")
	}
	if config.Resolver.Transport != "doh" {
		return errors.New("qualified ECS resolver transport must be doh")
	}
	endpoint, err := url.Parse(config.Resolver.Endpoint)
	if err != nil || endpoint.Scheme != "https" || endpoint.Hostname() == "" {
		return errors.New("qualified ECS resolver endpoint must be an HTTPS URL")
	}
	if endpoint.RawQuery != "" || endpoint.Fragment != "" {
		return errors.New("qualified ECS resolver endpoint must not contain query or fragment parameters")
	}
	if config.Resolver.Proxy != DNSProxyGroup {
		return fmt.Errorf("qualified ECS resolver proxy must be %q", DNSProxyGroup)
	}
	if strings.TrimSpace(config.ECS.Interface) == "" {
		return errors.New("ECS WAN interface provenance is required")
	}
	serverIP, serverIPErr := netip.ParseAddr(config.ECS.ServerIP)
	if serverIPErr != nil || !serverIP.Is4() || !isPublicECSAddress(serverIP) {
		return errors.New("ECS public-address observation server IP is invalid")
	}
	if err := validateECSProvenance(config.ECS.Source, config.ECS.Server, config.ECS.CountryCode); err != nil {
		return err
	}
	prefix, err := netip.ParsePrefix(config.ECS.Prefix)
	if err != nil || prefix != prefix.Masked() {
		return errors.New("ECS prefix must be a masked IP prefix")
	}
	if !isPublicECSAddress(prefix.Addr()) {
		return errors.New("ECS prefix must be publicly routable")
	}
	if prefix.Addr().Is4() {
		if prefix.Bits() <= 0 || prefix.Bits() > 24 {
			return errors.New("IPv4 ECS prefix must disclose no more than 24 bits")
		}
	} else if prefix.Bits() <= 0 || prefix.Bits() > 56 {
		return errors.New("IPv6 ECS prefix must disclose no more than 56 bits")
	}
	if !isSHA256(config.Measurement.ReportSHA256) || strings.TrimSpace(config.Measurement.ResolvPath) == "" {
		return errors.New("resolver measurement provenance is incomplete")
	}
	finished, err := parseTimestamp("report_finished_at", config.Measurement.ReportFinishedAt)
	if err != nil {
		return err
	}
	generated, err := parseTimestamp("generated_at", config.Measurement.GeneratedAt)
	if err != nil {
		return err
	}
	expires, err := parseTimestamp("expires_at", config.Measurement.ExpiresAt)
	if err != nil {
		return err
	}
	if generated.Before(finished) {
		return errors.New("generated_at must not precede report_finished_at")
	}
	if !expires.After(generated) {
		return errors.New("expires_at must be later than generated_at")
	}
	if !now.Before(expires) {
		return fmt.Errorf("%w at %s", ErrExpired, expires.Format(time.RFC3339Nano))
	}
	return nil
}

func validateECSProvenance(source, server, countryCode string) error {
	switch source {
	case ECSSTUNMainland:
		if server != MainlandSTUNServer {
			return errors.New("ECS mainland STUN server provenance is invalid")
		}
		if countryCode != "" {
			return errors.New("ECS STUN provenance must not claim an unmeasured country code")
		}
	case ECSHTTPSIPAPIIS:
		if server != IPAPIISServer {
			return errors.New("ECS ipapi.is HTTPS server provenance is invalid")
		}
		if countryCode != "CN" {
			return fmt.Errorf("ECS ipapi.is country code must be CN, got %q", countryCode)
		}
	default:
		return fmt.Errorf("unsupported ECS public-address observation source %q", source)
	}
	return nil
}

func isPublicECSAddress(address netip.Addr) bool {
	if !address.IsGlobalUnicast() || address.IsPrivate() {
		return false
	}
	for _, special := range []netip.Prefix{
		netip.MustParsePrefix("0.0.0.0/8"),
		netip.MustParsePrefix("100.64.0.0/10"),
		netip.MustParsePrefix("127.0.0.0/8"),
		netip.MustParsePrefix("169.254.0.0/16"),
		netip.MustParsePrefix("192.0.0.0/24"),
		netip.MustParsePrefix("192.0.2.0/24"),
		netip.MustParsePrefix("192.88.99.0/24"),
		netip.MustParsePrefix("198.18.0.0/15"),
		netip.MustParsePrefix("198.51.100.0/24"),
		netip.MustParsePrefix("203.0.113.0/24"),
		netip.MustParsePrefix("224.0.0.0/3"),
		netip.MustParsePrefix("2001:db8::/32"),
	} {
		if special.Contains(address) {
			return false
		}
	}
	return true
}

func Apply(mihomo map[string]any, config Config) error {
	if err := Validate(config); err != nil {
		return err
	}
	dns, ok := mihomo["dns"].(map[string]any)
	if !ok {
		return errors.New("runtime profile DNS configuration is missing or invalid")
	}
	policy, ok := dns["nameserver-policy"].(map[string]any)
	if !ok {
		return errors.New("runtime profile nameserver-policy is missing or invalid")
	}
	server := config.Resolver.Endpoint + "#" + config.Resolver.Proxy +
		"&ecs=" + config.ECS.Prefix + "&ecs-override=true"
	for _, domain := range config.Scope.Domains {
		if _, exists := policy[domain]; exists {
			return fmt.Errorf("qualified ECS scope domain %q conflicts with existing nameserver-policy", domain)
		}
		policy[domain] = []string{server}
	}
	return nil
}

func CanonicalDomainSHA256(domains []string) (string, error) {
	canonical, err := canonicalDomains(domains)
	if err != nil {
		return "", err
	}
	return hashDomains(canonical), nil
}

func canonicalDomains(domains []string) ([]string, error) {
	if len(domains) == 0 {
		return nil, errors.New("resolver scope domains are required")
	}
	seen := map[string]bool{}
	canonical := make([]string, 0, len(domains))
	for _, raw := range domains {
		domain := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(raw), "."))
		if err := validateDomain(domain); err != nil {
			return nil, err
		}
		if seen[domain] {
			return nil, fmt.Errorf("duplicate resolver scope domain %q", domain)
		}
		seen[domain] = true
		canonical = append(canonical, domain)
	}
	sort.Strings(canonical)
	return canonical, nil
}

func validateDomain(domain string) error {
	_, ipError := netip.ParseAddr(domain)
	if domain == "" || len(domain) > 253 || ipError == nil || strings.ContainsAny(domain, "/:@#&? \t") {
		return fmt.Errorf("invalid resolver scope domain %q", domain)
	}
	labels := strings.Split(domain, ".")
	if len(labels) < 2 {
		return fmt.Errorf("invalid resolver scope domain %q", domain)
	}
	for _, label := range labels {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return fmt.Errorf("invalid resolver scope domain %q", domain)
		}
		for _, char := range label {
			if (char < 'a' || char > 'z') && (char < '0' || char > '9') && char != '-' {
				return fmt.Errorf("invalid resolver scope domain %q", domain)
			}
		}
	}
	return nil
}

func hashDomains(domains []string) string {
	sum := sha256.Sum256([]byte(strings.Join(domains, "\n") + "\n"))
	return hex.EncodeToString(sum[:])
}

func isSHA256(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size
}

func parseTimestamp(name, value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid %s: %w", name, err)
	}
	return parsed, nil
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
