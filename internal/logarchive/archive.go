// Package logarchive persists the existing Mihomo controller log stream.
// It never changes runtime configuration, proxy selection, or runtime lifecycle.
package logarchive

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	Window              = 48 * time.Hour
	Budget        int64 = 32 << 20
	segmentLimit  int64 = 256 << 10
	SweepInterval       = 30 * time.Second
)

type Record struct {
	ReceivedAt time.Time `json:"received_at"`
	Kind       string    `json:"kind"`
	Level      string    `json:"level,omitempty"`
	Message    string    `json:"message,omitempty"`
	Reason     string    `json:"reason,omitempty"`
	Count      uint64    `json:"count,omitempty"`
}

type segment struct {
	path   string
	minute time.Time
	size   int64
}
type archive struct {
	dir      string
	segments []segment
	file     *os.File
	budget   int64
	evicted  uint64
}

func openArchive(dir string, now time.Time) (*archive, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, errors.New("log archive directory is required")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("log archive must be a real directory")
	}
	if err := os.Chmod(dir, 0700); err != nil {
		return nil, err
	}
	a := &archive{dir: dir, budget: Budget}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), "mihomo-") || !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}
		parts := strings.Split(entry.Name(), "-")
		if len(parts) != 3 {
			return nil, fmt.Errorf("invalid archive segment name: %s", entry.Name())
		}
		minute, err := time.Parse("20060102T1504Z", parts[1])
		if err != nil {
			return nil, err
		}
		info, err := entry.Info()
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
			return nil, fmt.Errorf("archive segment must be regular and 0600: %s", entry.Name())
		}
		if info.Size() > segmentLimit {
			return nil, fmt.Errorf("oversized archive segment: %s", entry.Name())
		}
		a.segments = append(a.segments, segment{filepath.Join(dir, entry.Name()), minute, info.Size()})
	}
	sort.Slice(a.segments, func(i, j int) bool { return a.segments[i].path < a.segments[j].path })
	if err := a.prune(now); err != nil {
		return nil, err
	}
	for a.used() > a.budget {
		if err := a.removeFirst(); err != nil {
			return nil, err
		}
		a.evicted++
	}
	return a, nil
}

// Charge at least one 4 KiB block per segment so small minute files do not
// bypass the storage budget. Filesystem metadata is outside this budget.
func charge(n int64) int64 {
	if n == 0 {
		return 4096
	}
	return ((n + 4095) / 4096) * 4096
}
func (a *archive) used() int64 {
	var n int64
	for _, s := range a.segments {
		n += charge(s.size)
	}
	return n
}
func (a *archive) close() error {
	if a.file != nil {
		err := a.file.Close()
		a.file = nil
		return err
	}
	return nil
}
func (a *archive) removeFirst() error {
	if len(a.segments) == 0 {
		return errors.New("no archive segment to evict")
	}
	if len(a.segments) == 1 {
		if err := a.close(); err != nil {
			return err
		}
	}
	if err := os.Remove(a.segments[0].path); err != nil {
		return err
	}
	a.segments = a.segments[1:]
	return nil
}

func (a *archive) prune(now time.Time) error {
	// Expire whole minute buckets conservatively, one minute before the exact
	// cutoff. With a 30s sweep this never intentionally retains >48h records;
	// at most two minutes near the lower boundary are discarded early.
	cutoff := now.UTC().Add(-Window + time.Minute)
	for i := 0; i < len(a.segments); {
		s := a.segments[i]
		if s.minute.After(cutoff) {
			i++
			continue
		}
		if i == len(a.segments)-1 {
			if err := a.close(); err != nil {
				return err
			}
		}
		if err := os.Remove(s.path); err != nil {
			return err
		}
		a.segments = append(a.segments[:i], a.segments[i+1:]...)
	}
	return nil
}

func (a *archive) append(r Record) error {
	if err := a.prune(r.ReceivedAt); err != nil {
		return err
	}
	data, err := json.Marshal(r)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if len(data) > 32<<10 {
		return errors.New("archive record exceeds 32 KiB")
	}
	minute := r.ReceivedAt.UTC().Truncate(time.Minute)
	if a.file != nil && (!a.segments[len(a.segments)-1].minute.Equal(minute) || a.segments[len(a.segments)-1].size+int64(len(data)) > segmentLimit) {
		if err := a.close(); err != nil {
			return err
		}
	}
	extra := charge(int64(len(data)))
	if a.file != nil {
		s := a.segments[len(a.segments)-1]
		extra = charge(s.size+int64(len(data))) - charge(s.size)
	}
	for a.used()+extra > a.budget {
		if err := a.removeFirst(); err != nil {
			return err
		}
		a.evicted++
		if a.file == nil {
			extra = charge(int64(len(data)))
		}
	}
	if a.file == nil {
		f, err := os.CreateTemp(a.dir, "mihomo-"+minute.Format("20060102T1504Z")+"-*.jsonl")
		if err != nil {
			return err
		}
		a.file = f
		a.segments = append(a.segments, segment{path: f.Name(), minute: minute})
	}
	n, err := a.file.Write(data)
	a.segments[len(a.segments)-1].size += int64(n)
	if err != nil {
		return err
	}
	return nil
}
