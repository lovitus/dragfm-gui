package main

import (
	"io/fs"
	"os"
	"path/filepath"

	"github.com/lovitus/dragfm-gui/frontend"
	"github.com/lovitus/dragfm-gui/internal/rsyncbridge"
	"github.com/lovitus/dragfm-gui/internal/vault"
	"github.com/lovitus/dragfm-gui/internal/webgui"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

func main() {
	if handled, code := rsyncbridge.ChildMain(os.Args[1:], os.Stdin, os.Stdout, os.Stderr); handled {
		os.Exit(code)
	}
	executable, err := os.Executable()
	if err != nil {
		executable = filepath.Join(".", "dragfm-gui")
	}
	vaultPath, err := vault.Locate(executable)
	if err != nil {
		panic(err)
	}
	assets, err := fs.Sub(frontend.Assets, "dist")
	if err != nil {
		panic(err)
	}
	app := webgui.New(vaultPath)
	err = wails.Run(&options.App{
		Title:                    "dragfm",
		Width:                    1440,
		Height:                   860,
		MinWidth:                 1080,
		MinHeight:                680,
		DisableResize:            false,
		Frameless:                false,
		StartHidden:              false,
		WindowStartState:         options.Normal,
		EnableDefaultContextMenu: true,
		AssetServer:              &assetserver.Options{Assets: assets},
		OnStartup:                app.Startup,
		OnShutdown:               app.Shutdown,
		Bind:                     []interface{}{app},
	})
	if err != nil {
		panic(err)
	}
}
