package corerun

import (
	"bufio"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestProcessRunningReleasesPIDFD(t *testing.T) {
	exited := exec.Command("/bin/sh", "-c", "exit 0")
	if err := exited.Run(); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name    string
		pid     int
		running bool
	}{
		{name: "running", pid: os.Getpid(), running: true},
		{name: "exited", pid: exited.Process.Pid},
	} {
		t.Run(tt.name, func(t *testing.T) {
			assertNoProcessFDGrowth(t, func() {
				for i := 0; i < 256; i++ {
					if got := processRunning(tt.pid); got != tt.running {
						t.Fatalf("processRunning(%d) = %t, want %t", tt.pid, got, tt.running)
					}
				}
			})
		})
	}
}

func TestProcessScanReleasesPIDFD(t *testing.T) {
	cmd := startProcessFDTestChild(t, false)
	for _, tt := range []struct {
		name    string
		names   map[string]bool
		readErr bool
		wantPID bool
	}{
		{name: "matched", names: map[string]bool{"sleep": true}, wantPID: true},
		{name: "unmatched", names: map[string]bool{}},
		{name: "metadata_error", names: map[string]bool{"sleep": true}, readErr: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if tt.readErr {
				original := readProcessComm
				readProcessComm = func(pid int) (string, bool, error) {
					if pid == cmd.Process.Pid {
						return "", false, syscall.EACCES
					}
					return original(pid)
				}
				t.Cleanup(func() { readProcessComm = original })
			}
			assertNoProcessFDGrowth(t, func() {
				for i := 0; i < 16; i++ {
					found := false
					for _, process := range findRuntimeProcessesByName(tt.names) {
						found = found || process.PID == cmd.Process.Pid
					}
					if found != tt.wantPID {
						t.Fatalf("scan found pid %d = %t, want %t", cmd.Process.Pid, found, tt.wantPID)
					}
				}
			})
		})
	}
}

func TestStopRuntimePIDsReleasesPIDFD(t *testing.T) {
	for _, force := range []bool{false, true} {
		name := "SIGTERM"
		if force {
			name = "SIGKILL"
		}
		t.Run(name, func(t *testing.T) {
			assertNoProcessFDGrowth(t, func() {
				for i := 0; i < 4; i++ {
					cmd := startProcessFDTestChild(t, force)
					pid := cmd.Process.Pid
					result, err := stopRuntimePIDs([]int{pid}, StopResult{}, 20*time.Millisecond, force)
					if err != nil || !result.Stopped || result.Forced != force || result.Signal != name {
						t.Fatalf("stop = %+v, %v", result, err)
					}
					if err := cmd.Wait(); err == nil {
						t.Fatal("child exited successfully, want signal termination")
					}
				}
			})
		})
	}
	t.Run("timeout", func(t *testing.T) {
		cmd := startProcessFDTestChild(t, true)
		assertNoProcessFDGrowth(t, func() {
			for i := 0; i < 16; i++ {
				result, err := stopRuntimePIDs([]int{cmd.Process.Pid}, StopResult{}, time.Nanosecond, false)
				if err != nil || result.Stopped || result.Forced || result.Error != "runtime did not stop before timeout" {
					t.Fatalf("stop = %+v, %v", result, err)
				}
			}
		})
	})
}

func TestProcessHandlesReleaseOnPermissionError(t *testing.T) {
	const helperEnv = "LOCALCLASH_PIDFD_PERMISSION_PID"
	if value := os.Getenv(helperEnv); value != "" {
		pid, err := strconv.Atoi(value)
		if err != nil {
			t.Fatal(err)
		}
		checkProcessPermissionFDs(t, pid)
		return
	}
	if os.Geteuid() != 0 {
		checkProcessPermissionFDs(t, 1)
		return
	}

	// Go's test executable can be inside a root-only build directory. Copy it
	// into a traversable directory before running the permission checks as nobody.
	dir, err := os.MkdirTemp("", "localclash-pidfd-permission-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	helper := filepath.Join(dir, "corerun.test")
	if err := os.WriteFile(helper, data, 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(helper, "-test.run=^TestProcessHandlesReleaseOnPermissionError$", "-test.v")
	cmd.Env = append(os.Environ(), helperEnv+"="+strconv.Itoa(os.Getpid()))
	cmd.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: 65534, Gid: 65534}}
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("unprivileged checks: %v\n%s", err, output)
	}
	t.Logf("unprivileged checks:\n%s", output)
}

