package customsites

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"localclash/internal/rules"
)

const (
	SchemaVersion = 1

	RouteProxy  = "proxy"
	RouteDirect = "direct"
	RouteReject = "reject"

	MatchFull     = "full"
	MatchWildcard = "wildcard"

	ProxyPolicyGroup  = "自訂代理網站"
	DirectPolicyGroup = "自訂直連網站"
	RejectPolicyGroup = "自訂禁止訪問網站"

	DefaultDir         = "custom-sites"
	ProxyFilename      = "proxy.json"
	DirectFilename     = "direct.json"
	RejectFilename     = "reject.json"
	RequiredAutoExit   = "⚡ 自动选择"
	RequiredManualExit = "🎯 手动选择"
)

var inputPattern = regexp.MustCompile(`^[a-z0-9*?.-]+$`)

var RegionalExits = []string{
	"🇭🇰 香港节点",
	"🇺🇸 美国节点",
	"🇯🇵 日本节点",
	"🇸🇬 新加坡节点",
	"🇹🇼 台湾节点",
	"🇰🇷 韩国节点",
}

type Paths struct {
	Proxy  string `json:"proxy"`
	Direct string `json:"direct"`
	Reject string `json:"reject"`
}

func DefaultPaths(root string) Paths {
	dir := filepath.Join(root, DefaultDir)
	return Paths{
		Proxy:  filepath.Join(dir, ProxyFilename),
		Direct: filepath.Join(dir, DirectFilename),
		Reject: filepath.Join(dir, RejectFilename),
	}
}

type Entry struct {
	ID       string `json:"id"`
	Match    string `json:"match"`
	Pattern  string `json:"pattern"`
	Sequence uint64 `json:"sequence"`
	AddedAt  string `json:"added_at"`
	Route    string `json:"-"`
}

type Document struct {
	Version int     `json:"version"`
	Route   string  `json:"route"`
	Entries []Entry `json:"entries"`
}

type Snapshot struct {
	Initialized  bool    `json:"initialized"`
	Proxy        []Entry `json:"proxy"`
	Direct       []Entry `json:"direct"`
	Reject       []Entry `json:"reject"`
	MaxSequence  uint64  `json:"max_sequence"`
	ProxyCount   int     `json:"proxy_count"`
	DirectCount  int     `json:"direct_count"`
	RejectCount  int     `json:"reject_count"`
	ProxySHA256  string  `json:"proxy_sha256,omitempty"`
	DirectSHA256 string  `json:"direct_sha256,omitempty"`
	RejectSHA256 string  `json:"reject_sha256,omitempty"`
}

type State struct {
	Initialized bool
	Proxy       Document
	Direct      Document
	Reject      Document
	// LegacyTwoFiles records a read of the pre-reject format. Its saved config
	// may omit the empty reject group when a failed first transaction rolls back.
	LegacyTwoFiles bool
}

func EmptyState() State {
	return State{
		Initialized: true,
		Proxy:       Document{Version: SchemaVersion, Route: RouteProxy, Entries: []Entry{}},
		Direct:      Document{Version: SchemaVersion, Route: RouteDirect, Entries: []Entry{}},
		Reject:      Document{Version: SchemaVersion, Route: RouteReject, Entries: []Entry{}},
	}
}

func Load(paths Paths) (State, error) {
	proxyExists, err := regularFileExists(paths.Proxy)
	if err != nil {
		return State{}, err
	}
	directExists, err := regularFileExists(paths.Direct)
	if err != nil {
		return State{}, err
	}
	rejectExists, err := regularFileExists(paths.Reject)
	if err != nil {
		return State{}, err
	}
	if !proxyExists && !directExists && !rejectExists {
		return State{}, nil
	}
	if !proxyExists || !directExists {
		return State{}, fmt.Errorf("custom site state is incomplete: proxy_exists=%t direct_exists=%t reject_exists=%t", proxyExists, directExists, rejectExists)
	}
	proxy, err := loadDocument(paths.Proxy, RouteProxy)
	if err != nil {
		return State{}, err
	}
	direct, err := loadDocument(paths.Direct, RouteDirect)
	if err != nil {
		return State{}, err
	}
	// Existing workspaces have only proxy.json and direct.json. Reading them
	// supplies an empty reject document without writing or changing old entries.
	state := EmptyState()
	state.Proxy, state.Direct = proxy, direct
	state.LegacyTwoFiles = !rejectExists
	if rejectExists {
		state.Reject, err = loadDocument(paths.Reject, RouteReject)
		if err != nil {
			return State{}, err
		}
	}
	if err := ValidateState(state); err != nil {
		return State{}, err
	}
	return state, nil
}

