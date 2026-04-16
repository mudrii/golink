package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/mudrii/golink/cmd"
)

var (
	version   = "dev"
	commit    = ""
	buildDate = ""
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	code := cmd.Execute(ctx, cmd.BuildInfo{
		Version:   version,
		Commit:    commit,
		BuildDate: buildDate,
	})
	os.Exit(code)
}
