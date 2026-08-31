package capabilitystore

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type Item struct {
	ID        string
	Candidate string
	Promoted  string
	Validate  func(string) error
}

type appliedItem struct {
	item           Item
	backup         string
	previousExists bool
}

func Promote(items []Item, transactionDir string) (func() error, error) {
	if len(items) == 0 {
		return nil, errors.New("capability snapshot promotion items are required")
	}
	for _, item := range items {
		if item.ID == "" || item.Candidate == "" || item.Promoted == "" || item.Validate == nil {
			return nil, errors.New("capability snapshot promotion item is incomplete")
		}
		if err := item.Validate(item.Candidate); err != nil {
			return nil, fmt.Errorf("validate candidate capability snapshot %q: %w", item.ID, err)
		}
	}
	applied := make([]appliedItem, 0, len(items))
	rollback := func() error {
		var rollbackErr error
		for index := len(applied) - 1; index >= 0; index-- {
			entry := applied[index]
			if err := os.Remove(entry.item.Promoted); err != nil && !errors.Is(err, os.ErrNotExist) && rollbackErr == nil {
				rollbackErr = fmt.Errorf("remove promoted capability snapshot %q: %w", entry.item.ID, err)
			}
			if entry.previousExists {
				if err := os.Rename(entry.backup, entry.item.Promoted); err != nil && rollbackErr == nil {
					rollbackErr = fmt.Errorf("restore capability snapshot %q: %w", entry.item.ID, err)
				}
			}
		}
		return rollbackErr
	}
	for _, item := range items {
		entry := appliedItem{item: item, backup: filepath.Join(transactionDir, "previous-"+filepath.Base(item.Promoted))}
		if _, err := os.Stat(item.Promoted); err == nil {
			if err := os.Rename(item.Promoted, entry.backup); err != nil {
				_ = rollback()
				return nil, fmt.Errorf("backup capability snapshot %q: %w", item.ID, err)
			}
			entry.previousExists = true
		} else if !errors.Is(err, os.ErrNotExist) {
			_ = rollback()
			return nil, fmt.Errorf("inspect capability snapshot %q: %w", item.ID, err)
		}
		applied = append(applied, entry)
		if err := os.Rename(item.Candidate, item.Promoted); err != nil {
			_ = rollback()
			return nil, fmt.Errorf("promote capability snapshot %q: %w", item.ID, err)
		}
	}
	return rollback, nil
}