func ValidateState(state State) error {
	if !state.Initialized {
		return nil
	}
	ids := map[string]string{}
	sequences := map[uint64]string{}
	for _, document := range []Document{state.Proxy, state.Direct, state.Reject} {
		if document.Version != SchemaVersion {
			return fmt.Errorf("custom site %s document version mismatch: expected %d, got %d", document.Route, SchemaVersion, document.Version)
		}
		if document.Route != RouteProxy && document.Route != RouteDirect && document.Route != RouteReject {
			return fmt.Errorf("custom site document route %q is invalid", document.Route)
		}
		for index, entry := range document.Entries {
			if err := validateEntry(entry, document.Route); err != nil {
				return fmt.Errorf("custom site %s entry %d: %w", document.Route, index+1, err)
			}
			if other, ok := ids[entry.ID]; ok {
				return fmt.Errorf("duplicate custom site id %q in %s and %s", entry.ID, other, document.Route)
			}
			ids[entry.ID] = document.Route
			if other, ok := sequences[entry.Sequence]; ok {
				return fmt.Errorf("duplicate custom site sequence %d in %s and %s", entry.Sequence, other, document.Route)
			}
			sequences[entry.Sequence] = document.Route
		}
	}
	return nil
}

func SnapshotFor(state State) Snapshot {
	if !state.Initialized {
		return Snapshot{Proxy: []Entry{}, Direct: []Entry{}, Reject: []Entry{}}
	}
	snapshot := Snapshot{
		Initialized: true,
		Proxy:       cloneEntries(state.Proxy.Entries, RouteProxy),
		Direct:      cloneEntries(state.Direct.Entries, RouteDirect),
		Reject:      cloneEntries(state.Reject.Entries, RouteReject),
	}
	sortEntries(snapshot.Proxy)
	sortEntries(snapshot.Direct)
	sortEntries(snapshot.Reject)
	snapshot.ProxyCount = len(snapshot.Proxy)
	snapshot.DirectCount = len(snapshot.Direct)
	snapshot.RejectCount = len(snapshot.Reject)
	for _, entries := range [][]Entry{snapshot.Proxy, snapshot.Direct, snapshot.Reject} {
		for _, entry := range entries {
			if entry.Sequence > snapshot.MaxSequence {
				snapshot.MaxSequence = entry.Sequence
			}
		}
	}
	return snapshot
}

func SnapshotChecked(state State) (Snapshot, error) {
	snapshot := SnapshotFor(state)
	if !state.Initialized {
		return snapshot, nil
	}
	var err error
	snapshot.ProxySHA256, err = documentSHA256(state.Proxy)
	if err != nil {
		return Snapshot{}, fmt.Errorf("hash custom site proxy document: %w", err)
	}
	snapshot.DirectSHA256, err = documentSHA256(state.Direct)
	if err != nil {
		return Snapshot{}, fmt.Errorf("hash custom site direct document: %w", err)
	}
	snapshot.RejectSHA256, err = documentSHA256(state.Reject)
	if err != nil {
		return Snapshot{}, fmt.Errorf("hash custom site reject document: %w", err)
	}
	return snapshot, nil
}