func checkProcessPermissionFDs(t *testing.T, pid int) {
	t.Helper()
	// Confirm EPERM before attempting SIGTERM; never signal a process that this
	// helper is allowed to terminate.
	if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.EPERM) {
		t.Fatalf("permission fixture kill(%d, 0) = %v, want EPERM", pid, err)
	}
	assertNoProcessFDGrowth(t, func() {
		for i := 0; i < 64; i++ {
			if !processRunning(pid) {
				t.Fatal("EPERM must still indicate a live process")
			}
			result, err := stopRuntimePIDs([]int{pid}, StopResult{}, time.Nanosecond, true)
			if !errors.Is(err, syscall.EPERM) || result.Stopped {
				t.Fatalf("stop = %+v, %v, want EPERM without stopping", result, err)
			}
		}
	})
}

func startProcessFDTestChild(t *testing.T, ignoreTERM bool) *exec.Cmd {
	t.Helper()
	trap := ""
	if ignoreTERM {
		trap = "trap '' TERM; "
	}
	// The shell's ignored SIGTERM disposition survives exec. Wait until exec
	// has completed so process-name scans see sleep rather than the launcher.
	cmd := exec.Command("/bin/sh", "-c", trap+"echo ready; exec sleep 30")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	scanner := bufio.NewScanner(stdout)
	if !scanner.Scan() || scanner.Text() != "ready" {
		t.Fatal("child did not become ready")
	}
	_ = stdout.Close()
	deadline := time.Now().Add(2 * time.Second)
	for {
		name, ok, err := readProcessComm(cmd.Process.Pid)
		if err == nil && ok && name == "sleep" {
			return cmd
		}
		if time.Now().After(deadline) {
			t.Fatalf("child did not exec sleep: name=%q, exists=%t, error=%v", name, ok, err)
		}
		time.Sleep(time.Millisecond)
	}
}

type processFDCounts struct {
	Total int
	PIDFD int
}

func countProcessFDs(t *testing.T) processFDCounts {
	t.Helper()
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Fatal(err)
	}
	var counts processFDCounts
	for _, entry := range entries {
		target, err := os.Readlink(filepath.Join("/proc/self/fd", entry.Name()))
		if os.IsNotExist(err) {
			continue // The directory handle from ReadDir has already closed.
		}
		if err != nil {
			t.Fatal(err)
		}
		counts.Total++
		if strings.Contains(target, "[pidfd]") {
			counts.PIDFD++
		}
	}
	return counts
}

func assertNoProcessFDGrowth(t *testing.T, check func()) {
	t.Helper()
	// Opening proc files can initialize Go's persistent epoll/eventfd pair.
	// Warm that setup before measuring resources owned by process handles.
	if _, err := os.ReadFile("/proc/self/stat"); err != nil {
		t.Fatal(err)
	}
	beforeProbe := countProcessFDs(t)
	process, err := os.FindProcess(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	withProbe := countProcessFDs(t)
	_ = process.Release()
	if withProbe.PIDFD <= beforeProbe.PIDFD {
		t.Skip("os.FindProcess does not open pidfds on this kernel/toolchain")
	}
	// Cleanup by a later GC must not conceal a missing explicit Release.
	previousGC := debug.SetGCPercent(-1)
	t.Cleanup(func() { debug.SetGCPercent(previousGC) })
	before := countProcessFDs(t)
	check()
	after := countProcessFDs(t)
	t.Logf("GC disabled: before=%+v after=%+v", before, after)
	if after.PIDFD > before.PIDFD || after.Total > before.Total {
		t.Fatalf("process FD growth: before=%+v after=%+v", before, after)
	}
}
