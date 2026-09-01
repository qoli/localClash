package materialtxn

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type snapshot struct {
	path   string
	backup string
	exists bool
}

// Run restores every declared path when apply fails. Callers must declare all
// durable paths that apply may mutate; undeclared side effects are forbidden.
func Run(paths []string, apply func() error) error {
	if len(paths) == 0 {
		return errors.New("material transaction paths are required")
	}
	if apply == nil {
		return errors.New("material transaction apply function is required")
	}
	root, err := os.MkdirTemp("", "localclash-material-transaction-*")
	if err != nil {
		return fmt.Errorf("create material transaction backup: %w", err)
	}
	defer os.RemoveAll(root)

	snapshots := make([]snapshot, 0, len(paths))
	seen := map[string]bool{}
	for index, path := range paths {
		path = filepath.Clean(path)
		if path == "." || seen[path] {
			return fmt.Errorf("material transaction path %q is invalid or duplicated", path)
		}
		seen[path] = true
		entry := snapshot{path: path, backup: filepath.Join(root, fmt.Sprintf("target-%03d", index))}
		if _, err := os.Lstat(path); err == nil {
			entry.exists = true
			if err := copyPath(path, entry.backup); err != nil {
				return fmt.Errorf("backup material transaction path %q: %w", path, err)
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("inspect material transaction path %q: %w", path, err)
		}
		snapshots = append(snapshots, entry)
	}

	if err := apply(); err != nil {
		if rollbackErr := restore(snapshots); rollbackErr != nil {
			return fmt.Errorf("material transaction failed: %w; rollback failed: %v", err, rollbackErr)
		}
		return fmt.Errorf("material transaction failed and prior state was restored: %w", err)
	}
	return nil
}

func restore(snapshots []snapshot) error {
	var firstErr error
	for index := len(snapshots) - 1; index >= 0; index-- {
		entry := snapshots[index]
		if err := os.RemoveAll(entry.path); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("remove mutated path %q: %w", entry.path, err)
			continue
		}
		if entry.exists {
			if err := copyPath(entry.backup, entry.path); err != nil && firstErr == nil {
				firstErr = fmt.Errorf("restore path %q: %w", entry.path, err)
			}
		}
	}
	return firstErr
}

func copyPath(source, target string) error {
	info, err := os.Lstat(source)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("symlink material transaction target is not supported")
	}
	if info.IsDir() {
		if err := os.MkdirAll(target, info.Mode().Perm()); err != nil {
			return err
		}
		entries, err := os.ReadDir(source)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if err := copyPath(filepath.Join(source, entry.Name()), filepath.Join(target, entry.Name())); err != nil {
				return err
			}
		}
		return nil
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("unsupported material transaction file mode %s", info.Mode())
	}
	data, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(target, data, info.Mode().Perm()); err != nil {
		return err
	}
	return os.Chtimes(target, info.ModTime(), info.ModTime())
}
