//go:build !linux && !darwin

package mihomotest

import (
	"os"
	"time"
)

// Without a change timestamp, recheck the digest on every observation.
func coreChangeTime(os.FileInfo) time.Time { return time.Time{} }
