//go:build !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd

package runtimesupervision

import (
	"errors"
	"sync"
)

var fallbackLock sync.Mutex

func WithLock(workDir string, fn func() error) error {
	if workDir == "" {
		return errors.New("runtime supervision lock workdir is required")
	}
	fallbackLock.Lock()
	defer fallbackLock.Unlock()
	return fn()
}
