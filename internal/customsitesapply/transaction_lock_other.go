//go:build !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd

package customsitesapply

import (
	"context"
	"errors"

	"localclash/internal/customsites"
)

func acquireTransactionLock(context.Context, customsites.Paths) (func(), error) {
	return nil, errors.New("custom site cross-process transactions are unsupported on this platform")
}
