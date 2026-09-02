package main

import (
	"context"
	"errors"
	"flag"
	"os"
	"os/signal"
	"syscall"

	"localclash/internal/logarchive"
)

func runLogCollector(args []string) error {
	fs := flag.NewFlagSet("logs collect", flag.ContinueOnError)
	config := fs.String("config", ".runtime/mihomo/config.yaml", "read-only Mihomo config for controller address and secret")
	dir := fs.String("output-dir", ".runtime/logs/mihomo-history", "48-hour archive directory (32 MiB budget)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("logs collect does not accept positional arguments")
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	return logarchive.Run(ctx, *config, *dir)
}
