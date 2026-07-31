package main

import (
	"os"

	"ecs/internal/app"
)

func main() {
	ctx, stop := notifyContext()
	defer stop()
	os.Exit(app.Main(ctx, os.Args[1:], os.Stdout, os.Stderr))
}
