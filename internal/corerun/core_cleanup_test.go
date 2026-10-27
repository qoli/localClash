package corerun

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestCoreCleanupWaitsForOldFlavorToStop(t *testing.T) {
	dir := t.TempDir()
	selected := filepath.Join(dir, "lc-mihomo-smart")
	previous := filepath.Join(dir, "lc-mihomo-meta")
	for _, path := range []string{selected, previous} {
		writeStartExecutable(t, path, "binary")
	}
	table := stubProcessTable(t)
	process := exec.Command("sleep", "30")
	if err := process.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = process.Process.Kill(); _ = process.Wait() }()
	table.add(process.Process.Pid, "lc-mihomo-meta", []string{previous, "-d", dir, "-f", filepath.Join(dir, "config.yaml")})
	removed, err := PruneUnusedCores(StatusOptions{CorePath: selected, ConfigPath: filepath.Join(dir, "config.yaml"), WorkDir: dir})
	if err != nil || len(removed) != 0 {
		t.Fatalf("cleanup while previous flavor runs: removed=%v err=%v", removed, err)
	}
	if _, err := os.Stat(previous); err != nil {
		t.Fatal("previous running core was removed", err)
	}
}
