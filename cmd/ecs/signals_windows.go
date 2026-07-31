//go:build windows

package main

import (
	"context"
	"os"
	"os/signal"
)

func notifyContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt)
}
