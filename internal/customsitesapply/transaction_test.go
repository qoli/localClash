package customsitesapply

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"localclash/internal/customsites"
	"localclash/internal/mihomotest"
)

func TestTransactFirstAddPromotesCandidateAndReportsPending(t *testing.T) {
	root := t.TempDir()
	opts := testTransactionOptions(t, root)
	opts.Input = TransactionInput{Version: 1, Operation: OperationAdd, Pattern: "ABC.*cdn?.com", Route: customsites.RouteProxy}
	var stages []string
	opts.Hooks.Progress = func(stage, _ string) { stages = append(stages, stage) }

	result, err := Transact(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if result.Apply.Effective || !result.Apply.PendingNextStart || !result.Apply.Promoted || !result.Apply.Validated {
		t.Fatalf("apply = %+v, want validated promoted pending state", result.Apply)
	}
	if result.Snapshot.ProxyCount != 1 || result.Snapshot.DirectCount != 0 || result.Snapshot.RejectCount != 0 || result.Snapshot.ProxySHA256 == "" || result.Snapshot.DirectSHA256 == "" || result.Snapshot.RejectSHA256 == "" {
		t.Fatalf("snapshot = %+v, want authoritative counts and hashes", result.Snapshot)
	}
	if got, want := strings.Join(stages, ","), "validate,load,candidate,render,config_test,runtime_status,promote,complete"; got != want {
		t.Fatalf("progress stages = %q, want %q", got, want)
	}
	pair, err := customsites.Load(opts.Paths)
	if err != nil {
		t.Fatal(err)
	}
	readBack, err := customsites.SnapshotChecked(pair)
	if err != nil {
		t.Fatal(err)
	}
	if readBack.ProxySHA256 != result.Snapshot.ProxySHA256 || readBack.DirectSHA256 != result.Snapshot.DirectSHA256 || readBack.RejectSHA256 != result.Snapshot.RejectSHA256 {
		t.Fatalf("read back snapshot = %+v, transaction snapshot = %+v", readBack, result.Snapshot)
	}
	for _, path := range []string{opts.Paths.Proxy, opts.Paths.Direct, opts.Paths.Reject, opts.GeneratedConfig, opts.AttestationPath} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("promoted path %q: %v", path, err)
		}
	}
}

func TestTransactReloadFailureRestoresPriorFilesAndRuntime(t *testing.T) {
	root := t.TempDir()
	initial := testTransactionOptions(t, root)
	initial.Input = TransactionInput{Version: 1, Operation: OperationAdd, Pattern: "old.example", Route: customsites.RouteDirect}
	if _, err := Transact(context.Background(), initial); err != nil {
		t.Fatal(err)
	}
	paths := []string{initial.Paths.Proxy, initial.Paths.Direct, initial.Paths.Reject, initial.GeneratedConfig, initial.AttestationPath}
	before := map[string][]byte{}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		before[path] = data
	}

	reloadCalls := 0
	opts := testTransactionOptions(t, root)
	opts.Input = TransactionInput{Version: 1, Operation: OperationAdd, Pattern: "new.example", Route: customsites.RouteReject}
	opts.Hooks.RuntimeStatus = func() (RuntimeStatus, error) { return RuntimeStatus{Running: true}, nil }
	opts.Hooks.Reload = func(context.Context, string) (ReloadStatus, error) {
		reloadCalls++
		if reloadCalls == 1 {
			return ReloadStatus{}, errors.New("controller rejected candidate")
		}
		return ReloadStatus{Reloaded: true, ReadBack: true}, nil
	}
	result, err := Transact(context.Background(), opts)
	if err == nil || !strings.Contains(err.Error(), "prior files and runtime restored") {
		t.Fatalf("error = %v, want explicit restored reload failure", err)
	}
	if !result.Apply.RolledBack || result.Apply.Effective || reloadCalls != 2 {
		t.Fatalf("result = %+v reload_calls=%d, want complete rollback", result, reloadCalls)
	}
	for _, path := range paths {
		after, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if string(after) != string(before[path]) {
			t.Fatalf("path %q changed after rollback", path)
		}
	}
}

func TestTransactRejectsNonV1AndMixedOperationFields(t *testing.T) {
	root := t.TempDir()
	for _, input := range []TransactionInput{
		{Version: 2, Operation: OperationAdd, Pattern: "a.example", Route: customsites.RouteProxy},
		{Version: 1, Operation: OperationAdd, ID: "unexpected", Pattern: "a.example", Route: customsites.RouteProxy},
		{Version: 1, Operation: OperationDelete, ID: "id", Pattern: "unexpected"},
	} {
		opts := testTransactionOptions(t, root)
		opts.Input = input
		if _, err := Transact(context.Background(), opts); err == nil {
			t.Fatalf("input %+v should fail", input)
		}
	}
}

