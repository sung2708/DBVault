package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/sung2708/DBVault/internal/cli"
	"github.com/sung2708/DBVault/internal/fault"
)

var version = "dev"
var commit = "unknown"
var buildDate = "unknown"

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	root := cli.New(cli.ResolveBuild(cli.Build{Version: version, Commit: commit, Date: buildDate}), os.Stdout, os.Stderr)
	command, err := root.ExecuteContextC(ctx)
	if err != nil {
		if command == nil {
			command = root
		}
		cli.WriteError(os.Stderr, command, err)
	}
	os.Exit(fault.ExitCode(err))
}
