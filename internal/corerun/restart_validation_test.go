package corerun

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"localclash/internal/mihomotest"
	"localclash/internal/runtimesupervision"
)

func TestRestartReusesPreStopValidationProof(t *testing.T) {
	fixture := newSupervisedFixture(t)
	before := readSupervisionState(t, fixture.workDir)
	queriesBefore, err := os.ReadFile(fixture.core + ".version-calls")
	if err != nil {
		t.Fatal(err)
	}
	var events []string
	injected := false
	result, err := Restart(context.Background(), RestartOptions{
		CorePath: fixture.core, ConfigPath: fixture.config, WorkDir: fixture.workDir,
		ValidationCachePath: before.ValidationCachePath, StopTimeout: 2 * time.Second,
		OnStage: func(event RestartStageEvent) {
			events = append(events, event.Stage+":"+event.Event)
			if event.Stage == "stop" && event.Event == "done" {
				// Change only the test fixture's response to a later -v invocation.
				// The core/config bytes and their existing passing proof stay intact.
				if writeErr := os.WriteFile(fixture.core+".version-fail", nil, 0o600); writeErr != nil {
					t.Fatal(writeErr)
				}
				injected = true
			}
		},
	})
	defer fixture.crash(result.Start.PID)
	if err != nil {
		t.Fatalf("restart transport error: %v", err)
	}
	if !injected || !result.ConfigValidation.Passed || !result.Stop.Stopped {
		t.Fatalf("did not reach validated stop boundary: injected=%t validated=%t stopped=%t error=%q events=%v", injected, result.ConfigValidation.Passed, result.Stop.Stopped, result.Error, events)
	}
	coreSHA, err := runtimesupervision.HashFile(fixture.core)
	if err != nil {
		t.Fatal(err)
	}
	configSHA, err := runtimesupervision.HashFile(fixture.config)
	if err != nil {
		t.Fatal(err)
	}
	if coreSHA != result.ConfigValidation.CoreSHA256 || configSHA != result.ConfigValidation.ConfigSHA256 {
		t.Fatal("fixture mutation unexpectedly changed validated core/config bytes")
	}
	queriesAfter, err := os.ReadFile(fixture.core + ".version-calls")
	if err != nil {
		t.Fatal(err)
	}
	queryCount := strings.Count(string(queriesAfter), "query") - strings.Count(string(queriesBefore), "query")
	status := Status(StatusOptions{CorePath: fixture.core, ConfigPath: fixture.config, WorkDir: fixture.workDir})
	after := readSupervisionState(t, fixture.workDir)
	t.Logf("preflight_pass=%t stopped=%t version_queries_during_restart=%d restarted=%t running=%t supervision=%s old_pid=%d supervisor_pid=%d error=%q stages=%v", result.ConfigValidation.Passed, result.Stop.Stopped, queryCount, result.Restarted, status.Running, after.State, fixture.pid, after.PID, result.Error, events)
	if !result.Restarted || result.Error != "" || !status.Running || after.State != runtimesupervision.StateRunning || after.PID == fixture.pid || queryCount != 1 {
		t.Fatal("restart must reuse its verified pre-stop proof without another fallible version query after stopping the old process")
	}
}

