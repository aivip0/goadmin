package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"goadminkit/backend/internal/bootstrap"
	"goadminkit/backend/internal/core/config"
	"goadminkit/backend/internal/core/logger"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	log := logger.New(cfg.LogLevel)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := bootstrap.Run(ctx, cfg, log); err != nil {
		log.Error("api stopped", "error", err)
		os.Exit(1)
	}
}
