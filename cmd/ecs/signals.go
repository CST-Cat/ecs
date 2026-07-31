package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
)

func notifyContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}
