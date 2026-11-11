package poolprepare

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"localclash/internal/capability"
	"localclash/internal/poolcontract"
)

func TestCandidatePoolsQualifyConcurrently(t *testing.T) {
	auto, _ := poolcontract.Lookup(poolcontract.AutoCapability)
	chatgpt, _ := poolcontract.Lookup(poolcontract.ChatGPTCapability)
	jobs := []poolPreparation{{pool: auto}, {pool: chatgpt}}
	started := make(chan string, len(jobs))
	release := make(chan struct{})
	done := make(chan struct{})
	chatgptError := errors.New("chatgpt probe failed")
	go func() {
		qualifyPreparations(jobs, func(pool poolcontract.Pool) (capability.Result, error) {
			started <- pool.Capability
			<-release
			if pool == chatgpt {
				return capability.Result{}, chatgptError
			}
			return capability.Result{Profile: pool.Capability, Probed: 1000}, nil
		})
		close(done)
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	observed := make(map[string]bool)
	for range jobs {
		select {
		case profile := <-started:
			observed[profile] = true
		case <-ctx.Done():
			close(release)
			<-done
			t.Fatal("both qualification runs must start before either finishes")
		}
	}
	close(release)
	<-done
	if !observed[auto.Capability] || !observed[chatgpt.Capability] {
		t.Fatalf("started profiles = %v", observed)
	}
	if jobs[0].result.Profile != auto.Capability || jobs[0].result.Probed != 1000 || jobs[0].err != nil || !errors.Is(jobs[1].err, chatgptError) {
		t.Fatalf("qualification results = %+v", jobs)
	}
}

func TestPrepareRechecksAutoProviderFromPreviousQualification(t *testing.T) {
	pool, _ := poolcontract.Lookup(poolcontract.AutoCapability)
	input, err := poolcontract.NewInput([]map[string]any{{"name": "node", "type": "ss"}})
	if err != nil {
		t.Fatal(err)
	}
	runtimeDir := t.TempDir()
	path := filepath.Join(runtimeDir, pool.Path(input.Revision))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	legacy, _, err := poolcontract.ProviderData(poolcontract.Pool{}, input, []string{"node"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, legacy, 0o600); err != nil {
		t.Fatal(err)
	}
	missingCore := filepath.Join(runtimeDir, "missing-core")
	ctx := context.Background()
	opts := Options{CorePath: missingCore, RuntimeDir: runtimeDir}
	if _, err := Prepare(ctx, input, []poolcontract.Pool{pool}, opts); err == nil || !strings.Contains(err.Error(), "inspect isolated proxy probe core") {
		t.Fatalf("prepare with a legacy provider = %v, want an attempted probe", err)
	}
	if got, err := os.ReadFile(path); err != nil || !bytes.Equal(got, legacy) {
		t.Fatalf("failed probe replaced the legacy provider: %v", err)
	}
	current, _, err := poolcontract.ProviderData(pool, input, []string{"node"})
	if err != nil {
		t.Fatal(err)
	}
	if err := poolcontract.WriteFile(path, current); err != nil {
		t.Fatal(err)
	}
	if got, err := Prepare(ctx, input, []poolcontract.Pool{pool}, opts); err != nil || len(got) != 0 {
		t.Fatalf("prepare with a current provider = %v, %v, want cached result", got, err)
	}
}
