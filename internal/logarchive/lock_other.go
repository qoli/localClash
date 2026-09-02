//go:build !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd

package logarchive

import "errors"

func lockArchive(string) (func(), error) {
	return nil, errors.New("log collection requires Unix file locking")
}