func documentSHA256(document Document) (string, error) {
	document.Entries = cloneEntries(document.Entries, document.Route)
	sortEntries(document.Entries)
	data, err := MarshalDocument(document)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func Add(state State, route, pattern string, now time.Time) (State, Entry, error) {
	if !state.Initialized {
		state = EmptyState()
	}
	if err := ValidateState(state); err != nil {
		return State{}, Entry{}, err
	}
	route = strings.ToLower(strings.TrimSpace(route))
	if route != RouteProxy && route != RouteDirect && route != RouteReject {
		return State{}, Entry{}, fmt.Errorf("custom site route must be %q, %q or %q", RouteProxy, RouteDirect, RouteReject)
	}
	normalized, match, err := NormalizePattern(pattern)
	if err != nil {
		return State{}, Entry{}, err
	}
	max := SnapshotFor(state).MaxSequence
	if max == ^uint64(0) {
		return State{}, Entry{}, errors.New("custom site sequence is exhausted")
	}
	id, err := newID()
	if err != nil {
		return State{}, Entry{}, err
	}
	if now.IsZero() {
		now = time.Now()
	}
	entry := Entry{
		ID:       id,
		Match:    match,
		Pattern:  normalized,
		Sequence: max + 1,
		AddedAt:  now.UTC().Format(time.RFC3339Nano),
		Route:    route,
	}
	switch route {
	case RouteProxy:
		state.Proxy.Entries = append(state.Proxy.Entries, entry)
	case RouteDirect:
		state.Direct.Entries = append(state.Direct.Entries, entry)
	case RouteReject:
		state.Reject.Entries = append(state.Reject.Entries, entry)
	}
	if err := ValidateState(state); err != nil {
		return State{}, Entry{}, err
	}
	state.LegacyTwoFiles = false
	return state, entry, nil
}

func Delete(state State, id string) (State, Entry, error) {
	if !state.Initialized {
		return State{}, Entry{}, errors.New("custom site state is not initialized")
	}
	if err := ValidateState(state); err != nil {
		return State{}, Entry{}, err
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return State{}, Entry{}, errors.New("custom site id is required")
	}
	for _, target := range []*Document{&state.Proxy, &state.Direct, &state.Reject} {
		for index, entry := range target.Entries {
			if entry.ID != id {
				continue
			}
			entry.Route = target.Route
			target.Entries = append(target.Entries[:index], target.Entries[index+1:]...)
			state.LegacyTwoFiles = false
			return state, entry, nil
		}
	}
	return State{}, Entry{}, fmt.Errorf("custom site id %q was not found", id)
}

func NormalizePattern(value string) (string, string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.TrimSuffix(value, ".")
	if value == "" {
		return "", "", errors.New("custom site pattern is required")
	}
	if strings.Contains(value, "://") || strings.ContainsAny(value, "/,:#@[]\\ ") {
		return "", "", fmt.Errorf("custom site pattern %q must be a host pattern without scheme, port, path, credentials, or IP literals", value)
	}
	if !inputPattern.MatchString(value) {
		return "", "", fmt.Errorf("custom site pattern %q contains unsupported characters", value)
	}
	if strings.HasPrefix(value, ".") || strings.Contains(value, "..") {
		return "", "", fmt.Errorf("custom site pattern %q has an invalid domain separator", value)
	}
	plain := strings.NewReplacer("*", "a", "?", "a").Replace(value)
	if net.ParseIP(plain) != nil || net.ParseIP(value) != nil {
		return "", "", fmt.Errorf("custom site pattern %q must not be an IP address", value)
	}
	match := MatchFull
	if strings.ContainsAny(value, "*?") {
		match = MatchWildcard
	}
	return value, match, nil
}

func ApplyToSelection(selection rules.Selection, state State) (rules.Selection, error) {
	if !state.Initialized {
		return selection, nil
	}
	if err := ValidateState(state); err != nil {
		return rules.Selection{}, err
	}
	out := cloneSelection(selection)
	for _, name := range []string{ProxyPolicyGroup, DirectPolicyGroup, RejectPolicyGroup} {
		if _, exists := out.PolicyGroups[name]; exists {
			return rules.Selection{}, fmt.Errorf("reserved_policy_group_name: %s", name)
		}
		if _, exists := out.ProxyGroups[name]; exists {
			return rules.Selection{}, fmt.Errorf("reserved_policy_group_name: %s", name)
		}
	}
	for _, required := range []string{RequiredAutoExit, RequiredManualExit} {
		group, exists := out.ProxyGroups[required]
		if !exists || (group.Optional && len(group.Nodes) == 0) {
			return rules.Selection{}, fmt.Errorf("custom site proxy policy requires exit %q", required)
		}
	}
	exits := []string{RequiredAutoExit, RequiredManualExit}
	regionNames := make([]string, 0, len(RegionalExits))
	for _, name := range RegionalExits {
		group, exists := out.ProxyGroups[name]
		if !exists || !group.Optional || len(group.Nodes) == 0 {
			continue
		}
		regionNames = append(regionNames, name)
	}
	exits = append(exits, regionNames...)
	out.PolicyGroups[ProxyPolicyGroup] = rules.PolicyGroup{Exits: exits, Manual: true}
	out.PolicyGroups[DirectPolicyGroup] = rules.PolicyGroup{Exits: []string{rules.TerminalDirect}, Manual: true}
	out.PolicyGroups[RejectPolicyGroup] = rules.PolicyGroup{Exits: []string{rules.TerminalReject}, Manual: true}
	out.RequiredTargets = append(out.RequiredTargets, ProxyPolicyGroup, DirectPolicyGroup, RejectPolicyGroup)

	entries := state.OrderedEntries()
	priority := make([]rules.CustomRule, 0, len(entries))
	for _, entry := range entries {
		target := DirectPolicyGroup
		if entry.Route == RouteProxy {
			target = ProxyPolicyGroup
		} else if entry.Route == RouteReject {
			target = RejectPolicyGroup
		}
		ruleType := "domain_suffix"
		if entry.Match == MatchWildcard {
			ruleType = "domain_wildcard"
		}
		priority = append(priority, rules.CustomRule{
			ID:     "custom-site:" + entry.ID,
			Target: target,
			Reason: "LuCI custom website decision",
			Rules:  []rules.CustomRuleLine{{Type: ruleType, Value: entry.Pattern}},
		})
	}
	out.PriorityCustomRules = append(priority, out.PriorityCustomRules...)
	return out, nil
}

// OrderedEntries returns independent copies of all decisions, newest first
// across routes. Rendering and runtime read-back share this ordering contract.
func (state State) OrderedEntries() []Entry {
	entries := cloneEntries(state.Proxy.Entries, RouteProxy)
	entries = append(entries, cloneEntries(state.Direct.Entries, RouteDirect)...)
	entries = append(entries, cloneEntries(state.Reject.Entries, RouteReject)...)
	sortEntries(entries)
	return entries
}

func MarshalDocument(document Document) ([]byte, error) {
	if document.Entries == nil {
		document.Entries = []Entry{}
	}
	data, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func cloneSelection(selection rules.Selection) rules.Selection {
	out := selection
	out.ProxyGroups = make(map[string]rules.ProxyGroup, len(selection.ProxyGroups))
	for key, value := range selection.ProxyGroups {
		value.Nodes = append([]string{}, value.Nodes...)
		out.ProxyGroups[key] = value
	}
	out.PolicyGroups = make(map[string]rules.PolicyGroup, len(selection.PolicyGroups)+3)
	for key, value := range selection.PolicyGroups {
		value.Exits = append([]string{}, value.Exits...)
		out.PolicyGroups[key] = value
	}
	out.PriorityCustomRules = append([]rules.CustomRule{}, selection.PriorityCustomRules...)
	out.RequiredTargets = append([]string{}, selection.RequiredTargets...)
	return out
}

func loadDocument(path, route string) (Document, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Document{}, err
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	var document Document
	if err := decoder.Decode(&document); err != nil {
		return Document{}, fmt.Errorf("decode custom site %s document %q: %w", route, path, err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("multiple JSON values")
		}
		return Document{}, fmt.Errorf("decode custom site %s document %q: %w", route, path, err)
	}
	if document.Route != route {
		return Document{}, fmt.Errorf("custom site document %q route mismatch: expected %q, got %q", path, route, document.Route)
	}
	for index := range document.Entries {
		document.Entries[index].Route = route
	}
	return document, nil
}

func validateEntry(entry Entry, route string) error {
	if strings.TrimSpace(entry.ID) == "" {
		return errors.New("id is required")
	}
	if entry.Sequence == 0 {
		return errors.New("sequence must be greater than zero")
	}
	normalized, match, err := NormalizePattern(entry.Pattern)
	if err != nil {
		return err
	}
	if normalized != entry.Pattern {
		return fmt.Errorf("pattern %q is not normalized; expected %q", entry.Pattern, normalized)
	}
	if entry.Match != match {
		return fmt.Errorf("match %q does not match pattern; expected %q", entry.Match, match)
	}
	if strings.TrimSpace(entry.AddedAt) == "" {
		return errors.New("added_at is required")
	}
	if _, err := time.Parse(time.RFC3339Nano, entry.AddedAt); err != nil {
		return fmt.Errorf("added_at is invalid: %w", err)
	}
	if route != RouteProxy && route != RouteDirect && route != RouteReject {
		return fmt.Errorf("route %q is invalid", route)
	}
	return nil
}

func cloneEntries(entries []Entry, route string) []Entry {
	out := append([]Entry{}, entries...)
	for index := range out {
		out[index].Route = route
	}
	return out
}

func sortEntries(entries []Entry) {
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].Sequence == entries[j].Sequence {
			return entries[i].ID < entries[j].ID
		}
		return entries[i].Sequence > entries[j].Sequence
	})
}

func newID() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("allocate custom site id: %w", err)
	}
	return hex.EncodeToString(bytes), nil
}

func regularFileExists(path string) (bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	if !info.Mode().IsRegular() {
		return false, fmt.Errorf("custom site path %q is not a regular file", path)
	}
	return true, nil
}