func TestTransactRejectAddDeleteAndLegacyRollback(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		t.Run(fmt.Sprintf("legacy=%t", legacy), func(t *testing.T) {
			opts := testTransactionOptions(t, t.TempDir())
			opts.Input = TransactionInput{Version: 1, Operation: OperationAdd, Pattern: "example.com", Route: customsites.RouteDirect}
			if _, err := Transact(context.Background(), opts); err != nil {
				t.Fatal(err)
			}
			if legacy {
				if err := os.Remove(opts.Paths.Reject); err != nil {
					t.Fatal(err)
				}
			}
			before := map[string][]byte{}
			for _, path := range []string{opts.Paths.Proxy, opts.Paths.Direct, opts.GeneratedConfig, opts.AttestationPath} {
				var err error
				before[path], err = os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
			}
			opts.Input = TransactionInput{Version: 1, Operation: OperationAdd, Pattern: "example.com", Route: customsites.RouteReject}
			opts.Hooks.RuntimeStatus = func() (RuntimeStatus, error) { return RuntimeStatus{Running: true}, nil }
			reloads := 0
			opts.Hooks.Reload = func(context.Context, string) (ReloadStatus, error) {
				reloads++
				if reloads == 1 {
					state, err := customsites.Load(opts.Paths)
					if err != nil || len(state.Reject.Entries) != 1 {
						t.Fatalf("candidate reject state was not promoted: %+v, %v", state, err)
					}
					return ReloadStatus{}, errors.New("forced read-back failure")
				}
				return ReloadStatus{Reloaded: true, ReadBack: true}, nil
			}
			result, err := Transact(context.Background(), opts)
			if err == nil || !result.Apply.RolledBack || result.Snapshot.RejectCount != 0 {
				t.Fatalf("rollback = %+v, %v", result, err)
			}
			for path, data := range before {
				after, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(after, data) {
					t.Fatalf("rollback changed %s: %v", path, err)
				}
			}
			if _, err := os.Stat(opts.Paths.Reject); legacy && !os.IsNotExist(err) {
				t.Fatalf("legacy rollback left a new reject document: %v", err)
			}
			result, err = Transact(context.Background(), opts)
			if err != nil || !result.Apply.Effective || result.Entry.Sequence != 2 || result.Snapshot.RejectCount != 1 {
				t.Fatalf("reject add = %+v, %v", result, err)
			}
			opts.Input = TransactionInput{Version: 1, Operation: OperationDelete, ID: result.Entry.ID}
			result, err = Transact(context.Background(), opts)
			if err != nil || !result.Apply.Effective || result.Entry.Route != customsites.RouteReject || result.Snapshot.RejectCount != 0 || result.Snapshot.DirectCount != 1 {
				t.Fatalf("reject delete = %+v, %v", result, err)
			}
		})
	}
}

func testTransactionOptions(t *testing.T, root string) TransactionOptions {
	paths := customsites.DefaultPaths(root)
	generated := filepath.Join(root, ".runtime", "mihomo", "config.yaml")
	attestation := mihomotest.DefaultAttestationPath(filepath.Dir(generated))
	return TransactionOptions{
		Paths:           paths,
		GeneratedConfig: generated,
		AttestationPath: attestation,
		Now:             time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC),
		Hooks: TransactionHooks{
			Render: func(_ context.Context, candidate customsites.Paths, output string) error {
				proxy, err := os.ReadFile(candidate.Proxy)
				if err != nil {
					return err
				}
				direct, err := os.ReadFile(candidate.Direct)
				if err != nil {
					return err
				}
				reject, err := os.ReadFile(candidate.Reject)
				if err != nil {
					return err
				}
				return os.WriteFile(output, append(append(append([]byte("rules:\n"), proxy...), direct...), reject...), 0o644)
			},
			Validate: func(_ context.Context, config, attestation string) (ValidationStatus, error) {
				sha, err := mihomotest.ConfigSHA256(config)
				if err != nil {
					return ValidationStatus{}, err
				}
				err = mihomotest.WriteAttestation(attestation, mihomotest.Attestation{
					Version:      1,
					Config:       config,
					WorkDir:      filepath.Dir(config),
					Core:         filepath.Join(root, "mihomo"),
					ConfigSHA256: sha,
					Passed:       true,
					TestedAt:     "2026-08-29T12:00:00Z",
				})
				return ValidationStatus{ConfigSHA256: sha}, err
			},
			RuntimeStatus: func() (RuntimeStatus, error) { return RuntimeStatus{}, nil },
			Reload: func(context.Context, string) (ReloadStatus, error) {
				t.Fatal("reload must not be called for inactive runtime")
				return ReloadStatus{}, nil
			},
		},
	}
}