func TestRestartRejectsValidationHashDrift(t *testing.T) {
	for _, boundary := range []string{"before_stop", "after_stop"} {
		for _, input := range []string{"core", "config"} {
			t.Run(boundary+"/"+input, func(t *testing.T) {
				fixture := newSupervisedFixture(t)
				before := readSupervisionState(t, fixture.workDir)
				injected := false
				result, err := Restart(context.Background(), RestartOptions{
					CorePath: fixture.core, ConfigPath: fixture.config, WorkDir: fixture.workDir,
					ValidationCachePath: before.ValidationCachePath, StopTimeout: 2 * time.Second,
					OnStage: func(event RestartStageEvent) {
						match := boundary == "before_stop" && event.Stage == "config_test" && event.Event == "done" || boundary == "after_stop" && event.Stage == "stop" && event.Event == "done"
						if !match {
							return
						}
						path := fixture.config
						if input == "core" {
							path = fixture.core
						}
						file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
						if err != nil {
							t.Fatal(err)
						}
						_, writeErr := file.WriteString("\n# changed after validation\n")
						closeErr := file.Close()
						if writeErr != nil {
							t.Fatal(writeErr)
						}
						if closeErr != nil {
							t.Fatal(closeErr)
						}
						injected = true
					},
				})
				defer fixture.crash(result.Start.PID)
				if err != nil {
					t.Fatal(err)
				}
				if !injected || !result.ConfigValidation.Passed || result.Restarted || result.Start.Started || !strings.Contains(result.Error, input+" SHA-256 no longer matches") {
					t.Fatalf("injected=%t result=%+v", injected, result)
				}
				status := Status(StatusOptions{CorePath: fixture.core, ConfigPath: fixture.config, WorkDir: fixture.workDir})
				after := readSupervisionState(t, fixture.workDir)
				if boundary == "before_stop" {
					if result.Stop.Stopped || !status.Running || status.PID != fixture.pid || after.State != runtimesupervision.StateRunning {
						t.Fatalf("pre-stop drift disturbed runtime: stop=%+v status=%+v state=%+v", result.Stop, status, after)
					}
				} else {
					if !result.Stop.Stopped || status.Running || after.State != runtimesupervision.StateStopped || after.PID != 0 {
						t.Fatalf("post-stop drift did not preserve stopped truth: stop=%+v status=%+v state=%+v", result.Stop, status, after)
					}
				}
			})
		}
	}
}

func TestRestartRejectsUndurableValidationBeforeStopping(t *testing.T) {
	fixture := newSupervisedFixture(t)
	cachePath := filepath.Join(t.TempDir(), "cache-is-directory")
	if err := os.Mkdir(cachePath, 0o700); err != nil {
		t.Fatal(err)
	}
	result, err := Restart(context.Background(), RestartOptions{
		CorePath: fixture.core, ConfigPath: fixture.config, WorkDir: fixture.workDir,
		ValidationCachePath: cachePath, StopTimeout: 2 * time.Second,
	})
	defer fixture.crash(result.Start.PID)
	if err != nil {
		t.Fatal(err)
	}
	if !result.ConfigValidation.Passed || result.ConfigValidation.CacheWriteError == "" || result.Stop.Stopped || !strings.Contains(result.Error, "durable validation cache") {
		t.Fatalf("result=%+v", result)
	}
	status := Status(StatusOptions{CorePath: fixture.core, ConfigPath: fixture.config, WorkDir: fixture.workDir})
	after := readSupervisionState(t, fixture.workDir)
	if !status.Running || status.PID != fixture.pid || after.State != runtimesupervision.StateRunning {
		t.Fatalf("undurable validation stopped runtime: status=%+v state=%+v", status, after)
	}
}

