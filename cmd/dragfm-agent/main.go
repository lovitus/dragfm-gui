package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/lovitus/dragfm-gui/internal/agentproto"
	"github.com/lovitus/dragfm-gui/internal/agentservice"
	"github.com/lovitus/dragfm-gui/internal/rsyncbridge"
)

func main() {
	os.Exit(run())
}

func run() int {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGHUP, syscall.SIGTERM)
	defer cancel()
	ctx, release, err := agentservice.OwnInstallation(ctx)
	if err != nil {
		return 1
	}
	defer release()
	if len(os.Args) > 1 && os.Args[1] == "--transfer-server" {
		if err := agentservice.ServeCommand(ctx, os.Args[2:], os.Stdin, os.Stdout, os.Stderr); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return 0
	}
	if handled, code := rsyncbridge.ChildMain(os.Args[1:], os.Stdin, os.Stdout, os.Stderr); handled {
		return code
	}
	if len(os.Args) == 2 && os.Args[1] == "--sftp" {
		if err := agentservice.ServeFiles(ctx, os.Stdin, os.Stdout); err != nil {
			return 1
		}
		return 0
	}
	connection, err := agentproto.Server(os.Stdin, os.Stdout)
	if err != nil {
		return 1
	}
	if err := agentservice.Serve(ctx, connection); err != nil {
		return 1
	}
	return 0
}
