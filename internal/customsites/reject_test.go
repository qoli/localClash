package customsites

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"localclash/internal/rules"
)

func TestRejectSharesNewestFirstOrderAndDeletionWithOtherRoutes(t *testing.T) {
	state := EmptyState()
	var entries []Entry
	for _, route := range []string{RouteDirect, RouteReject, RouteProxy, RouteReject} {
		var entry Entry
		var err error
		state, entry, err = Add(state, route, "example.com", time.Now())
		if err != nil {
			t.Fatal(err)
		}
		entries = append(entries, entry)
	}
	for index := len(entries) - 1; index >= 0; index-- {
		selection, err := ApplyToSelection(testSelection(), state)
		if err != nil {
			t.Fatal(err)
		}
		ordered := state.OrderedEntries()
		if len(ordered) != index+1 || ordered[0].ID != entries[index].ID || ordered[0].Sequence != uint64(index+1) {
			t.Fatalf("ordered = %+v, want latest remaining decision %+v", ordered, entries[index])
		}
		group := map[string]string{RouteDirect: DirectPolicyGroup, RouteProxy: ProxyPolicyGroup, RouteReject: RejectPolicyGroup}[entries[index].Route]
		if selection.PriorityCustomRules[0].Target != group {
			t.Fatalf("newest rule = %+v, want %q", selection.PriorityCustomRules[0], group)
		}
		if got := selection.PolicyGroups[RejectPolicyGroup]; !got.Manual || !reflect.DeepEqual(got.Exits, []string{rules.TerminalReject}) {
			t.Fatalf("reject policy = %+v, want only REJECT", got)
		}
		var deleted Entry
		state, deleted, err = Delete(state, entries[index].ID)
		if err != nil || deleted.Route != entries[index].Route {
			t.Fatalf("delete = %+v, %v", deleted, err)
		}
	}
	if got := state.OrderedEntries(); len(got) != 0 {
		t.Fatalf("remaining decisions = %+v", got)
	}
}

func TestLoadLegacyDocumentsDoesNotWriteAndKeepsIdentity(t *testing.T) {
	paths := DefaultPaths(t.TempDir())
	state := EmptyState()
	var err error
	state, _, err = Add(state, RouteDirect, "example.com", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	state, _, err = Add(state, RouteProxy, "api.example.com", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	before := map[string][]byte{}
	for path, document := range map[string]Document{paths.Proxy: state.Proxy, paths.Direct: state.Direct} {
		data, err := MarshalDocument(document)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		before[path] = data
	}
	loaded, err := Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	state.LegacyTwoFiles = true
	if !reflect.DeepEqual(loaded, state) {
		t.Fatalf("legacy state changed: got %+v, want %+v", loaded, state)
	}
	snapshot, err := SnapshotChecked(loaded)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Reject == nil || snapshot.RejectCount != 0 || snapshot.RejectSHA256 == "" || snapshot.MaxSequence != 2 {
		t.Fatalf("legacy snapshot = %+v", snapshot)
	}
	if _, err := os.Stat(paths.Reject); !os.IsNotExist(err) {
		t.Fatalf("read created reject document: %v", err)
	}
	for path, data := range before {
		after, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(after, data) {
			t.Fatalf("legacy file %q changed: %v", path, err)
		}
	}
	updated, entry, err := Add(loaded, RouteReject, "*.example.com", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if updated.LegacyTwoFiles || entry.Sequence != 3 || entry.Route != RouteReject || entry.Match != MatchWildcard || !reflect.DeepEqual(updated.Proxy, loaded.Proxy) || !reflect.DeepEqual(updated.Direct, loaded.Direct) {
		t.Fatalf("add after legacy load changed identity or order: %+v, %+v", updated, entry)
	}
}

func TestLoadRejectDocumentRejectsCorruptionAndIncompleteState(t *testing.T) {
	for _, test := range []struct {
		name string
		data string
		want string
	}{
		{"unknown field", `{"version":1,"route":"reject","entries":[],"unknown":true}`, "unknown field"},
		{"wrong route", `{"version":1,"route":"direct","entries":[]}`, "route mismatch"},
		{"unsupported version", `{"version":2,"route":"reject","entries":[]}`, "version mismatch"},
	} {
		t.Run(test.name, func(t *testing.T) {
			paths := DefaultPaths(t.TempDir())
			if err := os.MkdirAll(filepath.Dir(paths.Reject), 0o755); err != nil {
				t.Fatal(err)
			}
			for path, document := range map[string]Document{paths.Proxy: EmptyState().Proxy, paths.Direct: EmptyState().Direct} {
				data, err := MarshalDocument(document)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, data, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(paths.Reject, []byte(test.data), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := Load(paths); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Load = %v, want %q", err, test.want)
			}
			if err := os.Remove(paths.Proxy); err != nil {
				t.Fatal(err)
			}
			if _, err := Load(paths); err == nil || !strings.Contains(err.Error(), "state is incomplete") {
				t.Fatalf("Load = %v, want incomplete state", err)
			}
		})
	}
}

func TestRejectEntriesParticipateInValidationAndHashes(t *testing.T) {
	state := EmptyState()
	var err error
	state, _, err = Add(state, RouteReject, "example.com", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	state, _, err = Add(state, RouteReject, "*.example.net", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	one, err := SnapshotChecked(state)
	if err != nil {
		t.Fatal(err)
	}
	state.Reject.Entries[0], state.Reject.Entries[1] = state.Reject.Entries[1], state.Reject.Entries[0]
	two, err := SnapshotChecked(state)
	if err != nil || one.RejectSHA256 == "" || one.RejectSHA256 != two.RejectSHA256 || two.RejectCount != 2 || two.MaxSequence != 2 {
		t.Fatalf("reject hashes/counts = %+v / %+v, %v", one, two, err)
	}
	state.Proxy.Entries = append([]Entry{}, state.Reject.Entries[0])
	if err := ValidateState(state); err == nil || !strings.Contains(err.Error(), "duplicate custom site id") {
		t.Fatalf("duplicate reject ID error = %v", err)
	}
	state.Proxy.Entries[0].ID = "different-id"
	if err := ValidateState(state); err == nil || !strings.Contains(err.Error(), "duplicate custom site sequence") {
		t.Fatalf("duplicate reject sequence error = %v", err)
	}
}
