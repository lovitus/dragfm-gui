package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/lovitus/dragfm-gui/internal/agentproto"
	"github.com/lovitus/dragfm-gui/internal/agentservice"
	"github.com/lovitus/dragfm-gui/internal/rsyncbridge"
)

func main() {
	if handled, code := rsyncbridge.ChildMain(os.Args[1:], os.Stdin, os.Stdout, os.Stderr); handled {
		os.Exit(code)
	}
	connection, err := agentproto.Server(os.Stdin, os.Stdout)
	if err != nil {
		return
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGHUP, syscall.SIGTERM)
	defer cancel()
	cleanup := agentservice.OwnInstallation()
	defer cleanup()
	_ = agentservice.Serve(ctx, connection)
}
