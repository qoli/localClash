package materialtxn

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunRestoresFilesDirectoriesAndAbsentTargetsAfterFailure(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "intent.json")
	dir := filepath.Join(root, "capabilities")
	created := filepath.Join(root, "generated.yaml")
	if err := os.WriteFile(file, []byte("old-intent"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "old.json"), []byte("old-capability"), 0o600); err != nil {
		t.Fatal(err)
	}

	err := Run([]string{file, dir, created}, func() error {
		if err := os.WriteFile(file, []byte("new-intent"), 0o600); err != nil {
			return err
		}
		if err := os.RemoveAll(dir); err != nil {
			return err
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, "new.json"), []byte("new-capability"), 0o600); err != nil {
			return err
		}
		if err := os.WriteFile(created, []byte("new-config"), 0o600); err != nil {
			return err
		}
		return errors.New("validation failed")
	})
	if err == nil || !strings.Contains(err.Error(), "prior state was restored") {
		t.Fatalf("error = %v", err)
	}
	if data, _ := os.ReadFile(file); string(data) != "old-intent" {
		t.Fatalf("intent = %q", data)
	}
	if data, _ := os.ReadFile(filepath.Join(dir, "old.json")); string(data) != "old-capability" {
		t.Fatalf("capability = %q", data)
	}
	if _, err := os.Stat(filepath.Join(dir, "new.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("new capability survived rollback: %v", err)
	}
	if _, err := os.Stat(created); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("new generated config survived rollback: %v", err)
	}
}

func TestRunCommitsSuccessfulMutation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "intent.json")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Run([]string{path}, func() error { return os.WriteFile(path, []byte("new"), 0o600) }); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(path); string(data) != "new" {
		t.Fatalf("intent = %q", data)
	}
}