func TestStopWaitsForEntireRestartTransaction(t *testing.T) {
	fixture := newSupervisedFixture(t)
	before := readSupervisionState(t, fixture.workDir)
	stopped := make(chan struct{})
	releaseRestart := make(chan struct{})
	defer func() {
		select {
		case <-releaseRestart:
		default:
			close(releaseRestart)
		}
	}()
	type restartOutcome struct {
		result RestartResult
		err    error
	}
	restartDone := make(chan restartOutcome, 1)
	go func() {
		result, err := Restart(context.Background(), RestartOptions{
			CorePath: fixture.core, ConfigPath: fixture.config, WorkDir: fixture.workDir,
			ValidationCachePath: before.ValidationCachePath, StopTimeout: 2 * time.Second,
			OnStage: func(event RestartStageEvent) {
				if event.Stage == "stop" && event.Event == "done" {
					close(stopped)
					<-releaseRestart
				}
			},
		})
		restartDone <- restartOutcome{result, err}
	}()
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("restart never reached stopped boundary")
	}
	stopDone := make(chan error, 1)
	go func() {
		_, err := Stop(StopOptions{CorePath: fixture.core, ConfigPath: fixture.config, WorkDir: fixture.workDir, Timeout: 2 * time.Second})
		stopDone <- err
	}()
	select {
	case err := <-stopDone:
		t.Fatalf("Stop completed inside restart transaction: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	close(releaseRestart)
	select {
	case outcome := <-restartDone:
		defer fixture.crash(outcome.result.Start.PID)
		if outcome.err != nil || outcome.result.Error != "" || !outcome.result.Restarted {
			t.Fatalf("restart=%+v err=%v", outcome.result, outcome.err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("restart did not complete after releasing boundary")
	}
	select {
	case err := <-stopDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Stop did not acquire released lifecycle lock")
	}
	after := readSupervisionState(t, fixture.workDir)
	status := Status(StatusOptions{CorePath: fixture.core, ConfigPath: fixture.config, WorkDir: fixture.workDir})
	if after.State != runtimesupervision.StateStopped || after.PID != 0 || status.Running {
		t.Fatalf("Stop must win after restart: state=%+v status=%+v", after, status)
	}
}

func TestRestartRejectsChangedDurableValidationCache(t *testing.T) {
	for _, boundary := range []string{"before_stop", "after_stop"} {
		for _, change := range []string{"deleted", "malformed", "failed"} {
			t.Run(boundary+"/"+change, func(t *testing.T) {
				fixture := newSupervisedFixture(t)
				before := readSupervisionState(t, fixture.workDir)
				injected := false
				result, err := Restart(context.Background(), RestartOptions{
					CorePath: fixture.core, ConfigPath: fixture.config, WorkDir: fixture.workDir,
					ValidationCachePath: before.ValidationCachePath, StopTimeout: 2 * time.Second,
					OnStage: func(event RestartStageEvent) {
						match := boundary == "before_stop" && event.Stage == "config_test" && event.Event == "done" || boundary == "after_stop" && event.Stage == "stop" && event.Event == "done"
						if !match {
							return
						}
						switch change {
						case "deleted":
							if err := os.Remove(before.ValidationCachePath); err != nil {
								t.Fatal(err)
							}
						case "malformed":
							if err := os.WriteFile(before.ValidationCachePath, []byte("invalid json"), 0o600); err != nil {
								t.Fatal(err)
							}
						case "failed":
							data, err := os.ReadFile(before.ValidationCachePath)
							if err != nil {
								t.Fatal(err)
							}
							var cache struct {
								Version int                           `json:"version"`
								Entries []mihomotest.ValidationResult `json:"entries"`
							}
							if err := json.Unmarshal(data, &cache); err != nil {
								t.Fatal(err)
							}
							for i := range cache.Entries {
								cache.Entries[i].Passed = false
								cache.Entries[i].Error = "injected later validation failure"
							}
							data, err = json.Marshal(cache)
							if err != nil {
								t.Fatal(err)
							}
							if err := os.WriteFile(before.ValidationCachePath, data, 0o600); err != nil {
								t.Fatal(err)
							}
						}
						injected = true
					},
				})
				defer fixture.crash(result.Start.PID)
				if err != nil {
					t.Fatal(err)
				}
				if !injected || !result.ConfigValidation.Passed || result.Restarted || result.Start.Started || !strings.Contains(result.Error, "durable validation cache") {
					t.Fatalf("injected=%t result=%+v", injected, result)
				}
				if change == "failed" && !strings.Contains(result.Error, "injected later validation failure") {
					t.Fatalf("cache failure cause lost: %q", result.Error)
				}
				status := Status(StatusOptions{CorePath: fixture.core, ConfigPath: fixture.config, WorkDir: fixture.workDir})
				after := readSupervisionState(t, fixture.workDir)
				if boundary == "before_stop" {
					if result.Stop.Stopped || !status.Running || status.PID != fixture.pid || after.State != runtimesupervision.StateRunning {
						t.Fatalf("cache loss disturbed old runtime: stop=%+v status=%+v state=%+v", result.Stop, status, after)
					}
				} else {
					if !result.Stop.Stopped || status.Running || after.State != runtimesupervision.StateStopped || after.PID != 0 {
						t.Fatalf("cache loss armed an invalid proof: stop=%+v status=%+v state=%+v", result.Stop, status, after)
					}
				}
			})
		}
	}
}
