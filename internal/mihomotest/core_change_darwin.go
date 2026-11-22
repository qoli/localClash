package mihomotest

import (
	"os"
	"syscall"
	"time"
)

func coreChangeTime(info os.FileInfo) time.Time {
	t := info.Sys().(*syscall.Stat_t).Ctimespec
	return time.Unix(int64(t.Sec), int64(t.Nsec))
}
