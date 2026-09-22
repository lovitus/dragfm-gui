package main

import (
	"github.com/lovitus/dragfm-gui/internal/endpoint"
	"os"
	"path/filepath"

	"fyne.io/fyne/v2/app"
	"github.com/lovitus/dragfm-gui/internal/assets"
	"github.com/lovitus/dragfm-gui/internal/gui"
	"github.com/lovitus/dragfm-gui/internal/vault"
)

func main() {
	if handled, code := endpoint.FilesystemChildMain(os.Args[1:], os.Stdin, os.Stdout, os.Stderr); handled {
		os.Exit(code)
	}
	if !assets.Available() {
		panic("embedded Linux helper payloads are missing")
	}
	a := app.NewWithID("us.lovis.dragfm-gui")
	executable, err := os.Executable()
	if err != nil {
		executable = filepath.Join(".", "dragfm-gui")
	}
	path, err := vault.Locate(executable)
	if err != nil {
		panic(err)
	}
	gui.Start(a, path)
	a.Run()
}
