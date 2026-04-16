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
	os.Exit(run())
}

func run() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	return cmd.Execute(ctx, cmd.BuildInfo{
		Version:   version,
		Commit:    commit,
		BuildDate: buildDate,
	})
}
