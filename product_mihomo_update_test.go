package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMihomoCandidatesChangedUsesContentNotDownloadOccurrence(t *testing.T) {
	dir := t.TempDir()
	targetDir := filepath.Join(dir, "managed")
	candidateDir := filepath.Join(dir, "candidate")
	candidates := map[string]string{}
	for _, name := range []string{"lc-mihomo-meta", "lc-mihomo-smart"} {
		contents := "same-" + name
		writeUpdateTestFile(t, filepath.Join(targetDir, name), contents)
		candidate := filepath.Join(candidateDir, name)
		writeUpdateTestFile(t, candidate, contents)
		candidates[name] = candidate
	}

	changed, err := mihomoCandidatesChanged(candidates, targetDir)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("byte-identical Mihomo pair was reported as changed")
	}

	writeUpdateTestFile(t, candidates["lc-mihomo-smart"], "new-smart")
	changed, err = mihomoCandidatesChanged(candidates, targetDir)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("changed Smart core was reported as unchanged")
	}
}

func TestMihomoCandidatesChangedTreatsMissingTargetAsChange(t *testing.T) {
	dir := t.TempDir()
	candidate := filepath.Join(dir, "candidate", "lc-mihomo-meta")
	writeUpdateTestFile(t, candidate, "meta")

	changed, err := mihomoCandidatesChanged(map[string]string{"lc-mihomo-meta": candidate}, filepath.Join(dir, "managed"))
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("missing managed core was reported as unchanged")
	}
}

func TestPromoteMihomoCandidatesReplacesPair(t *testing.T) {
	dir := t.TempDir()
	targetDir := filepath.Join(dir, "managed")
	candidateDir := filepath.Join(dir, "candidate")
	if err := os.MkdirAll(candidateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"lc-mihomo-meta", "lc-mihomo-smart"} {
		writeUpdateTestFile(t, filepath.Join(targetDir, name), "old-"+name)
		writeUpdateTestFile(t, filepath.Join(candidateDir, name), "new-"+name)
	}

	promoted, err := promoteMihomoCandidates(map[string]string{
		"lc-mihomo-meta":  filepath.Join(candidateDir, "lc-mihomo-meta"),
		"lc-mihomo-smart": filepath.Join(candidateDir, "lc-mihomo-smart"),
	}, targetDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(promoted) != 2 {
		t.Fatalf("promoted = %+v, want both Mihomo flavors", promoted)
	}
	for _, name := range []string{"lc-mihomo-meta", "lc-mihomo-smart"} {
		data, err := os.ReadFile(filepath.Join(targetDir, name))
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != "new-"+name {
			t.Fatalf("%s = %q, want promoted candidate", name, data)
		}
		if _, err := os.Stat(filepath.Join(targetDir, name) + ".previous"); !os.IsNotExist(err) {
			t.Fatalf("rollback artifact for %s remains, err=%v", name, err)
		}
	}
}

func TestPromoteMihomoCandidatesRejectsStaleRollbackArtifactWithoutMutation(t *testing.T) {
	dir := t.TempDir()
	targetDir := filepath.Join(dir, "managed")
	candidateDir := filepath.Join(dir, "candidate")
	writeUpdateTestFile(t, filepath.Join(targetDir, "lc-mihomo-meta"), "old-meta")
	writeUpdateTestFile(t, filepath.Join(targetDir, "lc-mihomo-smart"), "old-smart")
	writeUpdateTestFile(t, filepath.Join(targetDir, "lc-mihomo-smart.previous"), "stale")
	writeUpdateTestFile(t, filepath.Join(candidateDir, "lc-mihomo-meta"), "new-meta")
	writeUpdateTestFile(t, filepath.Join(candidateDir, "lc-mihomo-smart"), "new-smart")

	_, err := promoteMihomoCandidates(map[string]string{
		"lc-mihomo-meta":  filepath.Join(candidateDir, "lc-mihomo-meta"),
		"lc-mihomo-smart": filepath.Join(candidateDir, "lc-mihomo-smart"),
	}, targetDir)
	if err == nil {
		t.Fatal("stale rollback artifact did not fail")
	}
	for name, want := range map[string]string{"lc-mihomo-meta": "old-meta", "lc-mihomo-smart": "old-smart"} {
		data, readErr := os.ReadFile(filepath.Join(targetDir, name))
		if readErr != nil {
			t.Fatal(readErr)
		}
		if string(data) != want {
			t.Fatalf("%s = %q, want unchanged %q", name, data, want)
		}
	}
}

func TestPromoteMihomoCandidatesRollsBackPairWhenPromotionFails(t *testing.T) {
	dir := t.TempDir()
	targetDir := filepath.Join(dir, "managed")
	candidateDir := filepath.Join(dir, "candidate")
	writeUpdateTestFile(t, filepath.Join(targetDir, "lc-mihomo-meta"), "old-meta")
	writeUpdateTestFile(t, filepath.Join(targetDir, "lc-mihomo-smart"), "old-smart")
	writeUpdateTestFile(t, filepath.Join(candidateDir, "lc-mihomo-meta"), "new-meta")

	_, err := promoteMihomoCandidates(map[string]string{
		"lc-mihomo-meta":  filepath.Join(candidateDir, "lc-mihomo-meta"),
		"lc-mihomo-smart": filepath.Join(candidateDir, "missing-smart"),
	}, targetDir)
	if err == nil {
		t.Fatal("missing second candidate did not fail promotion")
	}
	for name, want := range map[string]string{"lc-mihomo-meta": "old-meta", "lc-mihomo-smart": "old-smart"} {
		data, readErr := os.ReadFile(filepath.Join(targetDir, name))
		if readErr != nil {
			t.Fatal(readErr)
		}
		if string(data) != want {
			t.Fatalf("%s = %q after rollback, want %q", name, data, want)
		}
	}
}

func writeUpdateTestFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o755); err != nil {
		t.Fatal(err)
	}
}
